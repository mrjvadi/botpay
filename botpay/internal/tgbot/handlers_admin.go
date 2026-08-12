package tgbot

import (
	"context"
	"strconv"
	"strings"

	"github.com/google/uuid"
	tele "gopkg.in/telebot.v4"

	"github.com/mrjvadi/botpay/internal/i18n"
	"github.com/mrjvadi/botpay/internal/store"
	"github.com/mrjvadi/botpay/internal/wallet"
)

// ── ورود به پنل ──

// onAdmin (دستور /admin یا دکمه‌ی reply) داشبورد را نمایش می‌دهد.
func (h *Handler) onAdmin(c tele.Context) error {
	if !h.isAdmin(c) {
		return nil
	}
	return h.viewDashboard(c, false)
}

// onAdminCallback همه‌ی callbackهای پنل ادمین را مسیریابی می‌کند (فقط مالک).
func (h *Handler) onAdminCallback(ctx context.Context, c tele.Context, sub string, parts []string) error {
	if !h.isAdmin(c) {
		return c.Respond(&tele.CallbackResponse{Text: i18n.T(h.langOf(ctx, c), i18n.KNotAdmin)})
	}
	switch sub {
	case "home":
		return h.viewDashboard(c, true)
	case "withdraws":
		page := 0
		if len(parts) > 2 {
			page, _ = strconv.Atoi(parts[2])
		}
		return h.viewWithdrawals(c, page, true)
	case "wd":
		if len(parts) > 2 {
			return h.viewWithdrawDetail(c, parts[2], true)
		}
	case "approve":
		if len(parts) > 2 {
			return h.startApprove(ctx, c, parts[2])
		}
	case "reject":
		if len(parts) > 2 {
			return h.startReject(ctx, c, parts[2])
		}
	case "credit":
		return h.startCredit(ctx, c)
	case "credit_ok":
		return h.applyCredit(ctx, c)
	case "lookup":
		return h.startLookup(ctx, c)
	case "chain":
		return h.viewChain(c)
	case "cancel":
		h.states.del(c.Sender().ID)
		return h.viewDashboard(c, true)
	}
	return nil
}

// ── داشبورد ──

func (h *Handler) viewDashboard(c tele.Context, edit bool) error {
	ctx := context.Background()
	lang := h.langOf(ctx, c)
	stats, _ := h.st.GetStats(ctx)
	totalBal, _ := h.st.SumWalletBalances(ctx)
	frozen, _ := h.st.SumFrozen(ctx)
	if stats == nil {
		stats = &store.Stats{}
	}
	text := i18n.T(lang, i18n.KAdminDashboard,
		stats.TotalWallets, fmtTON(stats.TotalDeposits), fmtTON(stats.TotalPayments),
		stats.PendingWithdraw, fmtNano(totalBal), fmtNano(frozen))
	return h.render(c, edit, text, kbAdminMenu(lang))
}

// ── برداشت‌ها ──

func (h *Handler) viewWithdrawals(c tele.Context, page int, edit bool) error {
	ctx := context.Background()
	lang := h.langOf(ctx, c)
	reqs, _ := h.st.ListPendingWithdrawals(ctx)
	if len(reqs) == 0 {
		return h.render(c, edit, i18n.T(lang, i18n.KAdminNoWithdraws), kbAdminBack(lang))
	}
	totalPages := (len(reqs) + adminWithdrawPageSize - 1) / adminWithdrawPageSize
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}
	start := page * adminWithdrawPageSize
	end := start + adminWithdrawPageSize
	if end > len(reqs) {
		end = len(reqs)
	}
	pageReqs := reqs[start:end]

	var b strings.Builder
	b.WriteString(i18n.T(lang, i18n.KAdminWithdrawsTitle, len(reqs)))
	for _, r := range pageReqs {
		b.WriteString("\n\n")
		b.WriteString(i18n.T(lang, i18n.KAdminWithdrawItem,
			shortID(r.ID.String()), fmtNano(r.Amount), fmtNano(r.Fee),
			r.ToAddress, r.CreatedAt.Format("01/02 15:04")))
	}
	return h.render(c, edit, b.String(), kbAdminWithdrawList(lang, pageReqs, page, totalPages))
}

func (h *Handler) viewWithdrawDetail(c tele.Context, idStr string, edit bool) error {
	ctx := context.Background()
	lang := h.langOf(ctx, c)
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.Respond(&tele.CallbackResponse{Text: i18n.T(lang, i18n.KAdminBadID)})
	}
	req := h.findPendingWithdraw(ctx, id)
	if req == nil {
		return h.render(c, edit, i18n.T(lang, i18n.KAdminNoWithdraws), kbAdminBack(lang))
	}
	note := req.Note
	if note == "" {
		note = "—"
	}
	text := i18n.T(lang, i18n.KAdminWithdrawDetail,
		req.ID, fmtNano(req.Amount), fmtNano(req.Fee), fmtNano(req.Amount),
		req.ToAddress, req.CreatedAt.Format("2006/01/02 15:04"), note)
	return h.render(c, edit, text, kbAdminWithdrawDetail(lang, req.ID.String()))
}

