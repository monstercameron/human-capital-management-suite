package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestUXBLIND023StickyLastAndTypeahead(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	markup, err := ui.RenderToString(ui.CreateElement(DataTable, DataTableProps{
		Caption: "People", AriaLabel: "People", StickyFirst: true, StickyLast: true,
		Columns: []DataTableColumnProps{
			{ID: "person", Label: "Person"},
			{ID: "location", Label: "Location"},
			{ID: "actions", Label: "Actions"},
		},
		Rows: []DataTableRowProps{{ID: "worker-1", Cells: []DataTableCellProps{
			{ColumnID: "person", Text: "Jordan Lee", RowHeader: true},
			{ColumnID: "location", Text: "New York"},
			{ColumnID: "actions", Children: []ui.Node{ui.Text("Start promotion")}},
		}}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-table-sticky-first`, `data-table-sticky-last`,
		`inset-inline-start:0`, `inset-inline-end:0`,
		"Jordan Lee", "Start promotion",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("wide-table contract missing %q: %s", want, markup)
		}
	}

	people, err := ui.RenderToString(ui.CreateElement(PeopleTable, PeopleTableProps{
		I18nProps: I18nProps{Locale: locale},
		Rows: []PeopleRowProps{{
			ID: "worker-1", Name: "Jordan Lee", Href: "/people?person=worker-1",
			QuickActions: []PeopleQuickActionProps{{WorkflowID: "promotion", Href: "/journeys/new", AccessibleLabel: "Start promotion for Jordan Lee"}},
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(people, `people-row-actions data-table-sticky-last`) ||
		!strings.Contains(people, "Start promotion") || strings.Contains(people, "Start Promotion") {
		t.Fatalf("People action is not a reachable sentence-case trailing cell: %s", people)
	}

	filter, err := ui.RenderToString(ui.CreateElement(PeopleFilter, PeopleFilterProps{
		I18nProps: I18nProps{Locale: locale}, Query: "Jor", Action: "/people",
		OnFilter: func(string, string, string, bool) {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filter, `class="people-filter"`) || !strings.Contains(filter, `name="q"`) {
		t.Fatalf("directory search lost its live search field: %s", filter)
	}
}

// TestTodo_UXBLIND_023_Browser is the component-level browser contract. The
// repository's native lane tests do not launch a browser, so it pins the
// responsive overflow and sticky-cell rules consumed at the three audited
// viewport widths.
func TestUXBLIND023ResponsiveTableContract(t *testing.T) {
	css := Stylesheet()
	for _, width := range []string{"761px", "1051px", "1200px"} {
		if !strings.Contains(css, "min-width:"+width) {
			t.Fatalf("responsive table rules lack the audited breakpoint %s", width)
		}
	}
	for _, want := range []string{
		".people-directory .data-table-scroll{",
		"overflow:auto",
		".people-directory .data-table .data-table-cell",
		".people-row-actions",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("responsive table stylesheet missing %q", want)
		}
	}
}

func TestTodo_UXBLIND_026(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	for _, code := range []string{"en-US", "de-DE", "ar"} {
		placeholder := ResolveProductLocale(code).Text("global_search.topbar_placeholder")
		if strings.TrimSpace(placeholder) == "" || strings.Contains(placeholder, "…") {
			t.Fatalf("%s top-bar placeholder is not usable: %q", code, placeholder)
		}
	}

	single, err := ui.RenderToString(WorkflowLauncher(WorkflowLauncherProps{
		I18nProps: I18nProps{Locale: locale}, TotalCount: 1,
		Workflows: []WorkflowCardProps{{Name: "Promotion", Href: "/journeys/new"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(single, `id="workflow-search"`) {
		t.Fatalf("one workflow still renders an unnecessary filter: %s", single)
	}
	many, err := ui.RenderToString(WorkflowLauncher(WorkflowLauncherProps{
		I18nProps: I18nProps{Locale: locale}, TotalCount: 6,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(many, `id="workflow-search"`) {
		t.Fatalf("larger workflow list lost its filter: %s", many)
	}

	compensation, err := ui.RenderToString(EmploymentDetails(EmploymentDetailsProps{
		I18nProps: I18nProps{Locale: locale}, Title: locale.Text("person.compensation"),
		Description: locale.Text("person.compensation_detail"), Class: "compensation-details",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(compensation, "available for this task") || !strings.Contains(compensation, "hidden in Restricted details") {
		t.Fatalf("compensation copy still implies an unprovided task: %s", compensation)
	}
}

// TestTodo_UXBLIND_026_Browser is the component-level browser contract for
// the persistent shell controls and the labelled utility trigger.
func TestTodo_UXBLIND_026_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	search, err := ui.RenderToString(ui.CreateElement(GlobalSearch, GlobalSearchProps{I18nProps: I18nProps{Locale: locale}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(search, `placeholder="Search workspace"`) {
		t.Fatalf("global search did not use the shared short placeholder: %s", search)
	}
	drawer, err := ui.RenderToString(ui.CreateElement(UtilityDrawer, UtilityDrawerProps{
		I18nProps: I18nProps{Locale: locale}, Sections: []UtilityDrawerSection{{
			Title: "Related", Items: []UtilityDrawerItem{{Label: "People", Href: "/people"}},
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(drawer, `aria-label="Page utilities"`) || !strings.Contains(drawer, `class="utility-drawer-label">Page utilities`) {
		t.Fatalf("utility trigger is not labelled in both accessible and visible channels: %s", drawer)
	}
	css := Stylesheet()
	if !strings.Contains(css, `@media (max-width:430px){.utility-drawer-trigger .utility-drawer-label{display:none;}`) {
		t.Fatalf("mobile-only utility label collapse is missing")
	}
	if strings.Contains(css, `@media (min-width:431px) and (max-width:1050px){.utility-drawer-trigger .utility-drawer-label{display:none;}`) {
		t.Fatalf("tablet utility label is still hidden")
	}
}
