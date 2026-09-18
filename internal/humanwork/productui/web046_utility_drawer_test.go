package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// RED for WEB-046: contextual utility drawer. The topbar must offer a
// drawer whose sections derive from the current page context — registry
// parent/siblings plus the contextual authorized starts — never a second
// hierarchy, never an unauthorized destination, and absent entirely when
// the context yields nothing.
func TestTodo_WEB_046(t *testing.T) {
	history := ApplyRoleVisibility(testView(PageHistory), []string{"manager"})
	sections := utilityDrawerSections(history)
	related := findDrawerSection(sections, "related")
	if related == nil || len(related.Items) != 1 || related.Items[0].Href != "/workspace/app/work" {
		t.Fatalf("history drawer sections = %#v, want Related with My Work parent", sections)
	}
	if related.Items[0].Label != "My Work" {
		t.Fatalf("history parent label = %q, want registry My Work", related.Items[0].Label)
	}

	// UXAUDIT-011 removed Studio and the ten other unbuilt admin fallback
	// surfaces from navigation, so Roles now has only three real siblings
	// (Worker IDs, Organization visibility, Brand & appearance) plus the
	// Admin parent.
	roles := ApplyRoleVisibility(testView(PageRoles), []string{RoleHCMAdmin})
	adminSections := utilityDrawerSections(roles)
	adminRelated := findDrawerSection(adminSections, "related")
	if adminRelated == nil || len(adminRelated.Items) != 4 {

		t.Fatalf("roles drawer related = %#v, want Admin parent plus 3 siblings", adminSections)
	}
	if adminRelated.Items[0].Href != "/workspace/app/admin" {
		t.Fatalf("roles drawer first item = %#v, want Admin parent first", adminRelated.Items[0])
	}

	// A profile offers its launchable workflows when the identity may start them.
	person := testView(PagePerson)
	personSections := utilityDrawerSections(person)
	actions := findDrawerSection(personSections, "actions")
	if actions == nil || len(actions.Items) != 2 {
		t.Fatalf("person drawer sections = %#v, want Actions with 2 workflows", personSections)
	}
	if !strings.Contains(actions.Items[0].Href, "worker=worker-avery") {
		t.Fatalf("person action href = %q, want worker-bound launch", actions.Items[0].Href)
	}

	// A denied sibling set stays unlisted; a context with nothing to offer
	// renders no drawer at all.
	denied := ApplyPagePermissions(testView(PageRoles), []RolePagePermission{
		{Version: 1, RoleID: "viewer", Page: PageRoles, View: true},
	})
	if got := utilityDrawerSections(denied); len(got) != 0 {
		t.Fatalf("denied drawer sections = %#v, want none", got)
	}
	if got := utilityDrawerSections(testView(PageHome)); len(got) != 0 {
		t.Fatalf("home drawer sections = %#v, want none", got)
	}
	unknown := View{Page: PageID("no-such-page"), Locale: ResolveProductLocale("")}
	if got := utilityDrawerSections(unknown); len(got) != 0 {
		t.Fatalf("unknown page drawer sections = %#v, want none", got)
	}

	// Derivation is deterministic for one view.
	if again := utilityDrawerSections(history); !reflect.DeepEqual(sections, again) {
		t.Fatal("utility drawer derivation is not deterministic")
	}
}

// Golden: the rendered drawer for one authorized nested view.
func TestTodo_WEB_046_Golden(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageRoles), []string{RoleHCMAdmin})
	node, err := ui.RenderToString(UtilityDrawer(utilityDrawerProps(view)))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(node))
	got := hex.EncodeToString(digest[:])

	// UXAUDIT-011 removed Studio and the ten other unbuilt admin fallback
	// surfaces from navigation, shrinking the roles page's "Related pages"
	// section from 16 items to 4 (Admin, Worker IDs, Organization
	// visibility, Brand & appearance); re-pinned after inspecting the
	// rendered markup to confirm no stub page or unresolved key leaked in.
	// Re-pinned 2026-09-15: the rendered markup was already this digest at
	// main; inspected, it still lists exactly those four related pages.
	// Re-pinned 2026-09-18: the drawer now renders inside a root element (the
	// anchor for its popover and the "inside" for focus dismissal), is no
	// longer aria-modal, and has a head with its title and an icon close
	// button, matching the Start an action launcher. Inspected: the same four
	// related pages, no unresolved key.
	const want = "c4a3706d87e5974a069c6cc41eeb5511653ed4c9fc7f544663c514a35dc85da5"
	if got != want {
		t.Fatalf("utility drawer golden digest = %s, want %s", got, want)
	}
}

