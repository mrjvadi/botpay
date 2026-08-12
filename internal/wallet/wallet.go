// Package wallet منطق کسب‌وکار کیف پول را پیاده‌سازی می‌کند.
package wallet

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	natsclient "github.com/mrjvadi/botpay/shared/pkg/adapters/nats"
	"github.com/mrjvadi/botpay/shared/pkg/ports"

	"github.com/mrjvadi/botpay/internal/consensus"
	"github.com/mrjvadi/botpay/internal/i18n"
	"github.com/mrjvadi/botpay/internal/store"
	"github.com/mrjvadi/botpay/internal/ton"
)

// ErrInsufficientBalance خطای پایه برای تشخیص قابل‌اعتماد موجودی ناکافی
// (با errors.Is)، مستقل از متن دقیق پیام که می‌تواند جزئیات اضافه داشته
// باشد (مثلاً مقدار موجودی فعلی).
var ErrInsufficientBalance = errors.New("insufficient balance")

// NATS subjects برای رویدادهای botpay
const (
	SubjectDeposit = "botpay.deposit"    // واریز انجام شد
	SubjectPaid    = "botpay.paid"       // پرداخت به سرویس انجام شد
	SubjectInvoice = "botpay.invoice.%s" // invoice تأیید شد
)

// MinWithdrawNano حداقل مقدار برداشت (0.1 TON).
const MinWithdrawNano = 100_000_000

// NetworkFeeNano کارمزد شبکه TON برای برداشت.
const NetworkFeeNano = 10_000_000 // 0.01 TON

// Service service layer کیف پول.
type Service struct {
	store      *store.Store
	notify     Notifier
	nc         *natsclient.Client
	log        ports.Logger
	masterAddr string
	guard      *consensus.Guard // محافظ consensus
}

func New(st *store.Store, nc *natsclient.Client, log ports.Logger, masterAddr string, guard *consensus.Guard) *Service {
	return &Service{store: st, nc: nc, log: log, masterAddr: masterAddr, guard: guard}
}

// SetNotifier ربات تلگرام را برای اعلان فوری ست می‌کند.
func (s *Service) SetNotifier(n Notifier) { s.notify = n }

// GetOrCreate wallet کاربر را پیدا یا می‌سازد.
func (s *Service) GetOrCreate(ctx context.Context, telegramID int64) (*store.Wallet, error) {
	// آدرس اختصاصی = آدرس master + نشان‌دهنده کاربر در comment
	// (در TON همه به یک آدرس می‌فرستند ولی comment متفاوت است)
	return s.store.GetOrCreateWallet(ctx, telegramID, s.masterAddr)
}

// CreateDepositInvoice یک invoice جدید برای واریز می‌سازد.
// کاربر باید amount TON به masterAddr با comment = invoice.Code بفرستد.
func (s *Service) CreateDepositInvoice(ctx context.Context, telegramID int64, amountNano int64, serviceID, ref string) (*store.Invoice, error) {
	w, err := s.GetOrCreate(ctx, telegramID)
	if err != nil {
		return nil, err
	}

	code := genInvoiceCode()
	inv := &store.Invoice{
		WalletID:  w.ID,
		Code:      code,
		Amount:    amountNano,
		ServiceID: serviceID,
		Ref:       ref,
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}
	if err := s.store.CreateInvoice(ctx, inv); err != nil {
		return nil, err
	}

	s.log.Info("invoice created",
		ports.F("code", code),
		ports.F("amount_ton", float64(amountNano)/1e9),
		ports.F("telegram_id", telegramID))

	return inv, nil
}

