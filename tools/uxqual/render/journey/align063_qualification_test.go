package journey_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

func renderALIGN063(t *testing.T, page journey.Page) string {
	t.Helper()
	doc, err := journey.Document(page)
	if err != nil {
		t.Fatalf("journey.Document: %v", err)
	}
	return doc
}

// TestTodo_ALIGN_063 is the primary qualification: all three journey states
// must remain accessible and preserve the projector's already-localized
// strings. Locale metadata is checked separately in the conformance test so
// a missing document attribute does not hide value, naming, or motion data.
func TestTodo_ALIGN_063(t *testing.T) {
	for name, page := range map[string]journey.Page{
		"list":      journey.SampleListPage(),
		"detail":    journey.SampleDetailPage(),
		"completed": journey.SampleCompletedDetailPage(),
	} {
		t.Run(name, func(t *testing.T) {
			doc := renderALIGN063(t, page)
			if got := qual.CheckAssistiveNames(doc); !got.Pass {
				t.Errorf("assistive names: %s", got.Detail)
			}
			if got := qual.CheckResponsiveAtWidths(qual.ExtractInlineCSS(doc), 1440, 390, 320); !got.Pass {
				t.Errorf("responsive widths: %s", got.Detail)
			}
			if got := qual.CheckReducedMotion(qual.ExtractInlineCSS(doc)); !got.Pass {
				t.Errorf("reduced motion: %s", got.Detail)
			}
		})
	}
}

// TestTodo_ALIGN_063_Property proves long translated copy cannot turn into
// markup or disappear at a narrow viewport. The renderer owns escaping while
// CSS owns wrapping, so this test exercises both contracts together.
func TestTodo_ALIGN_063_Property(t *testing.T) {
	page := journey.SampleListPage()
	long := strings.Repeat("Sehr lange Beschreibung für die Prüfung und Genehmigung; ", 28) + "<script>alert('x')</script>"
	page.Title = long
	page.TenantLabel = long
	page.List.Empty = long
	page.Notice = nil
	page.List.Form.Fields[len(page.List.Form.Fields)-1].Value = long
	doc := renderALIGN063(t, page)
	if !strings.Contains(doc, "&lt;script&gt;alert(&#39;x&#39;)&lt;/script&gt;") {
		t.Error("long text was not escaped as text content")
	}
	if strings.Contains(doc, "<script>") {
		t.Error("long text created an executable script element")
	}
	if got := qual.CheckResponsiveAtWidths(qual.ExtractInlineCSS(doc), 1440, 390, 320); !got.Pass {
		t.Fatalf("long text lacks reflow protections: %s", got.Detail)
	}
}

// TestTodo_ALIGN_063_Golden pins the display forms that must survive a
// locale-aware projector: German punctuation and non-breaking spacing are
// data, not renderer-owned numbers to parse or normalize.
func TestTodo_ALIGN_063_Golden(t *testing.T) {
	page := journey.SampleListPage()
	page.Title = "Beförderungsreisen · Northwind People"
	page.TenantLabel = "Northwind Trading · DE"
	for i := range page.List.Journeys {
		page.List.Journeys[i].PayLine = "93.000,00 € → 98.000,00 € (+5,4 %)"
		page.List.Journeys[i].EffectiveDate = "1. Juni 2026"
		page.List.Journeys[i].Updated = "12. Mai 2026, 09:12 UTC"
	}
	page.List.Journeys[0].StageLabel = "Genehmigung ausstehend"
	for i := range page.List.People.Workers {
		page.List.People.Workers[i].PayLine = "93.000,00 €"
		page.List.People.Workers[i].HireDate = "1. Juni 2026"
	}
	doc := renderALIGN063(t, page)
	values := map[string]string{
		"money":   "93.000,00 € → 98.000,00 €",
		"percent": "+5,4 %",
		"date":    "1. Juni 2026",
		"stage":   "Genehmigung ausstehend",
	}
	if got := qual.CheckLocalizedValues(doc, values); !got.Pass {
		t.Fatalf("localized values changed or disappeared: %s", got.Detail)
	}
	if strings.Contains(doc, "USD 93,000.00") {
		t.Error("German money fixture retained the US currency rendering")
	}
}

