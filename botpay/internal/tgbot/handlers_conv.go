package tgbot

import (
	"context"
	"errors"
	"strconv"
	"strings"

	tele "gopkg.in/telebot.v4"

	"github.com/mrjvadi/botpay/internal/i18n"
	"github.com/mrjvadi/botpay/internal/wallet"
)

// errBadRecipient گیرنده‌ی نامعتبر (مثلاً آیدی عددی ناموجّه).
var errBadRecipient = errors.New("bad recipient")

// onText ورودی متنی را مسیریابی می‌کند: ابتدا گفتگوهای فعال، سپس دکمه‌های منو.
func (h *Handler) onText(c tele.Context) error {
	ctx := context.Background()
	uid := c.Sender().ID
	text := strings.TrimSpace(c.Text())
	lang := h.langOf(ctx, c)

	if st, ok := h.states.get(uid); ok {
		switch st.kind {
		case kindWithdraw:
			return h.handleWithdrawState(ctx, c, lang, st, text)
		case kindTransfer:
			return h.handleTransferState(ctx, c, lang, st, text)
		case kindDepositCustom:
			return h.handleDepositCustom(ctx, c, lang, text)
		case kindAdminApprove:
			return h.handleAdminApprove(ctx, c, lang, st, text)
		case kindAdminReject:
			return h.handleAdminReject(ctx, c, lang, st, text)
		case kindAdminCredit:
			return h.handleAdminCredit(ctx, c, lang, st, text)
		case kindAdminLookup:
			return h.handleAdminLookup(ctx, c, lang, text)
		}
	}

	switch text {
	case i18n.T(lang, i18n.KBtnWallet):
		return h.onWallet(c)
	case i18n.T(lang, i18n.KBtnDeposit):
		return h.onDeposit(c)
	case i18n.T(lang, i18n.KBtnWithdraw):
		return h.onWithdraw(c)
	case i18n.T(lang, i18n.KBtnTransfer):
		return h.onTransfer(c)
	case i18n.T(lang, i18n.KBtnHistory):
		return h.onHistoryCmd(c)
	case i18n.T(lang, i18n.KBtnHelp):
		return h.onHelp(c)
	case i18n.T(lang, i18n.KBtnLanguage):
		return h.onLanguage(c)
	case i18n.T(lang, i18n.KBtnAdmin):
		return h.onAdmin(c)
	}
	return nil
}

// ── Deposit (مبلغ دلخواه) ──

func (h *Handler) handleDepositCustom(ctx context.Context, c tele.Context, lang i18n.Lang, text string) error {
	uid := c.Sender().ID
	if isCancel(text) {
		h.states.del(uid)
		return c.Send(i18n.T(lang, i18n.KCancelled), h.mainKB(c, lang))
	}
	amt, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || amt <= 0 {
		return c.Send(i18n.T(lang, i18n.KDepositBadAmount), kbCancelOnly(lang))
	}
	h.states.del(uid)
	return h.makeInvoice(c, wallet.TONToNano(amt), false)
}

// ── Withdraw (addr → amount → confirm) ──

func (h *Handler) handleWithdrawState(ctx context.Context, c tele.Context, lang i18n.Lang, st convState, text string) error {
	uid := c.Sender().ID
	if isCancel(text) {
		h.states.del(uid)
		return c.Send(i18n.T(lang, i18n.KCancelled), h.mainKB(c, lang))
	}

	switch st.step {
	case "addr":
		if !wallet.IsValidTONAddress(text) {
			return c.Send(i18n.T(lang, i18n.KWithdrawBadAddr), tele.ModeHTML, kbCancelOnly(lang))
		}
		st.addr = text
		st.step = "amount"
		h.states.set(uid, st)

		w, err := h.wallet.GetOrCreate(ctx, uid)
		if err != nil {
			h.states.del(uid)
			return c.Send(i18n.T(lang, i18n.KErrGeneric), h.mainKB(c, lang))
		}
		feeTON := wallet.NanoToTON(wallet.NetworkFeeNano)
		maxAmount := w.TotalTON() - feeTON
		return c.Send(
			i18n.T(lang, i18n.KWithdrawAskAmount, addrHead(text), addrTail(text), fmtTON(maxAmount), fmtTON(feeTON)),
			tele.ModeHTML, kbCancelOnly(lang),
		)

	case "amount":
		amt, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil || amt <= 0 {
			return c.Send(i18n.T(lang, i18n.KWithdrawBadAmount), kbCancelOnly(lang))
		}
		st.amountNano = wallet.TONToNano(amt)
		st.step = "confirm"
		h.states.set(uid, st)

		feeNano := int64(wallet.NetworkFeeNano)
		total := st.amountNano + feeNano
		return c.Send(
			i18n.T(lang, i18n.KWithdrawConfirm, fmtNano(st.amountNano), fmtNano(feeNano), fmtNano(total), st.addr),
			tele.ModeHTML, kbConfirm(lang, "wdr:ok", "wdr:cancel"),
		)
	}
	return nil
}