// HandleDeposit پرداخت رسیده از TON blockchain را پردازش می‌کند.
// این تابع توسط ton.Watcher صدا زده می‌شود.
func (s *Service) HandleDeposit(ctx context.Context, event ton.DepositEvent) error {
	// پیدا کردن invoice با comment
	var w *store.Wallet
	var inv *store.Invoice

	if event.InvoiceCode != "" {
		var err error
		inv, err = s.store.FindPendingInvoiceByCode(ctx, event.InvoiceCode)
		if err != nil {
			return err
		}
	}

	if inv != nil {
		// بارگذاری wallet کامل با ID
		var err2 error
		w, err2 = s.store.GetWalletByID(ctx, inv.WalletID)
		if err2 != nil || w == nil {
			s.log.Error("HandleDeposit: wallet not found", ports.F("wallet_id", inv.WalletID))
			return fmt.Errorf("wallet not found for invoice")
		}
		event.WalletID = inv.WalletID.String()
	} else if event.InvoiceCode != "" {
		// invoice فعالی پیدا نشد → کامنت را به‌عنوان «کامنت شخصی» (pay_handle)
		// کاربر تطبیق بده. این اجازه می‌دهد کاربر همیشه با کامنت ثابت خود واریز کند.
		var err3 error
		w, err3 = s.store.GetWalletByPayHandle(ctx, event.InvoiceCode)
		if err3 != nil {
			return err3
		}
		if w != nil {
			event.WalletID = w.ID.String()
		}
	}

	if w == nil {
		// کامنت با هیچ invoice یا کاربری تطبیق نخورد → نادیده گرفته می‌شود.
		s.log.Info("deposit without matching comment",
			ports.F("comment", event.InvoiceCode),
			ports.F("from", event.FromAddr),
			ports.F("amount", event.AmountTON))
		return nil
	}

	// ── بررسی consensus برای واریز ───────────────────────────
	if s.guard != nil {
		if err := s.guard.CheckDeposit(ctx, w.TONAddress, w.TelegramID, event.AmountNano, event.TxHash); err != nil {
			s.log.Error("deposit rejected by consensus", ports.F("err", err))
			return err
		}
	}

	// ثبت deposit با تمام اطلاعاتِ on-chain (Comment خالی نگه‌داشته نمی‌شود؛
	// همه‌ی داده‌ی TON ذخیره می‌شود). Description خالی می‌ماند تا تاریخچه برچسبِ
	// نوعِ تراکنش را به زبانِ هر کاربر نمایش دهد.
	if _, err := s.store.Deposit(ctx, store.DepositRecord{
		WalletID:   w.ID,
		AmountNano: event.AmountNano,
		FeeNano:    event.FeeNano,
		TxHash:     event.TxHash,
		FromAddr:   event.FromAddr,
		ToAddr:     event.ToAddr,
		Comment:    event.InvoiceCode,
		LT:         event.LT,
		Utime:      event.Utime,
	}); err != nil {
		return fmt.Errorf("deposit to wallet: %w", err)
	}

	// ثبتِ مبلغِ دریافتی روی فاکتور (پشتیبانی از واریزِ جزئی). سرویس درخواست‌دهنده
	// فقط زمانی مطلع می‌شود که فاکتور به‌طور کامل پرداخت شده باشد.
	if inv != nil {
		paid, err := s.store.AddInvoiceReceipt(ctx, inv.ID, event.AmountNano, event.TxHash)
		if err != nil {
			s.log.Error("add invoice receipt failed", ports.F("code", inv.Code), ports.F("err", err))
		}
		if paid {
			s.publish(
				fmt.Sprintf(SubjectInvoice, inv.Code),
				map[string]any{
					"invoice_id":  inv.ID.String(),
					"code":        inv.Code,
					"ref":         inv.Ref,
					"service_id":  inv.ServiceID,
					"amount_nano": event.AmountNano,
					"wallet_id":   w.ID.String(),
					"tx_hash":     event.TxHash,
				},
			)
		}
	}

	// publish رویداد عمومی واریز
	s.publish(SubjectDeposit, ton.DepositEvent{
		WalletID:    w.ID.String(),
		AmountNano:  event.AmountNano,
		AmountTON:   event.AmountTON,
		TxHash:      event.TxHash,
		InvoiceCode: event.InvoiceCode,
	})

	s.log.Info("deposit processed",
		ports.F("wallet", w.ID),
		ports.F("amount_ton", event.AmountTON),
		ports.F("tx", event.TxHash))

	// ── publish wallet.deposit برای سرویس‌های دیگر ──────────
	s.publish("wallet.deposit", map[string]any{
		"telegram_id":  w.TelegramID,
		"wallet_id":    w.ID,
		"amount_nano":  event.AmountNano,
		"amount_ton":   event.AmountTON,
		"tx_hash":      event.TxHash,
		"invoice_code": event.InvoiceCode,
	})

	// ── اعلان فوری به کاربر (به زبان خودش) ──────────────────
	if s.notify != nil {
		newBalance := event.AmountTON
		if updated, _ := s.store.GetWalletByID(ctx, w.ID); updated != nil {
			newBalance = updated.TotalTON()
		}
		msg := i18n.T(i18n.Normalize(w.Lang), i18n.KDepositConfirmed, fmtTONStr(event.AmountTON), fmtTONStr(newBalance))
		if err := s.notify.SendHTML(ctx, w.TelegramID, msg); err != nil {
			s.log.Error("notify deposit failed", ports.F("err", err))
		}
	}

	return nil
}

