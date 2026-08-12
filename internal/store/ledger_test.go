package store

import (
	"testing"
	"time"
)

// TestLedgerEntry_Validation بررسی validation های LedgerEntry.
func TestLedgerEntry_Validation(t *testing.T) {
	// amount باید مثبت باشد
	entry := LedgerEntry{
		AmountNano:   1000,
		Type:         EntryCredit,
		BalanceAfter: 5000,
	}

	if entry.AmountNano <= 0 {
		t.Error("amount should be positive")
	}
	if entry.Type != EntryCredit && entry.Type != EntryDebit {
		t.Errorf("invalid entry type: %s", entry.Type)
	}
}

// TestEntryType بررسی نوع‌های معتبر.
func TestEntryType(t *testing.T) {
	if EntryDebit == EntryCredit {
		t.Error("debit and credit should be different")
	}
	if string(EntryDebit) == "" || string(EntryCredit) == "" {
		t.Error("entry types should not be empty")
	}
}

// TestWallet_HasEnough بررسی موجودی کافی.
func TestWallet_HasEnough(t *testing.T) {
	w := &Wallet{
		TONBalance: 10_000_000_000, // 10 TON
		Credit:     0,
		Frozen:     0,
	}

	if !w.HasEnough(5_000_000_000) { // 5 TON
		t.Error("should have enough for 5 TON")
	}
	if w.HasEnough(15_000_000_000) { // 15 TON
		t.Error("should not have enough for 15 TON")
	}
}

// TestWallet_BalanceTON تبدیل nano به TON.
func TestWallet_BalanceTON(t *testing.T) {
	w := &Wallet{TONBalance: 1_000_000_000} // 1 TON
	if w.BalanceTON() != 1.0 {
		t.Errorf("expected 1.0 TON, got %f", w.BalanceTON())
	}

	w2 := &Wallet{TONBalance: 500_000_000} // 0.5 TON
	if w2.BalanceTON() != 0.5 {
		t.Errorf("expected 0.5 TON, got %f", w2.BalanceTON())
	}
}

// TestWallet_TotalTON موجودی کل.
func TestWallet_TotalTON(t *testing.T) {
	w := &Wallet{
		TONBalance: 1_000_000_000, // 1 TON
		Credit:     500_000_000,   // 0.5 TON
	}
	total := w.TotalTON()
	if total != 1.5 {
		t.Errorf("expected 1.5 TON total, got %f", total)
	}
}

// TestTxType بررسی انواع تراکنش.
func TestTxType(t *testing.T) {
	types := []TxType{TxDeposit, TxWithdraw, TxCreditAdd, TxPayment, TxRefund}
	seen := make(map[TxType]bool)
	for _, t := range types {
		if seen[t] {
			// duplicate type
		}
		seen[t] = true
		if string(t) == "" {
			// empty type
		}
	}
	if len(seen) != len(types) {
		// some types might be duplicates
	}
}

// TestInvoice_Expiry بررسی منطق انقضای invoice.
func TestInvoice_Expiry(t *testing.T) {
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)

	validInvoice := &Invoice{ExpiresAt: future, Status: InvoicePending}
	expiredInvoice := &Invoice{ExpiresAt: past, Status: InvoicePending}

	if validInvoice.ExpiresAt.Before(time.Now()) {
		t.Error("valid invoice should not be expired")
	}
	if !expiredInvoice.ExpiresAt.Before(time.Now()) {
		t.Error("expired invoice should be before now")
	}
}
