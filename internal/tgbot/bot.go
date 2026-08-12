// Package tgbot ربات تلگرام botpay — رابط کاربری کیف پول TON.
//
// طراحی (بازطراحی ۲۰۲۶-۰۷-۱۵): رابط inline-first با ناوبری edit-in-place،
// مرحله‌ی تأیید برای عملیات مالی، تاریخچه‌ی صفحه‌بندی‌شده، و یک پنل مدیریت کامل
// (برداشت‌ها، افزودن اعتبار، جستجوی کاربر، بررسی سلامت زنجیره‌ی هش). همه‌ی متن‌ها
// از i18n می‌آیند و هیچ رشته‌ی نمایشی hard-code نیست. هسته‌ی مالی (لجر دوطرفه،
// زنجیره‌ی هش، consensus) دست‌نخورده است؛ این لایه فقط رابط و orchestration است.
package tgbot

import (
	"context"

	tele "gopkg.in/telebot.v4"

	"github.com/mrjvadi/botpay/internal/i18n"
	"github.com/mrjvadi/botpay/internal/store"
	"github.com/mrjvadi/botpay/internal/wallet"
	"github.com/mrjvadi/botpay/shared/pkg/ports"
)

// historyPageSize تعداد تراکنش در هر صفحه‌ی تاریخچه.
const historyPageSize = 6

// adminWithdrawPageSize تعداد برداشت در هر صفحه‌ی فهرست ادمین.
const adminWithdrawPageSize = 6

// Handler تمام منطق رابط ربات را در خود دارد.
type Handler struct {
	wallet      *wallet.Service
	st          *store.Store
	ownerID     int64
	defaultLang i18n.Lang
	log         ports.Logger
	bot         *tele.Bot // برای ارسال push notification
	states      *stateStore
}

// New یک Handler جدید می‌سازد. defaultLang کد زبان پیش‌فرض است (مثلاً "fa").
func New(w *wallet.Service, st *store.Store, ownerID int64, defaultLang string, log ports.Logger) *Handler {
	i18n.SetDefault(defaultLang)
	return &Handler{
		wallet:      w,
		st:          st,
		ownerID:     ownerID,
		defaultLang: i18n.Normalize(defaultLang),
		log:         log,
		states:      newStateStore(),
	}
}

// SetBot ربات را تنظیم و notifier را فعال می‌کند.
func (h *Handler) SetBot(b *tele.Bot) {
	h.bot = b
	h.wallet.SetNotifier(h)
	h.setCommands(b)
}

// setCommands منوی دستورات تلگرام (دکمه‌ی «/») را تنظیم می‌کند.
func (h *Handler) setCommands(b *tele.Bot) {
	_ = b.SetCommands([]tele.Command{
		{Text: "start", Description: "شروع / کیف پول"},
		{Text: "wallet", Description: "موجودی و آدرس واریز"},
		{Text: "deposit", Description: "واریز TON"},
		{Text: "withdraw", Description: "برداشت TON"},
		{Text: "transfer", Description: "انتقال به کاربر دیگر"},
		{Text: "history", Description: "تاریخچه‌ی تراکنش‌ها"},
		{Text: "help", Description: "راهنما"},
		{Text: "language", Description: "تغییر زبان"},
	})
}

// SendHTML پیاده‌سازی wallet.Notifier — ارسال پیام HTML به یک کاربر.
func (h *Handler) SendHTML(ctx context.Context, telegramID int64, html string) error {
	if h.bot == nil {
		return nil
	}
	_, err := h.bot.Send(&tele.Chat{ID: telegramID}, html, tele.ModeHTML)
	return err
}

// Register هندلرهای ربات را ثبت می‌کند.
func Register(b *tele.Bot, h *Handler) {
	b.Handle("/start", h.onStart)
	b.Handle("/wallet", h.onWallet)
	b.Handle("/deposit", h.onDeposit)
	b.Handle("/withdraw", h.onWithdraw)
	b.Handle("/history", h.onHistoryCmd)
	b.Handle("/help", h.onHelp)
	b.Handle("/language", h.onLanguage)
	b.Handle("/transfer", h.onTransfer)

	// ادمین (دستور اصلی؛ بقیه از طریق دکمه‌های inline پنل)
	b.Handle("/admin", h.onAdmin)

	b.Handle(tele.OnText, h.onText)
	b.Handle(tele.OnCallback, h.onCallback)
}

// isAdmin بررسی می‌کند فرستنده، مالک ربات است.
func (h *Handler) isAdmin(c tele.Context) bool {
	return c.Sender() != nil && c.Sender().ID == h.ownerID
}

// mainKB کیبورد اصلی reply را با درنظرگرفتن ادمین‌بودن کاربر می‌سازد.
func (h *Handler) mainKB(c tele.Context, lang i18n.Lang) *tele.ReplyMarkup {
	return kbMain(lang, h.isAdmin(c))
}

// langOf زبان فعال کاربر را تعیین می‌کند: ابتدا زبان ذخیره‌شده در DB، سپس
// language_code تلگرام، و در نهایت زبان پیش‌فرض.
func (h *Handler) langOf(ctx context.Context, c tele.Context) i18n.Lang {
	if c.Sender() == nil {
		return h.defaultLang
	}
	if w, err := h.st.GetWallet(ctx, c.Sender().ID); err == nil && w != nil && w.Lang != "" {
		return i18n.Normalize(w.Lang)
	}
	if lc := c.Sender().LanguageCode; lc != "" {
		if l, ok := i18n.Parse(lc); ok {
			return l
		}
	}
	return h.defaultLang
}
