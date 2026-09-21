package productui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductLocaleLocalizesShellNavigationAndComponents(t *testing.T) {
	view := ApplyRequest(testView(PagePeople), PageRequest{Locale: "de_de", FavoritePages: []PageID{PagePeople}})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<html lang="de-DE" dir="ltr"`, `data-hcm-catalog="product-ui.v1"`,
		`data-hcm-message-fallback="en-US"`,
		`>Mitarbeitende</`, `placeholder="Personen, Seiten, Workflows und Einstellungen suchen"`,
		`>Favoriten</li>`, `>Mitarbeitende suchen</label>`, `>Führungskraft</a>`, `>Alle Teams</option>`,
		`locale=de-DE`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("German product document missing %q", want)
		}
	}
	if strings.Contains(doc, "⟦") {
		t.Fatalf("German product document exposed an unknown message key: %s", doc)
	}
}

func TestDefaultProductLocaleDoesNotReportMessageFallback(t *testing.T) {
	if missing := MissingProductTranslations(DefaultProductLocale); len(missing) != 0 {
		t.Fatalf("English source catalogue has %d missing keys", len(missing))
	}
	doc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, `data-hcm-message-fallback=`) {
		t.Fatal("source-language document reports a translation fallback")
	}
}

func TestProductLocaleSetsRTLAndReportsUnsupportedFallback(t *testing.T) {
	rtl, err := Render(ApplyRequest(testView(PageHome), PageRequest{Locale: "ar"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`lang="ar"`, `dir="rtl"`, `>الرئيسية</`, `aria-label="اللغة"`} {
		if !strings.Contains(rtl, want) {
			t.Fatalf("RTL document missing %q", want)
		}
	}

	unsupported, err := Render(ApplyRequest(testView(PageHome), PageRequest{Locale: "zz-ZZ"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`lang="en-US"`, `data-hcm-locale-fallback="unsupported_locale"`, `data-hcm-requested-locale="zz-ZZ"`} {
		if !strings.Contains(unsupported, want) {
			t.Fatalf("unsupported-locale document missing %q", want)
		}
	}
}

func TestProductLocaleFormatsExactMoneyWithoutBinaryFloatingPoint(t *testing.T) {
	locale := ResolveProductLocale("de-DE")
	if got := locale.FormatMoney("1234.50", "USD", 2); got != "1.234,50 USD" {
		t.Fatalf("FormatMoney() = %q", got)
	}
	if got := locale.FormatNumber("1234567.5", 2); got != "1.234.567,50" {
		t.Fatalf("FormatNumber() = %q", got)
	}
	english := ResolveProductLocale("en-US")
	if got := english.FormatMoney("165000.00", "USD", 2); got != "USD 165,000.00" {
		t.Fatalf("English FormatMoney() = %q", got)
	}
	if got := percentage(english, "0.1800"); got != "18%" {
		t.Fatalf("percentage() = %q", got)
	}
	if got := percentage(locale, "0.125"); got != "12,5\u00a0%" {
		t.Fatalf("localized percentage() = %q", got)
	}
}

func TestComponentOwnedCopyCannotBypassI18n(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	allowedGlyphs := []string{`ui.Text("")`, `ui.Text("✓")`, `ui.Text("›")`, `ui.Text("●")`, `ui.Text("↗")`, `ui.Text("⌕")`, `ui.Text("—")`}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_components.go") {
			continue
		}
		body, readErr := os.ReadFile(filepath.Clean(entry.Name()))
		if readErr != nil {
			t.Fatal(readErr)
		}
		text := string(body)
		for _, allowed := range allowedGlyphs {
			text = strings.ReplaceAll(text, allowed, "")
		}
		if strings.Contains(text, `ui.Text("`) {
			t.Fatalf("%s owns untranslated visible copy; pass it through I18nProps or localized props", entry.Name())
		}
		if strings.Contains(text, `"aria-label": "`) || strings.Contains(text, `"placeholder": "`) {
			t.Fatalf("%s owns an untranslated accessible name or placeholder", entry.Name())
		}
	}
}

func TestEveryRegisteredPageHasCatalogKeys(t *testing.T) {
	locale := ResolveProductLocale(DefaultProductLocale)
	for _, page := range PageDefinitions() {
		for _, key := range []string{page.LabelKey, page.TitleKey, page.SubtitleKey} {
			if key == "" || strings.HasPrefix(locale.Text(key), "⟦") {
				t.Errorf("page %s has unresolved message key %q", page.ID, key)
			}
		}
	}
}
