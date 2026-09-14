package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-188: governed population building. The
// registry owns every product surface, but no population
// builder exists: building governed populations has no
// exposure point and the first surface invents
// population data by convention. The compiler needs the
// registered surface — canonical identity, route, and an
// honest fallback that builds nothing until the governed
// headcount service publishes, with population truth
// staying server authority — so the surface resolves
// today without a second source of business authority.
func TestTodo_WEB_188(t *testing.T) {
	definition, ok := LookupPage(PageGovernedPopulation)
	if !ok {
		t.Fatal("governed population building unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("governed population building incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageGovernedPopulation {
		t.Fatal("governed population building route does not round-trip")
	}
	doc, err := Render(testView(PageGovernedPopulation))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("governed population building exposes an unresolved message key")
	}
	for _, invented := range []string{"all engineers in Berlin", "2,300 members", "built by Omar", "population ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("governed population building invents population data: %q", invented)
		}
	}
}

// Golden: the registered governed population definition
// and its fallback copy.
func TestTodo_WEB_188_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageGovernedPopulation)
	if !ok {
		t.Fatal("governed population building unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("governed_population.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("governed_population.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "f98a9475302fe21ea54d56e3db13e98329a15b00fea23e543f22ff04601bec01"
	if got != want {
		t.Fatalf("governed population digest = %s, want %s", got, want)
	}
}

// Browser: governed population building renders
// deterministically and round-trips its route.
func TestTodo_WEB_188_Browser(t *testing.T) {
	first, err := Render(testView(PageGovernedPopulation))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageGovernedPopulation))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("governed population building renders nondeterministically")
	}
	definition, _ := LookupPage(PageGovernedPopulation)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageGovernedPopulation {
		t.Fatal("governed population building route does not round-trip")
	}
}

// Conformance: governed population building keeps the
// registry contract — visible to the employee, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_188_Conformance(t *testing.T) {
	if !PageVisible(PageGovernedPopulation, []string{"worker_self"}) {
		t.Fatal("governed population building hidden from the employee")
	}
	if PageVisible(PageGovernedPopulation, nil) {
		t.Fatal("governed population building visible without roles")
	}
	definition, _ := LookupPage(PageGovernedPopulation)
	if definition.PrimaryNav {
		t.Fatal("governed population building claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageGovernedPopulation), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("governed population building leaks a key in %s", code)
		}
	}
}
