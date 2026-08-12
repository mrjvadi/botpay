// Package protocol همه NATS subjects و message types را تعریف می‌کند.
//
// معماری:
//
//	هر bot یک engine کامل دارد — مستقیم به DB وصل است.
//	NATS فقط برای: deploy، heartbeat، رویدادهای cross-service
//
// Subjects:
//
//	deploy.{server_id}          ← apimanager → agentmanager (deploy bot)
//	agent.{server_id}.heartbeat ← agentmanager → apimanager
//	agent.{server_id}.result    ← agentmanager → apimanager (نتیجه دستور)
//	event.payment.{bot_id}      ← bot → apimanager (پرداخت تأیید شد)
//	event.instance.{bot_id}     ← apimanager → bot (تغییر وضعیت instance)
package protocol

import "fmt"

// ── Streams ────────────────────────────────────────────────

const (
	StreamDeploy = "DEPLOY" // دستورات deploy/stop/remove
	StreamAgent  = "AGENT"  // heartbeat + نتایج
	StreamEvents = "EVENTS" // رویدادهای cross-service
)

// ── Subjects ───────────────────────────────────────────────

// DeploySubject دستور deploy/stop/remove به یک agentmanager.
func DeploySubject(serverID string) string {
	return fmt.Sprintf("deploy.%s", serverID)
}

// HeartbeatSubject heartbeat از agentmanager به apimanager.
func HeartbeatSubject(serverID string) string {
	return fmt.Sprintf("agent.%s.heartbeat", serverID)
}

// ResultSubject نتیجه اجرای یک دستور Docker.
func ResultSubject(serverID string) string {
	return fmt.Sprintf("agent.%s.result", serverID)
}

// PaymentEventSubject پرداخت تأیید شده از bot به apimanager.
func PaymentEventSubject(botID int64) string {
	return fmt.Sprintf("event.payment.%d", botID)
}

// InstanceEventSubject تغییر وضعیت instance (از apimanager به bot).
func InstanceEventSubject(botID int64) string {
	return fmt.Sprintf("event.instance.%d", botID)
}

// ── Message Types ──────────────────────────────────────────

type MsgType string

const (
	// deploy
	MsgDeploy  MsgType = "deploy"
	MsgStop    MsgType = "stop"
	MsgRemove  MsgType = "remove"
	MsgStart   MsgType = "start"
	MsgRestart MsgType = "restart"

	// agent
	MsgHeartbeat MsgType = "heartbeat"
	MsgResult    MsgType = "result"

	// events
	MsgPaymentConfirmed MsgType = "payment_confirmed"
	MsgInstanceUpdated  MsgType = "instance_updated"
	MsgInstanceExpired  MsgType = "instance_expired"
)

// ── Deploy Command ─────────────────────────────────────────

// DeployCommand دستوری که apimanager به agentmanager می‌فرستد.
type DeployCommand struct {
	Type          MsgType           `json:"type"`
	ServerID      string            `json:"server_id"`
	ContainerName string            `json:"container_name"`
	ImageName     string            `json:"image_name"`
	ImageTag      string            `json:"image_tag"`
	EnvVars       map[string]string `json:"env_vars"`
	NetworkName   string            `json:"network_name,omitempty"`
	// ContainerID فقط برای stop/remove/restart لازم است
	ContainerID string `json:"container_id,omitempty"`
	// Settings تنظیمات اختیاری امنیت/منابع برای override پیش‌فرض‌های agentmanager.
	// nil یعنی «از پیش‌فرض‌های سرور استفاده کن».
	Settings *DeploySettings `json:"settings,omitempty"`

	// ── envelope auth ──
	// این چهار فیلد به agentmanager اجازه می‌دهند اصالت، تازگی و یک‌بارمصرف‌بودن
	// دستور deploy را تأیید کند (همان الگوی source-service/internal/bus.authorize).
	// اختیاری‌اند تا پیام‌های قدیمی همچنان unmarshal شوند؛ ولی نبودشان در
	// agentmanager منجر به رد شدن دستور می‌شود.
	ServiceID  string `json:"service_id,omitempty"`  // هویت سرویس فرستنده (مثلاً "botmanager")
	ServiceKey string `json:"service_key,omitempty"` // ComputeServiceKey(SERVICE_HMAC_SECRET, ServiceID)
	IssuedAt   int64  `json:"issued_at,omitempty"`   // unix seconds؛ برای پنجره‌ی تازگی
	Nonce      string `json:"nonce,omitempty"`       // یکتا به‌ازای هر دستور؛ برای جلوگیری از replay
}

