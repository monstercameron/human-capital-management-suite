package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-221: result lineage presentation. The
// registry owns every product surface, but no lineage
// presentation exists: showing where a result came from
// has no exposure point and the first surface invents
// lineage data by convention. The compiler needs the
// registered surface — canonical identity, route, and an
// honest fallback that presents nothing until the
// governed reporting service publishes, with lineage
// truth staying server authority — so the surface
// resolves today without a second source of business
// authority.
func TestTodo_WEB_221(t *testing.T) {
	definition, ok := LookupPage(PageResultLineage)
	if !ok {
		t.Fatal("result lineage presentation unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("result lineage presentation incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageResultLineage {
		t.Fatal("result lineage presentation route does not round-trip")
	}
	doc, err := Render(testView(PageResultLineage))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("result lineage presentation exposes an unresolved message key")
	}
	for _, invented := range []string{"sourced from payroll", "3 hops traced", "pinned by Odin", "lineage ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("result lineage presentation invents lineage data: %q", invented)
		}
	}
}

// Golden: the registered lineage definition and its
// fallback copy.
func TestTodo_WEB_221_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageResultLineage)
	if !ok {
		t.Fatal("result lineage presentation unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("result_lineage.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("result_lineage.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "caf042f728805454785f033bc6a655dff61224472e9a1577a8e894c0aa5afa5e"
	if got != want {
		t.Fatalf("result lineage presentation digest = %s, want %s", got, want)
	}
}

// Browser: result lineage presentation renders
// deterministically and round-trips its route.
func TestTodo_WEB_221_Browser(t *testing.T) {
	first, err := Render(testView(PageResultLineage))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageResultLineage))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("result lineage presentation renders nondeterministically")
	}
	definition, _ := LookupPage(PageResultLineage)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageResultLineage {
		t.Fatal("result lineage presentation route does not round-trip")
	}
}

// Conformance: result lineage presentation keeps the
// registry contract — visible to the manager, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_221_Conformance(t *testing.T) {
	if !PageVisible(PageResultLineage, []string{"manager"}) {
		t.Fatal("result lineage presentation hidden from the manager")
	}
	if PageVisible(PageResultLineage, nil) {
		t.Fatal("result lineage presentation visible without roles")
	}
	definition, _ := LookupPage(PageResultLineage)
	if definition.PrimaryNav {
		t.Fatal("result lineage presentation claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageResultLineage), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("result lineage presentation leaks a key in %s", code)
		}
	}
}