// publish یک رویداد را روی NATS منتشر می‌کند و در صورت نبودن اتصال، بی‌صدا
// رد می‌شود (حالت standalone). این از panic ناشی از nil بودن client جلوگیری می‌کند.
func (s *Service) publish(subject string, payload any) {
	if s.nc == nil {
		return
	}
	if err := s.nc.PublishCore(subject, payload); err != nil {
		s.log.Error("nats publish failed", ports.F("subject", subject), ports.F("err", err))
	}
}

// Pay کسر مبلغ از wallet برای پرداخت به سرویس.
// serviceID: شناسه سرویس (مثلاً "botmanager")
// ref: شناسه مرجع در آن سرویس (مثلاً plan_id)
func (s *Service) Pay(ctx context.Context, telegramID int64, amountNano int64, serviceID, ref, desc string) (*store.Transaction, error) {
	w, err := s.store.GetWallet(ctx, telegramID)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, fmt.Errorf("wallet not found")
	}
	if !w.HasEnough(amountNano) {
		return nil, fmt.Errorf("%w: have %.4f TON, need %.4f TON",
			ErrInsufficientBalance, w.TotalTON(), float64(amountNano)/1e9)
	}

	// ── بررسی consensus قبل از کسر ──────────────────────────
	if s.guard != nil {
		if err := s.guard.CheckDeduct(ctx, w.TONAddress, telegramID, amountNano, serviceID, ref); err != nil {
			return nil, fmt.Errorf("consensus: %w", err)
		}
	}

	tx, created, err := s.store.Deduct(ctx, w.ID, amountNano, serviceID, ref, desc)
	if err != nil {
		return nil, err
	}

	// publish رویداد پرداخت
	s.publish(SubjectPaid, map[string]any{
		"wallet_id":  w.ID.String(),
		"service_id": serviceID,
		"ref":        ref,
		"amount_ton": float64(amountNano) / 1e9,
		"tx_id":      tx.ID.String(),
	})

	// اعلان به کاربر فقط برای کسرِ تازه (نه retry idempotent).
	if created && s.notify != nil {
		newTotal := 0.0
		if updated, _ := s.store.GetWalletByID(ctx, w.ID); updated != nil {
			newTotal = updated.TotalTON()
		}
		msg := i18n.T(i18n.Normalize(w.Lang), i18n.KNotifyPayment,
			fmtTONStr(NanoToTON(amountNano)), serviceID, fmtTONStr(newTotal))
		if err := s.notify.SendHTML(ctx, telegramID, msg); err != nil {
			s.log.Error("notify payment failed", ports.F("err", err))
		}
	}

	return tx, nil
}

