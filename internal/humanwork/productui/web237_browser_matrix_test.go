package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-237: the production browser matrix. The
// registry owns every product surface, but no browser
// qualification exists: qualifying the production
// browser matrix has no exposure point and the first
// surface invents qualification data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that qualifies
// nothing until the governed qualification service
// publishes, with matrix truth staying server authority
// — so the surface resolves today without a second
// source of business authority.
func TestTodo_WEB_237(t *testing.T) {
	definition, ok := LookupPage(PageBrowserMatrix)
	if !ok {
		t.Fatal("production browser matrix unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("production browser matrix incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageBrowserMatrix {
		t.Fatal("production browser matrix route does not round-trip")
	}
	doc, err := Render(testView(PageBrowserMatrix))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("production browser matrix exposes an unresolved message key")
	}
	for _, invented := range []string{"Chrome 126 passed", "Safari pending", "qualified by Rook", "matrix ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("production browser matrix invents qualification data: %q", invented)
		}
	}
}

// Golden: the registered browser matrix definition and
// its fallback copy.
func TestTodo_WEB_237_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageBrowserMatrix)
	if !ok {
		t.Fatal("production browser matrix unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("browser_matrix.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("browser_matrix.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "090e25a05b91b38c1d2aed17ab95fbc0d9a18078e34449636bc60a30becadee9"
	if got != want {
		t.Fatalf("production browser matrix digest = %s, want %s", got, want)
	}
}

// Browser: production browser matrix renders
// deterministically and round-trips its route.
func TestTodo_WEB_237_Browser(t *testing.T) {
	first, err := Render(testView(PageBrowserMatrix))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageBrowserMatrix))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("production browser matrix renders nondeterministically")
	}
	definition, _ := LookupPage(PageBrowserMatrix)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageBrowserMatrix {
		t.Fatal("production browser matrix route does not round-trip")
	}
}

// Conformance: production browser matrix keeps the
// registry contract — visible to the platform admin,
// hidden from the role-less baseline and the plain
// employee, ordered, and honest in every locale.
func TestTodo_WEB_237_Conformance(t *testing.T) {
	if !PageVisible(PageBrowserMatrix, []string{RoleHCMAdmin}) {
		t.Fatal("production browser matrix hidden from the platform admin")
	}
	if PageVisible(PageBrowserMatrix, nil) {
		t.Fatal("production browser matrix visible without roles")
	}
	if PageVisible(PageBrowserMatrix, []string{"worker_self"}) {
		t.Fatal("production browser matrix visible to the plain employee")
	}
	definition, _ := LookupPage(PageBrowserMatrix)
	if definition.PrimaryNav {
		t.Fatal("production browser matrix claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageBrowserMatrix), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("production browser matrix leaks a key in %s", code)
		}
	}
}

// Security: unauthorized viewers are offered no browser
// matrix route — the qualification surface never leaks
// an admin route to identities that cannot open it.
func TestTodo_WEB_237_Security(t *testing.T) {
	for _, roles := range [][]string{nil, {"worker_self"}, {"manager"}} {
		doc, err := Render(ApplyRoleVisibility(testView(PageBrowserMatrix), roles))
		if err != nil {
			t.Fatal(err)
		}
		for _, route := range []string{"/workspace/app/admin/roles", "/workspace/app/admin/policy-studio"} {
			if strings.Contains(doc, route) {
				t.Fatalf("admin route %q leaks to %v", route, roles)
			}
		}
	}
	adminDoc, err := Render(ApplyRoleVisibility(testView(PageBrowserMatrix), []string{RoleHCMAdmin}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(adminDoc, "⟦") {
		t.Fatal("browser matrix leaks a message key to the platform admin")
	}
}

// Integration: UXAUDIT-011 requires navigation availability to derive from
// the page registry's admission gate rather than ParentNav wiring alone.
// The browser matrix keeps its ParentNav wiring to Admin -- registry
// identity and direct-route access are unchanged -- but declares no
// admitted capability, so the Admin submenu must not carry it as a child.
func TestTodo_WEB_237_Integration(t *testing.T) {
	_, items := projectNavigation(testView(PageAdmin))
	admin, ok := projectedNavigationItem(items, PageAdmin)
	if !ok {
		t.Fatal("Admin navigation group is missing")
	}
	for _, child := range admin.Children {
		if child.Page == PageBrowserMatrix {
			t.Fatalf("Admin submenu still carries the unadmitted browser matrix: %+v", admin)
		}
	}
	definition, ok := LookupPage(PageBrowserMatrix)
	if !ok || definition.ParentNav != PageAdmin || definition.Admitted {
		t.Fatalf("browser matrix registry entry changed unexpectedly: %+v", definition)
	}
	if _, ok := LookupRoute(definition.Route); !ok {
		t.Fatal("browser matrix route no longer resolves through the page registry")
	}
}

// Fault: the browser matrix stays honest under fault —
// a load error and an unknown locale still render
// deterministically with no leaked keys and no panic.
func TestTodo_WEB_237_Fault(t *testing.T) {
	view := testView(PageBrowserMatrix)
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
		t.Fatal("browser matrix renders nondeterministically under fault")
	}
	if strings.Contains(first, "⟦") {
		t.Fatal("browser matrix leaks a message key under fault")
	}
}