// onWithdrawConfirmCallback نتیجه‌ی دکمه‌ی تأیید/لغو برداشت.
func (h *Handler) onWithdrawConfirmCallback(ctx context.Context, c tele.Context, op string) error {
	uid := c.Sender().ID
	lang := h.langOf(ctx, c)
	st, ok := h.states.get(uid)
	if !ok || st.kind != kindWithdraw || st.step != "confirm" {
		return nil
	}
	if op == "cancel" {
		h.states.del(uid)
		_ = c.Edit(i18n.T(lang, i18n.KCancelled), tele.ModeHTML)
		return c.Send(i18n.T(lang, i18n.KHomeHint), h.mainKB(c, lang))
	}
	h.states.del(uid)
	req, err := h.wallet.RequestWithdraw(ctx, uid, st.addr, st.amountNano, "")
	if err != nil {
		_ = c.Edit(i18n.T(lang, i18n.KWithdrawError, err.Error()), tele.ModeHTML)
		return c.Send(i18n.T(lang, i18n.KHomeHint), h.mainKB(c, lang))
	}
	_ = c.Edit(
		i18n.T(lang, i18n.KWithdrawSubmitted, fmtNano(st.amountNano), fmtNano(wallet.NetworkFeeNano), req.ToAddress, req.ID),
		tele.ModeHTML,
	)
	return c.Send(i18n.T(lang, i18n.KHomeHint), h.mainKB(c, lang))
}

// ── Transfer (recipient → amount → confirm) ──

func (h *Handler) handleTransferState(ctx context.Context, c tele.Context, lang i18n.Lang, st convState, text string) error {
	uid := c.Sender().ID
	if isCancel(text) {
		h.states.del(uid)
		return c.Send(i18n.T(lang, i18n.KCancelled), h.mainKB(c, lang))
	}

	switch st.step {
	case "recipient":
		toID, display, err := h.resolveRecipient(ctx, strings.TrimSpace(text))
		if err != nil {
			return c.Send(i18n.T(lang, i18n.KTransferBadID), kbCancelOnly(lang))
		}
		if toID == 0 {
			return c.Send(i18n.T(lang, i18n.KTransferNoRecipient), kbCancelOnly(lang))
		}
		if toID == uid {
			return c.Send(i18n.T(lang, i18n.KTransferSelf), kbCancelOnly(lang))
		}
		st.toID = toID
		st.recipient = display
		st.step = "amount"
		h.states.set(uid, st)

		w, err := h.wallet.GetOrCreate(ctx, uid)
		if err != nil {
			h.states.del(uid)
			return c.Send(i18n.T(lang, i18n.KErrGeneric), h.mainKB(c, lang))
		}
		return c.Send(
			i18n.T(lang, i18n.KTransferAskAmount, display, fmtTON(w.TotalTON())),
			tele.ModeHTML, kbCancelOnly(lang),
		)

	case "amount":
		amt, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil || amt <= 0 {
			return c.Send(i18n.T(lang, i18n.KTransferBadAmount), kbCancelOnly(lang))
		}
		st.amountNano = wallet.TONToNano(amt)
		st.step = "confirm"
		h.states.set(uid, st)
		return c.Send(
			i18n.T(lang, i18n.KTransferConfirm, fmtNano(st.amountNano), st.recipient),
			tele.ModeHTML, kbConfirm(lang, "trf:ok", "trf:cancel"),
		)
	}
	return nil
}

// onTransferConfirmCallback نتیجه‌ی دکمه‌ی تأیید/لغو انتقال.
func (h *Handler) onTransferConfirmCallback(ctx context.Context, c tele.Context, op string) error {
	uid := c.Sender().ID
	lang := h.langOf(ctx, c)
	st, ok := h.states.get(uid)
	if !ok || st.kind != kindTransfer || st.step != "confirm" {
		return nil
	}
	if op == "cancel" {
		h.states.del(uid)
		_ = c.Edit(i18n.T(lang, i18n.KCancelled), tele.ModeHTML)
		return c.Send(i18n.T(lang, i18n.KHomeHint), h.mainKB(c, lang))
	}
	h.states.del(uid)
	if err := h.wallet.Transfer(ctx, uid, st.toID, st.amountNano, ""); err != nil {
		_ = c.Edit(i18n.T(lang, i18n.KTransferError, err.Error()), tele.ModeHTML)
		return c.Send(i18n.T(lang, i18n.KHomeHint), h.mainKB(c, lang))
	}
	_ = c.Edit(i18n.T(lang, i18n.KTransferDone, fmtNano(st.amountNano), st.recipient), tele.ModeHTML)
	return c.Send(i18n.T(lang, i18n.KHomeHint), h.mainKB(c, lang))
}

// resolveRecipient گیرنده را از handle یا آیدی عددی پیدا می‌کند و
// (telegramID, نمایش, خطا) برمی‌گرداند. telegramID=0 یعنی پیدا نشد.
func (h *Handler) resolveRecipient(ctx context.Context, text string) (int64, string, error) {
	if id, err := strconv.ParseInt(text, 10, 64); err == nil {
		if id <= 0 {
			return 0, "", errBadRecipient
		}
		return id, strconv.FormatInt(id, 10), nil
	}
	// در غیر این صورت به‌عنوان pay handle در نظر بگیر.
	w, err := h.st.GetWalletByPayHandle(ctx, text)
	if err != nil || w == nil {
		return 0, "", nil // پیدا نشد
	}
	return w.TelegramID, text, nil
}
