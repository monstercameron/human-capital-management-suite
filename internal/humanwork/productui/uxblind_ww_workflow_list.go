package productui

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const (
	workflowListQueryKey      = "workflow_q"
	workflowListStatusKey     = "workflow_status"
	workflowListSortKey       = "workflow_sort"
	workflowListPageKey       = "workflow_page"
	workflowListReferences    = "workflow_references"
	workflowListPageSize      = 12
	workflowListSortRecent    = "recent"
	workflowListSortName      = "name"
	workflowListSortStatus    = "status"
	workflowListStatusAll     = "all"
	workflowListStatusActive  = "active"
	workflowListStatusDraft   = "draft"
	workflowListStatusReview  = "review"
	workflowListStatusRetired = "retired"
)

type workflowListState struct {
	Query          string
	Status         string
	Sort           string
	Page           int
	ShowReferences bool
	Initialized    bool
}

type workflowListRow struct {
	Item      WorkflowCatalogItem
	Name      string
	Status    string
	StatusKey string
	Version   string
	Category  string
	Href      string
}

// uxblindWorkflowScalableCatalog is the address-backed workflow catalog. It
// narrows only the already-authorized projection supplied by the page; the
// selected workflow remains in the route while the list state changes.
func uxblindWorkflowScalableCatalog(props WorkflowDesignerPageProps, catalog []WorkflowCatalogItem, selectedID string) ui.Node {
	state := ui.UseState(workflowListStateFromHref(props.BaseHref))
	current := state.Get()
	if !current.Initialized {
		current.Initialized = true
		state.Set(current)
	}
	active := ui.UseState(0)

	setState := func(next workflowListState) {
		next = normalizeWorkflowListState(next)
		next.Initialized = true
		state.Set(next)
		if props.Navigate != nil {
			props.Navigate(workflowListStateHref(props.BaseHref, selectedID, next))
		}
	}

	queryInput := ui.UseEvent(func(event ui.InputEvent) {
		next := current
		next.Query = event.GetValue()
		next.Page = 1
		setState(next)
	})
	sortChange := ui.UseEvent(func(event ui.ChangeEvent) {
		next := current
		next.Sort = event.GetValue()
		next.Page = 1
		setState(next)
	})
	referencesChange := ui.UseEvent(func(event ui.ChangeEvent) {
		next := current
		next.ShowReferences = event.IsChecked()
		next.Page = 1
		setState(next)
	})

	rows := workflowListRows(props, catalog, current, selectedID)
	bounds := PaginateBounds(len(rows), current.Page, workflowListPageSize)
	current.Page = bounds.Page
	pageRows := PaginateCollection(rows, bounds.Page, workflowListPageSize).Items
	activeIndex := active.Get()
	selectedIndex := workflowListActiveIndex(pageRows, selectedID)
	if len(pageRows) > 0 && activeIndex >= len(pageRows) {
		activeIndex = selectedIndex
		if activeIndex < 0 {
			activeIndex = 0
		}
		active.Set(activeIndex)
	}
	activeID := ""
	if activeIndex >= 0 && activeIndex < len(pageRows) {
		activeID = workflowListRowID(pageRows[activeIndex].Item, activeIndex)
	}
	keyDown := ui.UseEvent(func(event ui.KeyboardEvent) {
		if len(pageRows) == 0 {
			return
		}
		index := activeIndex
		switch event.GetKey() {
		case "ArrowDown", "ArrowRight":
			event.PreventDefault()
			index = (index + 1) % len(pageRows)
		case "ArrowUp", "ArrowLeft":
			event.PreventDefault()
			index = (index - 1 + len(pageRows)) % len(pageRows)
		case "Home":
			event.PreventDefault()
			index = 0
		case "End":
			event.PreventDefault()
			index = len(pageRows) - 1
		case "Enter", " ":
			event.PreventDefault()
			if props.Navigate != nil {
				props.Navigate(pageRows[index].Href)
			}
			return
		default:
			return
		}
		if index >= 0 && index < len(pageRows) {
			active.Set(index)
			activeID = workflowListRowID(pageRows[index].Item, index)
		}
	})

	statusCounts := workflowListStatusCounts(catalog, current.Query)
	filterChips := workflowListFilterChips(props, current, selectedID, statusCounts)
	sortOptions := []ui.Node{
		html.Option(html.Props{Value: workflowListSortRecent, Selected: current.Sort == workflowListSortRecent}, ui.Text(props.Text("workflow_list.sort_recent"))),
		html.Option(html.Props{Value: workflowListSortName, Selected: current.Sort == workflowListSortName}, ui.Text(props.Text("workflow_list.sort_name"))),
		html.Option(html.Props{Value: workflowListSortStatus, Selected: current.Sort == workflowListSortStatus}, ui.Text(props.Text("workflow_list.sort_status"))),
	}

	searchID := "workflow-catalog-search"
	// The results table is absent in the empty state, so the search only
	// claims to control it while it exists.
	var searchControls map[string]string
	if len(rows) > 0 {
		searchControls = map[string]string{"controls": "workflow-catalog-table"}
	}
	search := html.Div(html.Props{Class: "workflow-list-search", Role: "search"},
		html.Label(html.Props{For: searchID, Class: "sr-only"}, ui.Text(props.Text("workflow_list.search_label"))),
		html.Input(html.Props{ID: searchID, Type: "search", Value: current.Query, Placeholder: props.Text("workflow_list.search_placeholder"), AutoComplete: "off", OnInput: queryInput, Aria: searchControls}),
	)
	sortControl := html.Label(html.Props{Class: "workflow-list-sort"},
		html.Span(html.Props{Class: "sr-only"}, ui.Text(props.Text("workflow_list.sort_label"))),
		html.Select(html.Props{OnChange: sortChange, Aria: map[string]string{"label": props.Text("workflow_list.sort_label")}}, sortOptions...),
	)
	referenceLabel := props.Text("workflow_designer.references_toggle")
	if current.ShowReferences {
		referenceLabel = props.Text("workflow_designer.references_hide")
	}
	references := html.Label(html.Props{Class: "workflow-list-references"},
		html.Input(html.Props{Type: "checkbox", Checked: current.ShowReferences, OnChange: referencesChange, Aria: map[string]string{"label": referenceLabel}}),
		ui.Text(referenceLabel),
	)
	toolbar := html.Div(html.Props{Class: "workflow-list-toolbar"}, search, sortControl, references, filterChips)
	note := html.P(html.Props{Class: "muted workflow-catalog-note"}, ui.Text(uxblindWorkflowCatalogNote(props, uxblindWorkflowActiveName(catalog))))

	countText := props.Text("workflow_list.result", map[string]string{"shown": strconv.Itoa(len(pageRows)), "total": strconv.Itoa(len(rows))})
	count := html.P(html.Props{Class: "workflow-list-count", Role: "status", Aria: map[string]string{"live": "polite"}}, ui.Text(countText))
	if len(rows) == 0 {
		return html.Div(html.Props{Class: "workflow-list-shell"}, note, toolbar, count,
			html.Div(html.Props{Class: "workflow-catalog-empty", Role: "status"},
				html.Strong(html.Props{}, ui.Text(props.Text("workflow_designer.empty_catalog_title"))),
				html.P(html.Props{Class: "muted"}, ui.Text(props.Text("workflow_list.no_match"))),
			),
		)
	}

	dataRows := make([]DataTableRowProps, 0, len(pageRows))
	for index, row := range pageRows {
		rowID := workflowListRowID(row.Item, index)
		linkProps := html.Props{ID: rowID, Class: "workflow-list-row-link", Aria: map[string]string{"label": row.Name}}
		if row.Item.WorkflowID == selectedID {
			linkProps.Class += " active"
			linkProps.Raw = map[string]any{"aria-current": "page"}
		}
		linkProps.Href = row.Href
		name := html.A(linkProps,
			html.Span(html.Props{Class: "workflow-list-name", Dir: "auto"}, ui.Text(row.Name)),
			html.Span(html.Props{Class: "workflow-list-id", Dir: "auto", Title: row.Item.WorkflowID}, ui.Text(row.Item.WorkflowID)),
		)
		dataRows = append(dataRows, DataTableRowProps{ID: rowID, Class: "workflow-list-data-row", Cells: []DataTableCellProps{
			{ColumnID: "workflow", Children: []ui.Node{name}, RowHeader: true},
			{ColumnID: "version", Text: row.Version},
			{ColumnID: "status", Children: []ui.Node{html.Span(html.Props{Class: "status-chip", Data: map[string]string{"tone": workflowPublicationTone(row.Item.Status)}}, ui.Text(row.Status))}},
			{ColumnID: "category", Text: row.Category},
			{ColumnID: "updated", Text: props.Text("common.not_reported")},
			{ColumnID: "owner", Text: props.Text("common.not_reported")},
			{ColumnID: "actions", Children: []ui.Node{workflowListRowAction(props, row)}},
		}})
	}

	columns := []DataTableColumnProps{
		{ID: "workflow", Label: props.Text("workflow_list.col_workflow"), Width: "18rem"},
		{ID: "version", Label: props.Text("workflow_list.col_version"), Width: "8rem"},
		{ID: "status", Label: props.Text("workflow_list.col_status"), Width: "10rem"},
		{ID: "category", Label: props.Text("workflow_list.col_category"), Width: "12rem"},
		{ID: "updated", Label: props.Text("workflow_list.col_updated"), Width: "10rem"},
		{ID: "owner", Label: props.Text("workflow_list.col_owner"), Width: "12rem"},
		{ID: "actions", Label: uxblindWorkflowDraftCopy(props.Locale, "edit"), Width: "9rem"},
	}
	table := ui.CreateElement(DataTable, DataTableProps{ID: "workflow-catalog-table", Columns: columns, Rows: dataRows, Caption: props.Text("workflow_list.table_label"), StickyFirst: true})
	tableShell := html.Div(html.Props{Class: "workflow-list-table-shell", TabIndex: html.TabIndexZero, OnKeyDown: keyDown, Aria: map[string]string{"label": props.Text("workflow_list.table_label"), "activedescendant": activeID}}, table)

	pager := workflowListPager(props, current, bounds, selectedID)
	return html.Div(html.Props{Class: "workflow-list-shell"}, note, toolbar, count, workflowListGroupHeadings(props, pageRows, current.ShowReferences), tableShell, pager)
}