// DeploySettings تنظیمات اختیاری امنیتی و محدودیت منابع برای هر container است.
// همه‌ی فیلدها اختیاری‌اند؛ مقدار صفر/nil یعنی «پیش‌فرض agentmanager اعمال شود».
// این ساختار اجازه می‌دهد هر bot محدودیت اختصاصی خودش را داشته باشد بدون اینکه
// پیش‌فرض‌های امن سرور دور زده شوند.
type DeploySettings struct {
	// MemoryMB سقف حافظه به مگابایت (۰ = پیش‌فرض سرور).
	MemoryMB int64 `json:"memory_mb,omitempty"`
	// CPUs تعداد هسته‌ی مجاز (مثلاً 0.5 یا 2) — ۰ = پیش‌فرض سرور.
	CPUs float64 `json:"cpus,omitempty"`
	// PidsLimit حداکثر تعداد پردازه؛ جلوی fork-bomb را می‌گیرد (۰ = پیش‌فرض سرور).
	PidsLimit int64 `json:"pids_limit,omitempty"`
	// ReadonlyRootfs اگر تنظیم شود فایل‌سیستم ریشه را read-only/نوشتنی می‌کند
	// (nil = پیش‌فرض سرور). برای bot هایی که روی دیسک می‌نویسند می‌توان false گذاشت.
	ReadonlyRootfs *bool `json:"readonly_rootfs,omitempty"`
	// CapAdd قابلیت‌های kernel که با وجود drop ALL دوباره اضافه می‌شوند
	// (مثلاً ["NET_BIND_SERVICE"]). پیش‌فرض: هیچ.
	CapAdd []string `json:"cap_add,omitempty"`
	// TmpfsSizeMB اندازه‌ی tmpfs برای /tmp وقتی rootfs فقط‌خواندنی است (۰ = پیش‌فرض سرور).
	TmpfsSizeMB int64 `json:"tmpfs_size_mb,omitempty"`
}

// ── Heartbeat ──────────────────────────────────────────────

// HeartbeatMsg وضعیت سرور که agentmanager هر N ثانیه ارسال می‌کند.
//
// CPUPercent/MemoryUsedMB/MemoryTotalMB اختیاری‌اند (pointer، nil = گزارش نشده): نسخه‌ی
// agentmanager موجود در زمان اضافه‌شدن این فیلدها (۲۰۲۶-۰۷-۰۳) این‌ها را پر نمی‌کرد — این‌جا
// فقط برای سازگاری روبه‌جلو تعریف شده‌اند تا وقتی agentmanager آن‌ها را اضافه کرد، بدون تغییر
// دوباره‌ی این struct مصرف شوند. تا آن زمان همیشه nil خواهند بود.
type HeartbeatMsg struct {
	Type          MsgType           `json:"type"`
	ServerID      string            `json:"server_id"`
	Timestamp     int64             `json:"ts"`
	Containers    []ContainerStatus `json:"containers"`
	CPUPercent    *float64          `json:"cpu_percent,omitempty"`
	MemoryUsedMB  *int64            `json:"memory_used_mb,omitempty"`
	MemoryTotalMB *int64            `json:"memory_total_mb,omitempty"`
}

// ContainerStatus وضعیت یک container.
type ContainerStatus struct {
	Name   string `json:"name"`
	Image  string `json:"image"`
	State  string `json:"state"`  // running | exited | paused
	Status string `json:"status"` // "Up 2 hours"
}

