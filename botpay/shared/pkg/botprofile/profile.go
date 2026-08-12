// Package botprofile یک قرارداد واحد برای پروفایل عمومی ربات‌های Telegram فراهم می‌کند.
package botprofile

import (
	"errors"
	"fmt"
	"strings"

	tele "gopkg.in/telebot.v4"
)

// Config رفتار همگام‌سازی startup را مشخص می‌کند.
type Config struct {
	Environment string
	ServiceName string
	Languages   []string
}

// IsProduction فقط نام‌های صریح production را قبول می‌کند تا dev/test هیچ‌وقت
// ناخواسته پروفایل واقعی BotFather را تغییر ندهند.
func IsProduction(environment string) bool {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "production", "prod":
		return true
	default:
		return false
	}
}

// ServiceName مقدار قابل‌تنظیم را ترجیح می‌دهد و در نبود آن نام استاندارد
// سرویس را برمی‌گرداند.
func ServiceName(configured, fallback string) string {
	if value := strings.TrimSpace(configured); value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

// Sync نامِ نمایشی، description، short-description (bio) و عکسِ فعلی را در
// startup همگام می‌کند — در production نام نمایشی برابرِ نامِ سرویس می‌شود؛
// در هر محیطِ غیرِproduction (development, staging, ...) همان نام با برچسبِ
// محیط پسوند می‌خورد (مثلاً «Uploader Bot (development)») تا یک instance
// تستی هیچ‌وقت با پروفایلِ قدیمی/دستی شبیهِ production روی تلگرام نماند —
// description/bio/عکس در هر دو حالت پاک می‌شوند. عملیات idempotent است و
// اجرای دوباره در هر restart نتیجه یکسان دارد.
func Sync(bot *tele.Bot, cfg Config) error {
	if bot == nil {
		return errors.New("bot profile: nil bot")
	}
	name := strings.TrimSpace(cfg.ServiceName)
	if name == "" {
		return errors.New("bot profile: empty service name")
	}
	if !IsProduction(cfg.Environment) {
		label := strings.TrimSpace(cfg.Environment)
		if label == "" {
			label = "dev"
		}
		name = fmt.Sprintf("%s (%s)", name, label)
	}

	languages := cfg.Languages
	if len(languages) == 0 {
		// مقدار خالی پروفایل پیش‌فرض است؛ fa/en localizationهای رسمی پروژه‌اند.
		languages = []string{"", "fa", "en"}
	}

	var errs []error
	for _, language := range uniqueLanguages(languages) {
		if err := bot.SetMyName(name, language); err != nil {
			errs = append(errs, fmt.Errorf("set name (%q): %w", language, err))
		}
		if err := bot.SetMyDescription("", language); err != nil {
			errs = append(errs, fmt.Errorf("clear description (%q): %w", language, err))
		}
		if err := bot.SetMyShortDescription("", language); err != nil {
			errs = append(errs, fmt.Errorf("clear short description (%q): %w", language, err))
		}
	}
	if _, err := bot.Raw("removeMyProfilePhoto", map[string]string{}); err != nil {
		errs = append(errs, fmt.Errorf("remove profile photo: %w", err))
	}
	return errors.Join(errs...)
}

func uniqueLanguages(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
