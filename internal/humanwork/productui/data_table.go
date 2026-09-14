package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// DataTableSortDirection is the WAI-ARIA sort state of one column.
type DataTableSortDirection string

const (
	DataTableUnsorted   DataTableSortDirection = "none"
	DataTableAscending  DataTableSortDirection = "ascending"
	DataTableDescending DataTableSortDirection = "descending"
)

// DataTableProps describes a rectangular data surface without coupling the
// renderer to a domain record. Columns determine order and visibility; rows
// address cells by column ID, so callers may reorder or omit columns without
// rebuilding every row.
type DataTableProps struct {
	// ID gives the semantic table a stable identity across projected updates.
	// Callers can use it to restore focus to a changed table without coupling
	// the reusable renderer to a route.
	ID        string
	Caption   string
	AriaLabel string
	SortLabel string
	Class     string
	Columns   []DataTableColumnProps
	Rows      []DataTableRowProps
}

// DataTableColumnProps configures one visible column. A non-empty Href makes
// the header sortable through progressive enhancement and software routing.
type DataTableColumnProps struct {
	ID       string
	Label    string
	Class    string
	Width    string
	Href     string
	Sort     DataTableSortDirection
	Navigate func(string)
	AlignEnd bool
}

// dataTableWidthClass is the closed width contract for table columns. Width
// values are converted to classes backed by the frozen product stylesheet;
// arbitrary CSS declarations never cross the Go/WASM boundary as a style
// attribute.
func dataTableWidthClass(width string) string {
	switch strings.TrimSpace(width) {
	case "8rem":
		return "data-table-width-8"
	case "10rem":
		return "data-table-width-10"
	case "12rem":
		return "data-table-width-12"
	case "14rem":
		return "data-table-width-14"
	case "16rem":
		return "data-table-width-16"
	case "18rem":
		return "data-table-width-18"
	case "20rem":
		return "data-table-width-20"
	default:
		return ""
	}
}

// DataTableRowProps is one addressable row. Cells may arrive in any order;
// DataTable renders them in the configured column order and pads omissions.
type DataTableRowProps struct {
	ID    string
	Class string
	Cells []DataTableCellProps
}

// DataTableCellProps carries arbitrary component content for one column.
type DataTableCellProps struct {
	ColumnID  string
	Class     string
	Text      string
	Children  []ui.Node
	RowHeader bool
}

type dataTableRowRenderProps struct {
	Columns []DataTableColumnProps
	Row     DataTableRowProps
	Cells   []DataTableCellProps
}

// DataTable renders a semantic, keyboard-scrollable table. The wrapper owns
// overflow so sticky headers remain attached to the matrix rather than the
// whole application shell.
func DataTable(props DataTableProps) ui.Node {
	columns := normalizedDataTableColumns(props.Columns)
	headings := make([]ui.Node, 0, len(columns))
	for _, column := range columns {
		headings = append(headings, ui.CreateElement(DataTableColumn, column))
	}
	columnIndexes := make(map[string]int, len(columns))
	for index, column := range columns {
		columnIndexes[column.ID] = index
	}
	rows := make([]ui.Node, 0, len(props.Rows))
	for _, row := range props.Rows {
		rows = append(rows, ui.CreateElement(dataTableRow, dataTableRowRenderProps{
			Columns: columns, Row: row, Cells: alignDataTableCells(row.Cells, columns, columnIndexes),
		}))
	}
	class := strings.TrimSpace("data-table " + props.Class)
	label := strings.TrimSpace(props.AriaLabel)
	if label == "" {
		label = strings.TrimSpace(props.Caption)
	}
	children := make([]ui.Node, 0, 2)
	if props.SortLabel != "" {
		children = append(children, html.Span(html.Props{Class: "data-table-sort-label people-sort-label"}, ui.Text(props.SortLabel)))
	}
	viewportID := "data-table-scroll"
	if props.ID != "" {
		viewportID = props.ID + "-viewport"
	}
	children = append(children, html.Table(html.Props{ID: props.ID, Class: class},
		html.Caption(html.Props{Class: "sr-only"}, ui.Text(props.Caption)),
		html.Thead(html.Props{}, html.Tr(html.Props{Class: "data-table-head people-columns"}, headings...)),
		html.Tbody(html.Props{Class: "data-table-body people-rows"}, rows...),
	))
	return ui.CreateElement(ScrollRegion, ScrollRegionProps{
		ID: viewportID, Class: "data-table-scroll", Role: "region", Focusable: true,
		RestoreScroll: true, Aria: map[string]string{"label": label}, Data: map[string]string{"preserve-scroll": "true", "preserve-focus": "true"}, Children: children,
	})
}