// ResultMsg نتیجه اجرای یک دستور Docker.
type ResultMsg struct {
	Type          MsgType `json:"type"`
	ServerID      string  `json:"server_id"`
	ContainerName string  `json:"container_name"`
	CommandType   string  `json:"command_type"`
	Success       bool    `json:"success"`
	Output        string  `json:"output,omitempty"`
	Error         string  `json:"error,omitempty"`
	Timestamp     int64   `json:"ts"`
}

// ── Events ─────────────────────────────────────────────────

// PaymentConfirmedEvent پرداخت تأیید شده.
// bot این رویداد را publish می‌کند تا apimanager رکورد را update کند.
type PaymentConfirmedEvent struct {
	Type      MsgType `json:"type"`
	BotID     int64   `json:"bot_id"`
	UserID    int64   `json:"user_id"`
	PlanID    string  `json:"plan_id"`
	Amount    float64 `json:"amount"`
	Gateway   string  `json:"gateway"`
	RefCode   string  `json:"ref_code"`
	Timestamp int64   `json:"ts"`
}

// InstanceUpdatedEvent تغییر وضعیت instance از apimanager به bot.
// مثلاً انقضا، تمدید، تغییر تنظیمات.
type InstanceUpdatedEvent struct {
	Type       MsgType        `json:"type"`
	BotID      int64          `json:"bot_id"`
	ChangeType string         `json:"change_type"` // expired | renewed | settings_changed
	Data       map[string]any `json:"data,omitempty"`
	Timestamp  int64          `json:"ts"`
}

// ── Service Provisioning Events ─────────────────────────────

const (
	// ServiceCreationRequested کاربر درخواست ایجاد سرویس داد.
	ServiceCreationRequested = "service.creation.requested"
	// ServiceCreationStarted agentmanager شروع به deploy کرد.
	ServiceCreationStarted = "service.creation.started"
	// ServiceCreationCompleted deploy موفق.
	ServiceCreationCompleted = "service.creation.completed"
	// ServiceCreationFailed deploy ناموفق → refund باید صورت گیرد.
	ServiceCreationFailed = "service.creation.failed"
	// ServiceStatusChanged تغییر وضعیت سرویس.
	ServiceStatusChanged = "service.status.changed"
)

// ServiceProvisionPayload payload رویداد provisioning.
type ServiceProvisionPayload struct {
	InstanceID  string `json:"instance_id"`
	OwnerID     string `json:"owner_id"`
	ServiceType string `json:"service_type"`
	PlanID      string `json:"plan_id"`
	BotToken    string `json:"bot_token,omitempty"`
	ServerID    string `json:"server_id,omitempty"`
	InvoiceCode string `json:"invoice_code,omitempty"`
	AmountNano  int64  `json:"amount_nano,omitempty"`
	Error       string `json:"error,omitempty"`
}

// ══════════════════════════════════════════════════════════════
// Pay subjects (NATS request/reply) — botpay به‌عنوان responder
// همه‌ی سرویس‌ها برای موجودی و پرداخت از این‌ها استفاده می‌کنند.
// هیچ سرویسی مستقیم به DB کیف پول دست نمی‌زند.
// ══════════════════════════════════════════════════════════════

const (
	// SubjPayBalance — گرفتن موجودی یک کاربر (request/reply)
	SubjPayBalance = "pay.balance"
	// SubjPayAuthorize — تأیید دسترسی یک سرویس به حساب کاربر
	SubjPayAuthorize = "pay.authorize"
	// SubjPayDeduct — کسر از حساب (پرداخت)؛ همه سرویس‌ها از این استفاده می‌کنند
	SubjPayDeduct = "pay.deduct"
	// SubjPayCredit — افزودن اعتبار (فقط admin/داخلی)
	SubjPayCredit = "pay.credit"
	// SubjPayTransfer — انتقال بین دو کاربر
	SubjPayTransfer = "pay.transfer"
	// SubjPayCreateInvoice — ساخت invoice واریز TON (برای شارژ کیف پول
	// از طریق انتقال مستقیم TON با کد comment، نه لینک پرداخت آنلاین)
	SubjPayCreateInvoice = "pay.invoice.create"
	// SubjPayInvoiceStatus — استعلامِ وضعیتِ یک فاکتورِ خاص با کُد آن.
	// botpay باید این را هندل کند: فاکتور را با Code پیدا و وضعیتش را برگرداند.
	SubjPayInvoiceStatus = "pay.invoice.status"
	// SubjPayQueue — queue group برای load balancing بین instanceهای botpay
	SubjPayQueue = "botpay-workers"
	// SubjWalletUpdated — رویداد تغییر موجودی (botpay → همه سرویس‌ها)
	// کلاینت‌ها با شنیدن این، کش Redis خود را باطل می‌کنند.
	SubjWalletUpdated = "wallet.updated"
)

