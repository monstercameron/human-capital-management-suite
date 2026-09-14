package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

func TestBreadcrumbTrailRendersFromAdmittedItemsWithoutPageView(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(BreadcrumbTrail, BreadcrumbTrailProps{
		AriaLabel: "Breadcrumbs",
		Items: []BreadcrumbItem{
			{Label: "Admin", Href: "/workspace/app/admin"},
			{Label: "Roles", Current: true},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`aria-label="Breadcrumbs"`, `href="/workspace/app/admin"`, `aria-current="page"`, "Roles"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("narrow breadcrumb trail missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, `href="Roles"`) {
		t.Fatalf("current crumb became interactive: %s", markup)
	}
}

// RED for WEB-044: meaningful breadcrumb resolution. Every rendered page
// must resolve its trail from the canonical PageDefinitions ParentNav chain
// (never a second hierarchy), drop ancestors the identity cannot open,
// name the selected person on a profile, and stay silent for unknown pages.
func TestTodo_WEB_044(t *testing.T) {
	history := ApplyRoleVisibility(testView(PageHistory), []string{"manager"})
	crumbs := ResolveBreadcrumbs(history)
	if len(crumbs) != 2 || crumbs[0].Label != "My Work" || crumbs[0].Current || crumbs[0].Href != "/workspace/app/work" {
		t.Fatalf("history trail ancestors = %#v, want My Work link first", crumbs)
	}
	if last := crumbs[len(crumbs)-1]; !last.Current || last.Label != "Work History" || last.Href != "/workspace/app/history" {
		t.Fatalf("history trail current = %#v, want current Work History", last)
	}

	roles := ApplyRoleVisibility(testView(PageRoles), []string{RoleHCMAdmin})
	adminCrumbs := ResolveBreadcrumbs(roles)
	if len(adminCrumbs) != 2 || adminCrumbs[0].Label != "Admin" || adminCrumbs[0].Href != "/workspace/app/admin" {
		t.Fatalf("roles trail = %#v, want Admin ancestor first", adminCrumbs)
	}
	if last := adminCrumbs[len(adminCrumbs)-1]; !last.Current || last.Label != "Roles & access" {
		t.Fatalf("roles trail current = %#v, want current Roles & access", last)
	}

	// An identity that cannot open Work still sees the honest page it was
	// served, but the unauthorized ancestor stays unlinked and unlisted.
	restricted := ApplyRoleVisibility(testView(PageHistory), []string{"worker_self"})
	restCrumbs := ResolveBreadcrumbs(restricted)
	if len(restCrumbs) != 1 || restCrumbs[0].Label != "Work History" || !restCrumbs[0].Current {
		t.Fatalf("restricted history trail = %#v, want current page only", restCrumbs)
	}

	// A profile names its worker instead of repeating the generic page title.
	person := testView(PagePerson)
	personCrumbs := ResolveBreadcrumbs(person)
	if len(personCrumbs) != 1 || personCrumbs[0].Label != "Avery Patel · NW-40118" || !personCrumbs[0].Current {
		t.Fatalf("person trail = %#v, want current Avery Patel · NW-40118", personCrumbs)
	}
	unselected := testView(PagePerson)
	unselected.SelectedPerson = ""
	bareCrumbs := ResolveBreadcrumbs(unselected)
	if len(bareCrumbs) != 1 || bareCrumbs[0].Label != "Person" {
		t.Fatalf("unselected person trail = %#v, want generic Person label", bareCrumbs)
	}

	unknown := View{Page: PageID("no-such-page"), Locale: ResolveProductLocale("")}
	if got := ResolveBreadcrumbs(unknown); got != nil {
		t.Fatalf("unknown page trail = %#v, want nil", got)
	}
}

// Golden: the rendered breadcrumb bar for one authorized view.
func TestTodo_WEB_044_Golden(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageHistory), []string{"manager"})
	node, err := ui.RenderToString(Breadcrumbs(view, ResolveBreadcrumbs(view)))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(node))
	got := hex.EncodeToString(digest[:])
	const want = "a2817ac7e72a35814cfa1e47f4eda20d18b6a31e3195c691f8ca98555db3cbd0"
	if got != want {
		t.Fatalf("breadcrumb bar golden digest = %s, want %s", got, want)
	}
}

