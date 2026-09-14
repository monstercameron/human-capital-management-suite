package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-240: the production frontend release
// gate. The registry owns every product surface, but no
// release gate exists: gating the production frontend
// release has no exposure point and the first surface
// invents gate data by convention. The compiler needs
// the registered surface — canonical identity, route,
// and an honest fallback that gates nothing until the
// governed release service publishes, with release truth
// staying server authority — so the surface resolves
// today without a second source of business authority.
func TestTodo_WEB_240(t *testing.T) {
	definition, ok := LookupPage(PageReleaseGate)
	if !ok {
		t.Fatal("production frontend release gate unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("production frontend release gate incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageReleaseGate {
		t.Fatal("production frontend release gate route does not round-trip")
	}
	doc, err := Render(testView(PageReleaseGate))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("production frontend release gate exposes an unresolved message key")
	}
	for _, invented := range []string{"release 4.2 open", "7 checks green", "approved by Sable", "gate ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("production frontend release gate invents gate data: %q", invented)
		}
	}
}

// Golden: the registered release gate definition and
// its fallback copy.
func TestTodo_WEB_240_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageReleaseGate)
	if !ok {
		t.Fatal("production frontend release gate unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("release_gate.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("release_gate.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "f8b12791a7574d2bd8490bd761a6602ac932fc1830fc5fe0b9f0d3c4f5748763"
	if got != want {
		t.Fatalf("production frontend release gate digest = %s, want %s", got, want)
	}
}

// Browser: production frontend release gate renders
// deterministically and round-trips its route.
func TestTodo_WEB_240_Browser(t *testing.T) {
	first, err := Render(testView(PageReleaseGate))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageReleaseGate))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("production frontend release gate renders nondeterministically")
	}
	definition, _ := LookupPage(PageReleaseGate)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageReleaseGate {
		t.Fatal("production frontend release gate route does not round-trip")
	}
}

// Conformance: production frontend release gate keeps
// the registry contract — visible to the platform
// admin, hidden from the role-less baseline and the
// plain employee, ordered, and honest in every locale.
func TestTodo_WEB_240_Conformance(t *testing.T) {
	if !PageVisible(PageReleaseGate, []string{RoleHCMAdmin}) {
		t.Fatal("production frontend release gate hidden from the platform admin")
	}
	if PageVisible(PageReleaseGate, nil) {
		t.Fatal("production frontend release gate visible without roles")
	}
	if PageVisible(PageReleaseGate, []string{"worker_self"}) {
		t.Fatal("production frontend release gate visible to the plain employee")
	}
	definition, _ := LookupPage(PageReleaseGate)
	if definition.PrimaryNav {
		t.Fatal("production frontend release gate claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageReleaseGate), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("production frontend release gate leaks a key in %s", code)
		}
	}
}

// Security: unauthorized viewers are offered no release
// route — the gate surface never leaks an admin route to
// identities that cannot open it.
func TestTodo_WEB_240_Security(t *testing.T) {
	for _, roles := range [][]string{nil, {"worker_self"}, {"manager"}} {
		doc, err := Render(ApplyRoleVisibility(testView(PageReleaseGate), roles))
		if err != nil {
			t.Fatal(err)
		}
		for _, route := range []string{"/workspace/app/admin/roles", "/workspace/app/admin/policy-studio"} {
			if strings.Contains(doc, route) {
				t.Fatalf("admin route %q leaks to %v", route, roles)
			}
		}
	}
	adminDoc, err := Render(ApplyRoleVisibility(testView(PageReleaseGate), []string{RoleHCMAdmin}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(adminDoc, "⟦") {
		t.Fatal("release gate leaks a message key to the platform admin")
	}
}

// Integration: the release gate participates in admin
// navigation — the Admin submenu carries it, so
// registry and navigation agree.
// UXAUDIT-011 requires navigation availability to derive from the page
// registry's admission gate rather than ParentNav wiring alone. The
// release gate keeps its ParentNav wiring to Admin -- registry identity
// and direct-route access are unchanged -- but declares no admitted
// capability, so the Admin submenu must not carry it as a child.
func TestTodo_WEB_240_Integration(t *testing.T) {
	_, items := projectNavigation(testView(PageAdmin))
	admin, ok := projectedNavigationItem(items, PageAdmin)
	if !ok {
		t.Fatal("Admin navigation group is missing")
	}
	for _, child := range admin.Children {
		if child.Page == PageReleaseGate {
			t.Fatalf("Admin submenu still carries the unadmitted release gate: %+v", admin)
		}
	}
	definition, ok := LookupPage(PageReleaseGate)
	if !ok || definition.ParentNav != PageAdmin || definition.Admitted {
		t.Fatalf("release gate registry entry changed unexpectedly: %+v", definition)
	}
	if _, ok := LookupRoute(definition.Route); !ok {
		t.Fatal("release gate route no longer resolves through the page registry")
	}
}

// Fault: the release gate stays honest under fault — a
// load error and an unknown locale still render
// deterministically with no leaked keys and no panic.
func TestTodo_WEB_240_Fault(t *testing.T) {
	view := testView(PageReleaseGate)
	view.LoadError = "dial release: connection refused"
	view.Locale = ResolveProductLocale("xx-unknown")
	view = ApplyLocale(view, view.Locale)
	first, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("release gate renders nondeterministically under fault")
	}
	if strings.Contains(first, "⟦") {
		t.Fatal("release gate leaks a message key under fault")
	}
}