// findPendingWithdraw یک برداشت منتظر را با شناسه پیدا می‌کند (از فهرست منتظرها).
func (h *Handler) findPendingWithdraw(ctx context.Context, id uuid.UUID) *store.WithdrawRequest {
	reqs, _ := h.st.ListPendingWithdrawals(ctx)
	for i := range reqs {
		if reqs[i].ID == id {
			return &reqs[i]
		}
	}
	return nil
}

// ── تأیید/رد برداشت (گفتگو) ──

func (h *Handler) startApprove(ctx context.Context, c tele.Context, id string) error {
	lang := h.langOf(ctx, c)
	if _, err := uuid.Parse(id); err != nil {
		return c.Respond(&tele.CallbackResponse{Text: i18n.T(lang, i18n.KAdminBadID)})
	}
	h.states.set(c.Sender().ID, convState{kind: kindAdminApprove, arg: id})
	return c.Send(i18n.T(lang, i18n.KAdminAskTxHash, id), tele.ModeHTML, kbCancelOnly(lang))
}

func (h *Handler) startReject(ctx context.Context, c tele.Context, id string) error {
	lang := h.langOf(ctx, c)
	if _, err := uuid.Parse(id); err != nil {
		return c.Respond(&tele.CallbackResponse{Text: i18n.T(lang, i18n.KAdminBadID)})
	}
	h.states.set(c.Sender().ID, convState{kind: kindAdminReject, arg: id})
	return c.Send(i18n.T(lang, i18n.KAdminAskReason, id), tele.ModeHTML, kbCancelOnly(lang))
}

func (h *Handler) handleAdminApprove(ctx context.Context, c tele.Context, lang i18n.Lang, st convState, text string) error {
	uid := c.Sender().ID
	if isCancel(text) {
		h.states.del(uid)
		return c.Send(i18n.T(lang, i18n.KCancelled), h.mainKB(c, lang))
	}
	id, err := uuid.Parse(st.arg)
	if err != nil {
		h.states.del(uid)
		return c.Send(i18n.T(lang, i18n.KAdminBadID), h.mainKB(c, lang))
	}
	h.states.del(uid)
	// از مسیر سرویس تا کاربرِ برداشت‌کننده هم اعلان بگیرد.
	if err := h.wallet.SettleWithdraw(ctx, id, strings.TrimSpace(text)); err != nil {
		return c.Send(i18n.T(lang, i18n.KAdminError, err.Error()), h.mainKB(c, lang))
	}
	return c.Send(i18n.T(lang, i18n.KAdminApproved, st.arg), tele.ModeHTML, h.mainKB(c, lang))
}

func (h *Handler) handleAdminReject(ctx context.Context, c tele.Context, lang i18n.Lang, st convState, text string) error {
	uid := c.Sender().ID
	if isCancel(text) {
		h.states.del(uid)
		return c.Send(i18n.T(lang, i18n.KCancelled), h.mainKB(c, lang))
	}
	id, err := uuid.Parse(st.arg)
	if err != nil {
		h.states.del(uid)
		return c.Send(i18n.T(lang, i18n.KAdminBadID), h.mainKB(c, lang))
	}
	h.states.del(uid)
	if err := h.wallet.RejectWithdraw(ctx, id, strings.TrimSpace(text)); err != nil {
		return c.Send(i18n.T(lang, i18n.KAdminError, err.Error()), h.mainKB(c, lang))
	}
	return c.Send(i18n.T(lang, i18n.KAdminRejected, st.arg), tele.ModeHTML, h.mainKB(c, lang))
}

// ── افزودن اعتبار (userID → amount → confirm) ──

func (h *Handler) startCredit(ctx context.Context, c tele.Context) error {
	lang := h.langOf(ctx, c)
	h.states.set(c.Sender().ID, convState{kind: kindAdminCredit, step: "user_id"})
	return c.Send(i18n.T(lang, i18n.KAdminAskCreditUserID), tele.ModeHTML, kbCancelOnly(lang))
}

