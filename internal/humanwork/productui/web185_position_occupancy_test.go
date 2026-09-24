package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// WEB-185 keeps the position occupancy route and presentation contract
// stable. Occupancy facts are rendered only from the server-authorized
// projection.
func TestTodo_WEB_185(t *testing.T) {
	definition, ok := LookupPage(PagePositionOccupancy)
	if !ok {
		t.Fatal("position occupancy presentation unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
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

// Golden: the registered position occupancy definition and its selection
// copy.
func TestTodo_WEB_185_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PagePositionOccupancy)
	if !ok {
		t.Fatal("position occupancy presentation unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("position_occupancy.select_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("position_occupancy.select_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	// Re-pinned 2026-09-24 after reviewing the occupancy selection copy.
	const want = "cd8e8b0442e6fb8b6c44307da530fa360f5bf9e6b4c7e7bfffcfb56b5f27de67"
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

// Conformance: the route remains role-scoped and its selection copy resolves
// in every supported locale.
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
