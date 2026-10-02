package chatui

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestS24_TimeZoneLabelsAreReadableInThreeLanguages(t *testing.T) {
	winter := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	summer := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		locale, zone string
		at           time.Time
		want         string
	}{
		{"en-US", "America/Denver", winter, "Denver (UTC−7)"},
		{"en-US", "America/Denver", summer, "Denver (UTC−6)"},
		{"en-US", "America/Los_Angeles", winter, "Los Angeles (UTC−8)"},
		{"en-US", "Asia/Kolkata", winter, "Kolkata (UTC+5:30)"},
		{"en-US", "Europe/Berlin", summer, "Berlin (UTC+2)"},
		{"en-US", "UTC", winter, "UTC"},
		{"en-US", "America/Argentina/Buenos_Aires", winter, "Buenos Aires (UTC−3)"},
		{"de-DE", "America/Denver", winter, "Denver (UTC−7)"},
		{"de-DE", "Asia/Kolkata", winter, "Kalkutta (UTC+5:30)"},
		{"de-DE", "Europe/Vienna", winter, "Wien (UTC+1)"},
		{"de-DE", "Asia/Tokyo", winter, "Tokio (UTC+9)"},
		{"ar", "America/Denver", winter, "دنفر (⁦UTC−٧⁩)"},
		{"ar", "Asia/Kolkata", winter, "كولكاتا (⁦UTC+٥:٣٠⁩)"},
		{"ar", "UTC", winter, "التوقيت العالمي"},
		// A zone the data does not know is listed by its city alone.
		{"en-US", "Mars/Olympus_Mons", winter, "Olympus Mons"},
	}
	for _, c := range cases {
		if got := s24ZoneLabel(c.locale, c.zone, c.at); got != c.want {
			t.Errorf("%s %s: label = %q, want %q", c.locale, c.zone, got, c.want)
		}
	}
}

// The list shows the readable name and keeps the IANA ID as the stored value.
func TestS24_PreferencesTimeZoneListKeepsTheIDAsValue(t *testing.T) {
	for _, c := range []struct{ locale, city string }{{"en-US", "Denver ("}, {"de-DE", "Denver ("}, {"ar", "دنفر ("}} {
		m := Model{State: StateReady, Locale: c.locale, Preferences: Preferences{QuietHours: true, QuietTimezone: "America/Denver"}}
		markup := renderNode(t, railPreferences(m, handlers{}))
		if !strings.Contains(markup, `value="America/Denver"`) {
			t.Errorf("%s: the stored value is not the IANA ID: %s", c.locale, markup)
		}
		if !strings.Contains(markup, ">"+c.city) || !strings.Contains(markup, "UTC") {
			t.Errorf("%s: the option does not read as a city with an offset: %s", c.locale, markup)
		}
		if regexp.MustCompile(`>[^<>]*([A-Za-z]+/[A-Za-z]|_)[^<>]*<`).MatchString(markup) {
			t.Errorf("%s: a raw zone ID is still visible text: %s", c.locale, markup)
		}
	}
}