// InvoiceRequest درخواست ساخت یک invoice واریز TON.
type InvoiceRequest struct {
	PayRequest
	AmountTON float64 `json:"amount_ton"`
	Ref       string  `json:"ref"` // مثلا plan_id
}

// InvoiceResponse پاسخ — کاربر باید AmountTON را به MasterAddress با
// comment = Code بفرستد (نه یک لینک پرداخت آنلاین؛ TON-deposit مبتنی بر
// تطبیق comment تراکنش است، طبق معماری واقعی botpay).
type InvoiceResponse struct {
	Code          string  `json:"code"`           // کد یکتا — کاربر باید این را در comment تراکنش بنویسد
	MasterAddress string  `json:"master_address"` // آدرس کیف پول پلتفرم
	AmountTON     float64 `json:"amount_ton"`
	ExpiresAt     int64   `json:"expires_at"` // unix timestamp
	Error         string  `json:"error,omitempty"`
}

// ── وضعیتِ فاکتور (Invoice Status) ────────────────────────────
// مقادیرِ ممکنِ وضعیتِ یک فاکتور.
const (
	InvoiceStatusPending  = "pending"   // هنوز پرداخت/دریافت نشده
	InvoiceStatusPaid     = "paid"      // کامل دریافت شد
	InvoiceStatusPartial  = "partial"   // بخشی دریافت شد
	InvoiceStatusExpired  = "expired"   // منقضی شده
	InvoiceStatusNotFound = "not_found" // یافت نشد
)

// InvoiceStatusRequest استعلامِ وضعیتِ یک فاکتور با کُد آن.
type InvoiceStatusRequest struct {
	PayRequest
	Code string `json:"code"`
}

// InvoiceStatusResponse وضعیتِ فاکتور.
type InvoiceStatusResponse struct {
	Status    string  `json:"status"`     // یکی از InvoiceStatus* بالا
	AmountTON float64 `json:"amount_ton"` // مبلغِ موردِ انتظار
	PaidTON   float64 `json:"paid_ton"`   // مبلغِ دریافت‌شده تا این لحظه
	ExpiresAt int64   `json:"expires_at"` // unix
	Error     string  `json:"error,omitempty"`
}

// WalletUpdatedEvent رویداد تغییر موجودی یک کاربر.
type WalletUpdatedEvent struct {
	TelegramID int64  `json:"telegram_id"`
	Reason     string `json:"reason"` // deposit | payment | refund | transfer
}

// PayCompletedSubject رویداد اتمام پرداخت برای یک سرویس خاص.
// هر سرویس به pay.completed.<service_id> خودش گوش می‌دهد.
func PayCompletedSubject(serviceID string) string {
	return "pay.completed." + serviceID
}

// PayCompletedEvent جزئیات یک پرداخت تکمیل‌شده برای سرویس درخواست‌کننده.
type PayCompletedEvent struct {
	ServiceID  string  `json:"service_id"`
	TelegramID int64   `json:"telegram_id"`
	AmountTON  float64 `json:"amount_ton"`
	Reason     string  `json:"reason"`   // مثلا "plan:pro"
	Ref        string  `json:"ref"`      // شناسه مرجع سرویس
	Metadata   string  `json:"metadata"` // JSON آزاد
	TxID       string  `json:"tx_id"`
	Success    bool    `json:"success"`
}

