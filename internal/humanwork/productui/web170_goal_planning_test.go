package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-170: goal planning. The registry owns every
// product surface, but no goal surface exists: planning
// goals has no exposure point and the first surface
// invents goal data by convention. The compiler needs the
// registered surface — canonical identity, route, and an
// honest fallback that plans nothing until the governed
// growth service publishes, with goal truth staying
// server authority — so the surface resolves today
// without a second source of business authority.
func TestTodo_WEB_170(t *testing.T) {
	definition, ok := LookupPage(PageGoalPlanning)
	if !ok {
		t.Fatal("goal planning unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("goal planning incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageGoalPlanning {
		t.Fatal("goal planning route does not round-trip")
	}
	doc, err := Render(testView(PageGoalPlanning))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("goal planning exposes an unresolved message key")
	}
	for _, invented := range []string{"goal:", "target:", "milestone:", "achieved ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("goal planning invents goal data: %q", invented)
		}
	}
}

// Golden: the registered goal planning definition and its
// fallback copy.
func TestTodo_WEB_170_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageGoalPlanning)
	if !ok {
		t.Fatal("goal planning unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("goal_planning.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("goal_planning.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "b41c568274f79b7ce258cfa73df1b78e4ba98adad0606f9ee8b90e37112fb607"
	if got != want {
		t.Fatalf("goal planning digest = %s, want %s", got, want)
	}
}

// Browser: goal planning renders deterministically and
// round-trips its route.
func TestTodo_WEB_170_Browser(t *testing.T) {
	first, err := Render(testView(PageGoalPlanning))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageGoalPlanning))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("goal planning renders nondeterministically")
	}
	definition, _ := LookupPage(PageGoalPlanning)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageGoalPlanning {
		t.Fatal("goal planning route does not round-trip")
	}
}

// Conformance: goal planning keeps the registry contract —
// visible to the employee, hidden from the role-less
// baseline, ordered, and honest in every locale.
func TestTodo_WEB_170_Conformance(t *testing.T) {
	if !PageVisible(PageGoalPlanning, []string{"worker_self"}) {
		t.Fatal("goal planning hidden from the employee")
	}
	if PageVisible(PageGoalPlanning, nil) {
		t.Fatal("goal planning visible without roles")
	}
	definition, _ := LookupPage(PageGoalPlanning)
	if definition.PrimaryNav {
		t.Fatal("goal planning claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageGoalPlanning), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("goal planning leaks a key in %s", code)
		}
	}
}