func workflowListRowAction(props WorkflowDesignerPageProps, row workflowListRow) ui.Node {
	if row.StatusKey != workflowListStatusDraft {
		return nil
	}
	draftHref := uxblindWorkflowDraftHref(props.BaseHref, row.Item)
	if draftHref == "" {
		return nil
	}
	label := uxblindWorkflowDraftCopy(props.Locale, "edit")
	return softwareLink(props.Navigate, html.Props{Class: "button secondary compact workflow-catalog-edit", Aria: map[string]string{"label": label}}, draftHref, ui.Text(label))
}

func workflowListGroupHeadings(props WorkflowDesignerPageProps, rows []workflowListRow, showReferences bool) ui.Node {
	counts := map[string]int{workflowListStatusActive: 0, workflowListStatusDraft: 0, workflowListStatusReview: 0, workflowListStatusRetired: 0}
	for _, row := range rows {
		counts[row.StatusKey]++
	}
	group := func(id, key string, count int) ui.Node {
		return html.H4(html.Props{ID: id}, ui.Text(props.Text(key)), html.Span(html.Props{Class: "count-badge", Aria: map[string]string{"label": strconv.Itoa(count)}}, ui.Text(props.Locale.FormatNumber(strconv.Itoa(count), 0))))
	}
	children := []ui.Node{
		group("workflow-list-published-heading", "workflow_designer.catalog_title", counts[workflowListStatusActive]),
		group("workflow-list-drafts-heading", "workflow_designer.drafts_heading", counts[workflowListStatusDraft]),
	}
	if showReferences {
		children = append(children, group("workflow-list-references-heading", "workflow_designer.references_heading", counts[workflowListStatusReview]+counts[workflowListStatusRetired]))
	}
	return html.Div(html.Props{Class: "workflow-list-group-headings", Aria: map[string]string{"label": props.Text("workflow_designer.catalog_heading")}}, children...)
}