// TestUtilityDrawerIsTheLaunchersKindOfPopover pins the structure the
// dismissal depends on. usePopoverFocusDismissal treats focus or a pointer
// landing outside the element with id "utility-drawer" as leaving the drawer;
// when the drawer rendered as a bare fragment there was no such element, so it
// never closed on blur. The popover is non-modal like the launcher's: a panel
// that closes when you click past it does not make the page inert.
func TestUtilityDrawerIsTheLaunchersKindOfPopover(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageRoles), []string{RoleHCMAdmin})
	node, err := ui.RenderToString(UtilityDrawer(utilityDrawerProps(view)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(node, `<div class="utility-drawer-root" id="utility-drawer">`) {
		t.Fatalf("the drawer is not wrapped in the root element its dismissal tests against: %.120s", node)
	}
	trigger := strings.Index(node, `id="utility-drawer-trigger"`)
	dialog := strings.Index(node, `id="utility-drawer-dialog"`)
	if trigger < 0 || dialog < trigger || !strings.HasSuffix(node, "</div></div>") {
		t.Fatal("the trigger and the dialog are not both inside the drawer root")
	}
	if strings.Contains(node, "aria-modal") {
		t.Fatal("the drawer declares itself modal but closes on an outside click")
	}
	head := strings.Index(node, `class="utility-drawer-head"`)
	closeAt := strings.Index(node, `id="utility-drawer-close"`)
	if head < 0 || closeAt < head || strings.Index(node, `class="utility-drawer-section"`) < closeAt {
		t.Fatal("the close control is not in the dialog head ahead of the sections")
	}
}

// TestUtilityDrawerGroupLabelOutranksTheTypeScale: the drawer's group label
// is an h2 for the outline but a 0.75rem label to the eye. The type scale's
// heading rule ties a single class and comes later, so the label must be
// scoped under the dialog or it renders at section-heading size.
func TestUtilityDrawerGroupLabelOutranksTheTypeScale(t *testing.T) {
	css := Stylesheet()
	rule := ".utility-drawer-dialog .utility-drawer-section-title{"
	at := strings.Index(css, rule)
	if at < 0 {
		t.Fatalf("the group label rule is not scoped under the dialog: %s missing", rule)
	}
	body := css[at+len(rule):]
	body = body[:strings.Index(body, "}")]
	if !strings.Contains(body, "font-size:0.75rem") {
		t.Fatalf("the group label is not the launcher's 0.75rem label size: %s", body)
	}
}

// Browser: trigger semantics, hidden dialog, in-shell destinations.
func TestTodo_WEB_046_Browser(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageRoles), []string{RoleHCMAdmin})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	trigger := findElementByID(root, "utility-drawer-trigger")
	if trigger == nil {
		t.Fatal("roles document renders no utility drawer trigger")
	}
	if xhtmlAttr(trigger, "aria-haspopup") != "dialog" || xhtmlAttr(trigger, "aria-controls") != "utility-drawer-dialog" || xhtmlAttr(trigger, "aria-expanded") != "false" {
		t.Fatal("drawer trigger lacks dialog semantics")
	}
	dialog := findElementByID(root, "utility-drawer-dialog")
	if dialog == nil {
		t.Fatal("roles document renders no utility drawer dialog")
	}
	if !hasAttr(dialog, "hidden") {
		t.Fatal("closed drawer dialog is not hidden")
	}
	for _, link := range collectElements(dialog, "a") {
		parsed, err := url.Parse(xhtmlAttr(link, "href"))
		if err != nil || parsed.IsAbs() || parsed.Host != "" {
			t.Fatalf("drawer link escapes the shell: %q", xhtmlAttr(link, "href"))
		}
		if _, ok := LookupRoute(parsed.Path); !ok {
			t.Fatalf("drawer link leaves the page registry: %q", xhtmlAttr(link, "href"))
		}
	}
	if len(collectElements(dialog, "a")) != 4 {

		t.Fatalf("drawer links = %d, want 4 related and no actions on roles page", len(collectElements(dialog, "a")))
	}

	homeDoc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	homeRoot, err := xhtml.Parse(strings.NewReader(homeDoc))
	if err != nil {
		t.Fatal(err)
	}
	if findElementByID(homeRoot, "utility-drawer-trigger") != nil {
		t.Fatal("home renders a drawer trigger with no contextual sections")
	}
}

// Conformance: locale labels, typed stylesheet, empty-drawer silence.
func TestTodo_WEB_046_Conformance(t *testing.T) {
	for locale, want := range map[string]string{"de-DE": "Seitenwerkzeuge", "ar": "أدوات الصفحة"} {
		view := ApplyRoleVisibility(testView(PageRoles), []string{RoleHCMAdmin})
		view.Locale = ResolveProductLocale(locale)
		view = ApplyLocale(view, view.Locale)
		node, err := ui.RenderToString(UtilityDrawer(utilityDrawerProps(view)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(node, `aria-label="`+want+`"`) {
			t.Fatalf("%s drawer dialog missing aria-label %q", locale, want)
		}
		if strings.Contains(node, "⟦") {
			t.Fatalf("%s drawer leaks an unresolved key: %s", locale, node)
		}
	}
	empty, err := ui.RenderToString(UtilityDrawer(utilityDrawerProps(testView(PageHome))))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(empty, "utility-drawer") {
		t.Fatalf("empty drawer renders markup: %s", empty)
	}
	css := Stylesheet()
	for _, want := range []string{".utility-drawer", ".utility-drawer-trigger", ".utility-drawer-dialog", ".utility-drawer-section"} {
		if !strings.Contains(css, want) {
			t.Fatalf("controls stylesheet missing %q", want)
		}
	}
}

func findDrawerSection(sections []UtilityDrawerSection, id string) *UtilityDrawerSection {
	for index := range sections {
		if sections[index].ID == id {
			return &sections[index]
		}
	}
	return nil
}
