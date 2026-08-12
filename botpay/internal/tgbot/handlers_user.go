package tgbot

import (
	"context"
	"strings"

	tele "gopkg.in/telebot.v4"

	"github.com/mrjvadi/botpay/internal/i18n"
	"github.com/mrjvadi/botpay/internal/store"
	"github.com/mrjvadi/botpay/internal/wallet"
)

// render یک نمای inline را نمایش می‌دهد: در پاسخ به callback با Edit (جای‌گزینیِ
// پیام)، وگرنه با Send (پیام تازه). همیشه HTML.
func (h *Handler) render(c tele.Context, edit bool, text string, kb *tele.ReplyMarkup) error {
	if edit {
		if err := c.Edit(text, tele.ModeHTML, kb); err == nil {
			return nil
		}
		// اگر Edit ممکن نبود (مثلاً پیام قدیمی)، پیام تازه بفرست.
	}
	return c.Send(text, tele.ModeHTML, kb)
}

// ── Home ──

// onStart صفحه‌ی خوش‌آمد + کیبورد اصلی reply.
func (h *Handler) onStart(c tele.Context) error {
	ctx := context.Background()
	lang := h.langOf(ctx, c)

	w, err := h.wallet.GetOrCreate(ctx, c.Sender().ID)
	if err != nil {
		return c.Send(i18n.T(lang, i18n.KErrGeneric))
	}
	return c.Send(
		i18n.T(lang, i18n.KStart, c.Sender().FirstName,
			fmtTON(w.BalanceTON()), fmtTON(w.CreditTON()), fmtTON(w.TotalTON())),
		tele.ModeHTML, h.mainKB(c, lang),
	)
}

// ── Wallet ──

func (h *Handler) onWallet(c tele.Context) error { return h.viewWallet(c, false) }

func (h *Handler) viewWallet(c tele.Context, edit bool) error {
	ctx := context.Background()
	lang := h.langOf(ctx, c)
	w, err := h.wallet.GetOrCreate(ctx, c.Sender().ID)
	if err != nil {
		return c.Send(i18n.T(lang, i18n.KErrGeneric), h.mainKB(c, lang))
	}
	text := i18n.T(lang, i18n.KWallet,
		fmtTON(w.BalanceTON()), fmtTON(w.CreditTON()), fmtTON(w.TotalTON()),
		frozenLine(lang, w.Frozen), w.TONAddress, w.PayHandle)
	return h.render(c, edit, text, kbWallet(lang))
}

// ── Deposit ──

func (h *Handler) onDeposit(c tele.Context) error { return h.viewDeposit(c, false) }

func (h *Handler) viewDeposit(c tele.Context, edit bool) error {
	ctx := context.Background()
	lang := h.langOf(ctx, c)
	return h.render(c, edit, i18n.T(lang, i18n.KDepositMenu), kbDepositMenu(lang))
}

// makeInvoice یک فاکتور واریز با مبلغ مشخص (nano، ۰ = هر مبلغ) می‌سازد و نمایش می‌دهد.
func (h *Handler) makeInvoice(c tele.Context, amountNano int64, edit bool) error {
	ctx := context.Background()
	lang := h.langOf(ctx, c)
	code, payURL, err := h.wallet.DepositInstructions(ctx, c.Sender().ID, amountNano, "manual", "direct")
	if err != nil {
		return c.Send(i18n.T(lang, i18n.KDepositErr), h.mainKB(c, lang))
	}
	text := i18n.T(lang, i18n.KDepositBody, h.wallet.MasterAddress(), code)
	return h.render(c, edit, text, kbDepositInvoice(lang, payURL, code))
}

// onCheckDeposit وضعیت یک invoice واریز را بررسی می‌کند (callback).
func (h *Handler) onCheckDeposit(ctx context.Context, c tele.Context, code string) error {
	lang := h.langOf(ctx, c)
	if code == "" {
		return nil
	}
	inv, _ := h.st.FindInvoiceByCode(ctx, code)
	if inv == nil {
		return c.Edit(i18n.T(lang, i18n.KCheckPending), tele.ModeHTML)
	}
	if inv.Status == store.InvoicePaid {
		w, err := h.wallet.GetOrCreate(ctx, c.Sender().ID)
		newBalance := wallet.NanoToTON(inv.Amount)
		if err == nil && w != nil {
			newBalance = w.TotalTON()
		}
		return c.Edit(
			i18n.T(lang, i18n.KCheckConfirmed, fmtNano(inv.Amount), fmtTON(newBalance)),
			tele.ModeHTML,
		)
	}
	return c.Edit(i18n.T(lang, i18n.KCheckUnconfirmed), tele.ModeHTML)
}

