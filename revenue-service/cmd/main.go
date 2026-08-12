package main

import (
	"context"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/mrjvadi/botpay/revenue-service/internal/api"
	revengine "github.com/mrjvadi/botpay/revenue-service/internal/engine"
	"github.com/mrjvadi/botpay/revenue-service/internal/store"
	"github.com/mrjvadi/botpay/shared/natspayclient"
	natsclient "github.com/mrjvadi/botpay/shared/pkg/adapters/nats"
	"github.com/mrjvadi/botpay/shared/pkg/auth"
	"github.com/mrjvadi/botpay/shared/pkg/config"
	"github.com/mrjvadi/botpay/shared/pkg/logger"
	"github.com/mrjvadi/botpay/shared/pkg/metrics"
	"github.com/mrjvadi/botpay/shared/pkg/ports"
)

type Config struct {
	PostgresDSN        string `mapstructure:"POSTGRES_DSN"`
	NatsURL            string `mapstructure:"NATS_URL"`
	NatsUser           string `mapstructure:"NATS_USERNAME"`
	NatsPass           string `mapstructure:"NATS_PASSWORD"`
	Port               int    `mapstructure:"PORT"`
	AdminKey           string `mapstructure:"ADMIN_API_KEY"`
	PlatformTelegramID int64  `mapstructure:"PLATFORM_TELEGRAM_ID"`
	HmacSecret         string `mapstructure:"SERVICE_HMAC_SECRET"`
}

func main() {
	var cfg Config
	config.MustLoad(&cfg)
	log := logger.MustNew(false)

	if cfg.Port == 0 {
		cfg.Port = 8088
	}

	// ── PostgreSQL ─────────────────────────────────────────
	db, err := gorm.Open(postgres.Open(cfg.PostgresDSN))
	if err != nil {
		log.Fatal("postgres", ports.F("err", err))
	}
	if err := store.AutoMigrate(db); err != nil {
		log.Fatal("migrate", ports.F("err", err))
	}
	st := store.New(db)

	// Seed default rules اگه وجود ندارن
	ctx := context.Background()
	if err := st.SeedDefaultRules(ctx); err != nil {
		log.Fatal("seed rules", ports.F("err", err))
	}

	// ── NATS ──────────────────────────────────────────────
	nc, err := natsclient.New(natsclient.Config{
		URL:      cfg.NatsURL,
		Username: cfg.NatsUser,
		Password: cfg.NatsPass,
		Name:     "revenue-service",
	})
	if err != nil {
		log.Fatal("nats", ports.F("err", err))
	}
	defer nc.Close()
	log.AttachNATS(nc, "revenue-service")

	// ── botpay client (NATS) ──────────────────────────────
	pay := &natspayAdapter{
		nc: natspayclient.New(nc, nil, natspayclient.Config{
			ServiceID:  "revenue-service",
			ServiceKey: auth.ComputeServiceKey(cfg.HmacSecret, "revenue-service"),
			Timeout:    10 * time.Second,
		}),
	}

	// ── Revenue Engine ─────────────────────────────────────
	eng := revengine.New(st, pay, log)
	eng.SetPlatformWallet(cfg.PlatformTelegramID)
	eng.SetNC(nc)

	// ── API ────────────────────────────────────────────────
	apiHandler := api.New(eng, st, nc, api.Config{AdminKey: cfg.AdminKey}, log)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true, "service": "revenue-service"})
	})

	apiHandler.Register(r)

	// ── شروع ──────────────────────────────────────────────
	shutCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// NATS listeners
	apiHandler.RegisterNATSListeners(shutCtx)

	// Worker loop — پردازش pending earnings
	go eng.RunWorker(shutCtx)

	// HTTP server
	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{Addr: addr, Handler: r}

	go func() {
		metrics.ServeMetrics(":9092")
		log.Info("revenue-service started",
			ports.F("addr", addr),
			ports.F("nats", cfg.NatsURL))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("http server", ports.F("err", err))
		}
	}()

	<-shutCtx.Done()
	log.Info("shutting down...")

	timeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(timeout)
	log.Info("revenue-service stopped")
}

// ── botpay NATS adapter ────────────────────────────────────
// پیاده‌سازی engine.PayClient از طریق NATS (جایگزین HTTP قدیمی).

type natspayAdapter struct {
	nc *natspayclient.Client
}

func (a *natspayAdapter) AddCredit(ctx context.Context, telegramID int64, amountTON float64, ref, desc string) (string, error) {
	if err := a.nc.Credit(ctx, telegramID, amountTON, ref, `{"src":"revenue-service"}`); err != nil {
		return "", err
	}
	return ref, nil
}

func (a *natspayAdapter) Deduct(ctx context.Context, telegramID int64, amountTON float64, ref, desc string) (string, error) {
	if _, err := a.nc.Deduct(ctx, telegramID, amountTON, desc, ref); err != nil {
		return "", err
	}
	return "", nil
}
