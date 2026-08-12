// Package ton بلاکچین TON را رصد می‌کند.
package ton

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	natsclient "github.com/mrjvadi/botpay/shared/pkg/adapters/nats"
	"github.com/mrjvadi/botpay/shared/pkg/ports"
)

type Config struct {
	MasterAddress string
	APIKey        string
	Network       string
	PollInterval  time.Duration
}

type DepositEvent struct {
	TelegramID  int64   `json:"telegram_id"`
	WalletID    string  `json:"wallet_id"`
	AmountNano  int64   `json:"amount_nano"`
	AmountTON   float64 `json:"amount_ton"`
	TxHash      string  `json:"tx_hash"`
	FromAddr    string  `json:"from_addr"`
	InvoiceCode string  `json:"invoice_code,omitempty"`
	// داده‌ی کاملِ on-chain — تمام اطلاعاتی که از TON می‌گیریم ذخیره می‌شود.
	ToAddr  string `json:"to_addr,omitempty"`
	LT      int64  `json:"lt,omitempty"`    // logical time
	Utime   int64  `json:"utime,omitempty"` // unix time on-chain
	FeeNano int64  `json:"fee_nano,omitempty"`
}

type PaymentHandler func(ctx context.Context, event DepositEvent) error

type Watcher struct {
	cfg        Config
	handler    PaymentHandler
	nc         *natsclient.Client
	log        ports.Logger
	httpClient *http.Client
	seenTx     map[string]bool // جلوگیری از پردازش تکراری
}

func New(cfg Config, handler PaymentHandler, nc *natsclient.Client, log ports.Logger) *Watcher {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 15 * time.Second
	}
	return &Watcher{
		cfg:        cfg,
		handler:    handler,
		nc:         nc,
		log:        log,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		seenTx:     make(map[string]bool),
	}
}

func (w *Watcher) Run(ctx context.Context) {
	w.log.Info("TON watcher started",
		ports.F("address", w.cfg.MasterAddress),
		ports.F("network", w.cfg.Network),
		ports.F("interval", w.cfg.PollInterval))

	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	// اول بار فوری
	w.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			w.log.Info("TON watcher stopped")
			return
		case <-ticker.C:
			w.poll(ctx)
		}
	}
}

func (w *Watcher) poll(ctx context.Context) {
	txs, err := w.fetchTransactions(ctx)
	if err != nil {
		// LITE_SERVER_UNKNOWN: خطای گذرا — node هنوز sync نشده یا lt قدیمی
		// فقط warn بزن، crash نکن
		w.log.Info("TON poll skipped (transient)",
			ports.F("err", err.Error()[:min(80, len(err.Error()))]))
		return
	}

	newCount := 0
	for _, tx := range txs {
		// فقط incoming تراکنش‌های با مقدار
		if tx.InMsg.Source == "" || tx.InMsg.Value == "0" || tx.InMsg.Value == "" {
			continue
		}

		// جلوگیری از پردازش تکراری
		if w.seenTx[tx.Hash] {
			continue
		}
		w.seenTx[tx.Hash] = true

		// parse amount / fee / lt (همه در API رشته‌اند)
		var amountNano, feeNano, lt int64
		fmt.Sscanf(tx.InMsg.Value, "%d", &amountNano)
		fmt.Sscanf(tx.Fee, "%d", &feeNano)
		fmt.Sscanf(tx.TransactionID.LT, "%d", &lt)
		if amountNano <= 0 {
			continue
		}

		newCount++
		event := DepositEvent{
			AmountNano:  amountNano,
			AmountTON:   float64(amountNano) / 1e9,
			TxHash:      tx.Hash,
			FromAddr:    tx.InMsg.Source,
			ToAddr:      tx.InMsg.Destination,
			InvoiceCode: tx.InMsg.Message,
			LT:          lt,
			Utime:       tx.Utime,
			FeeNano:     feeNano,
		}

		w.log.Info("new TON transaction",
			ports.F("amount", event.AmountTON),
			ports.F("from", event.FromAddr),
			ports.F("comment", event.InvoiceCode))

		if err := w.handler(ctx, event); err != nil {
			w.log.Error("deposit handler failed",
				ports.F("tx", tx.Hash), ports.F("err", err))
		}
	}

	if newCount > 0 {
		w.log.Info("TON txs processed", ports.F("count", newCount))
	}
}

// ── toncenter API ──────────────────────────────────────────

type tonTx struct {
	Hash          string `json:"hash"`
	Utime         int64  `json:"utime"`
	Fee           string `json:"fee"` // string در API (nano)
	TransactionID struct {
		LT string `json:"lt"` // string در API
	} `json:"transaction_id"`
	InMsg struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
		Value       string `json:"value"` // string در API
		Message     string `json:"message"`
	} `json:"in_msg"`
}

func (w *Watcher) fetchTransactions(ctx context.Context) ([]tonTx, error) {
	base := "https://toncenter.com/api/v2"
	if w.cfg.Network == "testnet" {
		base = "https://testnet.toncenter.com/api/v2"
	}

	// archival=true برای دسترسی به تراکنش‌های قدیمی‌تر
	// بدون lt — از آخرین تراکنش‌ها شروع می‌کند
	url := fmt.Sprintf("%s/getTransactions?address=%s&limit=20&archival=true",
		base, w.cfg.MasterAddress)
	if w.cfg.APIKey != "" {
		url += "&api_key=" + w.cfg.APIKey
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("toncenter: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool    `json:"ok"`
		Result []tonTx `json:"result"`
		Error  string  `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("toncenter: %s", result.Error)
	}

	return result.Result, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