// RequestWithdraw درخواست برداشت ایجاد می‌کند.
func (s *Service) RequestWithdraw(ctx context.Context, telegramID int64, toAddress string, amountNano int64, note string) (*store.WithdrawRequest, error) {
	if !IsValidTONAddress(toAddress) {
		return nil, fmt.Errorf("invalid TON address")
	}
	if amountNano < MinWithdrawNano {
		return nil, fmt.Errorf("minimum withdrawal is %.1f TON", float64(MinWithdrawNano)/1e9)
	}

	totalNeeded := amountNano + NetworkFeeNano
	w, err := s.store.GetWallet(ctx, telegramID)
	if err != nil || w == nil {
		return nil, fmt.Errorf("wallet not found")
	}
	if !w.HasEnough(totalNeeded) {
		return nil, fmt.Errorf("%w (including %.4f TON fee)", ErrInsufficientBalance, float64(NetworkFeeNano)/1e9)
	}

	req := &store.WithdrawRequest{
		WalletID:  w.ID,
		ToAddress: toAddress,
		Amount:    amountNano,
		Fee:       NetworkFeeNano,
		Note:      note,
	}
	if err := s.store.CreateWithdraw(ctx, req); err != nil {
		if errors.Is(err, store.ErrInsufficientBalance) {
			return nil, fmt.Errorf("%w (race condition — insufficient balance at write time)", ErrInsufficientBalance)
		}
		return nil, err
	}
	return req, nil
}

// DepositInstructions دستورالعمل واریز برای کاربر.
func (s *Service) DepositInstructions(ctx context.Context, telegramID int64, amountNano int64, serviceID, ref string) (string, string, error) {
	inv, err := s.CreateDepositInvoice(ctx, telegramID, amountNano, serviceID, ref)
	if err != nil {
		return "", "", err
	}

	// آدرس پرداخت با deep link
	payURL := fmt.Sprintf(
		"ton://transfer/%s?amount=%d&text=%s",
		s.masterAddr, amountNano, inv.Code,
	)

	return inv.Code, payURL, nil
}

// ── helpers ────────────────────────────────────────────────

func genInvoiceCode() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "PAY-" + strings.ToUpper(hex.EncodeToString(b))
}

// IsValidTONAddress یک اعتبارسنجی سبک برای آدرس TON (فرمت user-friendly).
// آدرس باید با UQ یا EQ شروع شده و دقیقاً ۴۸ کاراکتر باشد.
func IsValidTONAddress(addr string) bool {
	addr = strings.TrimSpace(addr)
	if len(addr) != 48 {
		return false
	}
	return strings.HasPrefix(addr, "UQ") || strings.HasPrefix(addr, "EQ")
}

// NanoToTON تبدیل nano-TON به TON.
func NanoToTON(nano int64) float64 { return float64(nano) / 1e9 }

// fmtTONStr مقدار TON را با حداکثر ۴ رقم اعشار و بدون صفرهای انتهایی قالب می‌کند
// (هم‌سو با نمایش ربات) — برای جای‌گذاری در قالب‌های %s پیام‌های اعلان.
func fmtTONStr(v float64) string {
	s := strconv.FormatFloat(v, 'f', 4, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-0" {
		s = "0"
	}
	return s
}

// TONToNano تبدیل TON به nano-TON.
func TONToNano(ton float64) int64 { return int64(ton * 1e9) }

// Store برگرداندن store برای استفاده مستقیم.
func (s *Service) Store() *store.Store { return s.store }

// MasterAddress آدرس کیف پول پلتفرم که کاربران باید برای واریز TON بفرستند.
func (s *Service) MasterAddress() string { return s.masterAddr }

// Transfer انتقال داخلی بین دو کاربر (P2P).
func (s *Service) Transfer(ctx context.Context, fromTelegramID, toTelegramID int64, amountNano int64, desc string) error {
	if fromTelegramID == toTelegramID {
		return fmt.Errorf("cannot transfer to yourself")
	}

	fromWallet, err := s.store.GetWallet(ctx, fromTelegramID)
	if err != nil || fromWallet == nil {
		return fmt.Errorf("sender wallet not found")
	}
	toWallet, err := s.store.GetWallet(ctx, toTelegramID)
	if err != nil || toWallet == nil {
		return fmt.Errorf("recipient wallet not found")
	}

	if !fromWallet.HasEnough(amountNano) {
		return ErrInsufficientBalance
	}

	_, _, err = s.store.Transfer(ctx, fromWallet.ID, toWallet.ID, amountNano, desc)
	if err != nil {
		return fmt.Errorf("transfer: %w", err)
	}

	// اعلان به گیرنده (به زبان خودش)
	if s.notify != nil {
		msg := i18n.T(i18n.Normalize(toWallet.Lang), i18n.KTransferReceived, fmtTONStr(NanoToTON(amountNano)), fromTelegramID)
		if err := s.notify.SendHTML(ctx, toTelegramID, msg); err != nil {
			s.log.Error("notify transfer failed", ports.F("err", err))
		}
	}

	s.log.Info("transfer done",
		ports.F("from", fromTelegramID),
		ports.F("to", toTelegramID),
		ports.F("amount_ton", NanoToTON(amountNano)))

	return nil
}

