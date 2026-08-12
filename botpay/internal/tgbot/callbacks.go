package tgbot

import (
	"context"
	"strconv"
	"strings"

	tele "gopkg.in/telebot.v4"

	"github.com/mrjvadi/botpay/internal/i18n"
)

// onCallback همه‌ی callbackهای inline را مسیریابی می‌کند.
// قالب data: "<ns>:<action>[:<arg>...]" (با حذف بایت کنترلی \f ابتدای آن).
func (h *Handler) onCallback(c tele.Context) error {
	ctx := context.Background()
	defer c.Respond()

	data := strings.TrimPrefix(c.Callback().Data, "\f")
	parts := strings.Split(data, ":")
	ns := parts[0]
	arg := ""
	if len(parts) > 1 {
		arg = parts[1]
	}

	switch ns {
	case "nav":
		return h.onNav(ctx, c, arg, parts)
	case "dep":
		return h.onDepCallback(ctx, c, arg, parts)
	case "wdr":
		return h.onWithdrawConfirmCallback(ctx, c, arg)
	case "trf":
		return h.onTransferConfirmCallback(ctx, c, arg)
	case "lang":
		return h.onSetLang(ctx, c, arg)
	case "adm":
		return h.onAdminCallback(ctx, c, arg, parts)
	}
	return nil
}

// onNav ناوبری اصلی (edit-in-place).
func (h *Handler) onNav(ctx context.Context, c tele.Context, sub string, parts []string) error {
	switch sub {
	case "wallet":
		return h.viewWallet(c, true)
	case "deposit":
		return h.viewDeposit(c, true)
	case "withdraw":
		return h.onWithdraw(c)
	case "transfer":
		return h.onTransfer(c)
	case "history":
		page := 0
		if len(parts) > 2 {
			page, _ = strconv.Atoi(parts[2])
		}
		return h.viewHistory(c, page, true)
	}
	return nil
}

// onDepCallback اکشن‌های صفحه‌ی واریز.
func (h *Handler) onDepCallback(ctx context.Context, c tele.Context, sub string, parts []string) error {
	switch sub {
	case "any":
		return h.makeInvoice(c, 0, true)
	case "amt":
		if len(parts) > 2 {
			if nano, err := strconv.ParseInt(parts[2], 10, 64); err == nil {
				return h.makeInvoice(c, nano, true)
			}
		}
		return nil
	case "custom":
		lang := h.langOf(ctx, c)
		h.states.set(c.Sender().ID, convState{kind: kindDepositCustom})
		return c.Send(i18n.T(lang, i18n.KDepositAskCustom), tele.ModeHTML, kbCancelOnly(lang))
	case "check":
		if len(parts) > 2 {
			return h.onCheckDeposit(ctx, c, parts[2])
		}
	}
	return nil
}
