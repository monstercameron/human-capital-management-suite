package productui

import "testing"

// TestLocalizedRecordDate: record dates read in the reader's locale, and
// anything that is not an ISO date or timestamp passes through untouched.
func TestLocalizedRecordDate(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		context := ResolveProductLocale(locale)
		got := localizedRecordDate(context, "2019-08-17")
		if got == "2019-08-17" || got == "" {
			t.Errorf("%s: the ISO date was not localized: %q", locale, got)
		}
		t.Logf("%s: 2019-08-17 -> %q", locale, got)
		if stamp := localizedRecordDate(context, "2020-01-16T09:30:00Z"); stamp == "2020-01-16T09:30:00Z" {
			t.Errorf("%s: the timestamp was not localized", locale)
		}
	}
	context := ResolveProductLocale("en-US")
	for _, raw := range []string{"", "Not a date", "2019-13-45"} {
		if got := localizedRecordDate(context, raw); got != raw {
			t.Errorf("%q was rewritten to %q; unparseable values pass through", raw, got)
		}
	}
}

// TestPercentSuffixFollowsTheLocale: a percentage carries the percent sign
// the reader's locale uses, not an English "%" appended to every language.
func TestPercentSuffixFollowsTheLocale(t *testing.T) {
	for locale, want := range map[string]string{"en-US": "%", "de-DE": " %", "ar": "٪"} {
		if got := percentSuffix(ResolveProductLocale(locale)); got != want {
			t.Errorf("%s: percent suffix = %q, want %q", locale, got, want)
		}
	}
}