// ── Withdraw (شروع گفتگو) ──

func (h *Handler) onWithdraw(c tele.Context) error {
	ctx := context.Background()
	lang := h.langOf(ctx, c)
	w, err := h.wallet.GetOrCreate(ctx, c.Sender().ID)
	if err != nil {
		return c.Send(i18n.T(lang, i18n.KErrGeneric), h.mainKB(c, lang))
	}
	minTON := wallet.NanoToTON(wallet.MinWithdrawNano)
	feeTON := wallet.NanoToTON(wallet.NetworkFeeNano)
	if w.TotalTON() < minTON+feeTON {
		return c.Send(i18n.T(lang, i18n.KWithdrawInsufficient, fmtTON(minTON), fmtTON(feeTON)), tele.ModeHTML, h.mainKB(c, lang))
	}
	h.states.set(c.Sender().ID, convState{kind: kindWithdraw, step: "addr"})
	return c.Send(
		i18n.T(lang, i18n.KWithdrawAskAddr, fmtTON(w.TotalTON()), fmtTON(feeTON)),
		tele.ModeHTML, kbCancelOnly(lang),
	)
}

// ── Transfer (شروع گفتگو) ──

func (h *Handler) onTransfer(c tele.Context) error {
	ctx := context.Background()
	lang := h.langOf(ctx, c)
	w, err := h.wallet.GetOrCreate(ctx, c.Sender().ID)
	if err != nil {
		return c.Send(i18n.T(lang, i18n.KErrGeneric), h.mainKB(c, lang))
	}
	if w.TotalTON() <= 0 {
		return c.Send(i18n.T(lang, i18n.KTransferInsufficient), h.mainKB(c, lang))
	}
	h.states.set(c.Sender().ID, convState{kind: kindTransfer, step: "recipient"})
	return c.Send(i18n.T(lang, i18n.KTransferAskID), tele.ModeHTML, kbCancelOnly(lang))
}

// ── History (صفحه‌بندی‌شده) ──

func (h *Handler) onHistoryCmd(c tele.Context) error { return h.viewHistory(c, 0, false) }

func (h *Handler) viewHistory(c tele.Context, page int, edit bool) error {
	ctx := context.Background()
	lang := h.langOf(ctx, c)
	w, err := h.wallet.GetOrCreate(ctx, c.Sender().ID)
	if err != nil {
		return c.Send(i18n.T(lang, i18n.KErrGeneric), h.mainKB(c, lang))
	}
	// حداکثر ۲۰۰ تراکنش اخیر را می‌گیریم و در حافظه صفحه‌بندی می‌کنیم.
	txs, err := h.st.ListTransactions(ctx, w.ID, 200)
	if err != nil || len(txs) == 0 {
		return h.render(c, edit, i18n.T(lang, i18n.KHistoryEmpty), kbWallet(lang))
	}
	totalPages := (len(txs) + historyPageSize - 1) / historyPageSize
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}
	start := page * historyPageSize
	end := start + historyPageSize
	if end > len(txs) {
		end = len(txs)
	}

	var b strings.Builder
	b.WriteString(i18n.T(lang, i18n.KHistoryTitle))
	b.WriteString("\n<i>")
	b.WriteString(i18n.T(lang, i18n.KHistoryPager, page+1, totalPages))
	b.WriteString("</i>\n")
	for _, tx := range txs[start:end] {
		b.WriteString("\n")
		b.WriteString(txLine(lang, tx))
		b.WriteString("\n")
	}
	return h.render(c, edit, b.String(), kbHistory(lang, page, totalPages))
}

// ── Help ──

func (h *Handler) onHelp(c tele.Context) error {
	ctx := context.Background()
	lang := h.langOf(ctx, c)
	return c.Send(i18n.T(lang, i18n.KHelp), tele.ModeHTML, h.mainKB(c, lang))
}
