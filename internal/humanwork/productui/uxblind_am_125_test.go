package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_125(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		"@media (min-width:761px) and (max-width:1100px){.topbar{",
		"grid-template-columns:minmax(0,1fr) auto auto auto;",
		"@media (min-width:761px) and (max-width:1100px){.topbar>.header-navigation-tools{grid-column:1 / -1;grid-row:2;",
		"@media (max-width:1100px){.header-navigation-tools>.global-search{flex:0 0 44px;min-width:44px;padding:0;width:44px;}",
		"@media (max-width:1100px){.utility-drawer-trigger .utility-drawer-label{display:none;}",
		"@media (max-width:1100px){.utility-drawer-trigger{",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("laptop header contract missing %q", want)
		}
	}
	if strings.Contains(css, "@media (min-width:431px) and (max-width:1050px){.utility-drawer-trigger .utility-drawer-label{display:none;") {
		t.Fatal("utility label collapse unexpectedly kept the old phone-only breakpoint")
	}
	for _, want := range []string{
		"@media (max-width:1100px){.header-navigation-tools>.global-search:focus-within{",
		"position:fixed",
		"inset-block-start:68px",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("focused search expansion missing %q", want)
		}
	}
}

// TestTodo_UXBLIND_125_Browser is the component-level browser contract. It
// proves that icon-mode controls keep their accessible names and tooltip text;
// the native lane tests do not launch a real browser.
func TestTodo_UXBLIND_125_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	drawer, err := ui.RenderToString(ui.CreateElement(UtilityDrawer, UtilityDrawerProps{
		I18nProps: I18nProps{Locale: locale}, Sections: []UtilityDrawerSection{{
			Title: "Related", Items: []UtilityDrawerItem{{Label: "People", Href: "/people"}},
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`aria-label="Page utilities"`,
		`title="Page utilities"`,
		`class="utility-drawer-label">Page utilities`,
	} {
		if !strings.Contains(drawer, want) {
			t.Fatalf("utility trigger lost %q: %s", want, drawer)
		}
	}
	search, err := ui.RenderToString(ui.CreateElement(GlobalSearch, GlobalSearchProps{I18nProps: I18nProps{Locale: locale}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`id="global-search-input"`,
		`aria-label="Search Human Capital Management Suite"`,
		`placeholder="Search workspace"`,
	} {
		if !strings.Contains(search, want) {
			t.Fatalf("search control lost %q: %s", want, search)
		}
	}
}
