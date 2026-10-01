package productui

import (
	"strings"
	"testing"
	"time"
)

func TestTodo_UXBLIND_020(t *testing.T) {
	locale := ResolveProductLocale("en-US").WithTimeZone("America/New_York")
	instant := time.Date(2026, time.September, 28, 21, 53, 0, 0, time.UTC)
	if got := locale.FormatDate(instant); got != "28 Sep 2026" {
		t.Fatalf("date = %q, want one product date form", got)
	}
	if got := locale.FormatTimestamp(instant); got != "28 Sep 2026, 17:53 EDT" {
		t.Fatalf("timestamp = %q, want viewer zone and abbreviation", got)
	}
	got := locale.FormatMoneyWithUnit("160000", "USD", "annual", 2)
	for _, want := range []string{"USD", "160,000.00", "per year"} {
		if !strings.Contains(got, want) {
			t.Fatalf("money = %q, missing %q", got, want)
		}
	}
}

func TestTodo_UXBLIND_020_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US").WithTimeZone("Asia/Tokyo")
	if got := FormatDisplayDate(locale, "2026-12-01"); got != "1 Dec 2026" {
		t.Fatalf("canonical date projection = %q", got)
	}
	if got := FormatDisplayTimestamp(locale, "28 Sep 2026, 17:53 UTC"); got != "29 Sep 2026, 02:53 JST" {
		t.Fatalf("display timestamp projection = %q", got)
	}
}

func TestTodo_UXBLIND_020_I18n(t *testing.T) {
	instant := time.Date(2026, time.September, 28, 21, 53, 0, 0, time.UTC)
	for _, code := range SupportedProductLocales() {
		locale := ResolveProductLocale(code)
		if missing := MissingProductTranslations(code); containsAny(missing, "format.pay_unit.year", "format.pay_unit.hour") {
			t.Fatalf("%s missing shared pay-unit translations: %v", code, missing)
		}
		if strings.TrimSpace(locale.FormatDate(instant)) == "" || strings.TrimSpace(locale.FormatTimestamp(instant)) == "" {
			t.Fatalf("%s returned an empty localized date or timestamp", code)
		}
		if !strings.Contains(locale.FormatMoneyWithUnit("160000", "USD", "annual", 2), "USD") {
			t.Fatalf("%s dropped the currency code", code)
		}
	}
}

func TestTodo_UXBLIND_043(t *testing.T) {
	english := ResolveProductLocale("en-US")
	german := ResolveProductLocale("de-DE")
	if english.Text("journey.stage_proposed") != "Ready to start approval" {
		t.Fatalf("English proposed status drifted: %q", english.Text("journey.stage_proposed"))
	}
	if german.Text("journey.stage_proposed") == "Vorgeschlagen" || german.Text("journey.stage_proposed") == "" {
		t.Fatalf("German proposed status is not equivalent: %q", german.Text("journey.stage_proposed"))
	}
}

func TestTodo_UXBLIND_043_Browser(t *testing.T) {
	for _, locale := range SupportedProductLocales() {
		if strings.HasPrefix(ResolveProductLocale(locale).Text("journey.stage_proposed"), "⟦") {
			t.Fatalf("%s has no journey proposed-status translation", locale)
		}
	}
}

func TestTodo_UXBLIND_043_I18n(t *testing.T) {
	keys := []string{"journey.stage_proposed", "journey.stage_awaiting_approval", "journey.stage_completed", "journey.stage_failed"}
	for _, key := range keys {
		for _, locale := range SupportedProductLocales() {
			if strings.TrimSpace(ResolveProductLocale(locale).Text(key)) == "" {
				t.Fatalf("%s has no translation for %s", locale, key)
			}
		}
	}
}

func TestTodo_UXBLIND_044(t *testing.T) {
	if got := LogicalArrow(ResolveProductLocale("en-US"), true); got != "→" {
		t.Fatalf("LTR forward arrow = %q", got)
	}
	if got := LogicalArrow(ResolveProductLocale("ar"), true); got != "←" {
		t.Fatalf("RTL forward arrow = %q", got)
	}
	if got := LogicalBackArrow(ResolveProductLocale("ar")); got != "→" {
		t.Fatalf("RTL back arrow = %q", got)
	}
}

func TestTodo_UXBLIND_044_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		copy := ResolveProductLocale(locale)
		if got := LogicalArrow(copy, true); got == LogicalBackArrow(copy) {
			t.Fatalf("%s uses one glyph for both directions", locale)
		}
	}
}

func TestTodo_UXBLIND_044_I18n(t *testing.T) {
	if ResolveProductLocale("ar").Direction != ResolveProductLocale("ar-SA").Direction {
		t.Fatal("Arabic regional tags did not retain RTL direction")
	}
}

func containsAny(values []string, wants ...string) bool {
	for _, value := range values {
		for _, want := range wants {
			if value == want {
				return true
			}
		}
	}
	return false
}