// DataTableColumn renders an accessible sortable or static column header.
func DataTableColumn(column DataTableColumnProps) ui.Node {
	class := strings.TrimSpace("data-table-column " + column.Class)
	if column.AlignEnd {
		class += " align-end"
	}
	if widthClass := dataTableWidthClass(column.Width); widthClass != "" {
		class += " " + widthClass
	}
	props := html.Props{Class: class, Raw: map[string]any{"scope": "col"}}
	if column.Href != "" && column.Sort != "" && column.Sort != DataTableUnsorted {
		props.Aria = map[string]string{"sort": string(column.Sort)}
	}
	if column.Href == "" {
		return html.Th(props, ui.Text(column.Label))
	}
	indicator := ""
	linkClass := "data-table-sort people-sort"
	if column.Sort == DataTableAscending {
		indicator, linkClass = " ↑", linkClass+" active"
	} else if column.Sort == DataTableDescending {
		indicator, linkClass = " ↓", linkClass+" active"
	}
	return html.Th(props, softwareLink(column.Navigate, html.Props{Class: linkClass, Title: column.Label}, column.Href, ui.Text(column.Label+indicator)))
}

func dataTableRow(props dataTableRowRenderProps) ui.Node {
	cells := make([]ui.Node, 0, len(props.Columns))
	for index, column := range props.Columns {
		cells = append(cells, dataTableCell(column, props.Cells[index]))
	}
	class := strings.TrimSpace("data-table-row " + props.Row.Class)
	return html.Tr(html.Props{Class: class, Data: map[string]string{"row-id": props.Row.ID}}, cells...)
}

func alignDataTableCells(cells []DataTableCellProps, columns []DataTableColumnProps, indexes map[string]int) []DataTableCellProps {
	result := make([]DataTableCellProps, len(columns))
	for index, column := range columns {
		result[index].ColumnID = column.ID
	}
	for _, cell := range cells {
		index, ok := indexes[strings.TrimSpace(cell.ColumnID)]
		if !ok {
			continue
		}
		cell.ColumnID = columns[index].ID
		result[index] = cell
	}
	return result
}

func dataTableCell(column DataTableColumnProps, cell DataTableCellProps) ui.Node {
	class := strings.TrimSpace("data-table-cell " + cell.Class)
	if column.AlignEnd {
		class += " align-end"
	}
	props := html.Props{Class: class, Data: map[string]string{"column": column.ID, "label": column.Label}}
	children := cell.Children
	if len(children) == 0 && cell.Text != "" {
		children = []ui.Node{ui.Text(cell.Text)}
	}
	if cell.RowHeader {
		props.Raw = map[string]any{"scope": "row"}
		return html.Th(props, children...)
	}
	return html.Td(props, children...)
}

// PaginateWindow is one resolved page of a larger, already-ordered
// collection: the page/pageCount/first/last/total coordinates a pager
// renders, plus exactly the items belonging to this page.
type PaginateWindow[T any] struct {
	Page, PageCount, First, Last, Total int
	Items                               []T
}

// PaginateCollection is the one pagination-window implementation every
// paged product collection uses. UXAUDIT-008 REFACTOR: People and History
// each carried their own copy of this exact arithmetic (paginatePeople in
// selectors.go, paginateHistory in page_history.go); this is that shared
// implementation, with each caller keeping only its own typed window and
// page-size normalization.
//
// pageSize must already be a normalized, in-range value -- this function
// does not invent a default for an invalid one, because "invalid" means
// different things to different callers' declared size sets (People and
// History both currently normalize to {10,20,50,100}, but this function
// must not assume that set is universal). A pageSize below 1 is floored to
// 1 rather than treated as "no limit": returning the entire collection for
// an unrecognized size would be the exact permissive-zero-value failure
// mode this project's proof standards forbid.
//
// An out-of-range requestedPage clamps to the nearest real page (1 or
// PageCount) rather than returning an empty window, so a stale bookmarked
// page number never silently produces a blank directory.
func PaginateCollection[T any](items []T, requestedPage, pageSize int) PaginateWindow[T] {
	if pageSize < 1 {
		pageSize = 1
	}
	total := len(items)
	pageCount := (total + pageSize - 1) / pageSize
	if pageCount < 1 {
		pageCount = 1
	}
	page := requestedPage
	if page < 1 {
		page = 1
	}
	if page > pageCount {
		page = pageCount
	}
	start := (page - 1) * pageSize
	end := start + pageSize
	if end > total {
		end = total
	}
	first := 0
	if total > 0 {
		first = start + 1
	}
	return PaginateWindow[T]{Page: page, PageCount: pageCount, First: first, Last: end, Total: total, Items: items[start:end]}
}

func normalizedDataTableColumns(columns []DataTableColumnProps) []DataTableColumnProps {
	result := make([]DataTableColumnProps, 0, len(columns))
	seen := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		column.ID = strings.TrimSpace(column.ID)
		if column.ID == "" {
			continue
		}
		if _, exists := seen[column.ID]; exists {
			continue
		}
		seen[column.ID] = struct{}{}
		if column.Sort == "" {
			column.Sort = DataTableUnsorted
		} else if column.Sort != DataTableUnsorted && column.Sort != DataTableAscending && column.Sort != DataTableDescending {
			// Unknown sort states must not reach aria-sort, where they would
			// create invalid accessibility semantics for an otherwise safe
			// reusable column definition.
			column.Sort = DataTableUnsorted
		}
		result = append(result, column)
	}
	return result
}
