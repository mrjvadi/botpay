// Package store مدل‌های DB و repository لایه botpay.
package store

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ── Wallet ─────────────────────────────────────────────────

// Wallet کیف پول هر کاربر.
// هر کاربر یک wallet دارد که در همه سرویس‌های پلتفرم مشترک است.
type Wallet struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	CreatedAt time.Time
	UpdatedAt time.Time

	// TelegramID شناسه یکتای کاربر در تلگرام
	TelegramID int64 `gorm:"uniqueIndex;not null"`

	// PayHandle آدرس محلی کوتاه و یکتا برای دریافت انتقال از کاربران دیگر
	// مثال: u_a1b2c3 — کاربر این را به دیگران می‌دهد تا برایش پول بفرستند
	PayHandle string `gorm:"uniqueIndex"`

	// TONBalance موجودی واقعی TON (nano-TON در DB)
	// همیشه از blockchain sync می‌شود
	TONBalance int64 `gorm:"default:0"` // nano-TON (1 TON = 1e9)

	// Credit موجودی اعتبار داخلی
	// واحد: صدم TON (برای دقت بیشتر)
	Credit int64 `gorm:"default:0"` // در واحد nano-TON

	// TONAddress آدرس TON اختصاصی این کاربر (برای واریز)
	// از HD wallet مشتق می‌شود
	TONAddress string `gorm:"index"` // uniqueIndex نیست — همه کاربرها masterAddr مشترک دارند

	// Frozen موجودی بلوک‌شده (در انتظار تأیید تراکنش)
	Frozen int64 `gorm:"default:0"`

	// IsActive
	IsActive bool `gorm:"default:true"`

	// Lang کد زبان انتخابی کاربر برای رابط ربات (مثلاً "fa" یا "en").
	// خالی یعنی هنوز انتخاب نشده → از زبان پیش‌فرض استفاده می‌شود.
	Lang string `gorm:"size:8"`
}

// BalanceTON موجودی TON به عدد اعشاری.
func (w *Wallet) BalanceTON() float64 { return float64(w.TONBalance) / 1e9 }

// CreditTON موجودی اعتبار به عدد اعشاری.
func (w *Wallet) CreditTON() float64 { return float64(w.Credit) / 1e9 }

// TotalTON مجموع TON + اعتبار.
func (w *Wallet) TotalTON() float64 { return w.BalanceTON() + w.CreditTON() }

// HasEnough بررسی می‌کند موجودی کافی هست.
// ابتدا از اعتبار کم می‌شود، بعد TON.
func (w *Wallet) HasEnough(amountNano int64) bool {
	return (w.TONBalance + w.Credit - w.Frozen) >= amountNano
}

// ── Transaction ────────────────────────────────────────────

type TxType string
type TxStatus string

const (
	TxDeposit   TxType = "deposit"    // واریز TON از blockchain
	TxWithdraw  TxType = "withdraw"   // برداشت TON به blockchain
	TxCreditAdd TxType = "credit_add" // افزایش اعتبار (توسط ادمین)
	TxPayment   TxType = "payment"    // پرداخت به سرویس
	TxRefund    TxType = "refund"     // بازگشت وجه

	TxPending   TxStatus = "pending"
	TxConfirmed TxStatus = "confirmed"
	TxFailed    TxStatus = "failed"
)

// Transaction هر تراکنش مالی.
type Transaction struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	CreatedAt time.Time

	WalletID uuid.UUID `gorm:"not null;index"`
	Type     TxType    `gorm:"not null;index"`
	Status   TxStatus  `gorm:"default:'pending';index"`

	// Amount به nano-TON (مثبت = واریز، منفی = برداشت)
	Amount int64 `gorm:"not null"`
	// Fee کارمزد تراکنش (برای برداشت)
	Fee int64 `gorm:"default:0"`

	// TON blockchain
	TxHash      string `gorm:"index"` // on-chain tx hash — partial unique در migration
	FromAddress string // آدرس فرستنده
	ToAddress   string // آدرس گیرنده
	// TxLT/TxUtime داده‌ی خام on-chain برای واریزهای TON (۰ برای تراکنش‌های داخلی).
	TxLT    int64 `gorm:"index"` // logical time تراکنش TON
	TxUtime int64 // unix time تراکنش TON

	// Internal
	// ServiceID سرویسی که این پرداخت برای آن بوده (مثلاً botmanager)
	ServiceID string
	// Ref شناسه مرجع در سرویس
	Ref         string
	Description string
	// Metadata اطلاعات شفاف دلخواه سرویس (JSON) — مثلا {"plan":"pro","duration":30}
	Metadata string `gorm:"type:text"`

	ConfirmedAt *time.Time
}

