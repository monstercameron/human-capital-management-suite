package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-165: benefit-program overview. The registry
// owns every product surface, but no benefits overview
// exists: an employee's programs have no exposure point
// and the first surface invents program data by
// convention. The compiler needs the registered surface —
// canonical identity, route, and an honest fallback that
// shows nothing until the governed benefits service
// publishes, with program truth staying server authority
// — so the surface resolves today without a second
// source of business authority.
func TestTodo_WEB_165(t *testing.T) {
	definition, ok := LookupPage(PageBenefitsOverview)
	if !ok {
		t.Fatal("benefit-program overview unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("benefit-program overview incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageBenefitsOverview {
		t.Fatal("benefits overview route does not round-trip")
	}
	doc, err := Render(testView(PageBenefitsOverview))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("benefits overview exposes an unresolved message key")
	}
	for _, invented := range []string{"program:", "enrolled:", "coverage:", "active ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("benefits overview invents program data: %q", invented)
		}
	}
}

// Golden: the registered benefits overview definition
// and its fallback copy.
func TestTodo_WEB_165_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageBenefitsOverview)
	if !ok {
		t.Fatal("benefit-program overview unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("benefits_overview.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("benefits_overview.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "74e0e873ba191872a81dcd0f204e6b6dcf579aab4dd17acf06d6d3111253e80f"
	if got != want {
		t.Fatalf("benefits overview digest = %s, want %s", got, want)
	}
}

// Browser: the benefits overview renders deterministically
// and round-trips its route.
func TestTodo_WEB_165_Browser(t *testing.T) {
	first, err := Render(testView(PageBenefitsOverview))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageBenefitsOverview))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("benefits overview renders nondeterministically")
	}
	definition, _ := LookupPage(PageBenefitsOverview)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageBenefitsOverview {
		t.Fatal("benefits overview route does not round-trip")
	}
}

// Conformance: the benefits overview keeps the registry
// contract — visible to the employee, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_165_Conformance(t *testing.T) {
	if !PageVisible(PageBenefitsOverview, []string{"worker_self"}) {
		t.Fatal("benefits overview hidden from the employee")
	}
	if PageVisible(PageBenefitsOverview, nil) {
		t.Fatal("benefits overview visible without roles")
	}
	definition, _ := LookupPage(PageBenefitsOverview)
	if definition.PrimaryNav {
		t.Fatal("benefits overview claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageBenefitsOverview), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("benefits overview leaks a key in %s", code)
		}
	}
}