// ── Request/Response contracts ────────────────────────────────

// PayRequest پایه‌ی همه‌ی درخواست‌های pay — شامل احراز هویت سرویس.
type PayRequest struct {
	ServiceID  string `json:"service_id"`  // مثلا "botmanager", "uploader"
	ServiceKey string `json:"service_key"` // کلید احراز هویت سرویس
	TelegramID int64  `json:"telegram_id"` // کاربر هدف
}

// BalanceRequest درخواست موجودی.
type BalanceRequest struct {
	PayRequest
}

// BalanceResponse پاسخ موجودی (واحدها TON، نه nano).
type BalanceResponse struct {
	TelegramID int64   `json:"telegram_id"`
	TONBalance float64 `json:"ton_balance"`
	Credit     float64 `json:"credit"`
	Total      float64 `json:"total"`
	Frozen     float64 `json:"frozen"`
	TONAddress string  `json:"ton_address"`
	Error      string  `json:"error,omitempty"`
}

// AuthorizeRequest درخواست تأیید دسترسی سرویس به حساب کاربر.
type AuthorizeRequest struct {
	PayRequest
}

// AuthorizeResponse نتیجه‌ی تأیید.
type AuthorizeResponse struct {
	Authorized bool   `json:"authorized"`
	Error      string `json:"error,omitempty"`
}

// DeductRequest درخواست کسر (پرداخت).
type DeductRequest struct {
	PayRequest
	AmountTON      float64 `json:"amount_ton"`
	Reason         string  `json:"reason"`          // مثلا "plan:starter", "vpn:monthly"
	Ref            string  `json:"ref"`             // شناسه مرجع در سرویس درخواست‌کننده
	Metadata       string  `json:"metadata"`        // JSON آزاد برای شفافیت
	IdempotencyKey string  `json:"idempotency_key"` // جلوگیری از کسر دوباره
}

// DeductResponse نتیجه‌ی کسر.
// ErrorCode کدهای خطای عددی pay — برای تشخیص قابل‌اعتماد نوع خطا، نه
// string matching روی پیام (که با تغییر فرمت پیام شکننده می‌شود).
type ErrorCode int

const (
	ErrCodeNone                ErrorCode = 0
	ErrCodeUnauthorized        ErrorCode = 1
	ErrCodeInsufficientBalance ErrorCode = 2
	ErrCodeInternal            ErrorCode = 3
)

type DeductResponse struct {
	Success    bool      `json:"success"`
	NewBalance float64   `json:"new_balance"`
	TxID       string    `json:"tx_id,omitempty"`
	Error      string    `json:"error,omitempty"`
	Code       ErrorCode `json:"code,omitempty"`
}

// ══════════════════════════════════════════════════════════════
// Member subjects (NATS request/reply) — member-bot به‌عنوان
// زیرساخت متمرکز چک عضویت. bot های فرعی به‌جای ادمین‌شدن در هر
// کانال، از این مسیر می‌پرسند «کاربر X عضو کانال Y هست؟».
// ══════════════════════════════════════════════════════════════

const (
	// SubjMemberCheck — درخواست چک عضویت (request/reply)
	SubjMemberCheck = "member.check"
	// SubjMemberQueue — queue group برای load balancing
	SubjMemberQueue = "memberbot-workers"
)

// MemberCheckRequest درخواست چک عضویت یک کاربر در یک کانال.
type MemberCheckRequest struct {
	ChannelID int64 `json:"channel_id"`
	UserID    int64 `json:"user_id"`
}

// MemberCheckResponse نتیجه‌ی چک عضویت.
type MemberCheckResponse struct {
	IsMember bool   `json:"is_member"`
	Cached   bool   `json:"cached"`
	Error    string `json:"error,omitempty"`
}

// ══════════════════════════════════════════════════════════════
// FreeBot subjects — وقتی botmanager یک instance با LockMode=free
// می‌سازد، ads-bot باید آن را در FreeBotSlot ثبت کند تا بعداً بتواند
// به یک کمپین اجاره‌ای وصلش کند.
// ══════════════════════════════════════════════════════════════