// ── تسویه‌ی برداشت و افزودن اعتبار (با اعلانِ کاربر) ─────────────
// این متدها لایه‌ی store را می‌پوشانند تا هر عملیاتِ تغییردهنده‌ی موجودی که
// توسط ادمین یا سرویس انجام می‌شود، به کاربرِ متأثر هم اطلاع بدهد (مثل مسیر
// deposit/transfer). اعلان‌ها best-effort هستند و در صورت خطا فقط log می‌شوند.

// SettleWithdraw یک برداشت را نهایی می‌کند (کسر موجودی) و به کاربر اطلاع می‌دهد.
func (s *Service) SettleWithdraw(ctx context.Context, id uuid.UUID, txHash string) error {
	req, _ := s.store.GetWithdraw(ctx, id) // پیش از تغییر وضعیت، برای مبلغ/کیف پول
	if err := s.store.CompleteWithdraw(ctx, id, txHash); err != nil {
		return err
	}
	s.notifyWithdraw(ctx, req, i18n.KNotifyWithdrawDone, txHash)
	return nil
}

// RejectWithdraw یک برداشت را رد می‌کند (frozen آزاد/بازگشت) و به کاربر اطلاع می‌دهد.
func (s *Service) RejectWithdraw(ctx context.Context, id uuid.UUID, reason string) error {
	req, _ := s.store.GetWithdraw(ctx, id)
	if err := s.store.RejectWithdraw(ctx, id, reason); err != nil {
		return err
	}
	s.notifyWithdraw(ctx, req, i18n.KNotifyWithdrawRejected, reason)
	return nil
}

// notifyWithdraw اعلان برداشت (تأیید یا رد) را به صاحب کیف پول می‌فرستد.
func (s *Service) notifyWithdraw(ctx context.Context, req *store.WithdrawRequest, key, arg string) {
	if req == nil || s.notify == nil {
		return
	}
	w, err := s.store.GetWalletByID(ctx, req.WalletID)
	if err != nil || w == nil {
		return
	}
	msg := i18n.T(i18n.Normalize(w.Lang), key, fmtTONStr(NanoToTON(req.Amount)), arg)
	if err := s.notify.SendHTML(ctx, w.TelegramID, msg); err != nil {
		s.log.Error("notify withdraw failed", ports.F("err", err), ports.F("key", key))
	}
}

// Credit اعتبار به کاربر اضافه می‌کند و فقط در صورت ایجاد creditِ تازه (نه retryِ
// idempotent) به او اطلاع می‌دهد. این تنها مسیرِ افزودن اعتبار در ربات/سرویس‌هاست.
func (s *Service) Credit(ctx context.Context, telegramID int64, amountNano int64, serviceID, ref, desc, metadata string) (*store.Transaction, error) {
	w, err := s.GetOrCreate(ctx, telegramID)
	if err != nil {
		return nil, err
	}
	tx, created, err := s.store.AddCredit(ctx, w.ID, amountNano, serviceID, ref, desc, metadata)
	if err != nil {
		return nil, err
	}
	if created && s.notify != nil {
		newTotal := NanoToTON(amountNano)
		if updated, _ := s.store.GetWalletByID(ctx, w.ID); updated != nil {
			newTotal = updated.TotalTON()
		}
		msg := i18n.T(i18n.Normalize(w.Lang), i18n.KNotifyCreditAdded, fmtTONStr(NanoToTON(amountNano)), fmtTONStr(newTotal))
		if err := s.notify.SendHTML(ctx, telegramID, msg); err != nil {
			s.log.Error("notify credit failed", ports.F("err", err))
		}
	}
	return tx, nil
}
