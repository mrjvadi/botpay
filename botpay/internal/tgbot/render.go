package tgbot

import (
	"strconv"
	"strings"

	"github.com/mrjvadi/botpay/internal/i18n"
	"github.com/mrjvadi/botpay/internal/store"
	"github.com/mrjvadi/botpay/internal/wallet"
)

// fmtNano یک مقدار nano-TON را به رشته‌ی خوانا تبدیل می‌کند (حداکثر ۴ رقم اعشار،
// بدون صفرهای انتهایی اضافه). مثال: 12500000000 → "12.5"، 1000000 → "0.001".
func fmtNano(nano int64) string {
	return fmtTON(wallet.NanoToTON(nano))
}

// fmtTON یک عدد TON (float) را مثل fmtNano قالب‌بندی می‌کند.
func fmtTON(v float64) string {
	s := strconv.FormatFloat(v, 'f', 4, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	if s == "" || s == "-0" {
		s = "0"
	}
	return s
}

// shortAddr ابتدای یک آدرس TON را برمی‌گرداند (۶ کاراکتر) برای نمایش فشرده.
func shortAddr(addr string) string {
	r := []rune(addr)
	if len(r) <= 10 {
		return addr
	}
	return string(r[:6])
}

// addrHead/addrTail برای نمایش "UQ12…ab34".
func addrHead(addr string) string {
	r := []rune(addr)
	if len(r) < 6 {
		return addr
	}
	return string(r[:6])
}
func addrTail(addr string) string {
	r := []rune(addr)
	if len(r) < 4 {
		return addr
	}
	return string(r[len(r)-4:])
}

// shortID شش کاراکتر اول یک UUID را برمی‌گرداند.
func shortID(id string) string {
	if len(id) > 6 {
		return id[:6]
	}
	return id
}

// frozenLine خط «بلوک‌شده» را می‌سازد اگر موجودی بلوک‌شده > 0 باشد، وگرنه خالی.
func frozenLine(lang i18n.Lang, frozenNano int64) string {
	if frozenNano <= 0 {
		return ""
	}
	return i18n.T(lang, i18n.KWalletFrozen, fmtNano(frozenNano))
}

// txMeta آیکن و کلید برچسب نوع تراکنش را برمی‌گرداند.
func txMeta(t store.TxType) (icon, labelKey string) {
	switch t {
	case store.TxDeposit:
		return "📥", i18n.KTxDeposit
	case store.TxWithdraw:
		return "📤", i18n.KTxWithdraw
	case store.TxCreditAdd:
		return "🎁", i18n.KTxCreditAdd
	case store.TxPayment:
		return "💸", i18n.KTxPayment
	case store.TxRefund:
		return "↩️", i18n.KTxRefund
	}
	return "💰", i18n.KTxPayment
}

// truncate رشته را به حداکثر n کاراکتر (rune-aware) کوتاه می‌کند.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// txLine یک خط تاریخچه را برای یک تراکنش قالب‌بندی می‌کند.
func txLine(lang i18n.Lang, tx store.Transaction) string {
	icon, labelKey := txMeta(tx.Type)
	amt := wallet.NanoToTON(tx.Amount)
	sign := "+"
	if amt < 0 {
		sign = "−"
		amt = -amt
	}
	status := ""
	if tx.Status == store.TxPending {
		status = " ⏳"
	} else if tx.Status == store.TxFailed {
		status = " ❌"
	}
	desc := tx.Description
	if desc == "" {
		desc = i18n.T(lang, labelKey)
	}
	desc = truncate(desc, 28)
	return i18n.T(lang, i18n.KHistoryLine,
		icon, sign, fmtTON(amt), status, tx.CreatedAt.Format("01/02 15:04"), desc)
}
