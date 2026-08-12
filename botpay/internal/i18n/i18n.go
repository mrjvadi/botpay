// Package i18n یک سیستم ترجمه‌ی سبک و بدون وابستگی برای ربات botpay فراهم می‌کند.
// فایل‌های زبان به صورت JSON در پوشه‌ی locales قرار دارند و هنگام build داخل
// باینری embed می‌شوند، بنابراین در زمان اجرا به فایل خارجی نیازی نیست.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

//go:embed locales/*.json
var localeFS embed.FS

// Lang کد زبان پشتیبانی‌شده.
type Lang string

const (
	FA Lang = "fa" // فارسی
	EN Lang = "en" // English
)

// DefaultLang زبان پیش‌فرض وقتی زبان کاربر مشخص یا معتبر نباشد.
var DefaultLang = FA

// supported فهرست زبان‌های معتبر به‌ترتیب نمایش در منوی انتخاب زبان.
var supported = []Lang{FA, EN}

// nativeName نام بومی هر زبان برای نمایش در دکمه‌ها.
var nativeName = map[Lang]string{
	FA: "🇮🇷 فارسی",
	EN: "🇬🇧 English",
}

// bundles نگاشت lang → (key → template). با sync.Once یک‌بار بارگذاری می‌شود.
var (
	bundles map[Lang]map[string]string
	once    sync.Once
)

func load() {
	bundles = make(map[Lang]map[string]string)
	for _, lang := range supported {
		data, err := localeFS.ReadFile("locales/" + string(lang) + ".json")
		if err != nil {
			panic(fmt.Sprintf("i18n: locale file missing for %q: %v", lang, err))
		}
		m := map[string]string{}
		if err := json.Unmarshal(data, &m); err != nil {
			panic(fmt.Sprintf("i18n: invalid locale json for %q: %v", lang, err))
		}
		bundles[lang] = m
	}
}

// SetDefault زبان پیش‌فرض را از روی یک رشته‌ی پیکربندی تنظیم می‌کند.
func SetDefault(code string) {
	if l, ok := Parse(code); ok {
		DefaultLang = l
	}
}

// Parse یک رشته را به Lang معتبر تبدیل می‌کند. اگر معتبر نباشد ok=false است.
// پیشوندها هم پذیرفته می‌شوند (مثلاً "en-US" → en) تا با language_code تلگرام سازگار باشد.
func Parse(code string) (Lang, bool) {
	code = strings.ToLower(strings.TrimSpace(code))
	for _, l := range supported {
		if code == string(l) || strings.HasPrefix(code, string(l)+"-") {
			return l, true
		}
	}
	return "", false
}

// Normalize یک رشته‌ی زبان را به یک Lang معتبر تبدیل می‌کند؛ در صورت نامعتبر
// بودن، زبان پیش‌فرض را برمی‌گرداند.
func Normalize(code string) Lang {
	if l, ok := Parse(code); ok {
		return l
	}
	return DefaultLang
}

// Supported فهرست زبان‌های پشتیبانی‌شده را برمی‌گرداند.
func Supported() []Lang { return append([]Lang(nil), supported...) }

// Name نام بومی زبان را برمی‌گرداند.
func Name(l Lang) string {
	if n, ok := nativeName[l]; ok {
		return n
	}
	return string(l)
}

// T متن کلید key را برای زبان lang برمی‌گرداند و args را با fmt.Sprintf جای‌گذاری می‌کند.
// اگر کلید در زبان درخواستی نباشد، به زبان پیش‌فرض و سپس به خود key سقوط می‌کند.
func T(lang Lang, key string, args ...any) string {
	once.Do(load)

	tpl, ok := lookup(lang, key)
	if !ok {
		tpl, ok = lookup(DefaultLang, key)
	}
	if !ok {
		return key // آخرین راه‌حل: خود کلید را نشان بده تا کمبود ترجمه دیده شود
	}
	if len(args) == 0 {
		return tpl
	}
	return fmt.Sprintf(tpl, args...)
}

func lookup(lang Lang, key string) (string, bool) {
	m, ok := bundles[lang]
	if !ok {
		return "", false
	}
	v, ok := m[key]
	return v, ok
}

// MissingKeys کلیدهایی را برمی‌گرداند که در یک زبان نسبت به زبان پیش‌فرض جا افتاده‌اند.
// برای تست‌ها/بررسی سلامت ترجمه‌ها مفید است.
func MissingKeys(lang Lang) []string {
	once.Do(load)
	base := bundles[DefaultLang]
	target := bundles[lang]
	var missing []string
	for k := range base {
		if _, ok := target[k]; !ok {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	return missing
}
