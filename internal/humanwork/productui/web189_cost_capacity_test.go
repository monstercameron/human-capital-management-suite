package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-189: cost and capacity simulation. The
// registry owns every product surface, but no cost and
// capacity simulation exists: simulating workforce cost
// and capacity has no exposure point and the first
// surface invents simulation data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that simulates
// nothing until the governed headcount service
// publishes, with simulation truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_189(t *testing.T) {
	definition, ok := LookupPage(PageCostCapacity)
	if !ok {
		t.Fatal("cost and capacity simulation unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("cost and capacity simulation incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageCostCapacity {
		t.Fatal("cost and capacity simulation route does not round-trip")
	}
	doc, err := Render(testView(PageCostCapacity))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("cost and capacity simulation exposes an unresolved message key")
	}
	for _, invented := range []string{"costs 4.2M per year", "capacity 88 pct", "simulated by Bea", "simulation ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("cost and capacity simulation invents simulation data: %q", invented)
		}
	}
}

// Golden: the registered cost and capacity definition
// and its fallback copy.
func TestTodo_WEB_189_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageCostCapacity)
	if !ok {
		t.Fatal("cost and capacity simulation unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("cost_capacity.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("cost_capacity.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "d3077c3a642b56f02199988550601017ab3515bfb218dbd236d0562459fcb35c"
	if got != want {
		t.Fatalf("cost and capacity digest = %s, want %s", got, want)
	}
}

// Browser: cost and capacity simulation renders
// deterministically and round-trips its route.
func TestTodo_WEB_189_Browser(t *testing.T) {
	first, err := Render(testView(PageCostCapacity))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageCostCapacity))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("cost and capacity simulation renders nondeterministically")
	}
	definition, _ := LookupPage(PageCostCapacity)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageCostCapacity {
		t.Fatal("cost and capacity simulation route does not round-trip")
	}
}

// Conformance: cost and capacity simulation keeps the
// registry contract — visible to the employee, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_189_Conformance(t *testing.T) {
	if !PageVisible(PageCostCapacity, []string{"worker_self"}) {
		t.Fatal("cost and capacity simulation hidden from the employee")
	}
	if PageVisible(PageCostCapacity, nil) {
		t.Fatal("cost and capacity simulation visible without roles")
	}
	definition, _ := LookupPage(PageCostCapacity)
	if definition.PrimaryNav {
		t.Fatal("cost and capacity simulation claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageCostCapacity), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("cost and capacity simulation leaks a key in %s", code)
		}
	}
}
