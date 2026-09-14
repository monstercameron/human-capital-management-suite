package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-217: the report catalog. The registry
// owns every product surface, but no report catalog
// exists: browsing governed reports has no exposure
// point and the first surface invents catalog data by
// convention. The compiler needs the registered surface
// — canonical identity, route, and an honest fallback
// that catalogs nothing until the governed reporting
// service publishes, with report truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_217(t *testing.T) {
	definition, ok := LookupPage(PageReportCatalog)
	if !ok {
		t.Fatal("report catalog unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("report catalog incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageReportCatalog {
		t.Fatal("report catalog route does not round-trip")
	}
	doc, err := Render(testView(PageReportCatalog))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("report catalog exposes an unresolved message key")
	}
	for _, invented := range []string{"headcount report ready", "42 curated reports", "refreshed hourly", "catalog ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("report catalog invents catalog data: %q", invented)
		}
	}
}

// Golden: the registered report catalog definition
// and its fallback copy.
func TestTodo_WEB_217_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageReportCatalog)
	if !ok {
		t.Fatal("report catalog unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("report_catalog.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("report_catalog.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "65e65694105f207bbef30b939f1883ec937cbbe26931b24504662511a959bc99"
	if got != want {
		t.Fatalf("report catalog digest = %s, want %s", got, want)
	}
}

// Browser: report catalog renders deterministically
// and round-trips its route.
func TestTodo_WEB_217_Browser(t *testing.T) {
	first, err := Render(testView(PageReportCatalog))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageReportCatalog))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("report catalog renders nondeterministically")
	}
	definition, _ := LookupPage(PageReportCatalog)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageReportCatalog {
		t.Fatal("report catalog route does not round-trip")
	}
}

// Conformance: report catalog keeps the registry
// contract — visible to the manager, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_217_Conformance(t *testing.T) {
	if !PageVisible(PageReportCatalog, []string{"manager"}) {
		t.Fatal("report catalog hidden from the manager")
	}
	if PageVisible(PageReportCatalog, nil) {
		t.Fatal("report catalog visible without roles")
	}
	definition, _ := LookupPage(PageReportCatalog)
	if definition.PrimaryNav {
		t.Fatal("report catalog claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageReportCatalog), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("report catalog leaks a key in %s", code)
		}
	}
}
