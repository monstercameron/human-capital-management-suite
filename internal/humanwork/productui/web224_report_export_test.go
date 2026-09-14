package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-224: governed report export. The registry
// owns every product surface, but no export surface
// exists: exporting governed reports has no exposure
// point and the first surface invents export data by
// convention. The compiler needs the registered surface
// — canonical identity, route, and an honest fallback
// that exports nothing until the governed reporting
// service publishes, with export truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_224(t *testing.T) {
	definition, ok := LookupPage(PageReportExport)
	if !ok {
		t.Fatal("governed report export unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("governed report export incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageReportExport {
		t.Fatal("governed report export route does not round-trip")
	}
	doc, err := Render(testView(PageReportExport))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("governed report export exposes an unresolved message key")
	}
	for _, invented := range []string{"export ready CSV", "2,000 rows sent", "requested by Cato", "export ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("governed report export invents export data: %q", invented)
		}
	}
}

// Golden: the registered export definition and its
// fallback copy.
func TestTodo_WEB_224_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageReportExport)
	if !ok {
		t.Fatal("governed report export unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("report_export.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("report_export.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "335b0ffcb57cd83521f50d0db41f60df84404b074fb4ccaf4eae409de3650da5"
	if got != want {
		t.Fatalf("governed report export digest = %s, want %s", got, want)
	}
}

// Browser: governed report export renders
// deterministically and round-trips its route.
func TestTodo_WEB_224_Browser(t *testing.T) {
	first, err := Render(testView(PageReportExport))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageReportExport))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("governed report export renders nondeterministically")
	}
	definition, _ := LookupPage(PageReportExport)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageReportExport {
		t.Fatal("governed report export route does not round-trip")
	}
}

// Conformance: governed report export keeps the
// registry contract — visible to the manager, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_224_Conformance(t *testing.T) {
	if !PageVisible(PageReportExport, []string{"manager"}) {
		t.Fatal("governed report export hidden from the manager")
	}
	if PageVisible(PageReportExport, nil) {
		t.Fatal("governed report export visible without roles")
	}
	definition, _ := LookupPage(PageReportExport)
	if definition.PrimaryNav {
		t.Fatal("governed report export claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageReportExport), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("governed report export leaks a key in %s", code)
		}
	}
}
