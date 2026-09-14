package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-207: offboarding impact simulation. The
// registry owns every product surface, but no offboarding
// simulation exists: simulating the impact of an exit has
// no exposure point and the first surface invents impact
// data by convention. The compiler needs the registered
// surface — canonical identity, route, and an honest
// fallback that simulates nothing until the governed
// lifecycle service publishes, with impact truth staying
// server authority — so the surface resolves today
// without a second source of business authority.
func TestTodo_WEB_207(t *testing.T) {
	definition, ok := LookupPage(PageOffboardingImpact)
	if !ok {
		t.Fatal("offboarding impact simulation unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("offboarding impact simulation incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageOffboardingImpact {
		t.Fatal("offboarding impact simulation route does not round-trip")
	}
	doc, err := Render(testView(PageOffboardingImpact))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("offboarding impact simulation exposes an unresolved message key")
	}
	for _, invented := range []string{"coverage gap 2 weeks", "payout 9,400", "backup is Theo", "impact ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("offboarding impact simulation invents impact data: %q", invented)
		}
	}
}

// Golden: the registered offboarding impact definition
// and its fallback copy.
func TestTodo_WEB_207_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageOffboardingImpact)
	if !ok {
		t.Fatal("offboarding impact simulation unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("offboarding_impact.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("offboarding_impact.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "499507a376aa55563ecf008fa418fa4f7831428557446236e558a411082e00b5"
	if got != want {
		t.Fatalf("offboarding impact simulation digest = %s, want %s", got, want)
	}
}

// Browser: offboarding impact simulation renders
// deterministically and round-trips its route.
func TestTodo_WEB_207_Browser(t *testing.T) {
	first, err := Render(testView(PageOffboardingImpact))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageOffboardingImpact))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("offboarding impact simulation renders nondeterministically")
	}
	definition, _ := LookupPage(PageOffboardingImpact)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageOffboardingImpact {
		t.Fatal("offboarding impact simulation route does not round-trip")
	}
}

// Conformance: offboarding impact simulation keeps the
// registry contract — visible to the employee, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_207_Conformance(t *testing.T) {
	if !PageVisible(PageOffboardingImpact, []string{"worker_self"}) {
		t.Fatal("offboarding impact simulation hidden from the employee")
	}
	if PageVisible(PageOffboardingImpact, nil) {
		t.Fatal("offboarding impact simulation visible without roles")
	}
	definition, _ := LookupPage(PageOffboardingImpact)
	if definition.PrimaryNav {
		t.Fatal("offboarding impact simulation claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageOffboardingImpact), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("offboarding impact simulation leaks a key in %s", code)
		}
	}
}
