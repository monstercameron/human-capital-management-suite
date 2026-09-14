package productui

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestProductCatalogCoverageIsStableAndExplicit keeps the product catalog
// audit reusable for every locale. Partial translation is allowed during a
// rollout, but it must be deterministic, sorted, and observable rather than
// silently turning into an empty accessible name.
func TestProductCatalogCoverageIsStableAndExplicit(t *testing.T) {
	keys := ProductCatalogKeys()
	if len(keys) == 0 {
		t.Fatal("product catalog has no reviewed English keys")
	}
	if !sort.StringsAreSorted(keys) {
		t.Fatal("product catalog keys are not sorted")
	}
	for _, locale := range SupportedProductLocales() {
		coverage := ProductCatalogCoverage(locale)
		if coverage.Locale != locale || coverage.CatalogVersion != productCatalogVersion || coverage.Fallback != LocaleFallbackNone {
			t.Fatalf("coverage identity = %+v, want locale %q and version %q", coverage, locale, productCatalogVersion)
		}
		if coverage.TotalKeys != len(keys) || coverage.TranslatedKeys+coverage.FallbackKeys != coverage.TotalKeys {
			t.Fatalf("coverage totals inconsistent for %s: %+v", locale, coverage)
		}
		if !sort.StringsAreSorted(coverage.MissingKeys) {
			t.Fatalf("missing keys are not sorted for %s", locale)
		}
		if len(coverage.MissingKeys) != coverage.FallbackKeys {
			t.Fatalf("missing-key count = %d, fallback count = %d for %s", len(coverage.MissingKeys), coverage.FallbackKeys, locale)
		}
	}
}

func TestProductLocaleAliasesPreserveLanguageAndDirection(t *testing.T) {
	tests := []struct {
		requested, resolved string
		direction           string
	}{
		{requested: "en", resolved: "en-US", direction: "ltr"},
		{requested: "de", resolved: "de-DE", direction: "ltr"},
		{requested: "de-AT", resolved: "de-DE", direction: "ltr"},
		{requested: "ar-EG", resolved: "ar", direction: "rtl"},
	}
	for _, tt := range tests {
		got := ResolveProductLocale(tt.requested)
		if got.Resolved != tt.resolved || string(got.Direction) != tt.direction || got.Fallback != LocaleFallbackNone {
			t.Errorf("ResolveProductLocale(%q) = %+v", tt.requested, got)
		}
	}
	unsupported := ResolveProductLocale("en-GB")
	if unsupported.Resolved != DefaultProductLocale || unsupported.Fallback != LocaleFallbackUnsupported {
		t.Fatalf("unsupported locale did not use explicit fallback: %+v", unsupported)
	}
	unsupported = ResolveProductLocale("fr-FR")
	if unsupported.Resolved != DefaultProductLocale || unsupported.Fallback != LocaleFallbackUnsupported {
		t.Fatalf("unsupported language did not use explicit fallback: %+v", unsupported)
	}
}

func TestProductTextResolutionDistinguishesFallbackAndUnknown(t *testing.T) {
	locale := ResolveProductLocale("de-DE")
	coverage := ProductCatalogCoverage("de-DE")
	if len(coverage.MissingKeys) == 0 {
		t.Skip("German catalog is complete; no fallback case to exercise")
	}

	key := coverage.MissingKeys[0]
	result, err := locale.Resolve(key)
	if err != nil {
		t.Fatalf("fallback resolution failed for %q: %v", key, err)
	}
	if result.Locale != DefaultProductLocale || result.FallbackPath != "de-DE -> "+DefaultProductLocale || result.Text == "" {
		t.Fatalf("fallback result = %+v", result)
	}
	if got := locale.Text(key); got != result.Text {
		t.Fatalf("Text(%q) = %q, Resolve = %q", key, got, result.Text)
	}

	unknown := "audit.unknown.message"
	if _, err := locale.Resolve(unknown); err == nil || !strings.Contains(err.Error(), unknown) {
		t.Fatalf("unknown key error = %v", err)
	}
	if got := locale.Text(unknown); got != "⟦"+unknown+"⟧" {
		t.Fatalf("unknown key marker = %q", got)
	}
}

func TestProductCatalogMessagesHaveRenderableValues(t *testing.T) {
	for locale, messages := range productMessages {
		for key, message := range messages {
			if strings.TrimSpace(message.Text) == "" && len(message.Plural) == 0 && len(message.Gender) == 0 {
				t.Errorf("%s catalog key %q has no renderable value", locale, key)
			}
		}
	}
	if got := ResolveProductLocale("ar").Direction; got != "rtl" {
		t.Fatalf("Arabic direction = %q", got)
	}
	if got := ProductCatalogKeys(); !reflect.DeepEqual(got, ProductCatalogKeys()) {
		t.Fatal("ProductCatalogKeys is not deterministic")
	}
}

func TestOrdinaryAvailabilityCopyUsesTaskLanguageButDiagnosticsStayPrecise(t *testing.T) {
	ordinaryKeys := []string{
		"headcount.unavailable_detail", "page.organization.subtitle",
		"organization.metadata_boundary", "organization.empty_description",
		"work.server_proposal", "person.employment_detail",
	}
	for _, locale := range SupportedProductLocales() {
		context := ResolveProductLocale(locale)
		for _, key := range ordinaryKeys {
			result, err := context.Resolve(key)
			if err != nil {
				t.Fatalf("%s %s: %v", locale, key, err)
			}
			lower := strings.ToLower(result.Text)
			for _, jargon := range []string{"server authority", "journeyservice", "live cell", "listworkers"} {
				if strings.Contains(lower, jargon) {
					t.Errorf("ordinary %s copy retains %q: %q", key, jargon, result.Text)
				}
			}
		}
	}
	availability, err := ResolveProductLocale("en-US").Resolve("admin.journeys_unavailable_reason")
	if err != nil || !strings.Contains(strings.ToLower(availability.Text), "try refreshing") {
		t.Fatalf("availability copy gives no safe recovery status: %+v, %v", availability, err)
	}
}
