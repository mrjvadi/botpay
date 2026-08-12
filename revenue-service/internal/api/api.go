// Package api REST API و NATS event handler برای Revenue Service.
//
// REST Endpoints:
//
//	POST /api/v1/revenue/earn    ← سرویس‌ها earning ایجاد می‌کنند
//	GET  /api/v1/revenue/rules   ← لیست قوانین
//	PUT  /api/v1/revenue/rules   ← ویرایش قانون (ادمین)
//	GET  /api/v1/revenue/stats   ← آمار
//	POST /api/v1/revenue/platform-wallet ← تنظیم wallet پلتفرم
//
// NATS Events:
//
//	earning.created → پردازش فوری
package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mrjvadi/botpay/revenue-service/internal/engine"
	"github.com/mrjvadi/botpay/revenue-service/internal/store"
	natsclient "github.com/mrjvadi/botpay/shared/pkg/adapters/nats"
	"github.com/mrjvadi/botpay/shared/pkg/ports"
)

// Config تنظیمات API.
type Config struct {
	AdminKey string
}

type Handler struct {
	engine *engine.Engine
	store  *store.Store
	nc     *natsclient.Client
	cfg    Config
	log    ports.Logger
}

func New(eng *engine.Engine, st *store.Store, nc *natsclient.Client, cfg Config, log ports.Logger) *Handler {
	return &Handler{engine: eng, store: st, nc: nc, cfg: cfg, log: log}
}

func (h *Handler) Register(r *gin.Engine) {
	api := r.Group("/api/v1/revenue")
	api.Use(h.authMiddleware())

	api.POST("/earn", h.createEarning)
	api.GET("/rules", h.listRules)
	api.PUT("/rules", h.updateRule)
	api.GET("/stats", h.getStats)
	api.POST("/platform-wallet", h.setPlatformWallet)
}

// RegisterNATSListeners به رویدادهای earning گوش می‌دهد.
func (h *Handler) RegisterNATSListeners(ctx context.Context) {
	// earning.created — پردازش فوری
	h.nc.QueueSubscribe("earning.created", "revenue-service", func(data []byte) {
		var msg EarningEvent
		if err := json.Unmarshal(data, &msg); err != nil {
			h.log.Error("earning.created decode", ports.F("err", err))
			return
		}
		if err := h.engine.CreateAndProcess(ctx,
			store.RevenueType(msg.Type),
			msg.OwnerTelegramID,
			msg.TotalNano,
			msg.BotID, msg.RefID, msg.Description,
		); err != nil {
			h.log.Error("earning.created process", ports.F("err", err))
		}
	})

	h.log.Info("NATS listeners registered", ports.F("subject", "earning.created"))
}

// ── Handlers ──────────────────────────────────────────────

// EarningEvent رویداد earning.created از NATS یا REST body.
type EarningEvent struct {
	Type            string `json:"type"             binding:"required"`
	OwnerTelegramID int64  `json:"owner_telegram_id" binding:"required"`
	TotalNano       int64  `json:"total_nano"        binding:"required"`
	BotID           string `json:"bot_id"`
	RefID           string `json:"ref_id"`
	Description     string `json:"description"`
}

// POST /earn
func (h *Handler) createEarning(c *gin.Context) {
	if !c.GetBool("is_admin") {
		fail(c, http.StatusForbidden, "admin only")
		return
	}

	var req EarningEvent
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.engine.CreateAndProcess(c.Request.Context(),
		store.RevenueType(req.Type),
		req.OwnerTelegramID,
		req.TotalNano,
		req.BotID, req.RefID, req.Description,
	); err != nil {
		h.log.Error("createEarning", ports.F("err", err))
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}

	ok(c, gin.H{"status": "processed"})
}

// GET /rules
func (h *Handler) listRules(c *gin.Context) {
	rules, err := h.store.ListRules(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, rules)
}

// PUT /rules
func (h *Handler) updateRule(c *gin.Context) {
	if !c.GetBool("is_admin") {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var rule store.RevenueRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if rule.OwnerPercent+rule.PlatformPercent != 100 {
		fail(c, http.StatusBadRequest, "owner_percent + platform_percent must equal 100")
		return
	}
	if err := h.store.UpsertRule(c.Request.Context(), &rule); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, rule)
}

// GET /stats
func (h *Handler) getStats(c *gin.Context) {
	stats, err := h.store.GetStats(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{
		"total_earnings":     stats.TotalEarnings,
		"total_owner_ton":    float64(stats.TotalOwnerNano) / 1e9,
		"total_platform_ton": float64(stats.PlatformNano) / 1e9,
		"pending_count":      stats.PendingCount,
	})
}

// POST /platform-wallet
func (h *Handler) setPlatformWallet(c *gin.Context) {
	if !c.GetBool("is_admin") {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var req struct {
		TelegramID int64  `json:"telegram_id" binding:"required"`
		Label      string `json:"label"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.SetPlatformWallet(c.Request.Context(), req.TelegramID, req.Label); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"telegram_id": req.TelegramID})
}

// ── Auth ───────────────────────────────────────────────────

func (h *Handler) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetHeader("X-API-Key")
		if key == "" {
			fail(c, http.StatusUnauthorized, "missing api key")
			return
		}
		if key == h.cfg.AdminKey {
			c.Set("is_admin", true)
		}
		c.Set("api_key", key)
		c.Next()
	}
}

func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": data})
}

func fail(c *gin.Context, code int, msg string) {
	c.AbortWithStatusJSON(code, gin.H{"ok": false, "message": msg})
}
