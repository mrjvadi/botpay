package wallet

import "context"

// Notifier interface برای ارسال پیام به کاربر.
// پیاده‌سازی در tgbot package است.
type Notifier interface {
	// SendHTML پیام HTML به telegram_id می‌فرستد.
	SendHTML(ctx context.Context, telegramID int64, html string) error
}