func workflowListRows(props WorkflowDesignerPageProps, catalog []WorkflowCatalogItem, state workflowListState, selectedID string) []workflowListRow {
	rows := make([]workflowListRow, 0, len(catalog))
	query := strings.TrimSpace(state.Query)
	for _, item := range catalog {
		statusKey := workflowListStatus(item)
		isReference := statusKey == workflowListStatusReview || statusKey == workflowListStatusRetired
		if isReference && !state.ShowReferences {
			continue
		}
		if state.Status != workflowListStatusAll && state.Status != "" && statusKey != state.Status {
			continue
		}
		category := workflowListCategory(item.WorkflowID)
		if !MatchesJourneyListQuery(query, item.Name, item.WorkflowID, category) {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = item.WorkflowID
		}
		version := item.SemanticVersion
		if version == "" {
			version = fmt.Sprintf("definition %d", item.Version)
		} else {
			version = "v" + version
		}
		status := displayWorkflowToken(WorkflowViewerProps{I18nProps: props.I18nProps}, "publication", item.Status)
		if statusKey == workflowListStatusReview {
			status = props.Text("workflow_designer.review_only")
		}
		rows = append(rows, workflowListRow{Item: item, Name: name, Status: status, StatusKey: statusKey, Version: version, Category: category, Href: workflowListStateHref(props.BaseHref, item.WorkflowID, state)})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := rows[i], rows[j]
		switch state.Sort {
		case workflowListSortStatus:
			if left.StatusKey != right.StatusKey {
				return left.StatusKey < right.StatusKey
			}
		case workflowListSortName:
			if strings.ToLower(left.Name) != strings.ToLower(right.Name) {
				return strings.ToLower(left.Name) < strings.ToLower(right.Name)
			}
		default:
			if left.Item.Version != right.Item.Version {
				return left.Item.Version > right.Item.Version
			}
		}
		if left.Item.WorkflowID != right.Item.WorkflowID {
			return left.Item.WorkflowID < right.Item.WorkflowID
		}
		return left.Name < right.Name
	})
	_ = selectedID
	return rows
}

