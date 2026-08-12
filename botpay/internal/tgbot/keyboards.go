package tgbot

import (
	"fmt"

	tele "gopkg.in/telebot.v4"

	"github.com/mrjvadi/botpay/internal/i18n"
	"github.com/mrjvadi/botpay/internal/store"
	"github.com/mrjvadi/botpay/internal/wallet"
)

// depositPresetsTON مبالغ پیش‌فرض دکمه‌های واریز.
var depositPresetsTON = []float64{1, 5, 10, 50}

// ── کیبورد اصلی (reply، همیشه چسبیده) ──

func kbMain(lang i18n.Lang, isAdmin bool) *tele.ReplyMarkup {
	kb := &tele.ReplyMarkup{ResizeKeyboard: true}
	rows := []tele.Row{
		kb.Row(kb.Text(i18n.T(lang, i18n.KBtnWallet))),
		kb.Row(kb.Text(i18n.T(lang, i18n.KBtnDeposit)), kb.Text(i18n.T(lang, i18n.KBtnWithdraw))),
		kb.Row(kb.Text(i18n.T(lang, i18n.KBtnTransfer)), kb.Text(i18n.T(lang, i18n.KBtnHistory))),
		kb.Row(kb.Text(i18n.T(lang, i18n.KBtnHelp)), kb.Text(i18n.T(lang, i18n.KBtnLanguage))),
	}
	if isAdmin {
		rows = append(rows, kb.Row(kb.Text(i18n.T(lang, i18n.KBtnAdmin))))
	}
	kb.Reply(rows...)
	return kb
}

func kbCancelOnly(lang i18n.Lang) *tele.ReplyMarkup {
	kb := &tele.ReplyMarkup{ResizeKeyboard: true}
	kb.Reply(kb.Row(kb.Text(i18n.T(lang, i18n.KBtnCancel))))
	return kb
}

// ── کیف پول (inline) ──

func kbWallet(lang i18n.Lang) *tele.ReplyMarkup {
	kb := &tele.ReplyMarkup{}
	kb.Inline(
		kb.Row(
			kb.Data(i18n.T(lang, i18n.KBtnWalletDep), "nav:deposit"),
			kb.Data(i18n.T(lang, i18n.KBtnWalletWdr), "nav:withdraw"),
		),
		kb.Row(
			kb.Data(i18n.T(lang, i18n.KBtnWalletTrf), "nav:transfer"),
			kb.Data(i18n.T(lang, i18n.KBtnWalletHist), "nav:history:0"),
		),
		kb.Row(kb.Data(i18n.T(lang, i18n.KBtnRefresh), "nav:wallet")),
	)
	return kb
}

// ── واریز ──

func kbDepositMenu(lang i18n.Lang) *tele.ReplyMarkup {
	kb := &tele.ReplyMarkup{}
	var presets []tele.Btn
	for _, p := range depositPresetsTON {
		presets = append(presets, kb.Data(fmt.Sprintf("%s TON", fmtTON(p)), fmt.Sprintf("dep:amt:%d", wallet.TONToNano(p))))
	}
	kb.Inline(
		kb.Row(presets[0], presets[1]),
		kb.Row(presets[2], presets[3]),
		kb.Row(
			kb.Data(i18n.T(lang, i18n.KBtnDepositCustom), "dep:custom"),
			kb.Data(i18n.T(lang, i18n.KBtnDepositAny), "dep:any"),
		),
		kb.Row(kb.Data(i18n.T(lang, i18n.KBtnBack), "nav:wallet")),
	)
	return kb
}

func kbDepositInvoice(lang i18n.Lang, payURL, code string) *tele.ReplyMarkup {
	kb := &tele.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.URL(i18n.T(lang, i18n.KBtnOpenWallet), payURL)),
		kb.Row(kb.Data(i18n.T(lang, i18n.KBtnCheckDeposit), "dep:check:"+code)),
		kb.Row(kb.Data(i18n.T(lang, i18n.KBtnBack), "nav:deposit")),
	)
	return kb
}

// ── تأیید (برداشت/انتقال) ──

func kbConfirm(lang i18n.Lang, okData, cancelData string) *tele.ReplyMarkup {
	kb := &tele.ReplyMarkup{}
	kb.Inline(kb.Row(
		kb.Data(i18n.T(lang, i18n.KBtnConfirm), okData),
		kb.Data(i18n.T(lang, i18n.KBtnCancel), cancelData),
	))
	return kb
}

// ── تاریخچه (inline + pager) ──

