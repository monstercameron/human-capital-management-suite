package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-219: the analysis floorplan. The registry
// owns every product surface, but no analysis floorplan
// exists: laying out governed analysis has no exposure
// point and the first surface invents analysis data by
// convention. The compiler needs the registered surface
// — canonical identity, route, and an honest fallback
// that lays out nothing until the governed reporting
// service publishes, with analysis truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_219(t *testing.T) {
	definition, ok := LookupPage(PageAnalysisFloorplan)
	if !ok {
		t.Fatal("analysis floorplan unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("analysis floorplan incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageAnalysisFloorplan {
		t.Fatal("analysis floorplan route does not round-trip")
	}
	doc, err := Render(testView(PageAnalysisFloorplan))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("analysis floorplan exposes an unresolved message key")
	}
	for _, invented := range []string{"attrition dashboard live", "4 pinned charts", "arranged by Yara", "analysis ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("analysis floorplan invents analysis data: %q", invented)
		}
	}
}

// Golden: the registered analysis floorplan definition
// and its fallback copy.
func TestTodo_WEB_219_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageAnalysisFloorplan)
	if !ok {
		t.Fatal("analysis floorplan unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("analysis_floorplan.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("analysis_floorplan.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "16e07862316c1c5e422f99289e72ef462525372dca2eea20d7fba3c67d19751d"
	if got != want {
		t.Fatalf("analysis floorplan digest = %s, want %s", got, want)
	}
}

// Browser: analysis floorplan renders deterministically
// and round-trips its route.
func TestTodo_WEB_219_Browser(t *testing.T) {
	first, err := Render(testView(PageAnalysisFloorplan))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageAnalysisFloorplan))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("analysis floorplan renders nondeterministically")
	}
	definition, _ := LookupPage(PageAnalysisFloorplan)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageAnalysisFloorplan {
		t.Fatal("analysis floorplan route does not round-trip")
	}
}

// Conformance: analysis floorplan keeps the registry
// contract — visible to the manager, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_219_Conformance(t *testing.T) {
	if !PageVisible(PageAnalysisFloorplan, []string{"manager"}) {
		t.Fatal("analysis floorplan hidden from the manager")
	}
	if PageVisible(PageAnalysisFloorplan, nil) {
		t.Fatal("analysis floorplan visible without roles")
	}
	definition, _ := LookupPage(PageAnalysisFloorplan)
	if definition.PrimaryNav {
		t.Fatal("analysis floorplan claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageAnalysisFloorplan), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("analysis floorplan leaks a key in %s", code)
		}
	}
}
