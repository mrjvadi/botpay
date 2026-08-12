package i18n

import (
	"regexp"
	"testing"
)

// verbRe همه‌ی فعل‌های قالب‌بندی fmt (به‌جز %%) را می‌یابد.
var verbRe = regexp.MustCompile(`%[#0\- +]*[0-9.]*[bcdeEfFgGoqstTvxX]`)

func countVerbs(s string) int {
	// %% را حذف کن تا به‌اشتباه شمرده نشود.
	clean := regexp.MustCompile(`%%`).ReplaceAllString(s, "")
	return len(verbRe.FindAllString(clean, -1))
}

// TestNoMissingKeys تضمین می‌کند هر زبان همه‌ی کلیدهای زبان پیش‌فرض را دارد.
func TestNoMissingKeys(t *testing.T) {
	for _, l := range Supported() {
		if miss := MissingKeys(l); len(miss) > 0 {
			t.Errorf("locale %q is missing %d keys: %v", l, len(miss), miss)
		}
	}
}

// TestVerbSymmetry تضمین می‌کند تعداد فعل‌های قالب‌بندی هر کلید در همه‌ی زبان‌ها
// یکسان است — وگرنه یک زبان با آرگومان‌های یکسان، خروجی %!s(MISSING) می‌دهد.
func TestVerbSymmetry(t *testing.T) {
	once.Do(load)
	base := bundles[DefaultLang]
	for _, l := range Supported() {
		if l == DefaultLang {
			continue
		}
		for k, baseVal := range base {
			got := bundles[l][k]
			if a, b := countVerbs(baseVal), countVerbs(got); a != b {
				t.Errorf("key %q: %s has %d verbs but %s has %d", k, DefaultLang, a, l, b)
			}
		}
	}
}
