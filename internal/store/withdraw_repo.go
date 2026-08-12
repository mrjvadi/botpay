package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CreateWithdraw درخواست برداشت را ثبت و موجودی را freeze می‌کند.
// برای جلوگیری از TOCTOU race: چک موجودی و increment frozen هر دو
// داخل همان تراکنش با SELECT FOR UPDATE انجام می‌شوند.
func (s *Store) CreateWithdraw(ctx context.Context, req *WithdrawRequest) error {
	total := req.Amount + req.Fee
	return s.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		var w Wallet
		if err := db.Set("gorm:query_option", "FOR UPDATE").
			Where("id = ?", req.WalletID).First(&w).Error; err != nil {
			return err
		}
		if !w.HasEnough(total) {
			return ErrInsufficientBalance
		}
		if err := db.Model(&Wallet{}).Where("id = ?", req.WalletID).
			UpdateColumn("frozen", gorm.Expr("frozen + ?", total)).
			Error; err != nil {
			return err
		}
		return db.Create(req).Error
	})
}

func (s *Store) ListPendingWithdrawals(ctx context.Context) ([]WithdrawRequest, error) {
	var reqs []WithdrawRequest
	err := s.db.WithContext(ctx).
		Where("status = 'pending'").
		Order("created_at ASC").
		Find(&reqs).Error
	return reqs, err
}

// CompleteWithdraw برداشت را نهایی می‌کند: کسر از موجودی و آزادسازی frozen،
// ثبت یک Transactionِ برداشت (با هشِ on-chain و مقصد) و یک بلوکِ debit در دفتر.
func (s *Store) CompleteWithdraw(ctx context.Context, id uuid.UUID, txHash string) error {
	now := time.Now()
	return s.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		var req WithdrawRequest
		if err := db.Where("id = ?", id).First(&req).Error; err != nil {
			return err
		}
		var w Wallet
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", req.WalletID).First(&w).Error; err != nil {
			return err
		}
		total := req.Amount + req.Fee
		if err := db.Model(&Wallet{}).Where("id = ?", req.WalletID).
			Updates(map[string]any{
				"ton_balance": gorm.Expr("ton_balance - ?", total),
				"frozen":      gorm.Expr("frozen - ?", total),
			}).Error; err != nil {
			return err
		}

		tx := &Transaction{
			WalletID:    req.WalletID,
			Type:        TxWithdraw,
			Status:      TxConfirmed,
			Amount:      -total,
			Fee:         req.Fee,
			TxHash:      txHash,
			ToAddress:   req.ToAddress,
			Ref:         req.ID.String(),
			Description: "withdrawal",
			ConfirmedAt: &now,
		}
		if err := db.Create(tx).Error; err != nil {
			return err
		}
		balanceAfter := (w.TONBalance + w.Credit) - total
		if err := s.appendLedger(db, req.WalletID, tx.ID, EntryDebit, total, balanceAfter, req.ID.String(), "withdrawal"); err != nil {
			return err
		}

		return db.Model(&WithdrawRequest{}).Where("id = ?", id).
			Updates(map[string]any{
				"status":       WithdrawDone,
				"tx_hash":      txHash,
				"processed_at": &now,
			}).Error
	})
}

// RejectWithdraw برداشت را رد می‌کند و frozen را آزاد می‌کند.
func (s *Store) RejectWithdraw(ctx context.Context, id uuid.UUID, reason string) error {
	return s.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		var req WithdrawRequest
		if err := db.Where("id = ?", id).First(&req).Error; err != nil {
			return err
		}
		// آزاد کردن frozen (آزادسازیِ hold — موجودیِ realized تغییر نمی‌کند، پس
		// بلوکِ دفتر ندارد؛ فقط یک Transactionِ اطلاع‌رسانی ثبت می‌شود).
		if err := db.Model(&Wallet{}).Where("id = ?", req.WalletID).
			UpdateColumn("frozen", gorm.Expr("frozen - ?", req.Amount+req.Fee)).Error; err != nil {
			return err
		}

		now := time.Now()
		refundTx := &Transaction{
			WalletID:    req.WalletID,
			Type:        TxRefund,
			Status:      TxConfirmed,
			Amount:      0, // hold آزاد شد؛ حرکتِ realized نداریم
			TxHash:      "int-" + uuid.New().String(),
			ToAddress:   req.ToAddress,
			Ref:         req.ID.String(),
			Description: "withdrawal rejected: " + reason,
			ConfirmedAt: &now,
		}
		if err := db.Create(refundTx).Error; err != nil {
			return err
		}

		return db.Model(&WithdrawRequest{}).Where("id = ?", id).
			Updates(map[string]any{
				"status":       WithdrawRejected,
				"admin_note":   reason,
				"processed_at": &now,
			}).Error
	})
}
