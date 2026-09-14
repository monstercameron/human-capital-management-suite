package workspace

import (
	"reflect"
	"strings"
	"testing"
)

// TestTodo_UX_004 is the PRIMARY proof for UX-004. It uses the pilot's real
// corpus seed and shows that locale formatting is a reversible presentation
// operation before the canonical request reaches the governed workspace.
func TestTodo_UX_004(t *testing.T) {
	seed, err := DefaultQuery()
	if err != nil {
		t.Fatalf("DefaultQuery: %v", err)
	}
	locale := ResolveLocale("de-DE")
	if locale.Fallback != LocaleFallbackNone || locale.Resolved != "de-DE" {
		t.Fatalf("ResolveLocale(de-DE) = %+v", locale)
	}

	money, err := FormatCurrency(locale, seed.ProposedBase, seed.Currency)
	if err != nil {
		t.Fatalf("FormatCurrency: %v", err)
	}
	amount, err := ParseCurrency(locale, money, seed.Currency)
	if err != nil {
		t.Fatalf("ParseCurrency(%q): %v", money, err)
	}
	if amount != seed.ProposedBase {
		t.Errorf("currency round trip = %q, want fixture amount %q", amount, seed.ProposedBase)
	}

	date, err := FormatDate(locale, seed.EffectiveDate)
	if err != nil {
		t.Fatalf("FormatDate: %v", err)
	}
	canonicalDate, err := ParseDate(locale, date)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", date, err)
	}
	if canonicalDate != seed.EffectiveDate {
		t.Errorf("date round trip = %q, want fixture date %q", canonicalDate, seed.EffectiveDate)
	}

	answers, err := CanonicalizePresentationAnswers(locale, seed.Currency, map[string]string{
		FieldProposedComp:   money,
		FieldEffectiveDate:  date,
		FieldBusinessReason: seed.BusinessReason,
	})
	if err != nil {
		t.Fatalf("CanonicalizePresentationAnswers: %v", err)
	}
	if answers[FieldProposedComp] != seed.ProposedBase || answers[FieldEffectiveDate] != seed.EffectiveDate {
		t.Errorf("localized answer normalization = %#v; want canonical fixture values", answers)
	}
}

// TestTodo_UX_004_Golden fixes the reviewed presentation vectors. Named
// month date formats avoid ambiguous numeric day/month interpretation.
func TestTodo_UX_004_Golden(t *testing.T) {
	cases := []struct {
		locale, number, money, date string
	}{
		{"en-US", "1,234,567.50", "USD 1,234,567.50", "September 03, 2026"},
		{"de-DE", "1.234.567,50", "1.234.567,50 USD", "03. September 2026"},
		{"ar", "1,234,567.50", "USD 1,234,567.50", "03 سبتمبر 2026"},
	}
	for _, tc := range cases {
		t.Run(tc.locale, func(t *testing.T) {
			locale := ResolveLocale(tc.locale)
			if got, err := FormatNumber(locale, "1234567.50"); err != nil || got != tc.number {
				t.Fatalf("FormatNumber = %q, %v; want %q", got, err, tc.number)
			}
			if got, err := FormatCurrency(locale, "1234567.50", "USD"); err != nil || got != tc.money {
				t.Fatalf("FormatCurrency = %q, %v; want %q", got, err, tc.money)
			}
			if got, err := FormatDate(locale, "2026-09-03"); err != nil || got != tc.date {
				t.Fatalf("FormatDate = %q, %v; want %q", got, err, tc.date)
			}
			if got, err := ParseNumber(locale, tc.number); err != nil || got != "1234567.50" {
				t.Fatalf("ParseNumber = %q, %v", got, err)
			}
		})
	}
	if got, diagnostic := Translate(ResolveLocale("de-DE"), "field.current_base", "Current base pay"); diagnostic != nil || got != "Aktuelles Grundgehalt" {
		t.Errorf("known German translation = %q, %+v", got, diagnostic)
	}
	arabic := ResolveLocale("ar")
	if arabic.Resolved != "ar" || arabic.Fallback != LocaleFallbackNone {
		t.Fatalf("ResolveLocale(ar) = %+v", arabic)
	}
	if got, diagnostic := Translate(arabic, "action.submit_for_approval", "Submit for approval"); diagnostic != nil || got != "إرسال للموافقة" {
		t.Errorf("known Arabic translation = %q, %+v", got, diagnostic)
	}
}

