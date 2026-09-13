package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-238: assistive-technology compatibility.
// The registry owns every product surface, but no
// compatibility qualification exists: qualifying
// assistive-technology compatibility has no exposure
// point and the first surface invents qualification data
// by convention. The compiler needs the registered
// surface — canonical identity, route, and an honest
// fallback that qualifies nothing until the governed
// qualification service publishes, with compatibility
// truth staying server authority — so the surface
// resolves today without a second source of business
// authority.
func TestTodo_WEB_238(t *testing.T) {
	definition, ok := LookupPage(PageAssistiveTech)
	if !ok {
		t.Fatal("assistive-technology compatibility unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("assistive-technology compatibility incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageAssistiveTech {
		t.Fatal("assistive-technology compatibility route does not round-trip")
	}
	doc, err := Render(testView(PageAssistiveTech))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("assistive-technology compatibility exposes an unresolved message key")
	}
	for _, invented := range []string{"NVDA passed", "VoiceOver pending", "tested by Asha", "compatible ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("assistive-technology compatibility invents qualification data: %q", invented)
		}
	}
}

// Golden: the registered compatibility definition and
// its fallback copy.
func TestTodo_WEB_238_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageAssistiveTech)
	if !ok {
		t.Fatal("assistive-technology compatibility unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("assistive_tech.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("assistive_tech.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "7a24b1f6fd229ff06c87bd2c286352aae84115f4bc7b49f4d6c389c1954c7f11"
	if got != want {
		t.Fatalf("assistive-technology compatibility digest = %s, want %s", got, want)
	}
}

// Browser: assistive-technology compatibility renders
// deterministically and round-trips its route.
func TestTodo_WEB_238_Browser(t *testing.T) {
	first, err := Render(testView(PageAssistiveTech))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageAssistiveTech))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("assistive-technology compatibility renders nondeterministically")
	}
	definition, _ := LookupPage(PageAssistiveTech)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageAssistiveTech {
		t.Fatal("assistive-technology compatibility route does not round-trip")
	}
}

// Conformance: assistive-technology compatibility keeps
// the registry contract — visible to the platform
// admin, hidden from the role-less baseline and the
// plain employee, ordered, and honest in every locale.
func TestTodo_WEB_238_Conformance(t *testing.T) {
	if !PageVisible(PageAssistiveTech, []string{RoleHCMAdmin}) {
		t.Fatal("assistive-technology compatibility hidden from the platform admin")
	}
	if PageVisible(PageAssistiveTech, nil) {
		t.Fatal("assistive-technology compatibility visible without roles")
	}
	if PageVisible(PageAssistiveTech, []string{"worker_self"}) {
		t.Fatal("assistive-technology compatibility visible to the plain employee")
	}
	definition, _ := LookupPage(PageAssistiveTech)
	if definition.PrimaryNav {
		t.Fatal("assistive-technology compatibility claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageAssistiveTech), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("assistive-technology compatibility leaks a key in %s", code)
		}
	}
}

// Security: unauthorized viewers are offered no assistive
// route — the qualification surface never leaks an admin
// route to identities that cannot open it.
func TestTodo_WEB_238_Security(t *testing.T) {
	for _, roles := range [][]string{nil, {"worker_self"}, {"manager"}} {
		doc, err := Render(ApplyRoleVisibility(testView(PageAssistiveTech), roles))
		if err != nil {
			t.Fatal(err)
		}
		for _, route := range []string{"/workspace/app/admin/roles", "/workspace/app/admin/policy-studio"} {
			if strings.Contains(doc, route) {
				t.Fatalf("admin route %q leaks to %v", route, roles)
			}
		}
	}
	adminDoc, err := Render(ApplyRoleVisibility(testView(PageAssistiveTech), []string{RoleHCMAdmin}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(adminDoc, "⟦") {
		t.Fatal("compatibility surface leaks a message key to the platform admin")
	}
}

// Integration: the compatibility surface participates
// in admin navigation — the Admin submenu carries it as
// its last child, so registry and navigation agree.
// UXAUDIT-011 requires navigation availability to derive from the page
// registry's admission gate rather than ParentNav wiring alone. Assistive
// tech keeps its ParentNav wiring to Admin -- registry identity and
// direct-route access are unchanged -- but declares no admitted
// capability, so the Admin submenu must not carry it as a child.
func TestTodo_WEB_238_Integration(t *testing.T) {
	_, items := projectNavigation(testView(PageAdmin))
	admin, ok := projectedNavigationItem(items, PageAdmin)
	if !ok {
		t.Fatal("Admin navigation group is missing")
	}
	for _, child := range admin.Children {
		if child.Page == PageAssistiveTech {
			t.Fatalf("Admin submenu still carries the unadmitted compatibility surface: %+v", admin)
		}
	}
	definition, ok := LookupPage(PageAssistiveTech)
	if !ok || definition.ParentNav != PageAdmin || definition.Admitted {
		t.Fatalf("assistive tech registry entry changed unexpectedly: %+v", definition)
	}
	if _, ok := LookupRoute(definition.Route); !ok {
		t.Fatal("assistive tech route no longer resolves through the page registry")
	}
}

// Fault: the compatibility surface stays honest under
// fault — a load error and an unknown locale still
// render deterministically with no leaked keys and no
// panic.
func TestTodo_WEB_238_Fault(t *testing.T) {
	view := testView(PageAssistiveTech)
	view.LoadError = "dial qualification: connection refused"
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
		t.Fatal("compatibility surface renders nondeterministically under fault")
	}
	if strings.Contains(first, "⟦") {
		t.Fatal("compatibility surface leaks a message key under fault")
	}
}
