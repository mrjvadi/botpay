package i18n

// کلیدهای ترجمه — به‌صورت ثابت تعریف شده‌اند تا از اشتباه تایپی در زمان کامپایل
// جلوگیری شود. مقدار هر ثابت دقیقاً با کلید متناظر در فایل‌های locales/*.json یکی است.
// (بازطراحی ۲۰۲۶-۰۷-۱۵: رابط inline-first + پنل ادمین کامل.)
const (
	// ── عمومی ──
	KErrGeneric = "err.generic"
	KCancelled  = "common.cancelled"
	KBack       = "common.back"
	KConfirm    = "common.confirm"
	KYes        = "common.yes"
	KNo         = "common.no"
	KNotAdmin   = "common.not_admin"

	// ── دکمه‌های کیبورد اصلی (reply) ──
	KBtnWallet   = "btn.wallet"
	KBtnDeposit  = "btn.deposit"
	KBtnWithdraw = "btn.withdraw"
	KBtnTransfer = "btn.transfer"
	KBtnHistory  = "btn.history"
	KBtnHelp     = "btn.help"
	KBtnLanguage = "btn.language"
	KBtnCancel   = "btn.cancel"
	KBtnAdmin    = "btn.admin"

	// ── دکمه‌های inline عمومی ──
	KBtnRefresh      = "btn.refresh"
	KBtnBack         = "btn.back"
	KBtnPrev         = "btn.prev"
	KBtnNext         = "btn.next"
	KBtnOpenWallet   = "btn.open_wallet"
	KBtnCheckDeposit = "btn.check_deposit"
	KBtnMax          = "btn.max"
	KBtnConfirm      = "btn.confirm"

	// ── start / home ──
	KStart    = "start.body"
	KHomeHint = "home.hint"

	// ── wallet ──
	KWallet        = "wallet.body"
	KWalletFrozen  = "wallet.frozen_line"
	KBtnWalletDep  = "btn.w_deposit"
	KBtnWalletWdr  = "btn.w_withdraw"
	KBtnWalletTrf  = "btn.w_transfer"
	KBtnWalletHist = "btn.w_history"

	// ── deposit ──
	KDepositErr       = "deposit.error"
	KDepositMenu      = "deposit.menu"
	KDepositAskCustom = "deposit.ask_custom"
	KDepositBadAmount = "deposit.bad_amount"
	KDepositBody      = "deposit.body"
	KBtnDepositAny    = "btn.deposit_any"
	KBtnDepositCustom = "btn.deposit_custom"

	// ── withdraw ──
	KWithdrawInsufficient = "withdraw.insufficient"
	KWithdrawAskAddr      = "withdraw.ask_addr"
	KWithdrawBadAddr      = "withdraw.bad_addr"
	KWithdrawAskAmount    = "withdraw.ask_amount"
	KWithdrawBadAmount    = "withdraw.bad_amount"
	KWithdrawConfirm      = "withdraw.confirm"
	KWithdrawSubmitted    = "withdraw.submitted"
	KWithdrawError        = "withdraw.error"

	// ── history ──
	KHistoryEmpty = "history.empty"
	KHistoryTitle = "history.title"
	KHistoryLine  = "history.line"
	KHistoryPager = "history.pager"

	// ── برچسب نوع تراکنش ──
	KTxDeposit   = "tx.deposit"
	KTxWithdraw  = "tx.withdraw"
	KTxCreditAdd = "tx.credit_add"
	KTxPayment   = "tx.payment"
	KTxRefund    = "tx.refund"

	// ── help ──
	KHelp = "help.body"

	// ── transfer ──
	KTransferInsufficient = "transfer.insufficient"
	KTransferAskID        = "transfer.ask_id"
	KTransferBadID        = "transfer.bad_id"
	KTransferNoRecipient  = "transfer.no_recipient"
	KTransferSelf         = "transfer.self"
	KTransferAskAmount    = "transfer.ask_amount"
	KTransferBadAmount    = "transfer.bad_amount"
	KTransferConfirm      = "transfer.confirm"
	KTransferDone         = "transfer.done"
	KTransferReceived     = "transfer.received"
	KTransferError        = "transfer.error"

	// ── check deposit / push ──
	KCheckPending     = "check.pending"
	KCheckUnconfirmed = "check.unconfirmed"
	KCheckConfirmed   = "check.confirmed"
	KDepositConfirmed = "deposit.confirmed"

	// ── اعلان‌های push به کاربرِ متأثر (برداشت/اعتبار توسط ادمین یا سرویس) ──
	KNotifyWithdrawDone     = "notify.withdraw_done"
	KNotifyWithdrawRejected = "notify.withdraw_rejected"
	KNotifyCreditAdded      = "notify.credit_added"
	KNotifyPayment          = "notify.payment"

	// ── language ──
	KLanguageAsk     = "language.ask"
	KLanguageChanged = "language.changed"

	// ── پنل ادمین — دکمه‌ها ──
	KBtnAdminWithdraws = "btn.admin_withdraws"
	KBtnAdminCredit    = "btn.admin_credit"
	KBtnAdminLookup    = "btn.admin_lookup"
	KBtnAdminChain     = "btn.admin_chain"
	KBtnAdminRefresh   = "btn.admin_refresh"
	KBtnAdminBack      = "btn.admin_back"
	KBtnApprove        = "btn.approve"
	KBtnReject         = "btn.reject"
	KBtnWdDetail       = "btn.wd_detail"

	// ── پنل ادمین — متن‌ها و جریان‌ها ──
	KAdminDashboard       = "admin.dashboard"
	KAdminNoWithdraws     = "admin.no_withdraws"
	KAdminWithdrawsTitle  = "admin.withdraws_title"
	KAdminWithdrawItem    = "admin.withdraw_item"
	KAdminWithdrawDetail  = "admin.withdraw_detail"
	KAdminAskTxHash       = "admin.ask_txhash"
	KAdminAskReason       = "admin.ask_reason"
	KAdminApproved        = "admin.approved"
	KAdminRejected        = "admin.rejected"
	KAdminBadID           = "admin.bad_id"
	KAdminBadParam        = "admin.bad_param"
	KAdminWalletNotFound  = "admin.wallet_not_found"
	KAdminError           = "admin.error"
	KAdminAskCreditUserID = "admin.ask_credit_userid"
	KAdminAskCreditAmount = "admin.ask_credit_amount"
	KAdminCreditConfirm   = "admin.credit_confirm"
	KAdminCreditAdded     = "admin.credit_added"
	KAdminAskLookup       = "admin.ask_lookup"
	KAdminUserCard        = "admin.user_card"
	KAdminUserNotFound    = "admin.user_not_found"
	KAdminUserRecent      = "admin.user_recent"
	KAdminChainChecking   = "admin.chain_checking"
	KAdminChainOK         = "admin.chain_ok"
	KAdminChainBroken     = "admin.chain_broken"
)
