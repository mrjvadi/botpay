// Package store مدل‌های Revenue Service.
package store

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ── Revenue Rule ───────────────────────────────────────────

// RevenueType نوع درآمد.
type RevenueType string

const (
	RevSubscription   RevenueType = "subscription"    // خرید پلن
	RevLockIncome     RevenueType = "lock_income"     // درآمد از lock کاربر
	RevLockRental     RevenueType = "lock_rental"     // اجاره lock
	RevAdIncome       RevenueType = "ad_income"       // درآمد تبلیغات
	RevReward         RevenueType = "reward"          // جایزه
	RevCommission     RevenueType = "commission"      // کمیسیون
	RevChannelRevenue RevenueType = "channel_revenue" // درآمد کانال — 90% به owner
	RevGroupRevenue   RevenueType = "group_revenue"   // درآمد گروه — 50% owner / 40% members
	RevMemberReward   RevenueType = "member_reward"   // پاداش عضو فعال گروه
)

// RevenueRule قانون تقسیم درآمد برای هر نوع.
type RevenueRule struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	CreatedAt time.Time
	UpdatedAt time.Time

	Type            RevenueType `gorm:"uniqueIndex;not null"`
	OwnerPercent    float64     `gorm:"not null"` // درصد صاحب ربات/کانال
	PlatformPercent float64     `gorm:"not null"` // درصد پلتفرم
	IsActive        bool        `gorm:"default:true"`
	Description     string
}

// ── Earning ────────────────────────────────────────────────

// EarningStatus وضعیت پردازش.
type EarningStatus string

const (
	EarningPending    EarningStatus = "pending"
	EarningProcessing EarningStatus = "processing"
	EarningDone       EarningStatus = "done"
	EarningFailed     EarningStatus = "failed"
)

// Earning یک رویداد درآمد که باید تقسیم شود.
type Earning struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	CreatedAt time.Time
	UpdatedAt time.Time

	// منبع درآمد
	Type      RevenueType `gorm:"not null;index"`
	TotalNano int64       `gorm:"not null"` // مبلغ کل به nano-TON

	// صاحب درآمد
	OwnerTelegramID int64  `gorm:"not null;index"` // telegram_id صاحب ربات/کانال
	BotID           string `gorm:"index"`          // bot_id مربوطه (اختیاری)

	// مرجع
	// index (نه unique، چون بعضی انواع Earning اصلاً RefID ندارند و چندتا
	// رکورد با ref_id="" باید مجاز بمانند) — idempotency واقعی در سطح
	// اپلیکیشن با FindEarningByRefID قبل از ساخت انجام می‌شود.
	RefID       string `gorm:"index"` // شناسه عملیات (invoice_id, order_id, ...)
	Description string

	// نتیجه
	Status       EarningStatus `gorm:"default:'pending';index"`
	OwnerNano    int64         // مقدار سهم owner
	PlatformNano int64         // مقدار سهم platform
	OwnerTxID    string        // tx_id در botpay
	PlatformTxID string
	ProcessedAt  *time.Time
	Error        string
}

// ── Platform Wallet ────────────────────────────────────────

// PlatformWallet آدرس/ID کیف پول پلتفرم برای دریافت کمیسیون.
type PlatformWallet struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	TelegramID int64     `gorm:"uniqueIndex;not null"` // telegram_id ادمین/پلتفرم
	Label      string
	IsDefault  bool `gorm:"default:false"`
	CreatedAt  time.Time
}

// ── AutoMigrate ────────────────────────────────────────────

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&RevenueRule{}, &Earning{}, &PlatformWallet{})
}

// DefaultRules قوانین پیش‌فرض.
func DefaultRules() []RevenueRule {
	return []RevenueRule{
		{Type: RevSubscription, OwnerPercent: 0, PlatformPercent: 100, Description: "خرید پلن — همه به پلتفرم"},
		{Type: RevLockIncome, OwnerPercent: 70, PlatformPercent: 30, Description: "درآمد lock — 70% به صاحب"},
		{Type: RevLockRental, OwnerPercent: 70, PlatformPercent: 30, Description: "اجاره lock — 70% به مالک"},
		{Type: RevAdIncome, OwnerPercent: 70, PlatformPercent: 30, Description: "درآمد تبلیغ — 70% به کانال"},
		{Type: RevReward, OwnerPercent: 100, PlatformPercent: 0, Description: "جایزه — همه به کاربر"},
	}
}
