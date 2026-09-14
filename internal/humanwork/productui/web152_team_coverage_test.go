package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-152: team-coverage review. The registry
// owns every product surface, but no coverage review
// exists: seeing who covers the team has no exposure
// point and the first surface invents coverage by
// convention. The compiler needs the registered surface —
// canonical identity, route, and an honest fallback that
// shows nothing until the governed time service
// publishes, with coverage truth staying server authority
// — so the surface resolves today without a second
// source of business authority.
func TestTodo_WEB_152(t *testing.T) {
	definition, ok := LookupPage(PageTeamCoverage)
	if !ok {
		t.Fatal("team-coverage review unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("team-coverage review incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageTeamCoverage {
		t.Fatal("team coverage route does not round-trip")
	}
	doc, err := Render(testView(PageTeamCoverage))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("team coverage exposes an unresolved message key")
	}
	for _, invented := range []string{"coverage:", "2 absent", "covered by:", "gap ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("team coverage invents coverage data: %q", invented)
		}
	}
}

// Golden: the registered coverage review definition and
// its fallback copy.
func TestTodo_WEB_152_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageTeamCoverage)
	if !ok {
		t.Fatal("team-coverage review unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("team_coverage.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("team_coverage.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "e81cba1f3d1b128a3d9f0c7af6c901be137d37ef85d5f991fd1ebc00120abfa3"
	if got != want {
		t.Fatalf("team coverage digest = %s, want %s", got, want)
	}
}

// Browser: coverage review renders deterministically and
// round-trips its route.
func TestTodo_WEB_152_Browser(t *testing.T) {
	first, err := Render(testView(PageTeamCoverage))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageTeamCoverage))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("team coverage renders nondeterministically")
	}
	definition, _ := LookupPage(PageTeamCoverage)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageTeamCoverage {
		t.Fatal("team coverage route does not round-trip")
	}
}

// Conformance: coverage review keeps the registry
// contract — visible to managers, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_152_Conformance(t *testing.T) {
	if !PageVisible(PageTeamCoverage, []string{"manager"}) {
		t.Fatal("team coverage hidden from managers")
	}
	if PageVisible(PageTeamCoverage, nil) {
		t.Fatal("team coverage visible without roles")
	}
	definition, _ := LookupPage(PageTeamCoverage)
	if definition.PrimaryNav {
		t.Fatal("team coverage claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageTeamCoverage), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("team coverage leaks a key in %s", code)
		}
	}
}
