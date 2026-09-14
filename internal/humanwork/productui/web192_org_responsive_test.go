package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-192: responsive organization exploration.
// The registry owns every product surface, but no
// responsive org exploration exists: exploring the
// organization across viewport sizes has no exposure
// point and the first surface invents responsive data by
// convention. The compiler needs the registered surface
// — canonical identity, route, and an honest fallback
// that explores nothing until the governed organization
// service publishes, with org truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_192(t *testing.T) {
	definition, ok := LookupPage(PageOrgResponsive)
	if !ok {
		t.Fatal("responsive organization exploration unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("responsive organization exploration incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageOrgResponsive {
		t.Fatal("responsive organization exploration route does not round-trip")
	}
	doc, err := Render(testView(PageOrgResponsive))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("responsive organization exploration exposes an unresolved message key")
	}
	for _, invented := range []string{"mobile org chart", "320 px tree", "tablet ✓ layout", "responsive ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("responsive organization exploration invents responsive data: %q", invented)
		}
	}
}

// Golden: the registered responsive org definition and
// its fallback copy.
func TestTodo_WEB_192_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageOrgResponsive)
	if !ok {
		t.Fatal("responsive organization exploration unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("org_responsive.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("org_responsive.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "f379b43f606730cef81ce4de47266c324f637364831cb34766f5ac0e44865a24"
	if got != want {
		t.Fatalf("responsive organization digest = %s, want %s", got, want)
	}
}

// Browser: responsive organization exploration renders
// deterministically and round-trips its route.
func TestTodo_WEB_192_Browser(t *testing.T) {
	first, err := Render(testView(PageOrgResponsive))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageOrgResponsive))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("responsive organization exploration renders nondeterministically")
	}
	definition, _ := LookupPage(PageOrgResponsive)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageOrgResponsive {
		t.Fatal("responsive organization exploration route does not round-trip")
	}
}

// Conformance: responsive organization exploration
// keeps the registry contract — visible to the
// employee, hidden from the role-less baseline,
// ordered, and honest in every locale.
func TestTodo_WEB_192_Conformance(t *testing.T) {
	if !PageVisible(PageOrgResponsive, []string{"worker_self"}) {
		t.Fatal("responsive organization exploration hidden from the employee")
	}
	if PageVisible(PageOrgResponsive, nil) {
		t.Fatal("responsive organization exploration visible without roles")
	}
	definition, _ := LookupPage(PageOrgResponsive)
	if definition.PrimaryNav {
		t.Fatal("responsive organization exploration claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageOrgResponsive), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("responsive organization exploration leaks a key in %s", code)
		}
	}
}
