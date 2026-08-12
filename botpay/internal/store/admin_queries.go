// admin_queries.go — کوئری‌های فقط‌خواندنی برای پنل ادمین ربات.
// این‌ها هیچ موجودی‌ای را تغییر نمی‌دهند (هسته‌ی مالی دست‌نخورده می‌ماند) و
// فقط برای نمایش آمار/جستجو در پنل استفاده می‌شوند.
package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GetWithdraw یک درخواست برداشت را با شناسه می‌خواند (فقط‌خواندنی) — برای اینکه
// لایه‌ی سرویس بتواند مبلغ و کیف پول را برای اعلان به کاربر بردارد. nil یعنی
// پیدا نشد.
func (s *Store) GetWithdraw(ctx context.Context, id uuid.UUID) (*WithdrawRequest, error) {
	var req WithdrawRequest
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&req).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// SumWalletBalances مجموع کل موجودی پلتفرم (TON + credit) را به nano-TON
// برمی‌گرداند — برای کارت آمار پنل ادمین.
func (s *Store) SumWalletBalances(ctx context.Context) (int64, error) {
	var res struct{ Sum int64 }
	err := s.db.WithContext(ctx).Model(&Wallet{}).
		Select("COALESCE(SUM(ton_balance + credit), 0) as sum").
		Scan(&res).Error
	return res.Sum, err
}

// SumFrozen مجموع کل موجودی بلوک‌شده (در انتظار برداشت) را به nano-TON برمی‌گرداند.
func (s *Store) SumFrozen(ctx context.Context) (int64, error) {
	var res struct{ Sum int64 }
	err := s.db.WithContext(ctx).Model(&Wallet{}).
		Select("COALESCE(SUM(frozen), 0) as sum").
		Scan(&res).Error
	return res.Sum, err
}

// CountLedgerEntries تعداد کل بلوک‌های زنجیره‌ی هش را برمی‌گرداند (برای نمایش
// کنار وضعیت سلامت زنجیره).
func (s *Store) CountLedgerEntries(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&LedgerEntry{}).Count(&n).Error
	return n, err
}
