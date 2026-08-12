// Package consensus چند worker مستقل با الگوریتم‌های رمزنگاری متفاوت را
// برای تأیید تراکنش‌ها مدیریت می‌کند.
//
// هر worker:
//   - یک local SQLite DB مستقل دارد
//   - با الگوریتم رمزنگاری خودش signature می‌زند
//   - record تراکنش را در DB خودش ذخیره می‌کند
//   - نتیجه را به Engine می‌فرستد
//
// Engine:
//   - منتظر می‌ماند تا ≥ threshold تعداد worker تأیید کنند
//   - اگه consensus نشد → تراکنش رد می‌شود
//   - timeout محافظت می‌کند
package consensus

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/mrjvadi/botpay/shared/pkg/ports"
)

// ── Transaction ────────────────────────────────────────────

// Tx تراکنشی که باید تأیید شود.
type Tx struct {
	ID          string // شناسه یکتا
	FromWallet  string // از کیف پول
	ToService   string // به سرویس
	AmountNano  int64  // مقدار (nano-TON)
	Description string
	Timestamp   time.Time
	// Metadata داده‌های اضافی
	Metadata map[string]string
}

// VoteResult نتیجه رأی یک worker.
type VoteResult struct {
	WorkerID  string
	Algorithm string
	Approved  bool
	Signature string // امضای دیجیتال worker روی Tx
	Reason    string // در صورت رد
	Timestamp time.Time
}

// ConsensusResult نتیجه نهایی پس از همه رأی‌ها.
type ConsensusResult struct {
	TxID      string
	Approved  bool
	Votes     []VoteResult
	YesCount  int
	NoCount   int
	Threshold int
	Duration  time.Duration
}

// ── Worker interface ───────────────────────────────────────

// Worker یک worker تأیید تراکنش.
type Worker interface {
	// ID شناسه یکتای worker.
	ID() string
	// Algorithm نام الگوریتم رمزنگاری.
	Algorithm() string
	// Verify تراکنش را بررسی و رأی می‌دهد.
	Verify(ctx context.Context, tx Tx) VoteResult
	// Close منابع را آزاد می‌کند.
	Close() error
}

// ── Engine ─────────────────────────────────────────────────

// Config تنظیمات Engine.
type Config struct {
	// Threshold حداقل رأی موافق برای تأیید (مثلاً 3 از 4)
	Threshold int
	// Timeout حداکثر زمان انتظار برای رأی همه worker ها
	Timeout time.Duration
	// DBDir دایرکتوری برای SQLite های worker ها
	DBDir string
}

// Engine مدیریت consensus بین worker ها.
type Engine struct {
	cfg     Config
	workers []Worker
	log     ports.Logger
	mu      sync.RWMutex
	stats   map[string]*WorkerStats
}

// WorkerStats آمار عملکرد یک worker.
type WorkerStats struct {
	TotalVerified  int64
	TotalApproved  int64
	TotalRejected  int64
	LastVerifiedAt time.Time
}

// NewEngine یک Engine جدید می‌سازد.
func NewEngine(cfg Config, log ports.Logger) *Engine {
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.Threshold == 0 {
		cfg.Threshold = 3 // از 4 worker
	}
	return &Engine{
		cfg:   cfg,
		log:   log,
		stats: map[string]*WorkerStats{},
	}
}

// AddWorker یک worker به Engine اضافه می‌کند.
func (e *Engine) AddWorker(w Worker) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.workers = append(e.workers, w)
	e.stats[w.ID()] = &WorkerStats{}
	e.log.Info("consensus worker registered",
		ports.F("id", w.ID()),
		ports.F("algo", w.Algorithm()))
}

// Verify تراکنش را به همه worker ها می‌فرستد و consensus می‌گیرد.
func (e *Engine) Verify(ctx context.Context, tx Tx) ConsensusResult {
	if tx.ID == "" {
		tx.ID = genTxID()
	}
	if tx.Timestamp.IsZero() {
		tx.Timestamp = time.Now()
	}

	start := time.Now()
	e.mu.RLock()
	workers := make([]Worker, len(e.workers))
	copy(workers, e.workers)
	e.mu.RUnlock()

	if len(workers) == 0 {
		return ConsensusResult{
			TxID:     tx.ID,
			Approved: false,
			Votes:    nil,
		}
	}

	// همه worker ها را به‌صورت موازی اجرا کن
	votes := make(chan VoteResult, len(workers))
	timeoutCtx, cancel := context.WithTimeout(ctx, e.cfg.Timeout)
	defer cancel()

	for _, w := range workers {
		w := w
		go func() {
			vote := w.Verify(timeoutCtx, tx)
			votes <- vote
		}()
	}

	// جمع‌آوری نتایج
	var allVotes []VoteResult
	yes, no := 0, 0

	for range workers {
		select {
		case vote := <-votes:
			allVotes = append(allVotes, vote)
			if vote.Approved {
				yes++
			} else {
				no++
			}

			// آپدیت آمار
			e.mu.Lock()
			if s, ok := e.stats[vote.WorkerID]; ok {
				s.TotalVerified++
				if vote.Approved {
					s.TotalApproved++
				} else {
					s.TotalRejected++
				}
				s.LastVerifiedAt = time.Now()
			}
			e.mu.Unlock()

		case <-timeoutCtx.Done():
			// worker timeout — رأی «نه» ضمنی
			no++
		}
	}

	approved := yes >= e.cfg.Threshold
	result := ConsensusResult{
		TxID:      tx.ID,
		Approved:  approved,
		Votes:     allVotes,
		YesCount:  yes,
		NoCount:   no,
		Threshold: e.cfg.Threshold,
		Duration:  time.Since(start),
	}

	e.log.Info("consensus result",
		ports.F("tx", tx.ID),
		ports.F("approved", approved),
		ports.F("yes", yes),
		ports.F("no", no),
		ports.F("duration", result.Duration.String()))

	return result
}

// Stats آمار همه worker ها را برمی‌گرداند.
func (e *Engine) Stats() map[string]*WorkerStats {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make(map[string]*WorkerStats, len(e.stats))
	for k, v := range e.stats {
		copy := *v
		result[k] = &copy
	}
	return result
}

// Close همه worker ها را می‌بندد.
func (e *Engine) Close() {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, w := range e.workers {
		w.Close()
	}
}

// WorkerCount تعداد worker های ثبت‌شده.
func (e *Engine) WorkerCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.workers)
}

// ── helpers ────────────────────────────────────────────────

func genTxID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "TX-" + hex.EncodeToString(b)
}

// TxPayload داده‌ای که worker ها روی آن امضا می‌زنند.
func TxPayload(tx Tx) string {
	return fmt.Sprintf("%s|%s|%s|%d|%d",
		tx.ID, tx.FromWallet, tx.ToService,
		tx.AmountNano, tx.Timestamp.UnixNano())
}