const SubjFreeBotCreated = "freebot.created"

// FreeBotCreatedEvent رویداد ساخت یک instance رایگان.
type FreeBotCreatedEvent struct {
	InstanceID string `json:"instance_id"` // uuid از shared/models.BotInstance.ID
	BotID      int64  `json:"bot_id"`
}

// MembershipJoinedEvent — همان payload که member-bot/internal/events/publisher.go
// روی subject "membership.joined" می‌فرستد.
const SubjMembershipJoined = "membership.joined"

type MembershipJoinedEvent struct {
	TelegramID  int64  `json:"telegram_id"`
	CommunityID int64  `json:"community_id"` // chatID تلگرام
	Source      string `json:"source"`       // "organic" | "invite_link"
	InviteHash  string `json:"invite_hash"`
	JoinedAt    int64  `json:"joined_at"`
	Username    string `json:"username"`
}

// SubjConfirmChannelAdmin — وقتی bot فرعی (مثلا uploader) تشخیص داد که در
// کانال هدف ادمین شده، این را به ads-bot اطلاع می‌دهد تا قفل‌کردن شروع شود.
const SubjConfirmChannelAdmin = "ads.confirm_channel_admin"

// ConfirmChannelAdminRequest درخواست تأیید ادمین‌شدن در کانال.
type ConfirmChannelAdminRequest struct {
	BotID int64 `json:"bot_id"`
}

// ConfirmChannelAdminResponse پاسخ ساده موفق/ناموفق.
type ConfirmChannelAdminResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// SubjBotStatusCheck — bot فرعیِ رایگان (uploader/vpn/archive) هنگام start و
// به‌صورت دوره‌ای می‌پرسد «آیا الان به یک کمپینِ اجاره‌ی قفلِ فعال در ads-bot
// وصل‌ام؟». این جایگزینِ کوئریِ مستقیمِ Postgres/bot_instances.lock_mode
// قبلیِ uploader-bot شد که با قطعِ کاملِ Postgres از ربات‌های محصول از کار
// افتاده بود — ads-bot (مالکِ واقعیِ FreeBotSlot/LockRentalCampaign) پاسخ
// می‌دهد، نه botmanager.
const SubjBotStatusCheck = "ads.bot_status_check"

// BotStatusRequest درخواستِ وضعیت با BotID تلگرامیِ خودِ bot.
type BotStatusRequest struct {
	BotID int64 `json:"bot_id"`
}

// BotStatusResponse پاسخِ وضعیت. CampaignID (اگر InCampaign=true) برای
// attribution دقیقِ گزارش‌های fraud-engine به همان کمپینِ مشخص استفاده
// می‌شود (رجوع fraudclient.ReportJoin).
type BotStatusResponse struct {
	InCampaign bool   `json:"in_campaign"`
	CampaignID string `json:"campaign_id,omitempty"`
	Error      string `json:"error,omitempty"`
}

// ══════════════════════════════════════════════════════════════
// License subjects (NATS request/reply) — license-service به‌عنوان
// responder. instance_id (=BotID) هر ربات ساخته‌شده را در برابر یک لایسنس
// معتبر پردازش می‌کند و اجاره‌ی همان instance روی بیش از یک سرور فیزیکی
// (کپی/کلون) را تشخیص می‌دهد.
//
// نکته‌ی طراحی: درست مثل pay.*، هر درخواست یک ServiceID/ServiceKey با خودش
// حمل می‌کند تا فقط سرویس‌های مرکزی مورد اعتماد (agentmanager, botmanager)
// بتوانند لایسنس صادر/باطل کنند — نه هر کلاینت NATS. license.verify از
// خودِ bot instance می‌آید و با instance_id+server_id خودش احراز می‌شود، نه
// با ServiceKey (چون container مشتری هرگز راز مادر را نمی‌بیند).
// ══════════════════════════════════════════════════════════════

