package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-231: authorization-policy simulation. The
// registry owns every product surface, but no policy
// simulation exists: simulating authorization policy has
// no exposure point and the first surface invents
// simulation data by convention. The compiler needs the
// registered surface — canonical identity, route, and an
// honest fallback that simulates nothing until the
// governed policy service publishes, with simulation
// truth staying server authority — so the surface
// resolves today without a second source of business
// authority.
func TestTodo_WEB_231(t *testing.T) {
	definition, ok := LookupPage(PagePolicySimulation)
	if !ok {
		t.Fatal("authorization-policy simulation unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("authorization-policy simulation incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PagePolicySimulation {
		t.Fatal("authorization-policy simulation route does not round-trip")
	}
	doc, err := Render(testView(PagePolicySimulation))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("authorization-policy simulation exposes an unresolved message key")
	}
	for _, invented := range []string{"manager may approve", "3 paths allowed", "tested by Pax", "simulation ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("authorization-policy simulation invents simulation data: %q", invented)
		}
	}
}

// Golden: the registered policy simulation definition
// and its fallback copy.
func TestTodo_WEB_231_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PagePolicySimulation)
	if !ok {
		t.Fatal("authorization-policy simulation unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("policy_simulation.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("policy_simulation.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "750155ff7d90b2a54b81add74cfa7771dd235c8679bda2bacf9f3fc59de7fcdd"
	if got != want {
		t.Fatalf("authorization-policy simulation digest = %s, want %s", got, want)
	}
}

// Browser: authorization-policy simulation renders
// deterministically and round-trips its route.
func TestTodo_WEB_231_Browser(t *testing.T) {
	first, err := Render(testView(PagePolicySimulation))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PagePolicySimulation))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("authorization-policy simulation renders nondeterministically")
	}
	definition, _ := LookupPage(PagePolicySimulation)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PagePolicySimulation {
		t.Fatal("authorization-policy simulation route does not round-trip")
	}
}

// Conformance: authorization-policy simulation keeps
// the registry contract — visible to the platform
// admin, hidden from the role-less baseline and the
// plain employee, ordered, and honest in every locale.
func TestTodo_WEB_231_Conformance(t *testing.T) {
	if !PageVisible(PagePolicySimulation, []string{RoleHCMAdmin}) {
		t.Fatal("authorization-policy simulation hidden from the platform admin")
	}
	if PageVisible(PagePolicySimulation, nil) {
		t.Fatal("authorization-policy simulation visible without roles")
	}
	if PageVisible(PagePolicySimulation, []string{"worker_self"}) {
		t.Fatal("authorization-policy simulation visible to the plain employee")
	}
	definition, _ := LookupPage(PagePolicySimulation)
	if definition.PrimaryNav {
		t.Fatal("authorization-policy simulation claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PagePolicySimulation), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("authorization-policy simulation leaks a key in %s", code)
		}
	}
}
