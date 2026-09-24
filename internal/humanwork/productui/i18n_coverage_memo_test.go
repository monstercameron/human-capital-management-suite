package productui

import (
	"reflect"
	"testing"
)

// TestProductCatalogCoverageMemoDoesNotRebuild pins the memo path: once a
// supported locale's report is built, later calls return the same report
// without rebuilding the English and localized catalogs. The client asks for
// the report on every route render, so a rebuild per call is a hot-path cost.
func TestProductCatalogCoverageMemoDoesNotRebuild(t *testing.T) {
	for _, locale := range SupportedProductLocales() {
		want := buildProductCatalogCoverage(locale)
		first := ProductCatalogCoverage(locale)
		// The defensive copy returns nil for an empty list; compare contents.
		if len(first.MissingKeys) == 0 && len(want.MissingKeys) == 0 {
			first.MissingKeys, want.MissingKeys = nil, nil
		}
		if !reflect.DeepEqual(first, want) {
			t.Fatalf("%s: memoized report = %+v, want %+v", locale, first, want)
		}

		buildAllocs := testing.AllocsPerRun(5, func() { buildProductCatalogCoverage(locale) })
		memoAllocs := testing.AllocsPerRun(20, func() { ProductCatalogCoverage(locale) })
		// A rebuild allocates two catalog maps with an entry per key; the memo
		// path allocates at most the caller's MissingKeys copy plus the locale
		// resolution.
		if memoAllocs*10 > buildAllocs {
			t.Fatalf("%s: second call allocated %.0f times against %.0f for a full build; the memo is not being used", locale, memoAllocs, buildAllocs)
		}
	}
}

// TestProductCatalogCoverageCopiesMissingKeys keeps the memoized report
// immutable from the caller's side.
func TestProductCatalogCoverageCopiesMissingKeys(t *testing.T) {
	var locale string
	var want []string
	for _, candidate := range SupportedProductLocales() {
		if missing := ProductCatalogCoverage(candidate).MissingKeys; len(missing) > 0 {
			locale, want = candidate, append([]string(nil), missing...)
			break
		}
	}
	if locale == "" {
		t.Skip("every supported locale is fully translated; nothing to mutate")
	}
	ProductCatalogCoverage(locale).MissingKeys[0] = "mutated.by.caller"
	if got := ProductCatalogCoverage(locale).MissingKeys; !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: caller mutation leaked into the memo: got %v, want %v", locale, got[:1], want[:1])
	}
}

// BenchmarkProductCatalogCoverage compares a warm memo lookup with a full
// build. The Memoized case should be orders of magnitude cheaper.
func BenchmarkProductCatalogCoverage(b *testing.B) {
	for _, locale := range SupportedProductLocales() {
		ProductCatalogCoverage(locale) // warm the memo
		b.Run("Memoized/"+locale, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				ProductCatalogCoverage(locale)
			}
		})
		b.Run("Build/"+locale, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				buildProductCatalogCoverage(locale)
			}
		})
	}
}
