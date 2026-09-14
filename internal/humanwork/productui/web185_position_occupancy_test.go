package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-185: position occupancy presentation. The
// registry owns every product surface, but no occupancy
// presentation exists: showing who holds a position has
// no exposure point and the first surface invents
// occupancy data by convention. The compiler needs the
// registered surface — canonical identity, route, and an
// honest fallback that presents nothing until the
// governed position service publishes, with occupancy
// truth staying server authority — so the surface
// resolves today without a second source of business
// authority.
func TestTodo_WEB_185(t *testing.T) {
	definition, ok := LookupPage(PagePositionOccupancy)
	if !ok {
		t.Fatal("position occupancy presentation unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("position occupancy presentation incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PagePositionOccupancy {
		t.Fatal("position occupancy route does not round-trip")
	}
	doc, err := Render(testView(PagePositionOccupancy))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("position occupancy presentation exposes an unresolved message key")
	}
	for _, invented := range []string{"held by Dana Cole", "vacancy rate 12 pct", "backfilled by Eli", "occupancy ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("position occupancy presentation invents occupancy data: %q", invented)
		}
	}
}

// Golden: the registered position occupancy definition
// and its fallback copy.
func TestTodo_WEB_185_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PagePositionOccupancy)
	if !ok {
		t.Fatal("position occupancy presentation unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("position_occupancy.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("position_occupancy.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "d5b8885c025faf6294fe3b72b2c4e82ed9ee1f5ffe9fd4c11fbd3e2b7152976a"
	if got != want {
		t.Fatalf("position occupancy digest = %s, want %s", got, want)
	}
}

// Browser: position occupancy presentation renders
// deterministically and round-trips its route.
func TestTodo_WEB_185_Browser(t *testing.T) {
	first, err := Render(testView(PagePositionOccupancy))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PagePositionOccupancy))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("position occupancy presentation renders nondeterministically")
	}
	definition, _ := LookupPage(PagePositionOccupancy)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PagePositionOccupancy {
		t.Fatal("position occupancy route does not round-trip")
	}
}

// Conformance: position occupancy presentation keeps
// the registry contract — visible to the employee,
// hidden from the role-less baseline, ordered, and
// honest in every locale.
func TestTodo_WEB_185_Conformance(t *testing.T) {
	if !PageVisible(PagePositionOccupancy, []string{"worker_self"}) {
		t.Fatal("position occupancy presentation hidden from the employee")
	}
	if PageVisible(PagePositionOccupancy, nil) {
		t.Fatal("position occupancy presentation visible without roles")
	}
	definition, _ := LookupPage(PagePositionOccupancy)
	if definition.PrimaryNav {
		t.Fatal("position occupancy presentation claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PagePositionOccupancy), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("position occupancy presentation leaks a key in %s", code)
		}
	}
}