// TestTodo_UX_004_Fault proves the parser refuses cross-locale punctuation,
// numeric day/month forms, and malformed currency instead of guessing.
func TestTodo_UX_004_Fault(t *testing.T) {
	for _, tc := range []struct {
		name string
		fn   func() error
	}{
		{"en number with German punctuation", func() error { _, err := ParseNumber(ResolveLocale("en-US"), "1.234,50"); return err }},
		{"de number with English punctuation", func() error { _, err := ParseNumber(ResolveLocale("de-DE"), "1,234.50"); return err }},
		{"un-grouped hybrid number", func() error { _, err := ParseNumber(ResolveLocale("en-US"), "12,34.50"); return err }},
		{"numeric day month", func() error { _, err := ParseDate(ResolveLocale("en-US"), "03/09/2026"); return err }},
		{"wrong currency position", func() error { _, err := ParseCurrency(ResolveLocale("de-DE"), "USD 1.234,50", "USD"); return err }},
		{"localized date in wrong locale", func() error { _, err := ParseDate(ResolveLocale("de-DE"), "September 03, 2026"); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fn(); err == nil {
				t.Fatal("ambiguous or malformed presentation was accepted")
			}
		})
	}
}

// TestTodo_UX_004_Security fixes the authority boundary: a locale can select
// no legal or governed context, and unsupported input is visibly defaulted.
func TestTodo_UX_004_Security(t *testing.T) {
	for structField := range reflect.TypeFor[LocaleContext]().Fields() {
		field := strings.ToLower(structField.Name)
		for _, forbidden := range []string{"legal", "jurisdiction", "authority", "country", "region", "policy", "payroll", "tax"} {
			if strings.Contains(field, forbidden) {
				t.Errorf("LocaleContext field %q grants or implies forbidden authority %q", field, forbidden)
			}
		}
	}
	unsupported := ResolveLocale("x-tenant-admin")
	if unsupported.Resolved != DefaultLocale || unsupported.Fallback != LocaleFallbackUnsupported {
		t.Fatalf("unsupported locale was silently accepted: %+v", unsupported)
	}
	value, diagnostic := Translate(unsupported, "missing.key", "English fallback")
	if diagnostic == nil || !strings.Contains(value, "[missing translation: missing.key]") {
		t.Fatalf("missing translation was hidden: value=%q diagnostic=%+v", value, diagnostic)
	}
	doc := localizeDocument(`<html lang="en"><main id="main-content"></main>`, unsupported, Query{EffectiveDate: "2026-09-03"}, []TranslationDiagnostic{*diagnostic})
	for _, want := range []string{`data-locale-fallback="unsupported_locale"`, `data-l10n-missing="missing.key"`, "using en-US"} {
		if !strings.Contains(doc, want) {
			t.Errorf("localized diagnostic document does not report %q: %s", want, doc)
		}
	}
}

// FuzzTodo_UX_004 is the FUZZ matrix proof. It accepts arbitrary untrusted
// locale/value text and asserts the formatter/parser boundary never panics;
// every value emitted from valid canonical data round-trips exactly.
func FuzzTodo_UX_004(f *testing.F) {
	f.Add("en-US", "1234567.50", "2026-09-03", "USD")
	f.Add("de-DE", "-0.25", "2024-02-29", "EUR")
	f.Add("x-unknown", "not-a-number", "03/09/2026", "usd")
	f.Fuzz(func(t *testing.T, requested, number, date, currency string) {
		locale := ResolveLocale(requested)
		formatted, err := FormatNumber(locale, number)
		if err == nil {
			got, parseErr := ParseNumber(locale, formatted)
			if parseErr != nil {
				t.Fatalf("ParseNumber(FormatNumber(%q)) = %v", number, parseErr)
			}
			negative, whole, fraction, canonicalErr := canonicalNumber(number)
			if canonicalErr != nil {
				t.Fatalf("FormatNumber accepted non-canonical %q", number)
			}
			want := sign(negative) + whole + decimalSuffix(fraction, ".")
			if got != want {
				t.Fatalf("number round trip = %q, want %q", got, want)
			}
		}
		formattedDate, dateErr := FormatDate(locale, date)
		if dateErr == nil {
			got, parseErr := ParseDate(locale, formattedDate)
			if parseErr != nil || got != date {
				t.Fatalf("date round trip = %q, %v; want %q", got, parseErr, date)
			}
		}
		_, _ = FormatCurrency(locale, number, currency)
		_, _ = CanonicalizePresentationAnswers(locale, currency, map[string]string{
			FieldProposedComp:  number,
			FieldEffectiveDate: date,
		})
	})
}
