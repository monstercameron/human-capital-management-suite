package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

func TestPageHeadingRendersResolvedIdentityWithoutPageView(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(PageHeading, PageHeadingProps{
		Identity: PageIdentity{Page: PageRoles, Title: "Roles", Subtitle: "Manage access"},
		Trail:    ui.Text(""),
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="page-head"`, `data-hcm-page="roles"`, `id="page-title"`, "Roles", "Manage access"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("narrow page heading missing %q: %s", want, markup)
		}
	}
}

// RED for WEB-045: canonical page-identity header. The header must resolve
// its identity (stable page id, localized title/subtitle, scope label and
// scope destination) from the registry in one governed resolution instead
// of scattering ad-hoc view fields, and must stamp the stable page id on
// the rendered header for every registered page.
func TestTodo_WEB_045(t *testing.T) {
	history := ApplyRoleVisibility(testView(PageHistory), []string{"manager"})
	identity := ResolvePageIdentity(history)
	if identity.Page != PageHistory || identity.Title != "Workflow History" || identity.Subtitle == "" {
		t.Fatalf("history identity = %#v, want registry title and subtitle", identity)
	}
	if identity.ScopeHref != "" || identity.ScopeLabel != "" {
		t.Fatalf("direct authority leaked global acting context: %#v", identity)
	}

	// The scope control never advertises settings the identity cannot open.
	// (Settings is a universal support page under role visibility, so the
	// denial case uses a permissions projection that withholds it.)
	restricted := ApplyPagePermissions(testView(PageHistory), []RolePagePermission{
		{Version: 1, RoleID: "viewer", Page: PageHistory, View: true},
	})
	restIdentity := ResolvePageIdentity(restricted)
	if restIdentity.ScopeHref != "" {
		t.Fatalf("restricted scope href = %q, want no settings destination", restIdentity.ScopeHref)
	}

	// Home greets the viewer; unknown pages fall back to Home, never blank.
	home := ResolvePageIdentity(testView(PageHome))
	if home.Page != PageHome || !strings.Contains(home.Title, "Taylor") {
		t.Fatalf("home identity = %#v, want greeting for Taylor", home)
	}
	unknown := ResolvePageIdentity(View{Page: PageID("no-such-page"), Locale: ResolveProductLocale("")})
	if unknown.Page != PageHome || unknown.Title == "" || unknown.Subtitle == "" {
		t.Fatalf("unknown page identity = %#v, want Home fallback", unknown)
	}

	// An admitted profile identifies its worker in the H1, using the same
	// governed label as the breadcrumb rather than generic registry copy.
	person := ResolvePageIdentity(testView(PagePerson))
	if person.Title != "Avery Patel · NW-40118" {
		t.Fatalf("person identity title = %q, want admitted display name and worker number", person.Title)
	}
}

// Golden: the rendered identity header for one authorized view.
func TestTodo_WEB_045_Golden(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageHistory), []string{"manager"})
	node, err := ui.RenderToString(PageIdentityHeader(view))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(node))
	got := hex.EncodeToString(digest[:])
	const want = "cc4d3b0c6e3b09d69b82eb3442dc4677b1a622849315a5237c1abea1d22cae5f"
	if got != want {
		t.Fatalf("page-identity header golden digest = %s, want %s", got, want)
	}
}

// Browser: stable identity stamp, labelledby wiring, scope control states.
func TestTodo_WEB_045_Browser(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageHistory), []string{"manager"})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	head := findPageHead(root)
	if head == nil {
		t.Fatal("history document renders no page-head")
	}
	if got := xhtmlAttr(head, "data-hcm-page"); got != "history" {
		t.Fatalf("page-head data-hcm-page = %q, want stable history id", got)
	}
	title := findPageTitle(head)
	if title == nil || textContent(title) != "Workflow History" {
		t.Fatal("page-head h1 does not carry the registry title")
	}
	mains := collectElements(root, "main")
	if len(mains) != 1 || xhtmlAttr(mains[0], "aria-labelledby") != "page-title" {
		t.Fatal("main landmark lost its page-title labelling")
	}
	scopeLink := findScopeLink(head)
	if scopeLink != nil {
		t.Fatal("informational scope must not masquerade as a context switch")
	}

	restrictedDoc, err := Render(ApplyPagePermissions(testView(PageHistory), []RolePagePermission{
		{Version: 1, RoleID: "viewer", Page: PageHistory, View: true},
	}))
	if err != nil {
		t.Fatal(err)
	}
	restrictedRoot, err := xhtml.Parse(strings.NewReader(restrictedDoc))
	if err != nil {
		t.Fatal(err)
	}
	restrictedHead := findPageHead(restrictedRoot)
	if restrictedHead == nil {
		t.Fatal("restricted history document renders no page-head")
	}
	if findScopeLink(restrictedHead) != nil {
		t.Fatal("restricted scope control advertises an unauthorized settings destination")
	}
}

// Conformance: every registered page resolves a complete identity in every
// catalog locale, and the header stamp always matches the stable page id.
func TestTodo_WEB_045_Conformance(t *testing.T) {
	for _, definition := range PageDefinitions() {
		roles := []string{"manager"}
		if !PageVisible(definition.ID, roles) {
			roles = []string{RoleHCMAdmin}
		}
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			view := ApplyRoleVisibility(testView(definition.ID), roles)
			view.Locale = ResolveProductLocale(locale)
			view = ApplyLocale(view, view.Locale)
			identity := ResolvePageIdentity(view)
			if identity.Page != definition.ID || identity.Title == "" || identity.Subtitle == "" {
				t.Fatalf("page %s locale %s identity incomplete: %#v", definition.ID, locale, identity)
			}
			if identity.ScopeLabel != "" || identity.ScopeHref != "" {
				t.Fatalf("page %s locale %s leaked direct acting context: %#v", definition.ID, locale, identity)
			}
			node, err := ui.RenderToString(PageIdentityHeader(view))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(node, `data-hcm-page="`+string(definition.ID)+`"`) {
				t.Fatalf("page %s header missing its stable identity stamp", definition.ID)
			}
			if strings.Contains(node, "⟦") {
				t.Fatalf("page %s locale %s header leaks an unresolved key: %s", definition.ID, locale, node)
			}
		}
	}
}

func textContent(node *xhtml.Node) string {
	var builder strings.Builder
	var walk func(current *xhtml.Node)
	walk = func(current *xhtml.Node) {
		if current.Type == xhtml.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}

func collectElements(root *xhtml.Node, tag string) []*xhtml.Node {
	var found []*xhtml.Node
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && node.Data == tag {
			found = append(found, node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func findScopeLink(head *xhtml.Node) *xhtml.Node {
	var found *xhtml.Node
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if found != nil {
			return
		}
		if node.Type == xhtml.ElementNode && node.Data == "a" {
			for ancestor := node.Parent; ancestor != nil && ancestor != head; ancestor = ancestor.Parent {
				for _, attr := range ancestor.Attr {
					if attr.Key == "class" && attr.Val == "scope-wrap" {
						found = node
						return
					}
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
