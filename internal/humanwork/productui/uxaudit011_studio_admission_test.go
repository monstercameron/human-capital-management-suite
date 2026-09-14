package productui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// uxaudit011BaselineDestinations are the thirteen genuinely admitted
// destinations the 2026-09-12 live audit's own primary-navigation fetch
// found beside Experience Studio (the fourteenth, RED item). UXAUDIT-011
// must remove Studio without disturbing any of these.
var uxaudit011BaselineDestinations = []string{
	"/workspace/app/home",
	"/workspace/app/myself",
	"/workspace/app/journeys",
	"/workspace/app/work",
	"/workspace/app/history",
	"/workspace/app/people",
	"/workspace/app/organization",
	"/workspace/app/insights",
	"/workspace/app/admin",
	"/workspace/app/admin/worker-ids",
	"/workspace/app/admin/roles",
	"/workspace/app/admin/organization-visibility",
	"/workspace/app/appearance",
}

// TestTodo_UXAUDIT_011 is the PRIMARY: Experience Studio must not remain a
// first-class menu item for a role that has no usable authorized authoring
// capability behind it -- which today is every role, since no governed
// page-builder service exists -- while every other admitted destination
// keeps its menu slot and the Studio route itself keeps working directly.
func TestTodo_UXAUDIT_011(t *testing.T) {
	adminView := ApplyRoleVisibility(testView(PageHome), []string{RoleHCMAdmin})
	doc, err := Render(adminView)
	if err != nil {
		t.Fatal(err)
	}
	navigation := findElementByID(mustParse(t, doc), "workspace-navigation")
	if navigation == nil {
		t.Fatal("shell rendered no navigation landmark")
	}
	if linkForRoute(navigation, "/workspace/app/studio") != nil {
		t.Fatal("Experience Studio still renders a navigation link for the platform admin")
	}
	if strings.Contains(navigationText(navigation), "Experience Studio") {
		t.Fatal("Experience Studio text still appears in navigation")
	}

	// Nothing usable was lost: every one of the fourteen live-audited
	// destinations other than Studio keeps its menu slot.
	for _, route := range uxaudit011BaselineDestinations {
		if linkForRoute(navigation, route) == nil {
			t.Fatalf("admitted destination %q disappeared from navigation", route)
		}
	}

	// Omission from the menu is not deletion of the page: the route still
	// resolves and still explains, in its own honest empty state, why it
	// is unavailable -- it does not simulate a configuration service the
	// cell never published.
	studioDoc, err := Render(ApplyRoleVisibility(testView(PageStudio), []string{RoleHCMAdmin}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(studioDoc, "Custom pages cannot be edited here yet") {
		t.Fatal("Studio route stopped rendering its explanatory unavailable state")
	}
	if strings.Contains(studioDoc, "Validation passed") || strings.Contains(studioDoc, "Request publication") {
		t.Fatal("Studio route started simulating a capability the cell does not have")
	}
}

// TestTodo_UXAUDIT_011_Browser is a static SSR/DOM assertion parsing the
// rendered document -- this package's established idiom for a "Browser"
// matrix entry (see UXAUDIT-006's evidence) -- rather than a live
// Playwright run. It walks the actual parsed element tree, not rendered
// text, so it cannot be satisfied by an anchor that merely avoids the
// literal label.
func TestTodo_UXAUDIT_011_Browser(t *testing.T) {
	doc, err := Render(ApplyRoleVisibility(testView(PageHome), []string{RoleHCMAdmin}))
	if err != nil {
		t.Fatal(err)
	}
	root := mustParse(t, doc)
	navigation := findElementByID(root, "workspace-navigation")
	if navigation == nil {
		t.Fatal("shell rendered no navigation landmark")
	}
	var studioAnchor *xhtml.Node
	walkElements(navigation, func(node *xhtml.Node) {
		if node.Data == "a" && attr(node, "href") == "/workspace/app/studio" {
			studioAnchor = node
		}
	})
	if studioAnchor != nil {
		t.Fatal("navigation DOM still contains a Studio anchor element")
	}

	// The Admin group's rendered subnav accordion shrank to exactly its
	// four admitted children (Worker IDs, Roles, Organization visibility,
	// Brand & appearance); Studio and the eleven other unbuilt admin
	// fallback surfaces claim no <a> element anywhere in the group. The
	// group is located by its stable data-hcm-nav-group="admin" attribute
	// rather than by class, since My Work is also a "nav-group".
	var adminGroup *xhtml.Node
	walkElements(navigation, func(node *xhtml.Node) {
		if adminGroup == nil && attr(node, "data-hcm-nav-group") == "admin" {
			adminGroup = node
		}
	})
	if adminGroup == nil {
		t.Fatal("Admin navigation group is missing from the DOM")
	}
	// Each leaf renders a nav-link anchor plus an optional nav-favorite
	// toggle anchor, so count only the navigational links: one overview
	// leaf (pointing back at Admin itself) plus the four admitted
	// children -- Studio and the eleven other unbuilt admin fallback
	// surfaces contribute none.
	navLinks := 0
	walkElements(adminGroup, func(node *xhtml.Node) {
		if node.Data == "a" && strings.Contains(attr(node, "class"), "nav-link") {
			navLinks++
		}
	})
	if navLinks != 5 {
		t.Fatalf("Admin subnav has %d nav-link anchors, want 5 (overview plus 4 admitted children)", navLinks)
	}

	// Direct navigation to the unadmitted route claims no active
	// navigation leaf anywhere in the DOM: omission from the menu must
	// not leave a dangling "current" claim on a group that no longer
	// lists the page.
	studioDoc, err := Render(ApplyRoleVisibility(testView(PageStudio), []string{RoleHCMAdmin}))
	if err != nil {
		t.Fatal(err)
	}
	if primaryNavAriaCurrentCount(studioDoc) != 0 {
		t.Fatal("an unadmitted route still claims an active primary navigation leaf")
	}
	studioRoot := mustParse(t, studioDoc)
	studioNavigation := findElementByID(studioRoot, "workspace-navigation")
	if studioNavigation == nil {
		t.Fatal("shell rendered no navigation landmark for the Studio route")
	}
	var current *xhtml.Node
	walkElements(studioNavigation, func(node *xhtml.Node) {
		if attr(node, "aria-current") == "page" {
			current = node
		}
	})
	if current != nil {
		t.Fatalf("unadmitted Studio route left a dangling aria-current=page element inside navigation: %v", current)
	}
}

// TestTodo_UXAUDIT_011_Security proves omission from the menu is a
// presentation change only, in both directions: a role that could not use
// Studio before still cannot (admission never grants authority), and a
// role that remains authorized to the route is not newly blocked by the
// admission gate (admission never revokes authority either). It also
// proves the same rule holds against a hostile, forged authoritative
// navigation answer, not only against the local registry-driven path.
func TestTodo_UXAUDIT_011_Security(t *testing.T) {
	// Admission and authorization are orthogonal: PageVisible's per-role
	// answer for Studio is exactly what it was before Admitted existed.
	if !PageVisible(PageStudio, []string{RoleHCMAdmin}) {
		t.Fatal("the platform admin lost direct authorization to Studio because of a presentation-only change")
	}
	for _, roles := range [][]string{nil, {}, {"worker_self"}, {"manager"}, {"hr_partner"}} {
		if PageVisible(PageStudio, roles) {
			t.Fatalf("roles %v gained authorization to Studio because of a presentation-only change", roles)
		}
	}

	// Omission does not leak existence differently by role: neither an
	// authorized nor an unauthorized viewer's rendered navigation ever
	// names Studio or its route.
	for _, roles := range [][]string{{RoleHCMAdmin}, {"worker_self"}, nil} {
		doc, err := Render(ApplyRoleVisibility(testView(PageHome), roles))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "/workspace/app/studio") || strings.Contains(doc, "Experience Studio") {
			t.Fatalf("roles %v disclosed the Studio destination", roles)
		}
	}

	// A hostile authoritative projection that tries to smuggle Studio
	// through as pre-authorized is rejected outright: the validator
	// checks admission, not merely the caller's own Authorized flag, so
	// forging that flag cannot resurrect an unadmitted page. The whole
	// answer fails closed rather than rendering a partial, tampered menu.
	forged := AuthorizedNavigationProjection{
		Version: 1,
		Items: []AuthorizedNavigationItem{
			{Page: PageHome, Label: "Home", LabelKey: "page.home.label", Icon: "home", Href: Path(PageHome), Authorized: true},
			{
				Page: PageAdmin, Label: "Admin", LabelKey: "page.admin.label", Icon: "admin", Href: Path(PageAdmin), Authorized: true,
				Children: []AuthorizedNavigationItem{
					{Page: PageAdmin, Label: "Admin overview", LabelKey: "nav.admin_overview", Icon: "admin", Href: Path(PageAdmin), Authorized: true},
					{Page: PageStudio, Label: "Experience Studio", LabelKey: "page.studio.label", Icon: "studio", Href: Path(PageStudio), Authorized: true},
				},
			},
		},
	}
	view := ApplyNavigationProjection(NewView(PageHome, "tenant", "principal", "scope"), forged)
	if len(view.Navigation) != 0 || len(view.NavigationSupport) != 0 {
		t.Fatalf("forged projection naming an unadmitted page did not fail closed: items=%d support=%d", len(view.Navigation), len(view.NavigationSupport))
	}
	doc, err := Render(ApplyLocale(view, ResolveProductLocale("en-US")))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "/workspace/app/studio") || strings.Contains(doc, "Experience Studio") {
		t.Fatal("fail-closed rejection still leaked the forged Studio destination into markup")
	}
}