func workflowListStatus(item WorkflowCatalogItem) string {
	status := strings.ToLower(strings.TrimSpace(item.Status))
	if status == "retired" || strings.EqualFold(strings.TrimSpace(item.Status), "RETIRED") {
		return workflowListStatusRetired
	}
	if workflowCatalogReviewOnly(item) {
		return workflowListStatusReview
	}
	if status == "draft" {
		return workflowListStatusDraft
	}
	return workflowListStatusActive
}

func workflowListCategory(workflowID string) string {
	workflowID = strings.TrimSpace(workflowID)
	if prefix, _, ok := strings.Cut(workflowID, "."); ok && prefix != "" {
		return prefix
	}
	return "Workflow"
}

func workflowListStatusCounts(catalog []WorkflowCatalogItem, query string) map[string]int {
	counts := map[string]int{workflowListStatusActive: 0, workflowListStatusDraft: 0, workflowListStatusReview: 0, workflowListStatusRetired: 0}
	for _, item := range catalog {
		key := workflowListStatus(item)
		if MatchesJourneyListQuery(query, item.Name, item.WorkflowID, workflowListCategory(item.WorkflowID)) {
			counts[key]++
		}
	}
	return counts
}

func workflowListGroupCount(catalog []WorkflowCatalogItem, group string) int {
	count := 0
	for _, item := range catalog {
		key := workflowListStatus(item)
		if group == "draft" && key == workflowListStatusDraft {
			count++
		}
		if group == "reference" && (key == workflowListStatusReview || key == workflowListStatusRetired) {
			count++
		}
		if group == "published" && key == workflowListStatusActive {
			count++
		}
	}
	return count
}