// ── Invoice ────────────────────────────────────────────────

type InvoiceStatus string

const (
	InvoicePending   InvoiceStatus = "pending"
	InvoicePaid      InvoiceStatus = "paid"
	InvoiceExpired   InvoiceStatus = "expired"
	InvoiceCancelled InvoiceStatus = "cancelled"
)

// Invoice فاکتور پرداخت — کاربر باید TON بفرستد با comment = Code.
type Invoice struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	CreatedAt time.Time
	UpdatedAt time.Time

	WalletID uuid.UUID `gorm:"not null;index"`

	// Code کد یکتا که کاربر باید در comment تراکنش بنویسد
	Code string `gorm:"uniqueIndex;not null"` // مثال: PAY-A1B2C3

	Amount int64 `gorm:"not null"` // nano-TON — مبلغِ موردِ انتظار (۰ = فاکتورِ باز/هر مبلغ)
	// ReceivedNano مجموعِ مبلغِ دریافت‌شده تا این لحظه (برای واریزِ جزئی).
	ReceivedNano int64         `gorm:"default:0"`
	Status       InvoiceStatus `gorm:"default:'pending';index"`

	// سرویس درخواست‌دهنده
	ServiceID string
	Ref       string // شناسه مرجع در سرویس (مثلاً plan_id)
	Metadata  string `gorm:"type:text"` // JSON

	ExpiresAt time.Time
	PaidAt    *time.Time
	TxHash    string
}

// IsExpired بررسی انقضا.
func (i *Invoice) IsExpired() bool {
	return time.Now().After(i.ExpiresAt)
}

// ── Withdrawal Request ─────────────────────────────────────

type WithdrawStatus string

const (
	WithdrawPending    WithdrawStatus = "pending"
	WithdrawApproved   WithdrawStatus = "approved"
	WithdrawProcessing WithdrawStatus = "processing"
	WithdrawDone       WithdrawStatus = "done"
	WithdrawRejected   WithdrawStatus = "rejected"
)

// WithdrawRequest درخواست برداشت.
type WithdrawRequest struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	CreatedAt time.Time
	UpdatedAt time.Time

	WalletID    uuid.UUID      `gorm:"not null;index"`
	ToAddress   string         `gorm:"not null"`
	Amount      int64          `gorm:"not null"` // nano-TON
	Fee         int64          // کارمزد شبکه
	Status      WithdrawStatus `gorm:"default:'pending';index"`
	TxHash      string
	Note        string // یادداشت کاربر
	AdminNote   string // یادداشت ادمین در صورت رد
	ProcessedAt *time.Time
}

// ── AutoMigrate ────────────────────────────────────────────

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&Wallet{},
		&Transaction{},
		&LedgerEntry{},
		&Invoice{},
		&WithdrawRequest{},
	)
}

// ── Double-Entry Ledger ────────────────────────────────────

// EntryType نوع entry در دفتر دوطرفه.
type EntryType string

const (
	EntryDebit  EntryType = "debit"  // کاهش موجودی
	EntryCredit EntryType = "credit" // افزایش موجودی
)

// LedgerEntry یک خط در دفتر کل.
// هر تراکنش دقیقاً دو entry دارد: یک debit + یک credit.
// مجموع همه entry ها همیشه باید صفر باشد (invariant).
type LedgerEntry struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	CreatedAt time.Time

	TransactionID uuid.UUID `gorm:"not null;index"` // FK به Transaction
	WalletID      uuid.UUID `gorm:"not null;index"` // کیف پول مربوطه
	Type          EntryType `gorm:"not null"`       // debit یا credit
	AmountNano    int64     `gorm:"not null"`       // همیشه مثبت
	BalanceAfter  int64     // موجودی بعد از این entry

	// ── Hash chain (blockchain-style) ─────────────────────────
	// هر entry به entry قبلی زنجیر می‌شود. تغییر هر entry، hash
	// آن و همه‌ی entryهای بعد را خراب می‌کند → دستکاری قابل‌کشف است.
	Seq      int64  `gorm:"uniqueIndex;autoIncrement"` // شماره ترتیبی بلوک در زنجیره
	PrevHash string `gorm:"index"`                     // hash بلوک قبلی
	Hash     string `gorm:"index"`                     // SHA256(محتوا + PrevHash)

	// برای auditing
	Ref  string
	Note string
}

// TransferPair یک جفت entry برای انتقال.
type TransferPair struct {
	Debit  LedgerEntry
	Credit LedgerEntry
}
