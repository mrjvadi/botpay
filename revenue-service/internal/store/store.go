package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Store struct{ db *gorm.DB }

func New(db *gorm.DB) *Store { return &Store{db: db} }

// ── Rules ──────────────────────────────────────────────────

func (s *Store) GetRule(ctx context.Context, t RevenueType) (*RevenueRule, error) {
	var rule RevenueRule
	err := s.db.WithContext(ctx).
		Where("type = ? AND is_active = true", t).
		First(&rule).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &rule, err
}

func (s *Store) UpsertRule(ctx context.Context, rule *RevenueRule) error {
	return s.db.WithContext(ctx).Save(rule).Error
}

func (s *Store) ListRules(ctx context.Context) ([]RevenueRule, error) {
	var rules []RevenueRule
	return rules, s.db.WithContext(ctx).Find(&rules).Error
}

// SeedDefaultRules قوانین پیش‌فرض را اگه وجود ندارن می‌سازد.
func (s *Store) SeedDefaultRules(ctx context.Context) error {
	for _, rule := range DefaultRules() {
		var existing RevenueRule
		err := s.db.WithContext(ctx).Where("type = ?", rule.Type).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := s.db.WithContext(ctx).Create(&rule).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// ── Earnings ───────────────────────────────────────────────

func (s *Store) CreateEarning(ctx context.Context, e *Earning) error {
	return s.db.WithContext(ctx).Create(e).Error
}

func (s *Store) GetEarning(ctx context.Context, id uuid.UUID) (*Earning, error) {
	var e Earning
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &e, err
}

func (s *Store) UpdateEarning(ctx context.Context, e *Earning) error {
	return s.db.WithContext(ctx).Save(e).Error
}

// FindEarningByRefID یک Earning موجود با همان ref_id را برمی‌گرداند (اگر
// باشد) — برای idempotency در CreateAndProcess، تا رویداد earning.created
// تکراری/replay-شده دوباره پرداخت نشود. ref_id خالی هرگز match نمی‌شود
// (چون بعضی انواع Earning اصلاً ref ندارند و نباید با هم تداخل کنند).
func (s *Store) FindEarningByRefID(ctx context.Context, refID string) (*Earning, error) {
	if refID == "" {
		return nil, nil
	}
	var e Earning
	err := s.db.WithContext(ctx).Where("ref_id = ?", refID).First(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &e, err
}

func (s *Store) ListPendingEarnings(ctx context.Context, limit int) ([]Earning, error) {
	var earnings []Earning
	return earnings, s.db.WithContext(ctx).
		Where("status = ?", EarningPending).
		Order("created_at ASC").
		Limit(limit).
		Find(&earnings).Error
}

func (s *Store) MarkProcessing(ctx context.Context, id uuid.UUID) (bool, error) {
	result := s.db.WithContext(ctx).Model(&Earning{}).
		Where("id = ? AND status = ?", id, EarningPending).
		Update("status", EarningProcessing)
	return result.RowsAffected == 1, result.Error
}

func (s *Store) MarkDone(ctx context.Context, id uuid.UUID, ownerTxID, platformTxID string, ownerNano, platformNano int64) error {
	now := time.Now()
	return s.db.WithContext(ctx).Model(&Earning{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":         EarningDone,
			"owner_tx_id":    ownerTxID,
			"platform_tx_id": platformTxID,
			"owner_nano":     ownerNano,
			"platform_nano":  platformNano,
			"processed_at":   &now,
		}).Error
}

func (s *Store) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error {
	now := time.Now()
	return s.db.WithContext(ctx).Model(&Earning{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":       EarningFailed,
			"error":        errMsg,
			"processed_at": &now,
		}).Error
}

// ── Platform Wallet ────────────────────────────────────────

func (s *Store) GetPlatformWallet(ctx context.Context) (*PlatformWallet, error) {
	var w PlatformWallet
	err := s.db.WithContext(ctx).
		Where("is_default = true").First(&w).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &w, err
}

func (s *Store) SetPlatformWallet(ctx context.Context, telegramID int64, label string) error {
	// قبلی رو default=false کن
	s.db.WithContext(ctx).Model(&PlatformWallet{}).
		Where("is_default = true").Update("is_default", false)

	var w PlatformWallet
	err := s.db.WithContext(ctx).Where("telegram_id = ?", telegramID).First(&w).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		w = PlatformWallet{TelegramID: telegramID, Label: label, IsDefault: true}
		return s.db.WithContext(ctx).Create(&w).Error
	}
	w.IsDefault = true
	w.Label = label
	return s.db.WithContext(ctx).Save(&w).Error
}

// Stats آمار درآمد.
type EarningStats struct {
	TotalEarnings  int64
	TotalOwnerNano int64
	PlatformNano   int64
	PendingCount   int64
}

func (s *Store) GetStats(ctx context.Context) (*EarningStats, error) {
	var stats EarningStats
	s.db.WithContext(ctx).Model(&Earning{}).
		Where("status = ?", EarningDone).
		Count(&stats.TotalEarnings)
	s.db.WithContext(ctx).Model(&Earning{}).
		Where("status = ?", EarningPending).
		Count(&stats.PendingCount)

	var sums struct {
		OwnerSum    int64
		PlatformSum int64
	}
	s.db.WithContext(ctx).Model(&Earning{}).
		Where("status = ?", EarningDone).
		Select("SUM(owner_nano) as owner_sum, SUM(platform_nano) as platform_sum").
		Scan(&sums)
	stats.TotalOwnerNano = sums.OwnerSum
	stats.PlatformNano = sums.PlatformSum
	return &stats, nil
}