// TestTodo_UXAUDIT_011_Conformance pins the registry rule itself: an
// unadmitted page never reaches navigation regardless of PrimaryNav or
// ParentNav wiring (the zero value for Admitted is not permissive), the
// rule holds generically across the whole registry with no per-route
// exception, and the authoritative-projection validator enforces the same
// admission gate a hostile caller cannot route around.
func TestTodo_UXAUDIT_011_Conformance(t *testing.T) {
	// The zero value is not permissive. Proven directly against the
	// eligibility predicates, independent of the live registry's current
	// contents, so this holds even if every existing entry were admitted.
	zeroValuePrimary := PageDefinition{ID: "uxaudit011-fixture-primary", PrimaryNav: true}
	if navigationPrimaryEligible(zeroValuePrimary, nil) {
		t.Fatal("a page declaring PrimaryNav with no Admitted value was treated as navigable")
	}
	admittedPrimary := zeroValuePrimary
	admittedPrimary.Admitted = true
	if !navigationPrimaryEligible(admittedPrimary, nil) {
		t.Fatal("an explicitly admitted primary page was rejected")
	}
	zeroValueChild := PageDefinition{ID: "uxaudit011-fixture-child", ParentNav: PageAdmin}
	if navigationChildEligible(zeroValueChild, nil) {
		t.Fatal("a page declaring ParentNav with no Admitted value was treated as navigable")
	}
	admittedChild := zeroValueChild
	admittedChild.Admitted = true
	if !navigationChildEligible(admittedChild, nil) {
		t.Fatal("an explicitly admitted child page was rejected")
	}
	// Visibility still applies on top of admission -- admission alone is
	// necessary but not sufficient.
	if navigationPrimaryEligible(admittedPrimary, func(PageID) bool { return false }) {
		t.Fatal("an admitted page bypassed per-role visibility")
	}

	// The rule holds generically across the whole registry: every page
	// wired into navigation (PrimaryNav or ParentNav) is present in the
	// rendered tree if and only if it is Admitted. No hardcoded page name
	// appears in this loop -- it is the same check for Studio and for
	// every one of the ten other unbuilt admin fallback surfaces sharing
	// its ParentNav wiring pattern.
	flattened := map[PageID]bool{}
	var walk func([]NavItem)
	walk = func(items []NavItem) {
		for _, item := range items {
			flattened[item.Page] = true
			walk(item.Children)
		}
	}
	walk(defaultNavigation(ResolveProductLocale("en-US")))

	navWired, admittedWired, unadmittedWired := 0, 0, 0
	for _, definition := range PageDefinitions() {
		if !definition.PrimaryNav && definition.ParentNav == "" {
			continue
		}
		navWired++
		switch {
		case definition.Admitted && !flattened[definition.ID]:
			t.Fatalf("%s declares an admitted capability and navigation wiring but was not rendered", definition.ID)
		case definition.Admitted:
			admittedWired++
		case flattened[definition.ID]:
			t.Fatalf("%s has no admitted capability yet still claims a navigation slot", definition.ID)
		default:
			unadmittedWired++
		}
	}
	if navWired == 0 || admittedWired == 0 || unadmittedWired == 0 {
		t.Fatalf("fixture is vacuous: wired=%d admitted=%d unadmitted=%d", navWired, admittedWired, unadmittedWired)
	}
	if flattened[PageStudio] {
		t.Fatal("Studio reached the rendered navigation tree")
	}

	// The projection validator enforces the identical rule for a
	// server-supplied authoritative answer: every currently-unadmitted,
	// otherwise well-formed navigation item is rejected on its own.
	for _, definition := range PageDefinitions() {
		if definition.Admitted {
			continue
		}
		item := AuthorizedNavigationItem{
			Page: definition.ID, Label: definition.Label, LabelKey: definition.LabelKey, Icon: definition.Icon,
			Href: definition.Route, Authorized: true,
		}
		projection := AuthorizedNavigationProjection{Version: 1, Items: []AuthorizedNavigationItem{item}}
		if err := validateAuthorizedNavigationProjection(projection); err == nil {
			t.Fatalf("%s: unadmitted page was accepted by the navigation projection validator", definition.ID)
		}
	}
}
