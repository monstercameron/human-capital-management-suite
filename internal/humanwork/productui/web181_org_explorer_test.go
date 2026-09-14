package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-181: organization explorer. The registry
// owns every product surface, but no explorer exists:
// browsing the organization has no exposure point and the
// first surface invents org data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that explores
// nothing until the governed organization service
// publishes, with org truth staying server authority — so
// the surface resolves today without a second source of
// business authority.
func TestTodo_WEB_181(t *testing.T) {
	definition, ok := LookupPage(PageOrgExplorer)
	if !ok {
		t.Fatal("organization explorer unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("organization explorer incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageOrgExplorer {
		t.Fatal("organization explorer route does not round-trip")
	}
	doc, err := Render(testView(PageOrgExplorer))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("organization explorer exposes an unresolved message key")
	}
	for _, invented := range []string{"org:", "division:", "reports:", "expanded ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("organization explorer invents org data: %q", invented)
		}
	}
}

// Golden: the registered organization explorer definition
// and its fallback copy.
func TestTodo_WEB_181_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageOrgExplorer)
	if !ok {
		t.Fatal("organization explorer unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("org_explorer.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("org_explorer.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "9d9f06cc8a6ce60d60ccce468e1ead15ebcc8f7102c0c49f089427c19364f316"
	if got != want {
		t.Fatalf("organization explorer digest = %s, want %s", got, want)
	}
}

// Browser: the organization explorer renders
// deterministically and round-trips its route.
func TestTodo_WEB_181_Browser(t *testing.T) {
	first, err := Render(testView(PageOrgExplorer))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageOrgExplorer))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("organization explorer renders nondeterministically")
	}
	definition, _ := LookupPage(PageOrgExplorer)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageOrgExplorer {
		t.Fatal("organization explorer route does not round-trip")
	}
}

// Conformance: the organization explorer keeps the
// registry contract — visible to the employee, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_181_Conformance(t *testing.T) {
	if !PageVisible(PageOrgExplorer, []string{"worker_self"}) {
		t.Fatal("organization explorer hidden from the employee")
	}
	if PageVisible(PageOrgExplorer, nil) {
		t.Fatal("organization explorer visible without roles")
	}
	definition, _ := LookupPage(PageOrgExplorer)
	if definition.PrimaryNav {
		t.Fatal("organization explorer claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageOrgExplorer), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("organization explorer leaks a key in %s", code)
		}
	}
}