func workflowListFilterChips(props WorkflowDesignerPageProps, state workflowListState, selectedID string, counts map[string]int) ui.Node {
	allCount := counts[workflowListStatusActive] + counts[workflowListStatusDraft]
	if state.ShowReferences {
		allCount += counts[workflowListStatusReview] + counts[workflowListStatusRetired]
	}
	values := []struct {
		key, label string
		count      int
	}{{workflowListStatusAll, props.Text("workflow_list.all"), allCount}, {workflowListStatusActive, props.Text("workflow_list.active"), counts[workflowListStatusActive]}, {workflowListStatusDraft, props.Text("workflow_list.draft"), counts[workflowListStatusDraft]}, {workflowListStatusReview, props.Text("workflow_list.review"), counts[workflowListStatusReview]}, {workflowListStatusRetired, props.Text("workflow_list.retired"), counts[workflowListStatusRetired]}}
	chips := make([]ui.Node, 0, len(values))
	for _, value := range values {
		next := state
		next.Status = value.key
		next.Page = 1
		if value.key == workflowListStatusReview || value.key == workflowListStatusRetired {
			next.ShowReferences = true
		}
		class := "workflow-list-filter-chip"
		if state.Status == value.key || (state.Status == "" && value.key == workflowListStatusAll) {
			class += " active"
		}
		chips = append(chips, html.A(html.Props{Class: class, Href: workflowListStateHref(props.BaseHref, selectedID, next), Aria: map[string]string{"label": fmt.Sprintf("%s %s", value.label, props.Locale.FormatNumber(strconv.Itoa(value.count), 0))}, Raw: map[string]any{"aria-current": map[bool]string{true: "page", false: ""}[strings.Contains(class, " active")]}},
			html.Span(html.Props{Class: "workflow-list-filter-label"}, ui.Text(value.label)),
			html.Span(html.Props{Class: "count-badge", Aria: map[string]string{"label": strconv.Itoa(value.count)}}, ui.Text(props.Locale.FormatNumber(strconv.Itoa(value.count), 0))),
		))
	}
	return html.Div(html.Props{Class: "workflow-list-filter-chips", Role: "group", Aria: map[string]string{"label": props.Text("workflow_list.status_filters")}}, chips...)
}

func workflowListPager(props WorkflowDesignerPageProps, state workflowListState, bounds PaginationBounds, selectedID string) ui.Node {
	previous := state
	previous.Page = bounds.Page - 1
	next := state
	next.Page = bounds.Page + 1
	return html.Nav(html.Props{Class: "workflow-list-pager", Aria: map[string]string{"label": props.Text("workflow_list.table_label")}},
		html.Span(html.Props{Class: "workflow-list-range"}, ui.Text(props.Text("workflow_list.range", map[string]string{"from": strconv.Itoa(bounds.First), "to": strconv.Itoa(bounds.Last), "total": strconv.Itoa(bounds.Total)}))),
		html.Button(html.Props{Class: "button secondary compact", Type: "button", Disabled: bounds.Page <= 1, OnClick: ui.UseEvent(func() {
			if props.Navigate != nil {
				props.Navigate(workflowListStateHref(props.BaseHref, selectedID, previous))
			}
		})}, ui.Text(props.Text("common.previous"))),
		html.Span(html.Props{Class: "workflow-list-page"}, ui.Text(props.Text("workflow_list.page", map[string]string{"page": strconv.Itoa(bounds.Page), "pages": strconv.Itoa(bounds.PageCount)}))),
		html.Button(html.Props{Class: "button secondary compact", Type: "button", Disabled: bounds.Page >= bounds.PageCount, OnClick: ui.UseEvent(func() {
			if props.Navigate != nil {
				props.Navigate(workflowListStateHref(props.BaseHref, selectedID, next))
			}
		})}, ui.Text(props.Text("common.next"))),
	)
}

func workflowListActiveIndex(rows []workflowListRow, selectedID string) int {
	for index, row := range rows {
		if row.Item.WorkflowID == selectedID {
			return index
		}
	}
	if len(rows) > 0 {
		return 0
	}
	return -1
}

func workflowListRowID(item WorkflowCatalogItem, index int) string {
	value := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, item.WorkflowID)
	return fmt.Sprintf("workflow-catalog-row-%s-%d", strings.Trim(value, "-"), index)
}

func workflowListStateFromHref(base string) workflowListState {
	state := workflowListState{Status: workflowListStatusAll, Sort: workflowListSortRecent, Page: 1}
	parsed, err := url.Parse(base)
	if err != nil {
		return state
	}
	values := parsed.Query()
	state.Query = values.Get(workflowListQueryKey)
	if value := values.Get(workflowListStatusKey); value != "" {
		state.Status = value
	}
	if value := values.Get(workflowListSortKey); value != "" {
		state.Sort = value
	}
	if value, err := strconv.Atoi(values.Get(workflowListPageKey)); err == nil && value > 0 {
		state.Page = value
	}
	state.ShowReferences = values.Get(workflowListReferences) == "1"
	return normalizeWorkflowListState(state)
}

