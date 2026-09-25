package chatui

import (
	"testing"
	"time"
)

// TestFormatDayFollowsLocale pins round 3 C-2: day dividers wrote English
// dates beside translated "Yesterday"/"Today" labels in de-DE and ar.
func TestFormatDayFollowsLocale(t *testing.T) {
	day := time.Date(2026, time.September, 22, 10, 0, 0, 0, time.UTC) // a Tuesday
	cases := []struct {
		locale         string
		withYear       bool
		want           string
		wantShortMonth string
	}{
		{"en-US", false, "Tuesday, September 22", "Sep 22"},
		{"en-US", true, "September 22, 2026", "Sep 22"},
		{"de-DE", false, "Dienstag, 22. September", "22. Sept."},
		{"de-DE", true, "22. September 2026", "22. Sept."},
		{"ar", false, "الثلاثاء، ٢٢ سبتمبر", "٢٢ سبتمبر"},
		{"ar", true, "٢٢ سبتمبر ٢٠٢٦", "٢٢ سبتمبر"},
		{"", false, "Tuesday, September 22", "Sep 22"},
	}
	for _, tc := range cases {
		if got := formatDay(tc.locale, day, tc.withYear); got != tc.want {
			t.Errorf("formatDay(%q, withYear=%v) = %q, want %q", tc.locale, tc.withYear, got, tc.want)
		}
		if got := formatShortDate(tc.locale, day); got != tc.wantShortMonth {
			t.Errorf("formatShortDate(%q) = %q, want %q", tc.locale, got, tc.wantShortMonth)
		}
	}
}
