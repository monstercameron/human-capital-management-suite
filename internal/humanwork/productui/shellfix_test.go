package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestShellFix_TabletNavigation(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`@media (min-width:761px) and (max-width:1023px){.app-shell:not(.nav-expanded) .shell-grid{grid-template-columns:72px minmax(0,1fr);}`,
		`.app-shell:not(.nav-expanded) .sidebar :is(.tenant,.nav-label,.nav-count,.subnav,.nav-favorite,.nav-section-label,.menu-filter){display:none!important;}`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("tablet rail rule missing %q", want)
		}
	}
	view := testView(PagePeople)
	view.NavExpanded = true
	if href := navigationToggleProps(view).Href; !strings.Contains(href, "nav=collapsed") {
		t.Fatalf("expanded preference does not toggle to compact: %s", href)
	}
	if href := currentPageHref(view, false); !strings.Contains(href, "nav=expanded") {
		t.Fatalf("explicit expanded preference was lost: %s", href)
	}
	collapsed := testView(PagePeople)
	collapsed.NavCollapsed = true
	if href := navigationToggleProps(collapsed).Href; !strings.Contains(href, "nav=expanded") {
		t.Fatalf("compact preference does not expose explicit expansion: %s", href)
	}
	if got := navigationCurrentKey(navigationSidebarProps(testView(PageAgentOperations))); got != string(PageAgentOperations) {
		t.Fatalf("current navigation scroll target = %q", got)
	}
}

func TestShellFix_DarkControlTokens(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{`:root[data-hcm-color-mode="dark"] .nav-favorite{background:var(--hcm-color-surface);border-color:var(--hcm-color-surface);}`} {
		if !strings.Contains(css, want) {
			t.Fatalf("dark control rule missing %q", want)
		}
	}
	checkbox := declarationsFor(css, `:root[data-hcm-color-mode="dark"] :where(.app-shell,.jn-embedded) input[type=checkbox]:not(:checked)`)
	for _, want := range []string{"appearance:none", "background:transparent", "border:1.5px solid var(--hcm-color-border)"} {
		if !strings.Contains(checkbox, want) {
			t.Fatalf("dark checkbox declaration missing %q: %s", want, checkbox)
		}
	}
}

func TestShellFix_NoLoadTimeLinkTint(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`:where(.app-shell) a.button.secondary{background:transparent;}`,
		`:where(.app-shell) a.button.secondary:is(:hover,:active){background:var(--hcm-hover-surface);}`,
		`:where(.app-shell) a.button.secondary:focus-visible{background:transparent;}`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("link button state rule missing %q", want)
		}
	}
}

func TestShellFix_PageFrameContract(t *testing.T) {
	node := ProductPageFrame(ProductPageFrameProps{
		Breadcrumbs: html.A(html.Props{Href: "/workspace/app/admin"}, ui.Text("Admin")),
		Title:       "Agents",
		TitleID:     "frame-title",
		Actions:     []ui.Node{html.A(html.Props{Class: "button secondary", Href: "/workspace/app/admin/agents"}, ui.Text("Operations"))},
		Body:        []ui.Node{html.P(html.Props{}, ui.Text("Body"))},
	})
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="product-page-frame"`, `class="product-page-frame-breadcrumbs"`, `class="product-page-frame-title-row"`, `id="frame-title"`, `class="product-page-frame-actions"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("frame markup missing %q: %s", want, markup)
		}
	}
}
