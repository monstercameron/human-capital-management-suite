package productui

import (
	stdhtml "html"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// These Browser matrix checks exercise the production Render boundary. The
// lane cannot start a dev server, so the browser-facing proof is the same
// parsed DOM and stylesheet contract used by the repository's other native
// Browser tests; computed layout remains an operator/browser-run concern.

func TestTodo_UIPOLISH_002_Browser(t *testing.T) {
	doc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	main := firstElement(root, "main")
	if main == nil {
		t.Fatal("production Home document has no main landmark")
	}
	grid := findElementsByClassContains(main, "home-grid")
	if len(grid) != 1 {
		t.Fatalf("production Home document has %d home grids, want one", len(grid))
	}
	gridStart := strings.Index(doc, `class="home-grid`)
	if gridStart < 0 {
		t.Fatal("production Home document lost the home-grid class")
	}
	markup := doc[gridStart:]
	primary := strings.Index(markup, `class="home-primary-rail side-stack"`)
	supporting := strings.Index(markup, `class="home-supporting-rail side-stack"`)
	if primary < 0 || supporting < 0 || primary >= supporting {
		t.Fatalf("Home semantic rails are missing or out of reading order: %s", markup[:shortLen(len(markup), 1200)])
	}
	css := Stylesheet()
	for _, want := range []string{
		`@media (max-width:760px){.home-grid,.side-stack,.insights-grid{grid-template-columns:1fr;}}`,
		`@media (min-width:761px){:root[data-hcm-density="compact"] .home-grid`,
		`@media (min-width:761px){:root[data-hcm-density="spacious"] .home-grid`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("production Home stylesheet is missing responsive rhythm contract %q", want)
		}
	}
}

func TestTodo_UIPOLISH_006_Browser(t *testing.T) {
	doc, err := Render(testView(PageSettings))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	controls := 0
	walkElements(root, func(node *xhtml.Node) {
		if node.Type != xhtml.ElementNode || (node.Data != "button" && node.Data != "input" && node.Data != "select" && node.Data != "textarea") {
			return
		}
		if attr(node, "type") == "hidden" {
			return
		}
		controls++
		if attr(node, "aria-label") == "" && attr(node, "aria-labelledby") == "" && strings.TrimSpace(nodeText(node)) == "" && !browserLabelFor(root, node) {
			t.Errorf("production Settings document contains an unnamed %s control", node.Data)
		}
	})
	if controls == 0 {
		t.Fatal("production Settings document rendered no interactive controls")
	}
	css := Stylesheet()
	for _, want := range []string{
		`--hcm-control-height:44px`,
		`:focus-visible`,
		`:disabled`,
		`:not(:disabled):active`,
		`--hcm-radius-control`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("production control stylesheet is missing state/geometry contract %q", want)
		}
	}
}

func TestTodo_UIPOLISH_007_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := ApplyLocale(testView(PageSettings), ResolveProductLocale(locale))
		view.LogoutHref = "/workspace/logout"
		doc, err := Render(view)
		if err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		html := firstElement(root, "html")
		if html == nil || attr(html, "lang") != locale || attr(html, "dir") == "" {
			t.Fatalf("production %s document lost locale or direction metadata", locale)
		}
		for _, key := range []string{"settings.account_group_title", "settings.preferences_group_title", "settings.signout_action"} {
			message := view.Locale.Text(key)
			if message == "" || !strings.Contains(doc, stdhtml.EscapeString(message)) {
				t.Errorf("production %s document omits semantic copy %s = %q", locale, key, message)
			}
		}
		if locale != DefaultProductLocale && strings.Contains(doc, ResolveProductLocale(DefaultProductLocale).Text("settings.account_group_title")) {
			t.Errorf("production %s document leaked the English settings group title", locale)
		}
	}
}

func TestTodo_UIPOLISH_008_Browser(t *testing.T) {
	view := testView(PageHistory)
	view.Appearance.Density = "spacious"
	view.StoredPreferences.Density = "compact"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	html := firstElement(root, "html")
	if html == nil || attr(html, "data-hcm-density") != "compact" {
		t.Fatalf("production History document did not apply the persisted personal density: %s", doc[:shortLen(len(doc), 800)])
	}
	if !strings.Contains(doc, `class="data-table-scroll"`) || !strings.Contains(doc, "<table") {
		t.Fatal("production History document lost its semantic table scroll region")
	}
	css := Stylesheet()
	for _, want := range []string{
		`.data-table-cell{padding-bottom:calc(3px + var(--hcm-space-1) * var(--hcm-density))`,
		`@media (max-width:420px){.data-table .data-table-cell`,
		`overflow-wrap:anywhere`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("production table stylesheet is missing density/reflow contract %q", want)
		}
	}
}

func shortLen(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func browserLabelFor(root, control *xhtml.Node) bool {
	id := attr(control, "id")
	if id == "" {
		return false
	}
	found := false
	walkElements(root, func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && node.Data == "label" && attr(node, "for") == id {
			found = true
		}
	})
	return found
}