const (
	// SubjLicenseIssue — صدور لایسنس برای یک instance تازه‌ساخته‌شده.
	// فقط سرویس‌های مرکزی (agentmanager/botmanager) مجازند.
	SubjLicenseIssue = "license.issue"
	// SubjLicenseVerify — خودِ bot instance دوره‌ای می‌پرسد «لایسنس من هنوز
	// معتبر و روی همین سرور است؟». اگر همان BotID از ServerID دیگری هم
	// check-in کند، license-service آن را به‌عنوان کلون/کپی علامت می‌زند.
	SubjLicenseVerify = "license.verify"
	// SubjLicenseRevoke — ابطال دستی لایسنس (مثلاً پایان اشتراک، تخلف).
	SubjLicenseRevoke = "license.revoke"
	// SubjLicenseQueue — queue group برای load balancing.
	SubjLicenseQueue = "license-workers"
)

// LicenseStatus وضعیت یک لایسنس.
type LicenseStatus string

const (
	LicenseActive  LicenseStatus = "active"
	LicenseRevoked LicenseStatus = "revoked"
	LicenseExpired LicenseStatus = "expired"
)

// LicenseIssueRequest — درخواست صدور لایسنس برای یک instance تازه.
type LicenseIssueRequest struct {
	ServiceID  string `json:"service_id"`  // "agentmanager" یا "botmanager"
	ServiceKey string `json:"service_key"` // HMAC(SERVICE_HMAC_SECRET, service_id)
	BotID      int64  `json:"bot_id"`
	InstanceID string `json:"instance_id"` // "bot_<BotID>"
	OwnerID    string `json:"owner_id"`    // uuid کاربر مالک
	ServerID   string `json:"server_id"`   // uuid سروری که اول‌بار روی آن deploy شد
	PlanID     string `json:"plan_id,omitempty"`
	ExpiresAt  int64  `json:"expires_at,omitempty"` // unix — ۰ یعنی نامحدود (تا ابطال دستی)
}

// LicenseIssueResponse — نتیجه‌ی صدور.
type LicenseIssueResponse struct {
	Success bool   `json:"success"`
	Token   string `json:"token,omitempty"` // JWT امضاشده — برای verify آفلاین سریع
	Error   string `json:"error,omitempty"`
}

// LicenseVerifyRequest — خودِ bot instance این را دوره‌ای می‌فرستد.
type LicenseVerifyRequest struct {
	BotID    int64  `json:"bot_id"`
	Token    string `json:"token"`     // توکنی که در زمان issue گرفته بود
	ServerID string `json:"server_id"` // سروری که همین الان از آن اجرا می‌شود
}

// LicenseVerifyResponse — نتیجه‌ی بررسی.
type LicenseVerifyResponse struct {
	Valid        bool   `json:"valid"`
	Status       string `json:"status"`                  // یکی از LicenseStatus بالا
	CloneWarning bool   `json:"clone_warning,omitempty"` // true یعنی از سرور دیگری هم check-in شده
	Error        string `json:"error,omitempty"`
}

// LicenseRevokeRequest — ابطال دستی (فقط سرویس‌های مرکزی).
type LicenseRevokeRequest struct {
	ServiceID  string `json:"service_id"`
	ServiceKey string `json:"service_key"`
	BotID      int64  `json:"bot_id"`
	Reason     string `json:"reason"`
}

// LicenseRevokeResponse — پاسخ ابطال.
type LicenseRevokeResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// SubjLicenseCloneDetected — رویداد pub/sub که license-service وقتی یک
// instance را همزمان از دو ServerID متفاوت می‌بیند منتشر می‌کند، تا
// botmanager بتواند به ادمین/مالک اطلاع دهد.
const SubjLicenseCloneDetected = "license.clone_detected"

// LicenseCloneDetectedEvent جزئیات تشخیص کلون.
type LicenseCloneDetectedEvent struct {
	BotID            int64  `json:"bot_id"`
	InstanceID       string `json:"instance_id"`
	KnownServerID    string `json:"known_server_id"`
	UnexpectedServer string `json:"unexpected_server_id"`
	DetectedAt       int64  `json:"detected_at"`
}