func (h *Handler) handleAdminCredit(ctx context.Context, c tele.Context, lang i18n.Lang, st convState, text string) error {
	uid := c.Sender().ID
	if isCancel(text) {
		h.states.del(uid)
		return c.Send(i18n.T(lang, i18n.KCancelled), h.mainKB(c, lang))
	}
	switch st.step {
	case "user_id":
		toID, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil || toID == 0 {
			return c.Send(i18n.T(lang, i18n.KAdminBadParam), kbCancelOnly(lang))
		}
		st.toID = toID
		st.step = "amount"
		h.states.set(uid, st)
		return c.Send(i18n.T(lang, i18n.KAdminAskCreditAmount, toID), tele.ModeHTML, kbCancelOnly(lang))
	case "amount":
		amt, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil || amt <= 0 {
			return c.Send(i18n.T(lang, i18n.KAdminBadParam), kbCancelOnly(lang))
		}
		st.amountNano = wallet.TONToNano(amt)
		st.step = "confirm"
		h.states.set(uid, st)
		return c.Send(
			i18n.T(lang, i18n.KAdminCreditConfirm, fmtNano(st.amountNano), st.toID),
			tele.ModeHTML, kbConfirm(lang, "adm:credit_ok", "adm:cancel"),
		)
	}
	return nil
}

func (h *Handler) applyCredit(ctx context.Context, c tele.Context) error {
	uid := c.Sender().ID
	lang := h.langOf(ctx, c)
	st, ok := h.states.get(uid)
	if !ok || st.kind != kindAdminCredit || st.step != "confirm" {
		return nil
	}
	h.states.del(uid)
	// از مسیر سرویس تا کاربر هم اعلان اعتبار بگیرد.
	if _, err := h.wallet.Credit(ctx, st.toID, st.amountNano, "botpay-admin", "manual:"+uuid.NewString(), "", ""); err != nil {
		_ = c.Edit(i18n.T(lang, i18n.KAdminError, err.Error()), tele.ModeHTML)
		return c.Send(i18n.T(lang, i18n.KHomeHint), h.mainKB(c, lang))
	}
	_ = c.Edit(i18n.T(lang, i18n.KAdminCreditAdded, fmtNano(st.amountNano), st.toID), tele.ModeHTML)
	return c.Send(i18n.T(lang, i18n.KHomeHint), h.mainKB(c, lang))
}

// ── جستجوی کاربر ──

func (h *Handler) startLookup(ctx context.Context, c tele.Context) error {
	lang := h.langOf(ctx, c)
	h.states.set(c.Sender().ID, convState{kind: kindAdminLookup})
	return c.Send(i18n.T(lang, i18n.KAdminAskLookup), tele.ModeHTML, kbCancelOnly(lang))
}

func (h *Handler) handleAdminLookup(ctx context.Context, c tele.Context, lang i18n.Lang, text string) error {
	uid := c.Sender().ID
	if isCancel(text) {
		h.states.del(uid)
		return c.Send(i18n.T(lang, i18n.KCancelled), h.mainKB(c, lang))
	}
	h.states.del(uid)
	text = strings.TrimSpace(text)

	var w *store.Wallet
	if id, err := strconv.ParseInt(text, 10, 64); err == nil {
		w, _ = h.st.GetWallet(ctx, id)
	} else {
		w, _ = h.st.GetWalletByPayHandle(ctx, text)
	}
	if w == nil {
		return c.Send(i18n.T(lang, i18n.KAdminUserNotFound), h.mainKB(c, lang))
	}

	var b strings.Builder
	b.WriteString(i18n.T(lang, i18n.KAdminUserCard,
		w.TelegramID, fmtTON(w.BalanceTON()), fmtTON(w.CreditTON()), fmtTON(w.TotalTON()),
		fmtNano(w.Frozen), w.TONAddress, w.PayHandle))
	txs, _ := h.st.ListTransactions(ctx, w.ID, 5)
	if len(txs) > 0 {
		b.WriteString(i18n.T(lang, i18n.KAdminUserRecent))
		for _, tx := range txs {
			b.WriteString("\n")
			b.WriteString(txLine(lang, tx))
		}
	}
	return c.Send(b.String(), tele.ModeHTML, h.mainKB(c, lang))
}

// ── سلامت زنجیره‌ی هش ──

func (h *Handler) viewChain(c tele.Context) error {
	ctx := context.Background()
	lang := h.langOf(ctx, c)
	_ = c.Edit(i18n.T(lang, i18n.KAdminChainChecking), tele.ModeHTML)
	res, err := h.st.VerifyChain(ctx)
	if err != nil {
		return h.render(c, true, i18n.T(lang, i18n.KAdminError, err.Error()), kbAdminBack(lang))
	}
	if res.Valid {
		return h.render(c, true, i18n.T(lang, i18n.KAdminChainOK, res.TotalBlocks), kbAdminBack(lang))
	}
	return h.render(c, true, i18n.T(lang, i18n.KAdminChainBroken, res.BrokenAtSeq, res.Reason), kbAdminBack(lang))
}
