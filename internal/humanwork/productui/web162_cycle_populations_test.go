package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-162: compensation-cycle populations. The
// registry owns every product surface, but no cycle
// population surface exists: scoping who is in the cycle
// has no exposure point and the first surface invents
// population data by convention. The compiler needs the
// registered surface — canonical identity, route, and an
// honest fallback that scopes nothing until the governed
// compensation service publishes, with population truth
// staying server authority — so the surface resolves
// today without a second source of business authority.
func TestTodo_WEB_162(t *testing.T) {
	definition, ok := LookupPage(PageCyclePopulations)
	if !ok {
		t.Fatal("compensation-cycle populations unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("compensation-cycle populations incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageCyclePopulations {
		t.Fatal("cycle populations route does not round-trip")
	}
	doc, err := Render(testView(PageCyclePopulations))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("cycle populations expose an unresolved message key")
	}
	for _, invented := range []string{"population:", "1,024 in", "scoped:", "included ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("cycle populations invent population data: %q", invented)
		}
	}
}

// Golden: the registered cycle populations definition
// and its fallback copy.
func TestTodo_WEB_162_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageCyclePopulations)
	if !ok {
		t.Fatal("compensation-cycle populations unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("cycle_populations.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("cycle_populations.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "4a54e28ed62d3ba30a26f41e61ea2904a1e9e3afec88f8b798109818ca35f98e"
	if got != want {
		t.Fatalf("cycle populations digest = %s, want %s", got, want)
	}
}

// Browser: cycle populations render deterministically and
// round-trip their route.
func TestTodo_WEB_162_Browser(t *testing.T) {
	first, err := Render(testView(PageCyclePopulations))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageCyclePopulations))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("cycle populations render nondeterministically")
	}
	definition, _ := LookupPage(PageCyclePopulations)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageCyclePopulations {
		t.Fatal("cycle populations route does not round-trip")
	}
}

// Conformance: cycle populations keep the registry
// contract — visible to managers, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_162_Conformance(t *testing.T) {
	if !PageVisible(PageCyclePopulations, []string{"manager"}) {
		t.Fatal("cycle populations hidden from managers")
	}
	if PageVisible(PageCyclePopulations, nil) {
		t.Fatal("cycle populations visible without roles")
	}
	definition, _ := LookupPage(PageCyclePopulations)
	if definition.PrimaryNav {
		t.Fatal("cycle populations claim primary navigation before their service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageCyclePopulations), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("cycle populations leak a key in %s", code)
		}
	}
}