func kbHistory(lang i18n.Lang, page, totalPages int) *tele.ReplyMarkup {
	kb := &tele.ReplyMarkup{}
	var pager []tele.Btn
	if page > 0 {
		pager = append(pager, kb.Data(i18n.T(lang, i18n.KBtnPrev), fmt.Sprintf("nav:history:%d", page-1)))
	}
	if page < totalPages-1 {
		pager = append(pager, kb.Data(i18n.T(lang, i18n.KBtnNext), fmt.Sprintf("nav:history:%d", page+1)))
	}
	rows := []tele.Row{}
	if len(pager) > 0 {
		rows = append(rows, kb.Row(pager...))
	}
	rows = append(rows, kb.Row(kb.Data(i18n.T(lang, i18n.KBtnBack), "nav:wallet")))
	kb.Inline(rows...)
	return kb
}

// ── زبان ──

func kbLanguage() *tele.ReplyMarkup {
	kb := &tele.ReplyMarkup{}
	var rows []tele.Row
	for _, l := range i18n.Supported() {
		rows = append(rows, kb.Row(kb.Data(i18n.Name(l), "lang:"+string(l))))
	}
	kb.Inline(rows...)
	return kb
}

// ── پنل ادمین ──

func kbAdminMenu(lang i18n.Lang) *tele.ReplyMarkup {
	kb := &tele.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data(i18n.T(lang, i18n.KBtnAdminWithdraws), "adm:withdraws:0")),
		kb.Row(
			kb.Data(i18n.T(lang, i18n.KBtnAdminCredit), "adm:credit"),
			kb.Data(i18n.T(lang, i18n.KBtnAdminLookup), "adm:lookup"),
		),
		kb.Row(
			kb.Data(i18n.T(lang, i18n.KBtnAdminChain), "adm:chain"),
			kb.Data(i18n.T(lang, i18n.KBtnAdminRefresh), "adm:home"),
		),
	)
	return kb
}

// kbAdminWithdrawList فهرست برداشت‌ها را با دکمه‌ی «جزئیات» برای هر مورد + pager می‌سازد.
func kbAdminWithdrawList(lang i18n.Lang, reqs []store.WithdrawRequest, page, totalPages int) *tele.ReplyMarkup {
	kb := &tele.ReplyMarkup{}
	var rows []tele.Row
	for _, r := range reqs {
		label := fmt.Sprintf("🔍 %s TON — %s", fmtNano(r.Amount), shortID(r.ID.String()))
		rows = append(rows, kb.Row(kb.Data(label, "adm:wd:"+r.ID.String())))
	}
	var pager []tele.Btn
	if page > 0 {
		pager = append(pager, kb.Data(i18n.T(lang, i18n.KBtnPrev), fmt.Sprintf("adm:withdraws:%d", page-1)))
	}
	if page < totalPages-1 {
		pager = append(pager, kb.Data(i18n.T(lang, i18n.KBtnNext), fmt.Sprintf("adm:withdraws:%d", page+1)))
	}
	if len(pager) > 0 {
		rows = append(rows, kb.Row(pager...))
	}
	rows = append(rows, kb.Row(kb.Data(i18n.T(lang, i18n.KBtnAdminBack), "adm:home")))
	kb.Inline(rows...)
	return kb
}

// kbAdminWithdrawDetail دکمه‌های تأیید/رد یک برداشت + بازگشت.
func kbAdminWithdrawDetail(lang i18n.Lang, id string) *tele.ReplyMarkup {
	kb := &tele.ReplyMarkup{}
	kb.Inline(
		kb.Row(
			kb.Data(i18n.T(lang, i18n.KBtnApprove), "adm:approve:"+id),
			kb.Data(i18n.T(lang, i18n.KBtnReject), "adm:reject:"+id),
		),
		kb.Row(kb.Data(i18n.T(lang, i18n.KBtnAdminBack), "adm:withdraws:0")),
	)
	return kb
}

// kbAdminBack فقط دکمه‌ی بازگشت به منوی مدیریت.
func kbAdminBack(lang i18n.Lang) *tele.ReplyMarkup {
	kb := &tele.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data(i18n.T(lang, i18n.KBtnAdminBack), "adm:home")))
	return kb
}

// isCancel بررسی می‌کند متن ورودی دستور انصراف باشد (در همه‌ی زبان‌ها).
func isCancel(text string) bool {
	if text == "/cancel" {
		return true
	}
	for _, l := range i18n.Supported() {
		if text == i18n.T(l, i18n.KBtnCancel) {
			return true
		}
	}
	return false
}
