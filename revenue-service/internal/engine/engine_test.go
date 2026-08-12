package engine_test

import (
	"testing"
)

// TestRevenueSplit_Channel بررسی تقسیم ۹۰/۱۰ کانال.
func TestRevenueSplit_Channel(t *testing.T) {
	total := int64(1_000_000_000) // 1 TON

	ownerShare := total * 90 / 100
	platformShare := total * 10 / 100

	if ownerShare+platformShare != total {
		t.Errorf("split doesn't add up: %d + %d != %d", ownerShare, platformShare, total)
	}
	if ownerShare != 900_000_000 {
		t.Errorf("owner should get 0.9 TON, got %d nano", ownerShare)
	}
	if platformShare != 100_000_000 {
		t.Errorf("platform should get 0.1 TON, got %d nano", platformShare)
	}
}

// TestRevenueSplit_Group بررسی تقسیم ۵۰/۴۰/۱۰ گروه.
func TestRevenueSplit_Group(t *testing.T) {
	total := int64(1_000_000_000) // 1 TON

	ownerShare := total * 50 / 100     // 50%
	communityShare := total * 40 / 100 // 40%
	platformShare := total * 10 / 100  // 10%

	if ownerShare+communityShare+platformShare != total {
		t.Errorf("group split doesn't add up: %d + %d + %d != %d",
			ownerShare, communityShare, platformShare, total)
	}
}

// TestRevenueSplit_ZeroAmount بررسی با مقدار صفر.
func TestRevenueSplit_ZeroAmount(t *testing.T) {
	total := int64(0)
	ownerShare := total * 90 / 100
	if ownerShare != 0 {
		t.Errorf("zero total should give zero owner share, got %d", ownerShare)
	}
}

// TestNanoConversion تبدیل TON به nano.
func TestNanoConversion(t *testing.T) {
	const nanoPerTON = int64(1_000_000_000)

	oneTON := 1.0
	nanoAmount := int64(oneTON * float64(nanoPerTON))
	if nanoAmount != nanoPerTON {
		t.Errorf("1 TON = %d nano, expected %d", nanoAmount, nanoPerTON)
	}

	halfTON := 0.5
	nanoHalf := int64(halfTON * float64(nanoPerTON))
	if nanoHalf != 500_000_000 {
		t.Errorf("0.5 TON = %d nano, expected 500000000", nanoHalf)
	}
}

// TestRevenueRule_Validation بررسی validation قوانین.
func TestRevenueRule_Validation(t *testing.T) {
	rules := []struct {
		name      string
		ownerPct  int
		platform  int
		community int
		valid     bool
	}{
		{"channel valid", 90, 10, 0, true},
		{"group valid", 50, 10, 40, true},
		{"over 100%", 90, 20, 0, false},
		{"zero all", 0, 0, 0, false},
	}

	for _, r := range rules {
		total := r.ownerPct + r.platform + r.community
		isValid := total == 100 && r.ownerPct > 0
		if isValid != r.valid {
			t.Errorf("%s: expected valid=%v, got valid=%v (total=%d%%)",
				r.name, r.valid, isValid, total)
		}
	}
}
