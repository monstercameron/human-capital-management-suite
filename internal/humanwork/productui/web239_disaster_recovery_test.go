package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-239: frontend disaster recovery. The
// registry owns every product surface, but no recovery
// proof exists: proving frontend disaster recovery has
// no exposure point and the first surface invents
// recovery data by convention. The compiler needs the
// registered surface — canonical identity, route, and an
// honest fallback that proves nothing until the governed
// recovery service publishes, with recovery truth
// staying server authority — so the surface resolves
// today without a second source of business authority.
func TestTodo_WEB_239(t *testing.T) {
	definition, ok := LookupPage(PageDisasterRecovery)
	if !ok {
		t.Fatal("frontend disaster recovery unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("frontend disaster recovery incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageDisasterRecovery {
		t.Fatal("frontend disaster recovery route does not round-trip")
	}
	doc, err := Render(testView(PageDisasterRecovery))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("frontend disaster recovery exposes an unresolved message key")
	}
	for _, invented := range []string{"failover tested", "RTO 15 min", "drilled by Mina", "recovery ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("frontend disaster recovery invents recovery data: %q", invented)
		}
	}
}

// Golden: the registered recovery definition and its
// fallback copy.
func TestTodo_WEB_239_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageDisasterRecovery)
	if !ok {
		t.Fatal("frontend disaster recovery unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("disaster_recovery.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("disaster_recovery.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "fb0b8aad78a27f581352571555e35a1dabaa18da562abcb4938cd1572a1904f7"
	if got != want {
		t.Fatalf("frontend disaster recovery digest = %s, want %s", got, want)
	}
}

// Browser: frontend disaster recovery renders
// deterministically and round-trips its route.
func TestTodo_WEB_239_Browser(t *testing.T) {
	first, err := Render(testView(PageDisasterRecovery))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageDisasterRecovery))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("frontend disaster recovery renders nondeterministically")
	}
	definition, _ := LookupPage(PageDisasterRecovery)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageDisasterRecovery {
		t.Fatal("frontend disaster recovery route does not round-trip")
	}
}

// Conformance: frontend disaster recovery keeps the
// registry contract — visible to the platform admin,
// hidden from the role-less baseline and the plain
// employee, ordered, and honest in every locale.
func TestTodo_WEB_239_Conformance(t *testing.T) {
	if !PageVisible(PageDisasterRecovery, []string{RoleHCMAdmin}) {
		t.Fatal("frontend disaster recovery hidden from the platform admin")
	}
	if PageVisible(PageDisasterRecovery, nil) {
		t.Fatal("frontend disaster recovery visible without roles")
	}
	if PageVisible(PageDisasterRecovery, []string{"worker_self"}) {
		t.Fatal("frontend disaster recovery visible to the plain employee")
	}
	definition, _ := LookupPage(PageDisasterRecovery)
	if definition.PrimaryNav {
		t.Fatal("frontend disaster recovery claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageDisasterRecovery), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("frontend disaster recovery leaks a key in %s", code)
		}
	}
}

// Security: unauthorized viewers are offered no recovery
// route — the proof surface never leaks an admin route
// to identities that cannot open it.
func TestTodo_WEB_239_Security(t *testing.T) {
	for _, roles := range [][]string{nil, {"worker_self"}, {"manager"}} {
		doc, err := Render(ApplyRoleVisibility(testView(PageDisasterRecovery), roles))
		if err != nil {
			t.Fatal(err)
		}
		for _, route := range []string{"/workspace/app/admin/roles", "/workspace/app/admin/policy-studio"} {
			if strings.Contains(doc, route) {
				t.Fatalf("admin route %q leaks to %v", route, roles)
			}
		}
	}
	adminDoc, err := Render(ApplyRoleVisibility(testView(PageDisasterRecovery), []string{RoleHCMAdmin}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(adminDoc, "⟦") {
		t.Fatal("recovery surface leaks a message key to the platform admin")
	}
}

// Integration: the recovery surface participates in
// admin navigation — the Admin submenu carries it, so
// registry and navigation agree.
// UXAUDIT-011 requires navigation availability to derive from the page
// registry's admission gate rather than ParentNav wiring alone. Disaster
// recovery keeps its ParentNav wiring to Admin -- registry identity and
// direct-route access are unchanged -- but declares no admitted
// capability, so the Admin submenu must not carry it as a child.
func TestTodo_WEB_239_Integration(t *testing.T) {
	_, items := projectNavigation(testView(PageAdmin))
	admin, ok := projectedNavigationItem(items, PageAdmin)
	if !ok {
		t.Fatal("Admin navigation group is missing")
	}
	for _, child := range admin.Children {
		if child.Page == PageDisasterRecovery {
			t.Fatalf("Admin submenu still carries the unadmitted recovery surface: %+v", admin)
		}
	}
	definition, ok := LookupPage(PageDisasterRecovery)
	if !ok || definition.ParentNav != PageAdmin || definition.Admitted {
		t.Fatalf("disaster recovery registry entry changed unexpectedly: %+v", definition)
	}
	if _, ok := LookupRoute(definition.Route); !ok {
		t.Fatal("disaster recovery route no longer resolves through the page registry")
	}
}

// Fault: the recovery surface stays honest under fault —
// a load error and an unknown locale still render
// deterministically with no leaked keys and no panic.
func TestTodo_WEB_239_Fault(t *testing.T) {
	view := testView(PageDisasterRecovery)
	view.LoadError = "dial recovery: connection refused"
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
		t.Fatal("recovery surface renders nondeterministically under fault")
	}
	if strings.Contains(first, "⟦") {
		t.Fatal("recovery surface leaks a message key under fault")
	}
}
