package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Stats آمار کلی پلتفرم.
type Stats struct {
	TotalWallets    int64
	TotalDeposits   float64 // TON
	TotalPayments   float64
	PendingWithdraw int64
}

func (s *Store) GetStats(ctx context.Context) (*Stats, error) {
	var stats Stats
	s.db.WithContext(ctx).Model(&Wallet{}).Count(&stats.TotalWallets)

	var deposits, payments struct{ Sum int64 }
	s.db.WithContext(ctx).Model(&Transaction{}).
		Where("type = ? AND status = ?", TxDeposit, TxConfirmed).
		Select("SUM(amount) as sum").Scan(&deposits)
	s.db.WithContext(ctx).Model(&Transaction{}).
		Where("type = ? AND status = ?", TxPayment, TxConfirmed).
		Select("SUM(ABS(amount)) as sum").Scan(&payments)
	s.db.WithContext(ctx).Model(&WithdrawRequest{}).
		Where("status = ?", WithdrawPending).Count(&stats.PendingWithdraw)

	stats.TotalDeposits = float64(deposits.Sum) / 1e9
	stats.TotalPayments = float64(payments.Sum) / 1e9
	return &stats, nil
}

// Transfer مبلغ را از wallet کاربر A به wallet کاربر B منتقل می‌کند.
// atomic — هر دو آپدیت در یک transaction انجام می‌شود. تفکیک credit/TON روی
// گیرنده حفظ می‌شود تا اعتبار غیرقابل‌برداشت به TON قابل‌برداشت تبدیل نشود.
func (s *Store) Transfer(ctx context.Context, fromID, toID uuid.UUID, amountNano int64, desc string) (*Transaction, *Transaction, error) {
	if amountNano <= 0 {
		return nil, nil, fmt.Errorf("invalid amount: must be positive")
	}
	var fromTx, toTx *Transaction

	err := s.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		// قفل هر دو wallet
		var from, to Wallet
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", fromID).First(&from).Error; err != nil {
			return fmt.Errorf("from wallet: %w", err)
		}
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", toID).First(&to).Error; err != nil {
			return fmt.Errorf("to wallet: %w", err)
		}

		if !from.HasEnough(amountNano) {
			return fmt.Errorf("insufficient balance")
		}

		// کسر از فرستنده (اول از credit، بعد TON)
		var creditUsed, tonUsed int64
		if from.Credit >= amountNano {
			creditUsed = amountNano
		} else {
			creditUsed = from.Credit
			tonUsed = amountNano - creditUsed
		}
		fromUpdates := map[string]any{}
		if creditUsed > 0 {
			fromUpdates["credit"] = gorm.Expr("credit - ?", creditUsed)
		}
		if tonUsed > 0 {
			fromUpdates["ton_balance"] = gorm.Expr("ton_balance - ?", tonUsed)
		}
		if err := db.Model(&Wallet{}).Where("id = ?", fromID).Updates(fromUpdates).Error; err != nil {
			return err
		}

		// افزایش به گیرنده — با حفظ تفکیک credit/TON.
		toUpdates := map[string]any{}
		if creditUsed > 0 {
			toUpdates["credit"] = gorm.Expr("credit + ?", creditUsed)
		}
		if tonUsed > 0 {
			toUpdates["ton_balance"] = gorm.Expr("ton_balance + ?", tonUsed)
		}
		if err := db.Model(&Wallet{}).Where("id = ?", toID).Updates(toUpdates).Error; err != nil {
			return err
		}

		now := time.Now()
		// TxHash یکتا برای internal transactions — جلوگیری از duplicate key
		internalID := "int-" + uuid.New().String()
		fromTx = &Transaction{
			WalletID:    fromID,
			Type:        TxPayment,
			Status:      TxConfirmed,
			Amount:      -amountNano,
			TxHash:      internalID + "-from",
			Description: desc,
			ConfirmedAt: &now,
		}
		toTx = &Transaction{
			WalletID:    toID,
			Type:        TxDeposit,
			Status:      TxConfirmed,
			Amount:      amountNano,
			TxHash:      internalID + "-to",
			Description: desc,
			ConfirmedAt: &now,
		}
		if err := db.Create(fromTx).Error; err != nil {
			return err
		}
		if err := db.Create(toTx).Error; err != nil {
			return err
		}
		// دفترِ هش: یک debit برای فرستنده و یک credit برای گیرنده (زنجیرِ متوالی).
		fromBalanceAfter := (from.TONBalance + from.Credit) - amountNano
		if err := s.appendLedger(db, fromID, fromTx.ID, EntryDebit, amountNano, fromBalanceAfter, "", desc); err != nil {
			return err
		}
		toBalanceAfter := (to.TONBalance + to.Credit) + amountNano
		return s.appendLedger(db, toID, toTx.ID, EntryCredit, amountNano, toBalanceAfter, "", desc)
	})
	return fromTx, toTx, err
}
