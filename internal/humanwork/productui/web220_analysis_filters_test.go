package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-220: authorized analysis filters. The
// registry owns every product surface, but no filter
// enforcement exists: filtering governed analysis by
// authorization has no exposure point and the first
// surface invents filter data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that filters
// nothing until the governed reporting service
// publishes, with filter truth staying server authority
// — so the surface resolves today without a second
// source of business authority.
func TestTodo_WEB_220(t *testing.T) {
	definition, ok := LookupPage(PageAnalysisFilters)
	if !ok {
		t.Fatal("authorized analysis filters unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("authorized analysis filters incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageAnalysisFilters {
		t.Fatal("authorized analysis filters route does not round-trip")
	}
	doc, err := Render(testView(PageAnalysisFilters))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("authorized analysis filters expose an unresolved message key")
	}
	for _, invented := range []string{"filtered to Berlin", "3 dimensions applied", "saved by Ines", "filters ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("authorized analysis filters invent filter data: %q", invented)
		}
	}
}

// Golden: the registered analysis filter definition
// and its fallback copy.
func TestTodo_WEB_220_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageAnalysisFilters)
	if !ok {
		t.Fatal("authorized analysis filters unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("analysis_filters.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("analysis_filters.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "8e7e65d3a7a5f020f5a40767204da46df48b790efddb34e21c67843b433118cd"
	if got != want {
		t.Fatalf("authorized analysis filters digest = %s, want %s", got, want)
	}
}

// Browser: authorized analysis filters render
// deterministically and round-trip their route.
func TestTodo_WEB_220_Browser(t *testing.T) {
	first, err := Render(testView(PageAnalysisFilters))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageAnalysisFilters))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("authorized analysis filters render nondeterministically")
	}
	definition, _ := LookupPage(PageAnalysisFilters)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageAnalysisFilters {
		t.Fatal("authorized analysis filters route does not round-trip")
	}
}

// Conformance: authorized analysis filters keep the
// registry contract — visible to the manager, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_220_Conformance(t *testing.T) {
	if !PageVisible(PageAnalysisFilters, []string{"manager"}) {
		t.Fatal("authorized analysis filters hidden from the manager")
	}
	if PageVisible(PageAnalysisFilters, nil) {
		t.Fatal("authorized analysis filters visible without roles")
	}
	definition, _ := LookupPage(PageAnalysisFilters)
	if definition.PrimaryNav {
		t.Fatal("authorized analysis filters claim primary navigation before their service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageAnalysisFilters), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("authorized analysis filters leak a key in %s", code)
		}
	}
}
