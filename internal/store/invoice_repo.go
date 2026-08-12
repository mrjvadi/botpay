package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Store) CreateInvoice(ctx context.Context, inv *Invoice) error {
	return s.db.WithContext(ctx).Create(inv).Error
}

func (s *Store) FindInvoiceByCode(ctx context.Context, code string) (*Invoice, error) {
	var inv Invoice
	err := s.db.WithContext(ctx).Where("code = ?", code).First(&inv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &inv, err
}

// FindPendingInvoiceByCode فقط invoiceهای pending را برمی‌گرداند.
func (s *Store) FindPendingInvoiceByCode(ctx context.Context, code string) (*Invoice, error) {
	var inv Invoice
	err := s.db.WithContext(ctx).Where("code = ? AND status = 'pending'", code).First(&inv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &inv, err
}

// AddInvoiceReceipt مبلغِ یک واریز را روی فاکتور جمع می‌زند و در صورت رسیدن به
// سقفِ مبلغ (یا فاکتورِ بازِ amount==0) آن را paid می‌کند. مقدارِ بازگشتی paid
// نشان می‌دهد که آیا این واریز فاکتور را به‌طور کامل پرداخت کرد یا نه. عملیات
// atomic است (قفلِ ردیف) تا واریزهای همزمان دوبار شمرده نشوند.
func (s *Store) AddInvoiceReceipt(ctx context.Context, invoiceID uuid.UUID, amountNano int64, txHash string) (bool, error) {
	var paid bool
	err := s.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		var inv Invoice
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", invoiceID).First(&inv).Error; err != nil {
			return err
		}

		newReceived := inv.ReceivedNano + amountNano
		updates := map[string]any{"received_nano": newReceived}

		if inv.Status == InvoicePending && (inv.Amount == 0 || newReceived >= inv.Amount) {
			now := time.Now()
			updates["status"] = InvoicePaid
			updates["tx_hash"] = txHash
			updates["paid_at"] = &now
			paid = true
		}
		return db.Model(&Invoice{}).Where("id = ?", invoiceID).Updates(updates).Error
	})
	return paid, err
}

func (s *Store) ConfirmInvoice(ctx context.Context, invoiceID uuid.UUID, txHash string) error {
	now := time.Now()
	return s.db.WithContext(ctx).Model(&Invoice{}).
		Where("id = ?", invoiceID).
		Updates(map[string]any{
			"status":  InvoicePaid,
			"tx_hash": txHash,
			"paid_at": &now,
		}).Error
}

func (s *Store) ExpireOldInvoices(ctx context.Context) error {
	return s.db.WithContext(ctx).Model(&Invoice{}).
		Where("status = 'pending' AND expires_at < ?", time.Now()).
		Update("status", InvoiceExpired).Error
}
