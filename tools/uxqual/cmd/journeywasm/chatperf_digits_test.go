//go:build !(js && wasm)

package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_CHATBUG_014_Digits: a clock or a count is written in the locale's
// numerals exactly as before, without asking the number formatter for the ten
// numerals again each time.
func TestTodo_CHATBUG_014_Digits(t *testing.T) {
	for _, tag := range productui.SupportedProductLocales() {
		locale := productui.ResolveProductLocale(tag)
		// What the formatter itself says, digit by digit.
		var want strings.Builder
		for _, r := range "09:41 PM 1,234" {
			if r >= '0' && r <= '9' {
				want.WriteString(locale.FormatNumber(string(r), 0))
				continue
			}
			want.WriteRune(r)
		}
		if got := localizeDigits("09:41 PM 1,234", locale); got != want.String() {
			t.Errorf("%s: localizeDigits = %q, want %q", tag, got, want.String())
		}
		// A context the formatter would resolve for itself reads the same table.
		if got := localizeDigits("7", productui.LocaleContext{Requested: tag}); got != locale.FormatNumber("7", 0) {
			t.Errorf("%s: an unresolved context wrote %q", tag, got)
		}
	}
	if got := localizeDigits("12:05", productui.ResolveProductLocale("ar")); got == "12:05" {
		t.Fatal("Arabic kept Latin digits: the table is not the locale's own")
	}
	if got := chatNumberFormatter("en-US")(0); got != "" {
		t.Fatalf("a count of zero is written %q, want nothing", got)
	}

	// The formatter is not asked again once a locale's numerals are known: a
	// label in a locale that keeps Latin digits costs no allocation at all, and
	// one that rewrites them costs only the string it builds.
	english, arabic := productui.ResolveProductLocale("en-US"), productui.ResolveProductLocale("ar")
	localizeDigits("1", english)
	localizeDigits("1", arabic)
	if allocs := testing.AllocsPerRun(50, func() { localizeDigits("3:04 PM", english) }); allocs != 0 {
		t.Fatalf("an English clock costs %.0f allocations; the numerals are being worked out again", allocs)
	}
	if allocs := testing.AllocsPerRun(50, func() { localizeDigits("15:04", arabic) }); allocs > 4 {
		t.Fatalf("an Arabic clock costs %.0f allocations; the numerals are being worked out again", allocs)
	}
}
