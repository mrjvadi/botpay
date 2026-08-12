// Package chainguard یکپارچگی زنجیره‌ی ledger را به‌صورت دوره‌ای پایش می‌کند.
//
// این لایه‌ی «مواظبت» است: هر چند ثانیه کل زنجیره‌ی هش‌شده را verify می‌کند
// و اگر دستکاری تشخیص دهد (مثلاً کسی مستقیم در DB یک مبلغ را عوض کرده)،
// رویداد هشدار می‌فرستد و به ادمین اطلاع می‌دهد.
//
// worker های consensus نیز هنگام تأیید هر تراکنش، tip زنجیره را امضا می‌کنند
// تا یک شاهد مستقل از حالت درست زنجیره داشته باشند.
package chainguard

import (
	"context"
	"time"

	natsclient "github.com/mrjvadi/botpay/shared/pkg/adapters/nats"
	"github.com/mrjvadi/botpay/shared/pkg/ports"

	"github.com/mrjvadi/botpay/internal/store"
)

// Subject رویداد هشدار دستکاری زنجیره.
const SubjChainTampered = "pay.chain.tampered"

// TamperAlert جزئیات یک دستکاری کشف‌شده.
type TamperAlert struct {
	BrokenAtSeq int64  `json:"broken_at_seq"`
	Reason      string `json:"reason"`
	TotalBlocks int64  `json:"total_blocks"`
	DetectedAt  string `json:"detected_at"`
}

// Guard پایشگر یکپارچگی زنجیره.
type Guard struct {
	store    *store.Store
	nc       *natsclient.Client
	log      ports.Logger
	ownerID  int64
	interval time.Duration
	notify   func(int64, string) // اطلاع به ادمین (telegramID, message)

	healthy bool
}

func New(st *store.Store, nc *natsclient.Client, log ports.Logger, ownerID int64) *Guard {
	return &Guard{
		store:    st,
		nc:       nc,
		log:      log,
		ownerID:  ownerID,
		interval: 30 * time.Second,
		healthy:  true,
	}
}

// SetNotifier تابع اطلاع‌رسانی به ادمین (مثلاً ارسال پیام تلگرام) را تنظیم می‌کند.
func (g *Guard) SetNotifier(fn func(telegramID int64, msg string)) { g.notify = fn }

// SetInterval فاصله‌ی بررسی را تغییر می‌دهد.
func (g *Guard) SetInterval(d time.Duration) {
	if d > 0 {
		g.interval = d
	}
}

// Healthy آخرین وضعیت سلامت زنجیره.
func (g *Guard) Healthy() bool { return g.healthy }

// Start حلقه‌ی پایش دوره‌ای را اجرا می‌کند (تا زمانی که ctx لغو نشده).
func (g *Guard) Start(ctx context.Context) {
	g.log.Info("chainguard started", ports.F("interval", g.interval.String()))

	// یک بررسی فوری در شروع
	g.checkOnce(ctx)

	ticker := time.NewTicker(g.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			g.log.Info("chainguard stopped")
			return
		case <-ticker.C:
			g.checkOnce(ctx)
		}
	}
}

// CheckNow یک بررسی فوری انجام می‌دهد و نتیجه را برمی‌گرداند.
func (g *Guard) CheckNow(ctx context.Context) (store.ChainVerifyResult, error) {
	return g.store.VerifyChain(ctx)
}

func (g *Guard) checkOnce(ctx context.Context) {
	res, err := g.store.VerifyChain(ctx)
	if err != nil {
		g.log.Error("chainguard: verify failed", ports.F("err", err))
		return
	}

	if res.Valid {
		if !g.healthy {
			g.log.Info("chainguard: chain recovered", ports.F("blocks", res.TotalBlocks))
		}
		g.healthy = true
		return
	}

	// ── دستکاری کشف شد ──────────────────────────────────────
	g.healthy = false
	g.log.Error("chainguard: TAMPERING DETECTED",
		ports.F("broken_at_seq", res.BrokenAtSeq),
		ports.F("reason", res.Reason))

	// رویداد NATS برای سایر سرویس‌ها / apimanager
	if g.nc != nil {
		_ = g.nc.PublishCore(SubjChainTampered, TamperAlert{
			BrokenAtSeq: res.BrokenAtSeq,
			Reason:      res.Reason,
			TotalBlocks: res.TotalBlocks,
			DetectedAt:  time.Now().Format(time.RFC3339),
		})
	}

	// اطلاع به ادمین
	if g.notify != nil && g.ownerID != 0 {
		g.notify(g.ownerID,
			"🚨 هشدار امنیتی: دستکاری در زنجیره‌ی پرداخت‌ها کشف شد!\n"+
				"بلوک شماره: "+itoa(res.BrokenAtSeq)+"\n"+
				"دلیل: "+res.Reason+"\n\n"+
				"سیستم پرداخت نیاز به بررسی فوری دارد.")
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
