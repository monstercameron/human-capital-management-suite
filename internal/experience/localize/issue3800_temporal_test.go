package localize

import (
	"regexp"
	"testing"
	"time"
)

func TestIssue3800_ClockAndDayGolden(t *testing.T) {
	at := time.Date(2026, 9, 22, 15, 4, 0, 0, time.UTC) // a Tuesday
	cases := []struct{ locale, clock, day, year, short string }{
		{"en-US", "3:04 PM", "Tuesday, September 22", "September 22, 2026", "Sep 22"},
		{"de-DE", "15:04", "Dienstag, 22. September", "22. September 2026", "22. Sept."},
		{"fr-FR", "15:04", "mardi 22 septembre", "22 septembre 2026", "22 sept."},
		{"es-ES", "15:04", "martes, 22 de septiembre", "22 de septiembre de 2026", "22 sept"},
		{"ja-JP", "15:04", "9月22日火曜日", "2026年9月22日", "9月22日"},
		{"ar", "١٥:٠٤", "الثلاثاء، ٢٢ سبتمبر", "٢٢ سبتمبر ٢٠٢٦", "٢٢ سبتمبر"},
	}
	for _, c := range cases {
		if got := FormatClock(c.locale, at); got != c.clock {
			t.Errorf("%s clock %q, want %q", c.locale, got, c.clock)
		}
		if got := FormatDay(c.locale, at, false); got != c.day {
			t.Errorf("%s day %q, want %q", c.locale, got, c.day)
		}
		if got := FormatDay(c.locale, at, true); got != c.year {
			t.Errorf("%s day with year %q, want %q", c.locale, got, c.year)
		}
		if got := FormatShortDay(c.locale, at); got != c.short {
			t.Errorf("%s short %q, want %q", c.locale, got, c.short)
		}
	}
	if regexp.MustCompile(`[0-9]`).MatchString(FormatClock("ar", at) + FormatDay("ar", at, true)) {
		t.Error("Arabic output uses Latin digits")
	}
}

func TestIssue3800_FormatTimeZoneOnlyWhenDifferent(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no tzdata")
	}
	ny, _ := time.LoadLocation("America/New_York")
	at := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	if got := FormatTime("en-US", at.In(ny), ny); got != "7:00 AM" {
		t.Errorf("same zone %q", got)
	}
	if got := FormatTime("en-US", at.In(ny), berlin); got != "7:00 AM EST" {
		t.Errorf("different zone %q", got)
	}
	if got := FormatTime("de-DE", at.In(berlin), nil); got != "13:00" {
		t.Errorf("unknown viewer %q", got)
	}
}

func TestIssue3800_FormatRelative(t *testing.T) {
	now := time.Date(2026, 9, 22, 15, 0, 0, 0, time.UTC)
	cases := []struct {
		locale string
		ago    time.Duration
		want   string
	}{
		{"en-US", 10 * time.Second, "now"},
		{"en-US", 5 * time.Minute, "5 minutes ago"},
		{"en-US", 1 * time.Hour, "1 hour ago"},
		{"en-US", -2 * time.Hour, "in 2 hours"},
		{"en-US", 30 * time.Hour, "yesterday"},
		{"en-US", 3 * 24 * time.Hour, "3 days ago"},
		{"de-DE", 2 * time.Hour, "vor 2 Stunden"},
		{"fr-FR", 2 * time.Hour, "il y a 2 heures"},
		{"es-ES", -3 * time.Hour, "dentro de 3 horas"},
		{"ja-JP", 5 * time.Minute, "5分前"},
		{"ar", 2 * time.Hour, "قبل ساعتين"},
		{"ar", 5 * time.Hour, "قبل ٥ ساعات"},
		{"ar", 30 * time.Minute, "قبل ٣٠ دقيقة"},
	}
	for _, c := range cases {
		got, ok := FormatRelative(c.locale, now.Add(-c.ago), now)
		if !ok || got != c.want {
			t.Errorf("%s %v: %q ok=%v, want %q", c.locale, c.ago, got, ok, c.want)
		}
	}
	if _, ok := FormatRelative("en-US", now.Add(-8*24*time.Hour), now); ok {
		t.Error("a week or more should fall back to a date")
	}
}
