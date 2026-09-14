package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-182: accessible organization outline. The
// registry owns every product surface, but no outline
// exists: an accessible org outline has no exposure point
// and the first surface invents outline data by
// convention. The compiler needs the registered surface —
// canonical identity, route, and an honest fallback that
// outlines nothing until the governed organization
// service publishes, with org truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_182(t *testing.T) {
	definition, ok := LookupPage(PageOrgOutline)
	if !ok {
		t.Fatal("accessible organization outline unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("accessible organization outline incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageOrgOutline {
		t.Fatal("organization outline route does not round-trip")
	}
	doc, err := Render(testView(PageOrgOutline))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("organization outline exposes an unresolved message key")
	}
	for _, invented := range []string{"reports to Ana", "level 3", "has 4 children", "nested ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("organization outline invents outline data: %q", invented)
		}
	}
}

// Golden: the registered organization outline definition
// and its fallback copy.
func TestTodo_WEB_182_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageOrgOutline)
	if !ok {
		t.Fatal("accessible organization outline unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("org_outline.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("org_outline.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "f1f3405d027fd592ef2aa46cea107dc2a589a5f748c6f655eb20449da2db9778"
	if got != want {
		t.Fatalf("organization outline digest = %s, want %s", got, want)
	}
}

// Browser: the organization outline renders
// deterministically and round-trips its route.
func TestTodo_WEB_182_Browser(t *testing.T) {
	first, err := Render(testView(PageOrgOutline))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageOrgOutline))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("organization outline renders nondeterministically")
	}
	definition, _ := LookupPage(PageOrgOutline)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageOrgOutline {
		t.Fatal("organization outline route does not round-trip")
	}
}

// Conformance: the organization outline keeps the
// registry contract — visible to the employee, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_182_Conformance(t *testing.T) {
	if !PageVisible(PageOrgOutline, []string{"worker_self"}) {
		t.Fatal("organization outline hidden from the employee")
	}
	if PageVisible(PageOrgOutline, nil) {
		t.Fatal("organization outline visible without roles")
	}
	definition, _ := LookupPage(PageOrgOutline)
	if definition.PrimaryNav {
		t.Fatal("organization outline claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageOrgOutline), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("organization outline leaks a key in %s", code)
		}
	}
}
