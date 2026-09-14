package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-141: the external candidate portal. The
// registry owns every product surface, but no portal
// exists: external candidates have no exposure point and
// the first surface invents portal content by convention.
// The compiler needs the registered page — canonical
// identity, route, role-less visibility for candidates
// outside the workspace, and an honest fallback that
// exposes no portal until the governed service publishes
// — so the portal resolves today without a second source
// of business authority.
func TestTodo_WEB_141(t *testing.T) {
	definition, ok := LookupPage(PagePortal)
	if !ok {
		t.Fatal("candidate portal unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("candidate portal incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PagePortal {
		t.Fatal("portal route does not round-trip")
	}
	if !PageVisible(PagePortal, nil) {
		t.Fatal("portal hidden from role-less external candidates")
	}
	doc, err := Render(testView(PagePortal))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("candidate portal exposes an unresolved message key")
	}
	for _, invented := range []string{"Apply now", "job listings:", "REQ-", "benefits:"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("candidate portal invents portal data: %q", invented)
		}
	}
}

// Golden: the registered portal definition and its
// fallback copy.
func TestTodo_WEB_141_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PagePortal)
	if !ok {
		t.Fatal("candidate portal unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("portal.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("portal.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "01d16437e8b97fdb0bdfb4861389ed742134f310705bfeb832463fc9bbd91432"
	if got != want {
		t.Fatalf("portal digest = %s, want %s", got, want)
	}
}

// Browser: the external candidate portal renders
// deterministically and round-trips its route.
func TestTodo_WEB_141_Browser(t *testing.T) {
	first, err := Render(testView(PagePortal))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PagePortal))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("candidate portal renders nondeterministically")
	}
	definition, _ := LookupPage(PagePortal)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PagePortal {
		t.Fatal("portal route does not round-trip")
	}
}

// Conformance: the portal keeps its contract — reachable
// without workspace roles, never primary navigation,
// ordered, and honest in every locale.
func TestTodo_WEB_141_Conformance(t *testing.T) {
	if !PageVisible(PagePortal, []string{}) {
		t.Fatal("portal hidden from external candidates")
	}
	definition, _ := LookupPage(PagePortal)
	if definition.PrimaryNav {
		t.Fatal("portal claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PagePortal), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("candidate portal leaks a key in %s", code)
		}
	}
}