// TestTodo_ALIGN_063_Security ensures translated labels and bidi text remain
// escaped and cannot smuggle attributes or executable elements into the DOM.
func TestTodo_ALIGN_063_Security(t *testing.T) {
	page := journey.SampleListPage()
	page.Title = "مراجعة الزيادة 2068(ar-SA)"
	page.List.Journeys[0].WorkerName = "نور حداد 2069"
	page.List.Journeys[0].PayLine = "93٬000٫00 د.إ → 98٬000٫00 د.إ (+5٫4 ٪)"
	doc := renderALIGN063(t, page)
	if got := qual.CheckLocalizedValues(doc, map[string]string{
		"bidi title":  page.Title,
		"bidi worker": page.List.Journeys[0].WorkerName,
		"arabic pay":  page.List.Journeys[0].PayLine,
	}); !got.Pass {
		t.Fatalf("RTL strings were not preserved: %s", got.Detail)
	}
	if strings.Contains(doc, "<script") || strings.Contains(doc, " onerror=") {
		t.Error("localized content introduced executable markup")
	}
	page.Locale = `ar-SA\"><script>alert(1)</script>`
	doc = renderALIGN063(t, page)
	if strings.Contains(doc, "<script>") || strings.Contains(doc, `lang="ar-SA"><script>alert`) {
		t.Error("locale metadata escaped into an executable element")
	}
}

// TestTodo_ALIGN_063_Integration scores every real journey document through
// the shared qualification helpers, keeping the three layouts on one matrix.
func TestTodo_ALIGN_063_Integration(t *testing.T) {
	pages := []journey.Page{journey.SampleListPage(), journey.SampleDetailPage(), journey.SampleCompletedDetailPage()}
	for i, page := range pages {
		doc := renderALIGN063(t, page)
		if got := qual.CheckAssistiveNames(doc); !got.Pass {
			t.Errorf("page %d assistive names: %s", i, got.Detail)
		}
		if got := qual.CheckResponsiveAtWidths(qual.ExtractInlineCSS(doc), 1440, 390, 320); !got.Pass {
			t.Errorf("page %d responsive matrix: %s", i, got.Detail)
		}
	}
}

// TestTodo_ALIGN_063_Fault proves that the qualification itself fails closed
// for missing language/direction, unnamed actions, and motion overrides.
func TestTodo_ALIGN_063_Fault(t *testing.T) {
	if got := qual.CheckDocumentLocale(`<html lang="en"><body><main></main></body></html>`, "ar-SA", "rtl"); got.Pass {
		t.Fatal("missing RTL metadata passed qualification")
	}
	if got := qual.CheckAssistiveNames(`<html><body><main><button></button></main></body></html>`); got.Pass {
		t.Fatal("unnamed button passed qualification")
	}
	if got := qual.CheckReducedMotion(`.loading{animation:spin 1s}`); got.Pass {
		t.Fatal("unbounded motion passed qualification")
	}
}

// TestTodo_ALIGN_063_Conformance records the production metadata gap. The
// journey renderer currently emits a generic English document root, so it
// cannot truthfully claim en-US, de-DE, or RTL document semantics. Keep this
// assertion visible until the serving contract supplies locale and direction.
func TestTodo_ALIGN_063_Conformance(t *testing.T) {
	for name, wantLanguage := range map[string]string{"en-US": "en-US", "de-DE": "de-DE", "ar-SA": "ar-SA"} {
		t.Run(name, func(t *testing.T) {
			page := journey.SampleListPage()
			page.Locale = name
			wantDirection := "ltr"
			if name == "ar-SA" {
				wantDirection = "rtl"
			}
			if name == "ar-SA" {
				page.Title = "مراجعة الزيادة"
			}
			doc := renderALIGN063(t, page)
			if got := qual.CheckDocumentLocale(doc, wantLanguage, wantDirection); !got.Pass {
				t.Errorf("locale metadata: %s", got.Detail)
			}
		})
	}
}

func TestTodo_ALIGN_063_DocumentIdentityDefaultsAndCanonicalizes(t *testing.T) {
	cases := []struct {
		name, locale, direction, want string
	}{
		{name: "default", want: `<html lang="en-US" dir="ltr">`},
		{name: "underscore locale", locale: "EN_us", want: `<html lang="en-US" dir="ltr">`},
		{name: "arabic inferred", locale: "ar-SA", want: `<html lang="ar-SA" dir="rtl">`},
		{name: "explicit override", locale: "ar-SA", direction: "ltr", want: `<html lang="ar-SA" dir="ltr">`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page := journey.SampleListPage()
			page.Locale, page.Direction = tc.locale, tc.direction
			doc := renderALIGN063(t, page)
			if !strings.Contains(doc, tc.want) {
				t.Fatalf("document identity missing %q", tc.want)
			}
		})
	}
}
