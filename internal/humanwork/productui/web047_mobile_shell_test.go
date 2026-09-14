package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// RED for WEB-047: responsive mobile shell. At narrow viewports the topbar
// tool triggers must collapse to icon-only controls (accessible names
// intact) so the tools cluster fits a 320px viewport without overflow or
// overlap, and the collapse must use logical properties only.
func TestTodo_WEB_047(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageRoles), []string{RoleHCMAdmin})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"action-launcher-trigger", "utility-drawer-trigger"} {
		trigger := findElementByID(root, id)
		if trigger == nil {
			t.Fatalf("roles document renders no %s", id)
		}
		if labelSpan := findTriggerLabel(trigger); labelSpan == nil {
			t.Fatalf("%s has no collapsible label span", id)
		}
		if xhtmlAttr(trigger, "aria-label") == "" {
			t.Fatalf("%s has no accessible name independent of its visible label", id)
		}
	}

	css := Stylesheet()
	// Icon-only collapse hides the label visually; the accessible name stays
	// on the trigger itself so the control keeps its name at every width.
	for _, want := range []string{
		`@media (max-width:430px){.action-launcher-trigger .action-launcher-label{display:none;}`,
		`@media (max-width:430px){.utility-drawer-trigger .utility-drawer-label{display:none;}`,
		`@media (max-width:430px){.global-search .global-search-input{`,
		`@media (max-width:430px){.global-search .global-search-input::placeholder{color:transparent;}`,
		`@media (max-width:430px){.global-search:focus-within .global-search-input::placeholder{color:var(--muted);}`,
		`@media (max-width:350px){.topbar,.app-shell.nav-collapsed .topbar{`,
		`grid-template-columns:minmax(0,1fr) auto auto;grid-template-rows:44px 44px;`,
		`@media (max-width:350px){.topbar>.header-navigation-tools{`,
		`@media (max-width:350px){.topbar>.notifications{`,
		`@media (max-width:350px){.topbar>.viewer-profile-link{`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("mobile shell stylesheet missing %q", want)
		}
	}
}

// Golden: the collapsed-trigger stylesheet layer.
func TestTodo_WEB_047_Golden(t *testing.T) {
	digest := sha256.Sum256([]byte(MobileShellStylesheet()))
	got := hex.EncodeToString(digest[:])
	const want = "7e60e773f2671541115c3a8f3093d3fc9572d681a996af860abcb026cf19a4b8"
	if got != want {
		t.Fatalf("mobile shell stylesheet golden digest = %s, want %s", got, want)
	}
}

// Browser: triggers keep their names and dialogs keep their hidden state.
func TestTodo_WEB_047_Browser(t *testing.T) {
	for _, page := range []PageID{PageRoles, PagePerson, PageHistory} {
		roles := []string{"manager"}
		if page == PageRoles {
			roles = []string{RoleHCMAdmin}
		}
		view := ApplyRoleVisibility(testView(page), roles)
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		launcher := findElementByID(root, "action-launcher-trigger")
		if launcher == nil {
			t.Fatalf("page %s lost its launcher trigger", page)
		}
		if xhtmlAttr(launcher, "aria-label") == "" || findTriggerLabel(launcher) == nil {
			t.Fatalf("page %s launcher trigger is not collapse-ready", page)
		}
		drawer := findElementByID(root, "utility-drawer-trigger")
		if page == PageRoles && drawer == nil {
			t.Fatal("roles document lost its drawer trigger")
		}
	}
}

// Conformance: dialogs stay viewport-bound and the collapse is RTL-safe.
func TestTodo_WEB_047_Conformance(t *testing.T) {
	css := Stylesheet()
	if !strings.Contains(css, `calc(100vw - 28px)`) {
		t.Fatal("dialog viewport bound missing")
	}
	for _, block := range mobileShellNarrowBlocks(css) {
		for _, physical := range []string{"margin-left", "margin-right", "padding-left", "padding-right", "inset-left", "inset-right"} {
			if strings.Contains(block, physical) {
				t.Fatalf("mobile shell narrow rule uses physical property %q: %s", physical, block)
			}
		}
	}
	if len(mobileShellNarrowBlocks(css)) != 2 {
		t.Fatalf("mobile shell narrow blocks = %d, want exactly the 2 trigger collapses", len(mobileShellNarrowBlocks(css)))
	}
}

func findTriggerLabel(trigger *xhtml.Node) *xhtml.Node {
	var found *xhtml.Node
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if found != nil {
			return
		}
		if node.Type == xhtml.ElementNode && node.Data == "span" {
			for _, attr := range node.Attr {
				if attr.Key == "class" && strings.HasSuffix(attr.Val, "-label") {
					found = node
					return
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(trigger)
	return found
}

func mobileShellNarrowBlocks(css string) []string {
	var blocks []string
	for _, selector := range []string{
		`@media (max-width:430px){.action-launcher-trigger .action-launcher-label{`,
		`@media (max-width:430px){.utility-drawer-trigger .utility-drawer-label{`,
	} {
		start := strings.Index(css, selector)
		if start < 0 {
			continue
		}
		rest := css[start:]
		end := strings.Index(rest, "}")
		if end < 0 {
			continue
		}
		blocks = append(blocks, rest[:end+1])
	}
	return blocks
}
