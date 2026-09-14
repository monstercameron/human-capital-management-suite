package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXAUDIT_024_MergedUX(t *testing.T) {
	name, mark := HeaderBrandIdentity(DefaultCustomerTheme(), "Harborcare Demo")
	if name != "Harborcare Demo" || mark != "HD" {
		t.Fatalf("generic platform wordmark displaced tenant: %q %q", name, mark)
	}
	custom := DefaultCustomerTheme()
	custom.BrandName, custom.BrandMark = "Northstar People", "NP"
	if name, mark := HeaderBrandIdentity(custom, "Harborcare Demo"); name != custom.BrandName || mark != custom.BrandMark {
		t.Fatalf("explicit customer branding lost: %q %q", name, mark)
	}
	withLogo := DefaultCustomerTheme()
	withLogo.BrandLogoURL = "/workspace/assets/customer-logo.svg"
	if name, _ := HeaderBrandIdentity(withLogo, "Harborcare Demo"); name != "Harborcare Demo" {
		t.Fatal("a logo without an explicit brand name must retain tenant identity")
	}
}

func TestTodo_UXAUDIT_024_Accessibility_MergedUX(t *testing.T) {
	name := "Harborcare National Health and Care Coordination Group"
	markup, err := ui.RenderToString(ui.CreateElement(BrandLogo, BrandLogoProps{Name: name, AccessibleName: name, Mark: "HC"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="sr-only"`) || !strings.Contains(markup, ">"+name+"</span>") {
		t.Fatalf("full company name missing from accessible logo fallback: %s", markup)
	}
	for _, locale := range SupportedProductLocales() {
		context := ResolveProductLocale(locale)
		for _, key := range []string{"nav.filter_pages", "nav.search_above", "nav.support_always"} {
			if got := context.Text(key); strings.Contains(got, "⟦") || strings.TrimSpace(got) == "" {
				t.Fatalf("%s lacks %s: %q", locale, key, got)
			}
		}
	}
}

func TestTodo_UXAUDIT_024_Regression_MergedUX(t *testing.T) {
	view := testView(PageHome)
	view.MenuQuery = "promote"
	props := navigationSidebarProps(view)
	if props.Tenant != "" {
		t.Fatal("tenant name is repeated below its header fallback wordmark")
	}
	markup, err := ui.RenderToString(ui.CreateElement(NavigationSidebar, props))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-hcm-search-scope="destinations"`, `placeholder="Filter pages"`, "Search people and workflows in the top search bar.", "Help and settings · always available"} {
		if !strings.Contains(markup, want) {
			t.Errorf("scoped menu discovery missing %q", want)
		}
	}
	if strings.Contains(markup, `class="nav-entry"`) && strings.Contains(markup, "Promotion workflow") {
		t.Fatal("menu filter misrepresented a workflow action as a destination")
	}
}

func TestTodo_UXAUDIT_024_CollapsedBrandKeepsOnlyCompactMark(t *testing.T) {
	css := Stylesheet()
	if !strings.Contains(css, `@media (min-width:761px){.app-shell.nav-collapsed .brand-cluster .brand-logo-slot .wordmark-label{display:none!important;}}`) {
		t.Fatal("collapsed desktop brand can still render the long name in a narrow rail")
	}
	if !strings.Contains(css, `@media (min-width:761px){.sidebar.collapsed .nav-empty,.sidebar.collapsed .nav-support-label{display:none!important;}}`) {
		t.Fatal("collapsed filtered rail can still render explanatory text vertically")
	}
	view := testView(PageHome)
	view.Tenant = "Harborcare Demo"
	view.NavCollapsed = true
	view.MenuQuery = "promote"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="app-shell nav-collapsed`, `title="Harborcare Demo"`, `>HD</span>`, `>Harborcare Demo</span>`} {
		if !strings.Contains(doc, want) {
			t.Errorf("collapsed branded shell missing %q", want)
		}
	}
	for _, route := range []string{"/workspace/app/help", "/workspace/app/settings"} {
		if !strings.Contains(doc, route) {
			t.Errorf("collapsed filtered rail lost the %s icon destination", route)
		}
	}
}
