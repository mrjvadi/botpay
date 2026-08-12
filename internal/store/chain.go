// chain.go — زنجیره‌ی هش‌شده برای LedgerEntry (blockchain-style).
//
// هر LedgerEntry یک بلوک است که به بلوک قبلی زنجیر می‌شود:
//
//	Hash = SHA256( Seq | TransactionID | WalletID | Type | Amount | BalanceAfter | PrevHash )
//
// اگر کسی مقدار یک entry را در دیتابیس تغییر دهد، Hash آن دیگر با محتوا
// همخوانی ندارد و PrevHash بلوک بعدی هم نامعتبر می‌شود → کل زنجیره از آن
// نقطه به بعد می‌شکند و دستکاری قابل‌کشف است.
package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"gorm.io/gorm"
)

// GenesisHash هش بلوک صفر (شروع زنجیره).
const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// ComputeHash هش یک entry را بر اساس محتوا و PrevHash محاسبه می‌کند.
// این تابع قطعی (deterministic) است — همان ورودی همیشه همان خروجی.
func ComputeHash(e *LedgerEntry) string {
	payload := fmt.Sprintf("%d|%s|%s|%s|%d|%d|%s",
		e.Seq,
		e.TransactionID.String(),
		e.WalletID.String(),
		string(e.Type),
		e.AmountNano,
		e.BalanceAfter,
		e.PrevHash,
	)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// lastChainEntryTx مثل lastChainEntry ولی درون یک transaction با قفل ردیف.
// قفل FOR UPDATE تضمین می‌کند دو تراکنش همزمان Seq تکراری نگیرند.
func (s *Store) lastChainEntryTx(tx *gorm.DB) (seq int64, hash string, err error) {
	var last LedgerEntry
	res := tx.Set("gorm:query_option", "FOR UPDATE").
		Order("seq DESC").First(&last)
	if res.Error != nil {
		return 0, GenesisHash, nil
	}
	return last.Seq, last.Hash, nil
}

// lastChainEntry آخرین بلوک زنجیره را برمی‌گرداند (بیشترین Seq).
// اگر زنجیره خالی باشد، (0, GenesisHash) برمی‌گرداند.
func (s *Store) lastChainEntry(ctx context.Context) (seq int64, hash string, err error) {
	var last LedgerEntry
	res := s.db.WithContext(ctx).Order("seq DESC").First(&last)
	if res.Error != nil {
		// زنجیره خالی است → genesis
		return 0, GenesisHash, nil
	}
	return last.Seq, last.Hash, nil
}

// linkEntry یک entry را به انتهای زنجیره وصل می‌کند (Seq, PrevHash, Hash را پر می‌کند).
// باید درون همان transaction ای صدا زده شود که entry را insert می‌کند.
// prevSeq و prevHash از آخرین بلوک قبلی می‌آیند.
func linkEntry(e *LedgerEntry, prevSeq int64, prevHash string) {
	e.Seq = prevSeq + 1
	e.PrevHash = prevHash
	e.Hash = ComputeHash(e)
}

// ── تأیید یکپارچگی زنجیره ──────────────────────────────────────

// ChainVerifyResult نتیجه‌ی بررسی یکپارچگی زنجیره.
type ChainVerifyResult struct {
	Valid       bool
	TotalBlocks int64
	BrokenAtSeq int64 // اولین بلوکی که شکسته (0 = سالم)
	Reason      string
}

// VerifyChain کل زنجیره را از ابتدا تا انتها بررسی می‌کند.
// برای هر بلوک: (۱) hash بازمحاسبه‌شده با hash ذخیره‌شده یکی است؟
//
//	(۲) PrevHash با Hash بلوک قبلی یکی است؟
//
// اگر هر کدام نقض شود، یعنی دیتابیس دستکاری شده.
func (s *Store) VerifyChain(ctx context.Context) (ChainVerifyResult, error) {
	const batchSize = 1000
	var (
		offset   int64
		prevHash = GenesisHash
		prevSeq  int64
		total    int64
	)

	for {
		var batch []LedgerEntry
		err := s.db.WithContext(ctx).
			Order("seq ASC").
			Limit(batchSize).
			Offset(int(offset)).
			Find(&batch).Error
		if err != nil {
			return ChainVerifyResult{}, fmt.Errorf("verify chain: %w", err)
		}
		if len(batch) == 0 {
			break
		}

		for i := range batch {
			e := &batch[i]
			total++

			// (۱) ترتیب Seq پیوسته باشد
			if e.Seq != prevSeq+1 {
				return ChainVerifyResult{
					Valid: false, TotalBlocks: total, BrokenAtSeq: e.Seq,
					Reason: fmt.Sprintf("seq gap: expected %d, got %d", prevSeq+1, e.Seq),
				}, nil
			}
			// (۲) PrevHash با بلوک قبلی هماهنگ باشد
			if e.PrevHash != prevHash {
				return ChainVerifyResult{
					Valid: false, TotalBlocks: total, BrokenAtSeq: e.Seq,
					Reason: "prev_hash mismatch — chain broken",
				}, nil
			}
			// (۳) hash بازمحاسبه‌شده با hash ذخیره‌شده یکی باشد
			if ComputeHash(e) != e.Hash {
				return ChainVerifyResult{
					Valid: false, TotalBlocks: total, BrokenAtSeq: e.Seq,
					Reason: "hash mismatch — entry was tampered",
				}, nil
			}

			prevHash = e.Hash
			prevSeq = e.Seq
		}
		offset += int64(len(batch))
	}

	return ChainVerifyResult{Valid: true, TotalBlocks: total}, nil
}
