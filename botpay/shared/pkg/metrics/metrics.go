// Package metrics — Prometheus instrumentation برای همه سرویس‌ها.
package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ── متریک‌های مشترک ──────────────────────────────────────

var (
	// RequestDuration زمان پاسخ HTTP request ها.
	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "creatorbot_http_request_duration_seconds",
		Help:    "مدت زمان HTTP request ها",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
	}, []string{"service", "method", "path", "status"})

	// NATSMessagesTotal تعداد پیام‌های NATS.
	NATSMessagesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "creatorbot_nats_messages_total",
		Help: "تعداد کل پیام‌های NATS",
	}, []string{"service", "subject", "direction"})

	// ActiveInstances تعداد instance های فعال.
	ActiveInstances = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "creatorbot_active_instances_total",
		Help: "تعداد instance های در حال اجرا",
	})

	// DeployTotal تعداد deploy ها.
	DeployTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "creatorbot_deploy_total",
		Help: "تعداد کل deploy ها",
	}, []string{"service_type", "status"})

	// PlanPurchaseTotal تعداد خرید پلن.
	PlanPurchaseTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "creatorbot_plan_purchase_total",
		Help: "تعداد خرید پلن",
	}, []string{"plan_name", "status"})

	// WalletTransactionTotal تعداد تراکنش‌های کیف پول.
	WalletTransactionTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "creatorbot_wallet_transaction_total",
		Help: "تعداد تراکنش‌های کیف پول",
	}, []string{"type", "status"})

	// FraudScoreHistogram توزیع امتیاز fraud.
	FraudScoreHistogram = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "creatorbot_fraud_score_distribution",
		Help:    "توزیع امتیاز fraud کاربران",
		Buckets: []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100},
	})

	// RevenueTotal درآمد توزیع‌شده.
	RevenueTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "creatorbot_revenue_distributed_ton",
		Help: "کل درآمد توزیع‌شده به TON",
	}, []string{"type"})

	// BotManagerActions تعداد action های botmanager.
	BotManagerActions = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "creatorbot_botmanager_actions_total",
		Help: "تعداد action های botmanager",
	}, []string{"action", "status"})

	// DBQueryDuration زمان query های DB.
	DBQueryDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "creatorbot_db_query_duration_seconds",
		Help:    "مدت زمان query های دیتابیس",
		Buckets: []float64{.001, .005, .01, .05, .1, .5, 1},
	}, []string{"service", "operation"})
)

// ── Server ─────────────────────────────────────────────────

// ServeMetrics یک HTTP server برای Prometheus scrape شروع می‌کند.
func ServeMetrics(addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	})
	go http.ListenAndServe(addr, mux)
}

// ── Helper ها ──────────────────────────────────────────────

// TrackDuration زمان یک عملیات را اندازه‌گیری می‌کند.
func TrackDuration(hist *prometheus.HistogramVec, service, op string) func() {
	start := time.Now()
	return func() {
		hist.WithLabelValues(service, op).Observe(time.Since(start).Seconds())
	}
}

// IncDeploy یک deploy را ثبت می‌کند.
func IncDeploy(serviceType, status string) {
	DeployTotal.WithLabelValues(serviceType, status).Inc()
}

// IncPlanPurchase یک خرید پلن را ثبت می‌کند.
func IncPlanPurchase(planName, status string) {
	PlanPurchaseTotal.WithLabelValues(planName, status).Inc()
}

// IncWalletTx یک تراکنش کیف پول را ثبت می‌کند.
func IncWalletTx(txType, status string) {
	WalletTransactionTotal.WithLabelValues(txType, status).Inc()
}

// ObserveFraudScore امتیاز fraud را ثبت می‌کند.
func ObserveFraudScore(score float64) {
	FraudScoreHistogram.Observe(score)
}

// AddRevenue درآمد توزیع‌شده را ثبت می‌کند.
func AddRevenue(revType string, amountTON float64) {
	RevenueTotal.WithLabelValues(revType).Add(amountTON)
}
