package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UIPOLISH_004_MergedUX(t *testing.T) {
	css := UIPolish004ScrollStylesheet()
	for _, want := range []string{
		`.primary-nav,.data-table-scroll{overscroll-behavior-inline:contain;}`,
		`@media (max-width:760px){.primary-nav{overflow-x:auto;overflow-y:visible;overscroll-behavior-inline:contain;scrollbar-color:auto;scrollbar-gutter:auto;scrollbar-width:auto;}}`,
		`scroll-margin-block:var(--hcm-space-3,1.5rem);`,
		`scroll-margin-inline:var(--hcm-space-2,1rem);`,
		`:where(.nav-drawer,.popover-panel,.modal-dialog,.action-launcher-dialog,.utility-drawer-dialog,.people-workflow-options,[role=dialog]){overscroll-behavior:contain;}`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("scroll stylesheet missing %q\n%s", want, css)
		}
	}
	if strings.Contains(css, "overflow-x:hidden") || strings.Contains(css, "overflow-y:auto") || strings.Contains(css, "scrollbar-gutter:stable") {
		t.Fatal("scroll stylesheet overrode an existing scroll owner")
	}
}

func TestTodo_UIPOLISH_004_RenderContract(t *testing.T) {
	node := DataTable(DataTableProps{
		ID: "people-directory-table", Caption: "People", AriaLabel: "People",
		Columns: []DataTableColumnProps{{ID: "name", Label: "Name"}},
		Rows:    []DataTableRowProps{{ID: "worker-1", Cells: []DataTableCellProps{{ColumnID: "name", Text: "Ada"}}}},
	})
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="people-directory-table-viewport"`, `class="data-table-scroll"`, `role="region"`, `tabIndex="0"`, `data-preserve-scroll="true"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("production table markup missing %q: %s", want, markup)
		}
	}
}

func TestTodo_UIPOLISH_004_Accessibility_MergedUX(t *testing.T) {
	css := UIPolish004ScrollStylesheet()
	if strings.Contains(css, "overflow:hidden") || strings.Contains(css, "overflow: hidden") {
		t.Fatal("scroll contract hides overflow")
	}
	if !strings.Contains(css, "overscroll-behavior-inline") {
		t.Fatal("scroll contract has no RTL-safe inline scrolling hook")
	}
}

func TestTodo_UIPOLISH_004_ScrollRegression(t *testing.T) {
	css := UIPolish004ScrollStylesheet()
	if strings.Contains(css, ".main-scroll{") {
		t.Fatal("scroll stylesheet redeclared the existing main scroll owner")
	}
	if strings.Contains(css, ".main-scroll::-webkit-scrollbar") {
		t.Fatal("page owner acquired a competing styled scrollbar")
	}
	final := Stylesheet()
	if !strings.Contains(final, `.people-directory,.people-directory .data-table-scroll`) || !strings.Contains(final, `max-height:none;overflow:visible`) {
		t.Fatal("final product stylesheet lost people-directory page-owned vertical scrolling")
	}
}

func TestTodo_UIPOLISH_004_ScrollbarTokens(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`--hcm-scrollbar-size:10px;`,
		`--hcm-nav-scrollbar-size:10px;`,
		`--hcm-nav-scrollbar-size-rail:7px;`,
		`:is(.main-scroll,.global-search-panel,.data-table-scroll,.action-launcher-dialog,.utility-drawer-dialog,.people-workflow-options)::-webkit-scrollbar{height:var(--hcm-scrollbar-size);width:var(--hcm-scrollbar-size);}`,
		`@media (forced-colors:active){:where(.app-shell,.jn-embedded) :is(.main-scroll,.global-search-panel,.data-table-scroll,.action-launcher-dialog,.utility-drawer-dialog,.people-workflow-options){scrollbar-color:ButtonText Canvas;}}`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("scrollbar token contract missing %q", want)
		}
	}
	if strings.Contains(css, `:is(.main-scroll,.primary-nav,`) {
		t.Fatal("generic scrollbar selector would override the dedicated narrow navigation rail")
	}
}