// Browser: landmarks, list structure, link vs current semantics, placement.
func TestTodo_WEB_044_Browser(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageHistory), []string{"manager"})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	bar := findBreadcrumbNav(root)
	if bar == nil {
		t.Fatal("history document renders no breadcrumb landmark")
	}
	if label := xhtmlAttr(bar, "aria-label"); label != "Breadcrumbs" {
		t.Fatalf("breadcrumb aria-label = %q, want Breadcrumbs", label)
	}
	items := breadcrumbItems(bar)
	if len(items) != 2 {
		t.Fatalf("breadcrumb items = %d, want 2", len(items))
	}
	link := findFirst(items[0], "a")
	if link == nil || xhtmlAttr(link, "href") != "/workspace/app/work" {
		t.Fatal("first breadcrumb is not a link to the Work ancestor")
	}
	current := findFirst(items[1], "span")
	if current == nil || xhtmlAttr(current, "aria-current") != "page" {
		t.Fatal("current breadcrumb is not a span marked aria-current=page")
	}
	if current.Parent != items[1] || findFirst(items[1], "a") != nil {
		t.Fatal("current breadcrumb must not contain a link")
	}
	head := findPageHead(root)
	if head == nil || !isBeforeIn(head, bar, findPageTitle(head)) {
		t.Fatal("breadcrumb bar is not placed above the page title")
	}
}

// Conformance: locale labels, single-crumb silence, in-shell destinations.
func TestTodo_WEB_044_Conformance(t *testing.T) {
	for locale, want := range map[string]string{"de-DE": "Brotkrumen", "ar": "مسار التنقل"} {
		view := ApplyRoleVisibility(testView(PageHistory), []string{"manager"})
		view.Locale = ResolveProductLocale(locale)
		node, err := ui.RenderToString(Breadcrumbs(view, ResolveBreadcrumbs(view)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(node, `aria-label="`+want+`"`) {
			t.Fatalf("%s breadcrumb bar missing aria-label %q: %s", locale, want, node)
		}
	}
	home := testView(PageHome)
	homeBar, err := ui.RenderToString(Breadcrumbs(home, ResolveBreadcrumbs(home)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(homeBar, "<nav") {
		t.Fatalf("single generic crumb renders a landmark: %s", homeBar)
	}
	for _, page := range []PageID{PageHistory, PageRoles, PageWorkerIDs, PageStudio} {
		roles := []string{"manager"}
		if page == PageRoles || page == PageWorkerIDs || page == PageStudio {
			roles = []string{RoleHCMAdmin}
		}
		view := ApplyRoleVisibility(testView(page), roles)
		for _, crumb := range ResolveBreadcrumbs(view) {
			if crumb.Current {
				continue
			}
			if _, ok := LookupRoute(crumb.Href); !ok {
				t.Fatalf("page %s ancestor crumb links outside the registry: %q", page, crumb.Href)
			}
		}
	}
	css := Stylesheet()
	for _, want := range []string{".breadcrumbs", ".breadcrumbs ol", ".breadcrumb-separator"} {
		if !strings.Contains(css, want) {
			t.Fatalf("controls stylesheet missing %q", want)
		}
	}
}

func xhtmlAttr(node *xhtml.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func findBreadcrumbNav(root *xhtml.Node) *xhtml.Node {
	var found *xhtml.Node
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if found != nil {
			return
		}
		if node.Type == xhtml.ElementNode && node.Data == "nav" {
			for _, attr := range node.Attr {
				if attr.Key == "class" && strings.Contains(attr.Val, "breadcrumbs") {
					found = node
					return
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func breadcrumbItems(bar *xhtml.Node) []*xhtml.Node {
	var items []*xhtml.Node
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && node.Data == "li" && node.Parent != nil && node.Parent.Data == "ol" {
			items = append(items, node)
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(bar)
	return items
}

func findFirst(root *xhtml.Node, tag string) *xhtml.Node {
	var found *xhtml.Node
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if found != nil {
			return
		}
		if node.Type == xhtml.ElementNode && node.Data == tag {
			found = node
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func findPageHead(root *xhtml.Node) *xhtml.Node {
	var found *xhtml.Node
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if found != nil {
			return
		}
		if node.Type == xhtml.ElementNode && node.Data == "div" {
			for _, attr := range node.Attr {
				if attr.Key == "class" && attr.Val == "page-head" {
					found = node
					return
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func findPageTitle(head *xhtml.Node) *xhtml.Node {
	var found *xhtml.Node
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if found != nil {
			return
		}
		if node.Type == xhtml.ElementNode && node.Data == "h1" {
			for _, attr := range node.Attr {
				if attr.Key == "id" && attr.Val == "page-title" {
					found = node
					return
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(head)
	return found
}

func isBeforeIn(root, first, second *xhtml.Node) bool {
	if first == nil || second == nil {
		return false
	}
	order := map[*xhtml.Node]int{}
	index := 0
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		order[node] = index
		index++
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	firstOrder, okFirst := order[first]
	secondOrder, okSecond := order[second]
	return okFirst && okSecond && firstOrder < secondOrder
}
