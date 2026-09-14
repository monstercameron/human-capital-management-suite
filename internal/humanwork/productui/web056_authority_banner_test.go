package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// RED for WEB-056: persistent acting-authority banner. Whenever the server
// projects a valid current authority context, the shell must carry a
// persistent strip naming exactly that authority — tenant, acting context,
// delegator, expiry, elevation — straight from the same projection that
// feeds the switcher, never a second record. Direct or missing authority is
// intentionally quiet.
func TestTodo_WEB_056(t *testing.T) {
	view := testView(PageHome)
	view.ContextSwitcher = web056Fixture()
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	shell := findAppShell(root)
	if shell == nil {
		t.Fatal("document renders no app shell")
	}
	banner := findElementByID(root, "acting-authority")
	if banner == nil {
		t.Fatal("projected authority context renders no banner")
	}
	body := textContent(banner)
	for _, want := range []string{"HarborCare", "Covering HR", "Maya Chen", "2026-09-18"} {
		if !strings.Contains(body, want) {
			t.Fatalf("authority banner missing %q: %q", want, body)
		}
	}
	header := findFirst(shell, "header")
	grid := findClassNode(shell, "shell-grid")
	if header == nil || grid == nil {
		t.Fatal("shell chrome incomplete")
	}
	if !isBeforeIn(shell, header, banner) || !isBeforeIn(shell, banner, grid) {
		t.Fatal("authority banner is not placed between topbar and content")
	}

	// UXAUDIT-007: an own (non-delegated, non-elevated) authority is now
	// quiet. This case previously asserted the opposite -- that a plain
	// "Your own authority" projection still banners -- which was exactly
	// the RED the live audit found ("Acting as yourself" persisting on an
	// ordinary self-context page). Persistence now covers only the
	// authority that actually changes what the viewer can do: delegated,
	// view-as, elevated, or break-glass, never ordinary self.
	own := testView(PageHome)
	own.ContextSwitcher = web056Fixture()
	own.ContextSwitcher.Current = AuthorityContext{
		TenantID: "tenant-a", TenantName: "HarborCare",
		ActingContextID: "self-a", ActingContextName: "Your own authority",
	}
	ownDoc, err := Render(own)
	if err != nil {
		t.Fatal(err)
	}
	ownRoot, err := xhtml.Parse(strings.NewReader(ownDoc))
	if err != nil {
		t.Fatal(err)
	}
	if ownBanner := findElementByID(ownRoot, "acting-authority"); ownBanner != nil {
		t.Fatalf("ordinary self authority renders a banner: %q", textContent(ownBanner))
	}

	// No projection means no strip.
	quietDoc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	quietRoot, err := xhtml.Parse(strings.NewReader(quietDoc))
	if err != nil {
		t.Fatal(err)
	}
	if findElementByID(quietRoot, "acting-authority") != nil {
		t.Fatal("shell renders an authority banner without a projection")
	}
}

// Golden: the authority banner for a fixed delegated projection.
func TestTodo_WEB_056_Golden(t *testing.T) {
	node, err := ui.RenderToString(ActingAuthorityBanner(web056Fixture()))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(node))
	if got := hex.EncodeToString(digest[:]); got != "de98e9a1b9c981bbf36b7c223707ea0dfffe72f202d3bdf30d54fc7fab9c9cdf" {
		t.Fatalf("authority banner golden mismatch:\n%s\nwant digest de98e9a1b9c981bbf36b7c223707ea0dfffe72f202d3bdf30d54fc7fab9c9cdf", node)
	}
}

// Browser: the banner strip parses as a labelled section placed ahead of
// the content grid, with no positive tabindex to steal keyboard flow.
func TestTodo_WEB_056_Browser(t *testing.T) {
	view := testView(PageHome)
	view.ContextSwitcher = web056Fixture()
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	banner := findElementByID(root, "acting-authority")
	if banner == nil {
		t.Fatal("rendered document has no authority banner")
	}
	labelledBy := xhtmlAttr(banner, "aria-labelledby")
	if labelledBy == "" {
		t.Fatal("authority banner names no labelling element")
	}
	if findElementByID(root, labelledBy) == nil {
		t.Fatalf("authority banner labelling element %q missing", labelledBy)
	}
	var positive int
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			for _, attr := range node.Attr {
				if attr.Key == "tabindex" && strings.TrimSpace(attr.Val) != "" && attr.Val != "0" && !strings.HasPrefix(attr.Val, "-") {
					positive++
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(banner)
	if positive != 0 {
		t.Fatalf("authority banner carries %d positive tabindex stops", positive)
	}
}

// Conformance: locales, stylesheet, invalid projections render nothing.
func TestTodo_WEB_056_Conformance(t *testing.T) {
	for locale, title := range map[string]string{"en-US": "Acting authority", "de-DE": "Handelnde Autorität", "ar": "سلطة التصرف"} {
		props := web056Fixture()
		props.I18nProps = I18nProps{Locale: ResolveProductLocale(locale)}
		node, err := ui.RenderToString(ActingAuthorityBanner(props))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(node, title) {
			t.Fatalf("%s banner missing title %q: %s", locale, title, node)
		}
		if strings.Contains(node, "⟦") {
			t.Fatalf("%s banner leaks an unresolved key: %s", locale, node)
		}
	}
	for name, props := range map[string]ContextSwitcherProps{
		"empty":   {},
		"invalid": {Current: AuthorityContext{Delegated: true, Delegator: "Maya Chen"}},
		"direct": {Current: AuthorityContext{
			TenantID: "tenant-a", TenantName: "HarborCare", ActingContextID: "self-a", ActingContextName: "Your own authority",
		}},
	} {
		node, err := ui.RenderToString(ActingAuthorityBanner(props))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(node, "acting-authority") {
			t.Fatalf("%s projection renders a banner: %s", name, node)
		}
	}
	css := Stylesheet()
	for _, want := range []string{".acting-authority-banner", ".acting-authority-title", ".acting-authority-detail"} {
		if !strings.Contains(css, want) {
			t.Fatalf("controls stylesheet missing %q", want)
		}
	}
}

func web056Fixture() ContextSwitcherProps {
	return ContextSwitcherProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Current: AuthorityContext{
			TenantID: "tenant-a", TenantName: "HarborCare",
			ActingContextID: "delegate-a", ActingContextName: "Covering HR",
			Delegated: true, Elevated: true, Delegator: "Maya Chen", ExpiresAt: "2026-09-18",
		},
		State:      ContextSwitcherReady,
		Controller: &ContextSwitchController{},
	}
}
