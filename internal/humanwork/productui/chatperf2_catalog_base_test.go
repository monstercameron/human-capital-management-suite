package productui

import (
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
)

// TestTodo_CHATBUG_014_CatalogBuiltOnce: the finished catalog of a locale is
// built once and shared by the registry, the key list and the coverage report,
// with the answers each of them gave when it built its own.
func TestTodo_CHATBUG_014_CatalogBuiltOnce(t *testing.T) {
	for locale, source := range productMessages {
		shared := productCatalogBase(locale)
		if again := productCatalogBase(locale); reflect.ValueOf(again).Pointer() != reflect.ValueOf(shared).Pointer() {
			t.Fatalf("%s: the finished catalog was built a second time", locale)
		}
		if fresh := productCatalogMessages(locale, source); !reflect.DeepEqual(shared, fresh) {
			t.Fatalf("%s: the shared catalog differs from a freshly built one", locale)
		}
	}

	// The readers no longer build a catalog of their own. A build copies a few
	// thousand messages, so a reader that went back to building one would
	// allocate far more than this.
	english := DefaultProductLocale
	if allocs := testing.AllocsPerRun(3, func() { _ = buildProductCatalogCoverage(english) }); allocs > 20 {
		t.Fatalf("the English coverage report made %.0f allocations: it is building catalogs again", allocs)
	}
	if allocs := testing.AllocsPerRun(3, func() { _ = buildProductCatalogCoverage("de-DE") }); allocs > 200 {
		t.Fatalf("the German coverage report made %.0f allocations: it is building catalogs again", allocs)
	}
	if allocs := testing.AllocsPerRun(3, func() { _ = ProductCatalogKeys() }); allocs > 50 {
		t.Fatalf("the key list made %.0f allocations: it is building a catalog again", allocs)
	}

	// English is complete by definition; another locale is compared key by key.
	report := buildProductCatalogCoverage(english)
	if report.TotalKeys != len(productCatalogBase(english)) || report.TranslatedKeys != report.TotalKeys || report.FallbackKeys != 0 || len(report.MissingKeys) != 0 {
		t.Fatalf("English coverage = %+v", report)
	}
	base, localized := productCatalogMessages(english, productMessages[english]), productCatalogMessages("de-DE", productMessages["de-DE"])
	var missing []string
	for key := range base {
		if _, ok := localized[key]; !ok {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	german := buildProductCatalogCoverage("de-DE")
	if german.TotalKeys != len(base) || german.FallbackKeys != len(missing) || german.TranslatedKeys != len(base)-len(missing) || !(len(missing) == 0 && len(german.MissingKeys) == 0 || reflect.DeepEqual(german.MissingKeys, missing)) {
		t.Fatalf("German coverage = %d total, %d fallback; want %d total, %d fallback", german.TotalKeys, german.FallbackKeys, len(base), len(missing))
	}

	// A tenant's overlay goes on a copy: the shared catalog keeps its own text.
	const key = "shell.connecting"
	before := productCatalogBase(english)[key]
	merged := mergeProductCatalog(english, map[string]localize.Message{key: {Text: "tenant text"}, "tenant.only": {Text: "x"}})
	if merged[key].Text != "tenant text" || merged["tenant.only"].Text != "x" || len(merged) != len(productCatalogBase(english))+1 {
		t.Fatalf("the merged catalog lost the overlay: %q, %d keys", merged[key].Text, len(merged))
	}
	if after := productCatalogBase(english); after[key].Text != before.Text || before.Text == "" {
		t.Fatalf("the overlay wrote into the shared catalog: %q", after[key].Text)
	}
	if _, leaked := productCatalogBase(english)["tenant.only"]; leaked {
		t.Fatal("a tenant key reached the shared catalog")
	}

	// The registry still serves the same text as before.
	if got := ResolveProductLocale(english).Text(key); got != before.Text {
		t.Fatalf("registry text = %q, want %q", got, before.Text)
	}
	// A locale with no built-in catalog still gets a finished one of its own.
	if unknown := productCatalogBase("fr-FR"); reflect.ValueOf(unknown).Pointer() == reflect.ValueOf(productCatalogBase(english)).Pointer() {
		t.Fatal("an unknown locale was given the English catalog itself")
	}
}

// TestTodo_CHATBUG_014_LocaleMenuDoesNotBuildOtherCatalogs: the language menu
// in every page's header names each language without registering that
// language's catalog, and names it exactly as it did.
func TestTodo_CHATBUG_014_LocaleMenuDoesNotBuildOtherCatalogs(t *testing.T) {
	// The answer is what asking the language itself gives.
	for _, code := range SupportedProductLocales() {
		candidate := ResolveProductLocale(code)
		if got, want := productLocaleOwnLabel(candidate, code), candidate.Text(productLocaleLabelKey(code)); got != want || got == "" {
			t.Fatalf("%s is listed as %q, want %q", code, got, want)
		}
	}

	// A registry that has registered nothing yet shows what a page's first
	// render pays for.
	saved := productMessageRegistry
	productMessageRegistry = newProductCatalogRegistry()
	t.Cleanup(func() { productMessageRegistry = saved })

	view := ApplyLocale(NewView(PageChat, "Tenant", "Reader", ""), ResolveProductLocale("en-US"))
	props := localePreferencesProps(view)
	labels := map[string]string{}
	for _, option := range props.Options {
		labels[option.Code] = option.Label
	}
	if labels["en-US"] != "English (US)" || labels["de-DE"] != "Deutsch" || labels["ar"] != "العربية" {
		t.Fatalf("language menu labels = %v", labels)
	}
	if !productMessageRegistry.registered("en-US", productCatalogVersion) {
		t.Fatal("the English page did not register the English catalog")
	}
	for _, other := range []string{"de-DE", "ar"} {
		if productMessageRegistry.registered(other, productCatalogVersion) {
			t.Fatalf("an English page's language menu registered the %s catalog", other)
		}
	}

	// A tenant catalog that does name a language is still asked.
	german := ResolveProductLocale("de-DE")
	german.CatalogVersion = "activated.fixture"
	if err := productMessageRegistry.Register(localize.Catalog{Locale: "de-DE", Version: "activated.fixture", Messages: map[string]localize.Message{"shell.locale_de": {Text: "Deutsch (Mandant)"}}}); err != nil {
		t.Fatal(err)
	}
	if got := productLocaleOwnLabel(german, "de-DE"); got != "Deutsch (Mandant)" {
		t.Fatalf("an activated catalog's own name was not used: %q", got)
	}
}

// TestTodo_CHATBUG_014_NavigationTextCheck: the navigation label check decides
// ASCII characters without the Unicode tables and still refuses exactly the
// characters it refused: every control and format character.
func TestTodo_CHATBUG_014_NavigationTextCheck(t *testing.T) {
	for character := rune(0); character < 0x3000; character++ {
		if !utf8.ValidRune(character) {
			continue
		}
		text := "a" + string(character) + "b"
		want := !unicode.IsControl(character) && !unicode.Is(unicode.Cf, character)
		if got := validNavigationText(text, true); got != want {
			t.Fatalf("a label holding U+%04X is accepted = %v, want %v", character, got, want)
		}
	}
	for text, want := range map[string]bool{
		"People": true, "Mein Arbeitsbereich": true, "الدردشة": true, "Docs & Files (2)": true,
		"": false, " padded": false, "trailing ": false, "tab\there": false, "line\nbreak": false, "del\x7f": false,
		"soft­hyphen": false, "zero​width": false, "bidi‮override": false, "bad\xffbytes": false,
		strings.Repeat("x", maxAuthorizedNavigationText): true, strings.Repeat("x", maxAuthorizedNavigationText+1): false,
	} {
		if got := validNavigationText(text, true); got != want {
			t.Errorf("validNavigationText(%q) = %v, want %v", text, got, want)
		}
	}
	if !validNavigationText("", false) {
		t.Error("an optional label may be empty")
	}
}
