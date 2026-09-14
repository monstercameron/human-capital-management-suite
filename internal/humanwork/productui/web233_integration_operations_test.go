package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-233: integration operations. The registry
// owns every product surface, but no integration
// operations exist: operating governed integrations has
// no exposure point and the first surface invents
// operation data by convention. The compiler needs the
// registered surface — canonical identity, route, and an
// honest fallback that operates nothing until the
// governed integration service publishes, with operation
// truth staying server authority — so the surface
// resolves today without a second source of business
// authority.
func TestTodo_WEB_233(t *testing.T) {
	definition, ok := LookupPage(PageIntegrationOperations)
	if !ok {
		t.Fatal("integration operations unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("integration operations incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageIntegrationOperations {
		t.Fatal("integration operations route does not round-trip")
	}
	doc, err := Render(testView(PageIntegrationOperations))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("integration operations expose an unresolved message key")
	}
	for _, invented := range []string{"payroll sync live", "3 connectors healthy", "run by Oren", "operations ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("integration operations invent operation data: %q", invented)
		}
	}
}

// Golden: the registered integration definition and
// its fallback copy.
func TestTodo_WEB_233_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageIntegrationOperations)
	if !ok {
		t.Fatal("integration operations unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("integration_operations.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("integration_operations.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "006457ca3105376345f6131c3eb7129db4443b31b45f584f8f27ead890eceefe"
	if got != want {
		t.Fatalf("integration operations digest = %s, want %s", got, want)
	}
}

// Browser: integration operations render
// deterministically and round-trip their route.
func TestTodo_WEB_233_Browser(t *testing.T) {
	first, err := Render(testView(PageIntegrationOperations))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageIntegrationOperations))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("integration operations renders nondeterministically")
	}
	definition, _ := LookupPage(PageIntegrationOperations)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageIntegrationOperations {
		t.Fatal("integration operations route does not round-trip")
	}
}

// Conformance: integration operations keep the
// registry contract — visible to the platform admin,
// hidden from the role-less baseline and the plain
// employee, ordered, and honest in every locale.
func TestTodo_WEB_233_Conformance(t *testing.T) {
	if !PageVisible(PageIntegrationOperations, []string{RoleHCMAdmin}) {
		t.Fatal("integration operations hidden from the platform admin")
	}
	if PageVisible(PageIntegrationOperations, nil) {
		t.Fatal("integration operations visible without roles")
	}
	if PageVisible(PageIntegrationOperations, []string{"worker_self"}) {
		t.Fatal("integration operations visible to the plain employee")
	}
	definition, _ := LookupPage(PageIntegrationOperations)
	if definition.PrimaryNav {
		t.Fatal("integration operations claim primary navigation before their service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageIntegrationOperations), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("integration operations leak a key in %s", code)
		}
	}
}
