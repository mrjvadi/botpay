package tgbot

import "sync"

// convKind نوع گفتگوی چندمرحله‌ای فعال برای یک کاربر.
type convKind int

const (
	kindNone          convKind = iota
	kindWithdraw               // برداشت: addr → amount → confirm(callback)
	kindTransfer               // انتقال: recipient → amount → confirm(callback)
	kindDepositCustom          // واریز با مبلغ دلخواه (تک‌مرحله)
	kindAdminApprove           // ادمین: دریافت txhash برای تأیید برداشت
	kindAdminReject            // ادمین: دریافت دلیل رد برداشت
	kindAdminCredit            // ادمین: افزودن اعتبار (userID → amount → confirm)
	kindAdminLookup            // ادمین: جستجوی کاربر (تک‌مرحله)
)

// convState وضعیت یک گفتگوی چندمرحله‌ای را نگه می‌دارد.
type convState struct {
	kind       convKind
	step       string
	addr       string // برداشت: آدرس مقصد
	toID       int64  // انتقال/اعتبار: آیدی عددی گیرنده
	recipient  string // انتقال: نمایشِ گیرنده (handle یا id) برای پیام تأیید
	amountNano int64  // مبلغِ در انتظارِ تأیید (برداشت/انتقال/اعتبار)
	arg        string // ادمین: شناسه‌ی برداشت (UUID) برای تأیید/رد
}

// stateStore نگه‌دارنده‌ی thread-safe وضعیت گفتگوها بر اساس شناسه‌ی کاربر.
// از مقدار (نه اشاره‌گر) استفاده می‌کند تا هیچ ساختار قابل‌تغییری بین goroutineها
// به اشتراک گذاشته نشود.
type stateStore struct {
	mu sync.Mutex
	m  map[int64]convState
}

func newStateStore() *stateStore {
	return &stateStore{m: make(map[int64]convState)}
}

func (s *stateStore) get(id int64) (convState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[id]
	return st, ok
}

func (s *stateStore) set(id int64, st convState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = st
}

func (s *stateStore) del(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
}