func normalizeWorkflowListState(state workflowListState) workflowListState {
	state.Query = strings.Join(strings.Fields(state.Query), " ")
	if state.Status != workflowListStatusActive && state.Status != workflowListStatusDraft && state.Status != workflowListStatusReview && state.Status != workflowListStatusRetired {
		state.Status = workflowListStatusAll
	}
	if state.Sort != workflowListSortName && state.Sort != workflowListSortStatus {
		state.Sort = workflowListSortRecent
	}
	if state.Page < 1 {
		state.Page = 1
	}
	return state
}

func workflowListStateHref(base, workflowID string, state workflowListState) string {
	href := workflowDesignerHref(base, workflowID, "")
	parsed, err := url.Parse(href)
	if err != nil {
		return href
	}
	values := parsed.Query()
	state = normalizeWorkflowListState(state)
	set := func(key, value string) {
		if value == "" {
			values.Del(key)
			return
		}
		values.Set(key, value)
	}
	set(workflowListQueryKey, state.Query)
	if state.Status == workflowListStatusAll {
		set(workflowListStatusKey, "")
	} else {
		set(workflowListStatusKey, state.Status)
	}
	if state.Sort == workflowListSortRecent {
		set(workflowListSortKey, "")
	} else {
		set(workflowListSortKey, state.Sort)
	}
	if state.Page <= 1 {
		set(workflowListPageKey, "")
	} else {
		set(workflowListPageKey, strconv.Itoa(state.Page))
	}
	if state.ShowReferences {
		set(workflowListReferences, "1")
	} else {
		set(workflowListReferences, "")
	}
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

func workflowListStylesheet() string {
	return `.workflow-list-shell{display:grid;gap:var(--hcm-space-2);min-inline-size:0}
.workflow-list-toolbar{display:grid;grid-template-columns:minmax(0,1fr);gap:var(--hcm-space-2);align-items:center}
.workflow-list-search,.workflow-list-sort{display:grid;gap:.25rem;min-inline-size:0}.workflow-list-search input,.workflow-list-sort select{inline-size:100%;min-inline-size:0;min-block-size:2.5rem}
.workflow-list-references{display:flex;align-items:center;gap:var(--hcm-space-1);min-inline-size:0;min-block-size:2.5rem;white-space:normal}.workflow-list-references input{accent-color:var(--accent);flex:none}
.workflow-list-filter-chips{display:flex;flex-wrap:wrap;gap:var(--hcm-space-1);grid-column:1/-1}
.workflow-list-filter-chip{display:inline-flex;align-items:center;gap:var(--hcm-space-1);min-block-size:2.25rem;padding-inline:var(--hcm-space-2);border:1px solid var(--control-border,var(--line));border-radius:999px;color:var(--ink);text-decoration:none;font-size:var(--hcm-font-size-small)}
.workflow-list-group-headings h4{display:flex;align-items:center;gap:var(--hcm-space-1)}
.workflow-list-filter-chip:hover,.workflow-list-filter-chip:focus-visible,.workflow-list-filter-chip.active{background:var(--soft);border-color:var(--accent)}
.workflow-list-count,.workflow-list-groups{margin:0;color:var(--muted);font-size:var(--hcm-font-size-small)}
.workflow-list-table-shell{min-inline-size:0;outline:none}.workflow-list-table-shell:focus-visible{outline:2px solid var(--hcm-color-focus,var(--accent));outline-offset:2px}
.workflow-list-row-link{display:grid;gap:.125rem;color:var(--ink);text-decoration:none;min-inline-size:0}.workflow-list-row-link:hover,.workflow-list-row-link:focus-visible{color:var(--accent);text-decoration:underline}.workflow-list-row-link.active{font-weight:700}.workflow-list-name{font-weight:600}.workflow-list-id{color:var(--muted);font-size:var(--hcm-font-size-small);min-inline-size:0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.workflow-list-pager{display:flex;flex-wrap:wrap;align-items:center;justify-content:flex-end;gap:var(--hcm-space-2)}.workflow-list-range,.workflow-list-page{color:var(--muted);font-size:var(--hcm-font-size-small)}
@media (max-width:640px){.workflow-list-toolbar{grid-template-columns:1fr}.workflow-list-sort,.workflow-list-references{inline-size:100%}.workflow-list-pager{justify-content:space-between}.workflow-list-pager .workflow-list-range{inline-size:100%}}
@media (prefers-reduced-motion:reduce){.workflow-list-filter-chip,.workflow-list-row-link{transition:none}}`
}
