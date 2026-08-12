package consensus

import (
	"context"
	"fmt"
	"time"

	"github.com/mrjvadi/botpay/shared/pkg/ports"
)

// Guard لایه‌ای بین wallet service و consensus engine.
// هر عملیات مالی قبل از اجرا از Guard رد می‌شود.
type Guard struct {
	engine *Engine
	log    ports.Logger
}

func NewGuard(engine *Engine, log ports.Logger) *Guard {
	return &Guard{engine: engine, log: log}
}

// CheckDeduct قبل از کسر موجودی، consensus می‌گیرد.
func (g *Guard) CheckDeduct(ctx context.Context, fromWallet string, telegramID int64, amountNano int64, serviceID, ref string) error {
	tx := Tx{
		FromWallet:  fmt.Sprintf("tg-%d", telegramID),
		ToService:   serviceID,
		AmountNano:  amountNano,
		Description: ref,
		Timestamp:   time.Now(),
		Metadata: map[string]string{
			"wallet":     fromWallet,
			"service_id": serviceID,
			"ref":        ref,
		},
	}

	result := g.engine.Verify(ctx, tx)
	if !result.Approved {
		reasons := g.collectReasons(result)
		g.log.Info("consensus rejected deduct",
			ports.F("tx", result.TxID),
			ports.F("yes", result.YesCount),
			ports.F("no", result.NoCount),
			ports.F("reasons", reasons))
		return fmt.Errorf("transaction rejected by consensus (%d/%d votes): %s",
			result.YesCount, result.Threshold, reasons)
	}

	g.log.Info("consensus approved deduct",
		ports.F("tx", result.TxID),
		ports.F("yes", result.YesCount),
		ports.F("duration", result.Duration.String()))
	return nil
}

// CheckDeposit واریز جدید را تأیید می‌کند.
func (g *Guard) CheckDeposit(ctx context.Context, toWallet string, telegramID int64, amountNano int64, txHash string) error {
	// اگه txHash خالیه → consensus را skip کن
	if txHash == "" {
		g.log.Info("CheckDeposit: no txHash, skipping consensus")
		return nil
	}
	tx := Tx{
		ID:          "deposit-" + txHash,
		FromWallet:  txHash,
		ToService:   fmt.Sprintf("wallet-tg-%d", telegramID),
		AmountNano:  amountNano,
		Description: "blockchain_deposit",
		Timestamp:   time.Now(),
		Metadata: map[string]string{
			"tx_hash":   txHash,
			"to_wallet": toWallet,
		},
	}

	result := g.engine.Verify(ctx, tx)
	if !result.Approved {
		reasons := g.collectReasons(result)
		g.log.Info("consensus rejected deposit",
			ports.F("tx_hash", txHash),
			ports.F("reasons", reasons))
		return fmt.Errorf("deposit rejected by consensus: %s", reasons)
	}

	return nil
}

// CheckWithdraw برداشت را بررسی می‌کند — سخت‌ترین بررسی.
func (g *Guard) CheckWithdraw(ctx context.Context, fromWallet string, telegramID int64, amountNano int64, toAddress string) error {
	tx := Tx{
		FromWallet:  fmt.Sprintf("tg-%d-withdraw", telegramID),
		ToService:   toAddress,
		AmountNano:  amountNano,
		Description: "withdrawal",
		Timestamp:   time.Now(),
		Metadata: map[string]string{
			"to_address": toAddress,
			"type":       "withdrawal",
		},
	}

	// برداشت باید همه worker ها تأیید کنند (threshold = len(workers))
	// موقتاً از engine پیش‌فرض استفاده می‌کنیم
	result := g.engine.Verify(ctx, tx)
	if !result.Approved {
		return fmt.Errorf("withdrawal rejected by consensus (%d/%d)",
			result.YesCount, result.Threshold)
	}

	// برای برداشت باید هیچ رأی مخالفی نباشد
	if result.NoCount > 0 {
		return fmt.Errorf("withdrawal requires unanimous consensus, %d votes against", result.NoCount)
	}

	return nil
}

// EngineStatus وضعیت engine را برمی‌گرداند.
func (g *Guard) EngineStatus() map[string]any {
	stats := g.engine.Stats()
	workerInfo := make([]map[string]any, 0, len(stats))
	for id, s := range stats {
		workerInfo = append(workerInfo, map[string]any{
			"id":        id,
			"total":     s.TotalVerified,
			"approved":  s.TotalApproved,
			"rejected":  s.TotalRejected,
			"last_seen": s.LastVerifiedAt.Format(time.RFC3339),
		})
	}
	return map[string]any{
		"workers":      workerInfo,
		"threshold":    g.engine.cfg.Threshold,
		"worker_count": g.engine.WorkerCount(),
	}
}

func (g *Guard) collectReasons(result ConsensusResult) string {
	var reasons []string
	for _, v := range result.Votes {
		if !v.Approved && v.Reason != "" {
			reasons = append(reasons, v.WorkerID+": "+v.Reason)
		}
	}
	if len(reasons) == 0 {
		return "unknown"
	}
	return joinStrs(reasons, "; ")
}

func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func joinStrs(ss []string, sep string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
