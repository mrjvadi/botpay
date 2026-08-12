// Package store — ledger.go
// دفترِ هش‌زنجیره‌ایِ append-only برای botpay.
//
// هر حرکتِ موجودی (واریز، پرداخت، اعتبار، برداشت، انتقال) دقیقاً یک یا دو
// LedgerEntry تولید می‌کند که به بلوکِ قبلی زنجیر می‌شود (chain.go). این کار
// درونِ همان transactionِ عملیات و با appendLedger انجام می‌شود تا اتمیک بماند و
// هرگز موجودی بدونِ بلوکِ متناظر تغییر نکند.
//
// invariant per-wallet:  Σcredit − Σdebit == ton_balance + credit  (مانده‌ی realized)
package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// appendLedger یک بلوکِ هش‌زنجیره‌ای برای یک حرکتِ موجودی درج می‌کند. باید درونِ
// همان db.Transactionِ عملیات فراخوانی شود. amountNano همیشه مثبت است؛ جهت با
// etype (debit/credit) مشخص می‌شود. balanceAfter = مجموعِ realizedِ (ton_balance +
// credit) کیف پول پس از این حرکت.
func (s *Store) appendLedger(db *gorm.DB, walletID, txID uuid.UUID, etype EntryType, amountNano, balanceAfter int64, ref, note string) error {
	prevSeq, prevHash, err := s.lastChainEntryTx(db)
	if err != nil {
		return err
	}
	e := &LedgerEntry{
		ID:            uuid.New(),
		CreatedAt:     time.Now(),
		TransactionID: txID,
		WalletID:      walletID,
		Type:          etype,
		AmountNano:    amountNano,
		BalanceAfter:  balanceAfter,
		Ref:           ref,
		Note:          note,
	}
	linkEntry(e, prevSeq, prevHash)
	return db.Create(e).Error
}

// GetLedgerEntries تاریخچه entries یک wallet.
func (s *Store) GetLedgerEntries(ctx context.Context, walletID uuid.UUID, limit int) ([]LedgerEntry, error) {
	var entries []LedgerEntry
	err := s.db.WithContext(ctx).
		Where("wallet_id = ?", walletID).
		Order("created_at DESC").
		Limit(limit).
		Find(&entries).Error
	return entries, err
}

// VerifyLedgerBalance یکپارچگی دفتر را برای یک کیف پول بررسی می‌کند:
// مجموع creditها منهای debitها باید برابرِ مانده‌ی realized (ton_balance + credit)
// باشد. اگر برابر نباشد، یعنی یا بلوکی جا افتاده یا موجودی خارج از این مسیر
// دستکاری شده است.
func (s *Store) VerifyLedgerBalance(ctx context.Context, walletID uuid.UUID) (bool, error) {
	var wallet Wallet
	if err := s.db.WithContext(ctx).First(&wallet, "id = ?", walletID).Error; err != nil {
		return false, err
	}
	var creditSum, debitSum int64
	s.db.WithContext(ctx).Model(&LedgerEntry{}).
		Where("wallet_id = ? AND type = ?", walletID, EntryCredit).
		Select("COALESCE(SUM(amount_nano), 0)").Scan(&creditSum)
	s.db.WithContext(ctx).Model(&LedgerEntry{}).
		Where("wallet_id = ? AND type = ?", walletID, EntryDebit).
		Select("COALESCE(SUM(amount_nano), 0)").Scan(&debitSum)

	return creditSum-debitSum == wallet.TONBalance+wallet.Credit, nil
}
