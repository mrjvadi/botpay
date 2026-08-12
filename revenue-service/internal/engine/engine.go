// Package engine منطق اصلی Revenue Service.
// هر Earning رو می‌گیره، rule رو اعمال می‌کنه، و از طریق botpay پرداخت می‌کنه.
package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/mrjvadi/botpay/revenue-service/internal/store"
	"github.com/mrjvadi/botpay/shared/pkg/ports"
)

// PayClient interface برای ارتباط با botpay.
type PayClient interface {
	Deduct(ctx context.Context, telegramID int64, amountTON float64, ref, desc string) (string, error)
	AddCredit(ctx context.Context, telegramID int64, amountTON float64, ref, desc string) (string, error)
}

// Engine پردازش و تقسیم درآمد.
type Engine struct {
	store              *store.Store
	pay                PayClient
	log                ports.Logger
	retryInterval      time.Duration
	platformTelegramID int64
	nc                 natsPublisher
}

// natsPublisher interface برای publish به NATS.
type natsPublisher interface {
	PublishCore(subject string, payload any) error
}

func New(st *store.Store, pay PayClient, log ports.Logger) *Engine {
	return &Engine{
		store:         st,
		pay:           pay,
		log:           log,
		retryInterval: 30 * time.Second,
	}
}

// ── Process ────────────────────────────────────────────────

// ProcessEarning یک Earning را پردازش و تقسیم می‌کند.
func (e *Engine) ProcessEarning(ctx context.Context, earning *store.Earning) error {
	// ── ۰. اعتبارسنجی مبلغ ───────────────────────────────────
	if earning.TotalNano <= 0 {
		e.store.MarkFailed(ctx, earning.ID, "invalid total_nano: must be > 0")
		return fmt.Errorf("invalid total_nano: %d (must be > 0)", earning.TotalNano)
	}

	e.log.Info("processing earning",
		ports.F("id", earning.ID),
		ports.F("type", earning.Type),
		ports.F("total_ton", float64(earning.TotalNano)/1e9))

	// ── ۱. دریافت قانون ──────────────────────────────────────
	rule, err := e.store.GetRule(ctx, earning.Type)
	if err != nil {
		return fmt.Errorf("get rule: %w", err)
	}
	if rule == nil {
		return fmt.Errorf("no rule for type: %s", earning.Type)
	}

	// ── ۲. محاسبه تقسیم ──────────────────────────────────────
	ownerNano := int64(float64(earning.TotalNano) * rule.OwnerPercent / 100)
	platformNano := earning.TotalNano - ownerNano

	e.log.Info("revenue split",
		ports.F("total", earning.TotalNano),
		ports.F("owner_pct", rule.OwnerPercent),
		ports.F("owner_nano", ownerNano),
		ports.F("platform_nano", platformNano))

	// ── ۳. claim اتمیک؛ فقط برنده اجازه‌ی payout دارد ─────────
	claimed, err := e.store.MarkProcessing(ctx, earning.ID)
	if err != nil {
		return fmt.Errorf("claim earning: %w", err)
	}
	if !claimed {
		return nil
	}

	// ── ۴. پرداخت به صاحب ────────────────────────────────────
	var ownerTxID string
	if ownerNano > 0 && earning.OwnerTelegramID != 0 {
		ownerTON := float64(ownerNano) / 1e9
		desc := fmt.Sprintf("درآمد %s — %s", earning.Type, earning.Description)
		txID, err := e.pay.AddCredit(ctx, earning.OwnerTelegramID, ownerTON, "earning:"+earning.ID.String()+":owner", desc)
		if err != nil {
			e.store.MarkFailed(ctx, earning.ID, "owner payment failed: "+err.Error())
			return fmt.Errorf("owner payment: %w", err)
		}
		ownerTxID = txID
		e.log.Info("owner paid",
			ports.F("telegram_id", earning.OwnerTelegramID),
			ports.F("amount_ton", ownerTON),
			ports.F("tx", txID))
	}

	// ── ۵. پرداخت به پلتفرم ──────────────────────────────────
	var platformTxID string
	if platformNano > 0 {
		platformWallet, _ := e.store.GetPlatformWallet(ctx)
		if platformWallet != nil {
			platformTON := float64(platformNano) / 1e9
			desc := fmt.Sprintf("platform commission — %s", earning.Type)
			txID, err := e.pay.AddCredit(ctx, platformWallet.TelegramID, platformTON, "earning:"+earning.ID.String()+":platform", desc)
			if err != nil {
				// platform payment fail = soft error (owner قبلاً پرداخت شده)
				e.log.Error("platform payment failed",
					ports.F("err", err))
			} else {
				platformTxID = txID
			}
		}
	}

	// ── ۶. ثبت نتیجه ─────────────────────────────────────────
	return e.store.MarkDone(ctx, earning.ID, ownerTxID, platformTxID, ownerNano, platformNano)
}

