package store

import (
	"testing"

	"github.com/google/uuid"
)

// newEntry یک بلوکِ خام برای تست می‌سازد.
func newEntry(seq int64, wallet uuid.UUID, etype EntryType, amount, balAfter int64) *LedgerEntry {
	return &LedgerEntry{
		ID:            uuid.New(),
		TransactionID: uuid.New(),
		WalletID:      wallet,
		Type:          etype,
		AmountNano:    amount,
		BalanceAfter:  balAfter,
	}
}

// TestComputeHashDeterministic هش باید قطعی باشد و با هر تغییرِ محتوا عوض شود.
func TestComputeHashDeterministic(t *testing.T) {
	w := uuid.New()
	e := newEntry(1, w, EntryCredit, 1000, 1000)
	e.PrevHash = GenesisHash
	h1 := ComputeHash(e)
	if h1 != ComputeHash(e) {
		t.Fatal("hash must be deterministic for the same content")
	}
	// تغییرِ مبلغ باید هش را عوض کند (هسته‌ی تشخیص دستکاری).
	e.AmountNano = 2000
	if ComputeHash(e) == h1 {
		t.Fatal("hash must change when amount changes")
	}
}

// TestChainLinksAndTamperDetection یک زنجیره‌ی سه‌بلوکی می‌سازد و بررسی می‌کند که
// (۱) هر بلوک درست زنجیر می‌شود و هشِ ذخیره‌شده با بازمحاسبه می‌خواند، و
// (۲) دستکاریِ یک بلوک، همان بررسی‌هایی را می‌شکند که VerifyChain انجام می‌دهد.
func TestChainLinksAndTamperDetection(t *testing.T) {
	w := uuid.New()
	e1 := newEntry(0, w, EntryCredit, 1000, 1000)
	linkEntry(e1, 0, GenesisHash)
	e2 := newEntry(0, w, EntryDebit, 400, 600)
	linkEntry(e2, e1.Seq, e1.Hash)
	e3 := newEntry(0, w, EntryCredit, 250, 850)
	linkEntry(e3, e2.Seq, e2.Hash)

	chain := []*LedgerEntry{e1, e2, e3}

	// سلامت: Seq پیوسته، PrevHash درست، هش بازمحاسبه‌شده برابر.
	prevSeq, prevHash := int64(0), GenesisHash
	for i, e := range chain {
		if e.Seq != prevSeq+1 {
			t.Fatalf("block %d: seq gap: got %d want %d", i, e.Seq, prevSeq+1)
		}
		if e.PrevHash != prevHash {
			t.Fatalf("block %d: prev_hash mismatch", i)
		}
		if ComputeHash(e) != e.Hash {
			t.Fatalf("block %d: stored hash doesn't match content", i)
		}
		prevSeq, prevHash = e.Seq, e.Hash
	}

	// دستکاری: مبلغِ بلوک وسط را عوض کن → هشِ ذخیره‌شده دیگر با محتوا نمی‌خواند
	// و PrevHash بلوک بعدی هم می‌شکند (دقیقاً چیزی که VerifyChain می‌گیرد).
	e2.AmountNano = 999_999
	if ComputeHash(e2) == e2.Hash {
		t.Fatal("tampering the amount must invalidate the stored hash")
	}
	if e3.PrevHash == ComputeHash(e2) {
		t.Fatal("tampering must break the next block's prev_hash link")
	}
}
