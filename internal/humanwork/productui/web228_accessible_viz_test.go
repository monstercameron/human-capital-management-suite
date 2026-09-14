package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-228: accessible data visualization. The
// registry owns every product surface, but no accessible
// visualization exists: visualizing governed data
// accessibly has no exposure point and the first surface
// invents visualization data by convention. The compiler
// needs the registered surface — canonical identity,
// route, and an honest fallback that visualizes nothing
// until the governed reporting service publishes, with
// visualization truth staying server authority — so the
// surface resolves today without a second source of
// business authority.
func TestTodo_WEB_228(t *testing.T) {
	definition, ok := LookupPage(PageAccessibleViz)
	if !ok {
		t.Fatal("accessible data visualization unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("accessible data visualization incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageAccessibleViz {
		t.Fatal("accessible data visualization route does not round-trip")
	}
	doc, err := Render(testView(PageAccessibleViz))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("accessible data visualization exposes an unresolved message key")
	}
	for _, invented := range []string{"bar chart rendered", "trend up 4 pct", "described by Li", "chart ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("accessible data visualization invents visualization data: %q", invented)
		}
	}
}

// Golden: the registered visualization definition and
// its fallback copy.
func TestTodo_WEB_228_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageAccessibleViz)
	if !ok {
		t.Fatal("accessible data visualization unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("accessible_viz.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("accessible_viz.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "186bed649890b87f44d72dbd23734e84a072d3672dc0a187f4026bb8050463e5"
	if got != want {
		t.Fatalf("accessible data visualization digest = %s, want %s", got, want)
	}
}

// Browser: accessible data visualization renders
// deterministically and round-trips its route.
func TestTodo_WEB_228_Browser(t *testing.T) {
	first, err := Render(testView(PageAccessibleViz))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageAccessibleViz))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("accessible data visualization renders nondeterministically")
	}
	definition, _ := LookupPage(PageAccessibleViz)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageAccessibleViz {
		t.Fatal("accessible data visualization route does not round-trip")
	}
}

// Conformance: accessible data visualization keeps the
// registry contract — visible to the manager, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_228_Conformance(t *testing.T) {
	if !PageVisible(PageAccessibleViz, []string{"manager"}) {
		t.Fatal("accessible data visualization hidden from the manager")
	}
	if PageVisible(PageAccessibleViz, nil) {
		t.Fatal("accessible data visualization visible without roles")
	}
	definition, _ := LookupPage(PageAccessibleViz)
	if definition.PrimaryNav {
		t.Fatal("accessible data visualization claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageAccessibleViz), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("accessible data visualization leaks a key in %s", code)
		}
	}
}
