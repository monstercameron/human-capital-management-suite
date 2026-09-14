package productui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestDataTableRendersConfigurableRectangularMatrix(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(DataTable, DataTableProps{
		Caption: "Compensation matrix", AriaLabel: "Compensation matrix results", SortLabel: "Sort matrix",
		Columns: []DataTableColumnProps{
			{ID: "worker", Label: "Worker", Href: "/workspace/app/matrix?sort=worker", Sort: DataTableAscending, Width: "14rem"},
			{ID: "salary", Label: "Salary", AlignEnd: true},
			{ID: "band", Label: "Band"},
		},
		Rows: []DataTableRowProps{
			{ID: "worker-1", Cells: []DataTableCellProps{{ColumnID: "salary", Text: "$100.00"}, {ColumnID: "worker", Text: "Avery", RowHeader: true}, {ColumnID: "band", Text: "P3"}}},
			{ID: "worker-2", Cells: []DataTableCellProps{{ColumnID: "worker", Text: "Bianca", RowHeader: true}, {ColumnID: "salary", Text: "$120.00"}}},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`role="region"`, `aria-label="Compensation matrix results"`, `<table`, `<caption`, `<thead`, `<tbody`,
		`scope="col"`, `scope="row"`, `aria-sort="ascending"`, `data-row-id="worker-1"`,
		`data-column="salary"`, `data-label="Salary"`, `data-table-width-14`, `tabIndex="0"`,
		`data-preserve-scroll="true"`, `data-preserve-focus="true"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("configurable data table missing %q\n%s", want, markup)
		}
	}
	if first, second := strings.Index(markup, "Avery"), strings.Index(markup, "$100.00"); first < 0 || second < first {
		t.Fatal("cell input order overrode configured column order")
	}
	if got := strings.Count(markup, `data-column="band"`); got != 2 {
		t.Fatalf("short row was not padded to a rectangular matrix: band cells=%d", got)
	}
	if strings.Contains(markup, "people-") {
		t.Fatalf("neutral data table inherited directory-specific classes: %s", markup)
	}
}

func TestDataTableStyleHooksBelongToTheCaller(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(DataTable, DataTableProps{
		Caption: "Report", SortLabel: "Sort report", HeaderClass: "report-columns", BodyClass: "report-rows", SortLabelClass: "report-sort-label",
		Columns: []DataTableColumnProps{{ID: "name", Label: "Name", Href: "/report?sort=name", Sort: DataTableAscending, SortLinkClass: "report-sort"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="data-table-head report-columns"`, `class="data-table-body report-rows"`,
		`class="data-table-sort-label report-sort-label"`, `class="data-table-sort report-sort active"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("data table lost caller-owned class %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "people-") {
		t.Fatalf("caller-owned report table inherited People styling: %s", markup)
	}

	people, err := ui.RenderToString(ui.CreateElement(PeopleTable, PeopleTableProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Columns:   []PeopleSortColumnProps{{ID: peopleSortName, Label: "Person", Href: "/workspace/app/people?sort=name", Active: true}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="data-table-head people-columns"`, `class="data-table-body people-rows"`, `class="data-table-sort-label people-sort-label"`, `class="data-table-sort people-sort active"`} {
		if !strings.Contains(people, want) {
			t.Fatalf("People table lost its existing style hook %q: %s", want, people)
		}
	}
}

func TestDataTableMarksEverySortableHeaderAndKeepsStableIdentity(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(DataTable, DataTableProps{
		ID: "people-directory-table", Caption: "People", AriaLabel: "People", Columns: []DataTableColumnProps{
			{ID: "name", Label: "Name", Href: "/people?sort=name", Sort: DataTableAscending},
			{ID: "team", Label: "Team", Href: "/people?sort=team", Sort: DataTableUnsorted},
			{ID: "actions", Label: "Actions"},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="people-directory-table-viewport"`, `id="people-directory-table"`, `aria-sort="ascending"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("data table missing %q: %s", want, markup)
		}
	}
	if got := strings.Count(markup, `aria-sort=`); got != 1 {
		t.Fatalf("active sorted headers=%d want=1: %s", got, markup)
	}
}

func TestDataTableNormalizesUnsafeColumnDefinitions(t *testing.T) {
	columns := normalizedDataTableColumns([]DataTableColumnProps{
		{ID: " name ", Label: "Name"}, {ID: "", Label: "Missing"}, {ID: "name", Label: "Duplicate"}, {ID: "role", Label: "Role", Sort: DataTableSortDirection("invalid")},
	})
	if len(columns) != 2 || columns[0].ID != "name" || columns[0].Sort != DataTableUnsorted || columns[1].ID != "role" || columns[1].Sort != DataTableUnsorted {
		t.Fatalf("normalized columns = %+v", columns)
	}
}

func TestDataTableWidthUsesTheClosedClassContract(t *testing.T) {
	for _, test := range []struct {
		name, width, want string
	}{
		{name: "approved", width: "14rem", want: "data-table-width-14"},
		{name: "arbitrary-css", width: `14rem; color: red`, want: ""},
		{name: "external-css", width: `url(https://attacker.invalid)`, want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			markup, err := ui.RenderToString(ui.CreateElement(DataTable, DataTableProps{
				Columns: []DataTableColumnProps{{ID: "column", Label: "Column", Width: test.width}},
			}))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(markup, "style=") {
				t.Fatalf("width %q emitted a CSP-blocked style attribute: %s", test.width, markup)
			}
			if test.want != "" && !strings.Contains(markup, test.want) {
				t.Fatalf("width %q class contract mismatch: %s", test.width, markup)
			}
		})
	}
}

func TestDataTableScalesAcrossMatrixShapes(t *testing.T) {
	for _, shape := range []struct{ rows, columns int }{{1, 1}, {0, 12}, {25, 2}, {100, 20}} {
		t.Run(fmt.Sprintf("%dx%d", shape.rows, shape.columns), func(t *testing.T) {
			columns := make([]DataTableColumnProps, shape.columns)
			for column := range columns {
				columns[column] = DataTableColumnProps{ID: fmt.Sprintf("c%d", column), Label: fmt.Sprintf("Column %d", column)}
			}
			rows := make([]DataTableRowProps, shape.rows)
			for row := range rows {
				cells := make([]DataTableCellProps, shape.columns)
				for column := range cells {
					cells[column] = DataTableCellProps{ColumnID: columns[column].ID, Text: fmt.Sprintf("r%dc%d", row, column)}
				}
				rows[row] = DataTableRowProps{ID: fmt.Sprintf("r%d", row), Cells: cells}
			}
			markup, err := ui.RenderToString(ui.CreateElement(DataTable, DataTableProps{Caption: "Matrix", Columns: columns, Rows: rows}))
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Count(markup, `class="data-table-column`); got != shape.columns {
				t.Fatalf("headers=%d want=%d", got, shape.columns)
			}
			if got := strings.Count(markup, `class="data-table-row"`); got != shape.rows {
				t.Fatalf("rows=%d want=%d", got, shape.rows)
			}
		})
	}
}

func TestDataTableStylesKeepHeadersStickyAndMobileCellsVisible(t *testing.T) {
	for _, fragment := range []string{
		`.data-table thead{position:sticky`,
		`overflow:auto`,
		`content:attr(data-label)`,
		`.data-table .data-table-cell{background:transparent!important;`,
	} {
		if !strings.Contains(dataTableStylesStylesheet(), fragment) {
			t.Fatalf("data table styles missing %q", fragment)
		}
	}
}

func TestPaginateBoundsAndCollectionSharePageGeometry(t *testing.T) {
	for _, test := range []struct {
		name              string
		total, page, size int
		want              PaginationBounds
	}{
		{name: "empty", total: 0, page: 500, size: 20, want: PaginationBounds{Page: 1, PageCount: 1}},
		{name: "before-first", total: 61, page: -4, size: 20, want: PaginationBounds{Page: 1, PageCount: 4, First: 1, Last: 20, Total: 61}},
		{name: "middle", total: 61, page: 3, size: 20, want: PaginationBounds{Page: 3, PageCount: 4, First: 41, Last: 60, Total: 61}},
		{name: "after-last", total: 61, page: 500, size: 20, want: PaginationBounds{Page: 4, PageCount: 4, First: 61, Last: 61, Total: 61}},
		{name: "invalid-size", total: 2, page: 1, size: 0, want: PaginationBounds{Page: 1, PageCount: 2, First: 1, Last: 1, Total: 2}},
	} {
		t.Run(test.name, func(t *testing.T) {
			bounds := PaginateBounds(test.total, test.page, test.size)
			if bounds != test.want {
				t.Fatalf("bounds = %+v, want %+v", bounds, test.want)
			}
			items := make([]int, test.total)
			for index := range items {
				items[index] = index + 1
			}
			window := PaginateCollection(items, test.page, test.size)
			if window.Page != bounds.Page || window.PageCount != bounds.PageCount || window.First != bounds.First || window.Last != bounds.Last || window.Total != bounds.Total {
				t.Fatalf("collection %+v disagrees with bounds %+v", window, bounds)
			}
			wantLen := 0
			if bounds.Total > 0 {
				wantLen = bounds.Last - bounds.First + 1
			}
			if got := len(window.Items); got != wantLen {
				t.Fatalf("items = %d for %+v", got, bounds)
			}
		})
	}
}

func BenchmarkDataTableRender(b *testing.B) {
	for _, shape := range []struct{ rows, columns int }{{50, 6}, {1000, 12}} {
		b.Run(fmt.Sprintf("%dx%d", shape.rows, shape.columns), func(b *testing.B) {
			columns := make([]DataTableColumnProps, shape.columns)
			for column := range columns {
				columns[column] = DataTableColumnProps{ID: fmt.Sprintf("c%d", column), Label: fmt.Sprintf("Column %d", column)}
			}
			rows := make([]DataTableRowProps, shape.rows)
			for row := range rows {
				cells := make([]DataTableCellProps, shape.columns)
				for column := range cells {
					cells[column] = DataTableCellProps{ColumnID: columns[column].ID, Text: "value"}
				}
				rows[row] = DataTableRowProps{ID: fmt.Sprint(row), Cells: cells}
			}
			props := DataTableProps{Caption: "Benchmark matrix", Columns: columns, Rows: rows}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := ui.RenderToString(ui.CreateElement(DataTable, props)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