// ── Batch processor ────────────────────────────────────────

// RunWorker یک worker loop که pending earnings رو پردازش می‌کند.
func (e *Engine) RunWorker(ctx context.Context) {
	e.log.Info("revenue worker started")
	ticker := time.NewTicker(e.retryInterval)
	defer ticker.Stop()

	// اول بار فوری
	e.processBatch(ctx)

	for {
		select {
		case <-ctx.Done():
			e.log.Info("revenue worker stopped")
			return
		case <-ticker.C:
			e.processBatch(ctx)
		}
	}
}

func (e *Engine) processBatch(ctx context.Context) {
	earnings, err := e.store.ListPendingEarnings(ctx, 50)
	if err != nil {
		e.log.Error("list pending earnings", ports.F("err", err))
		return
	}
	for _, earning := range earnings {
		earningCopy := earning
		if err := e.ProcessEarning(ctx, &earningCopy); err != nil {
			e.log.Error("process earning failed",
				ports.F("id", earning.ID), ports.F("err", err))
		}
	}
}

// ── Create Earning ─────────────────────────────────────────

// CreateAndProcess یک Earning می‌سازد و فوری پردازش می‌کند.
func (e *Engine) CreateAndProcess(ctx context.Context,
	revType store.RevenueType,
	ownerTelegramID int64,
	totalNano int64,
	botID, refID, desc string,
) error {
	if totalNano <= 0 {
		return fmt.Errorf("invalid total_nano: %d (must be > 0)", totalNano)
	}

	// ── idempotency: اگر RefID قبلاً ثبت شده، همان رکورد را برگردان ──
	if refID != "" {
		existing, err := e.store.FindEarningByRefID(ctx, refID)
		if err != nil {
			return fmt.Errorf("check existing earning: %w", err)
		}
		if existing != nil {
			e.log.Info("duplicate earning skipped",
				ports.F("ref_id", refID), ports.F("existing_id", existing.ID))
			return nil
		}
	}

	earning := &store.Earning{
		Type:            revType,
		TotalNano:       totalNano,
		OwnerTelegramID: ownerTelegramID,
		BotID:           botID,
		RefID:           refID,
		Description:     desc,
		Status:          store.EarningPending,
	}
	if err := e.store.CreateEarning(ctx, earning); err != nil {
		return fmt.Errorf("create earning: %w", err)
	}
	return e.ProcessEarning(ctx, earning)
}

func (e *Engine) SetPlatformWallet(telegramID int64) {
	e.platformTelegramID = telegramID
}

func (e *Engine) SetNC(nc natsPublisher) {
	e.nc = nc
}

// ProcessGroupRevenue درآمد گروه را با تقسیم ۵۰/۴۰/۱۰ پردازش می‌کند.
// ۴۰٪ به NATS فرستاده می‌شود تا community-service بین اعضا توزیع کند.
func (e *Engine) ProcessGroupRevenue(ctx context.Context, earning *store.Earning) error {
	ownerNano := earning.TotalNano * 50 / 100
	memberPoolNano := earning.TotalNano * 40 / 100
	platformNano := earning.TotalNano - ownerNano - memberPoolNano

	// پرداخت به owner
	if ownerNano > 0 && earning.OwnerTelegramID != 0 {
		e.pay.AddCredit(ctx, earning.OwnerTelegramID,
			float64(ownerNano)/1e9, "earning:"+earning.ID.String()+":group-owner", "درآمد گروه — سهم owner")
	}

	// ارسال member pool به community-service
	if memberPoolNano > 0 {
		e.nc.PublishCore("community.member.pool", map[string]any{
			"community_id": earning.BotID,
			"pool_nano":    memberPoolNano,
			"ref_id":       earning.ID,
		})
	}

	// پرداخت به platform
	if platformNano > 0 && e.platformTelegramID != 0 {
		e.pay.AddCredit(ctx, e.platformTelegramID,
			float64(platformNano)/1e9, "earning:"+earning.ID.String()+":group-platform", "کمیسیون پلتفرم")
	}

	return e.store.MarkDone(ctx, earning.ID, "", "", 0, 0)
}
