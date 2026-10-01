package productui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func workflowHistoryPage(view View) ui.Node {
	allRuns := workflowHistoryRuns(view)
	filter := WorkflowRunHistoryFilter{
		WorkflowType: view.WorkflowQuery, StatusGroup: view.HistoryOutcome, Query: view.HistoryQuery,
		Person: view.HistoryPerson, Requester: view.HistoryRequester, Sort: view.HistorySort, Direction: view.HistoryDirection,
		From: view.JourneyList.From, To: view.JourneyList.To,
		Page: view.HistoryPage, PageSize: view.HistoryPageSize, Relationship: workflowHistoryRelationshipVisible,
	}
	if filter.StatusGroup == "" {
		filter.StatusGroup = view.JourneyList.Status
	}
	if filter.Query == "" {
		filter.Query = view.JourneyList.Query
	}
	if filter.Sort == "" {
		filter.Sort = view.JourneyList.Sort
		if filter.Sort == JourneyListSortRecent {
			filter.Sort = "started"
		} else if filter.Sort == JourneyListSortOldest {
			filter.Sort = "started"
			filter.Direction = "asc"
		}
	}
	runs := FilterWorkflowHistoryRuns(allRuns, filter)
	window := workflowHistoryWindowFor(runs, filter.Page, filter.PageSize)
	exportHref := statefulHref(view, PageReportExport, "source", "workflow_history", "history_q", filter.Query, "workflow_q", filter.WorkflowType, "outcome", filter.StatusGroup, "history_person", filter.Person, "history_requester", filter.Requester, JourneyListFromKey, filter.From, JourneyListToKey, filter.To, "history_sort", filter.Sort, "history_dir", filter.Direction)
	children := []ui.Node{
		ui.CreateElement(SectionHeading, SectionHeadingProps{Title: view.Locale.Text("workflow_history.heading"), Description: view.Locale.Text("workflow_history.description"), Trailing: softwareLink(view.Navigate, html.Props{Class: "button secondary"}, exportHref, ui.Text(view.Locale.Text("workflow_history.export")))}),
		workflowHistoryFilters(view, filter),
	}
	if strings.TrimSpace(view.LoadError) != "" {
		children = append(children, ui.CreateElement(EmptyState, EmptyStateProps{Title: view.Locale.Text("shell.page_unavailable"), Description: view.Locale.Text("shell.load_recovery"), Role: "alert"}))
		return html.Section(html.Props{Class: workflowHistoryPageClass(view)}, children...)
	}
	if len(window.Items) == 0 {
		children = append(children, ui.CreateElement(EmptyState, EmptyStateProps{Title: view.Locale.Text("workflow_history.empty"), Description: view.Locale.Text("workflow_history.empty_detail")}))
		return html.Section(html.Props{Class: workflowHistoryPageClass(view)}, children...)
	}
	children = append(children, html.Div(html.Props{Class: "workflow-history-results", Raw: map[string]any{"aria-live": "polite"}},
		html.P(html.Props{Class: "muted"}, ui.Text(workflowHistoryRecordCount(view, window.Total))),
		ui.CreateElement(DataTable, workflowHistoryTableProps(view, window.Items, filter)),
		workflowHistoryPagination(view, window, filter),
	))
	return html.Section(html.Props{Class: workflowHistoryPageClass(view)}, children...)
}

func workflowHistoryPageClass(view View) string {
	return "workflow-history-page workflow-history" + historyDensityClass(historyDensityFromTheme(view.EffectiveAppearance()))
}

func workflowHistoryRecordCount(view View, total int) string {
	key := "workflow_history.records"
	if total == 1 {
		key = "workflow_history.record"
	}
	return fmt.Sprintf("%d %s", total, view.Locale.Text(key))
}

func workflowHistoryCompatibilityPage(view View) ui.Node {
	return workflowHistoryPage(view)
}

func workflowHistoryFilters(view View, filter WorkflowRunHistoryFilter) ui.Node {
	statusOptions := []ui.Node{html.Option(html.Props{Value: "", Selected: filter.StatusGroup == "", Raw: map[string]any{"value": ""}}, ui.Text(view.Locale.Text("workflow_history.status_all")))}
	for _, value := range JourneyStatusFilterValues() {
		statusOptions = append(statusOptions, html.Option(html.Props{Value: value, Selected: filter.StatusGroup == value}, ui.Text(view.Locale.Text("workflow_history.status_"+value))))
	}
	sortOptions := []ui.Node{
		html.Option(html.Props{Value: "started", Selected: filter.Sort == "started"}, ui.Text(view.Locale.Text("workflow_history.sort_started"))),
		html.Option(html.Props{Value: "updated", Selected: filter.Sort == "updated"}, ui.Text(view.Locale.Text("workflow_history.sort_updated"))),
		html.Option(html.Props{Value: "status", Selected: filter.Sort == "status"}, ui.Text(view.Locale.Text("workflow_history.sort_status"))),
	}
	controls := []ui.Node{
		LabeledControl(LabeledControlProps{For: "workflow-history-query", Label: view.Locale.Text("workflow_history.search"), Control: html.Tag("input", html.Props{ID: "workflow-history-query", Name: "history_q", Value: filter.Query, Raw: map[string]any{"type": "search", "placeholder": view.Locale.Text("workflow_history.search_placeholder")}})}),
		LabeledControl(LabeledControlProps{For: "workflow-history-type", Label: view.Locale.Text("workflow_history.workflow"), Control: html.Tag("input", html.Props{ID: "workflow-history-type", Name: "workflow_q", Value: filter.WorkflowType, Raw: map[string]any{"type": "search"}})}),
		LabeledControl(LabeledControlProps{For: "workflow-history-requester", Label: view.Locale.Text("workflow_history.requester"), Control: html.Tag("input", html.Props{ID: "workflow-history-requester", Name: "history_requester", Value: filter.Requester, Raw: map[string]any{"type": "search"}})}),
		LabeledControl(LabeledControlProps{For: "workflow-history-status", Label: view.Locale.Text("workflow_history.status"), Control: html.Select(html.Props{ID: "workflow-history-status", Name: "outcome", Value: filter.StatusGroup}, statusOptions...)}),
		LabeledControl(LabeledControlProps{For: "workflow-history-person", Label: view.Locale.Text("workflow_history.person"), Control: html.Tag("input", html.Props{ID: "workflow-history-person", Name: "history_person", Value: filter.Person, Raw: map[string]any{"type": "search"}})}),
		LabeledControl(LabeledControlProps{For: "workflow-history-from", Label: view.Locale.Text("workflow_history.from"), Control: html.Tag("input", html.Props{ID: "workflow-history-from", Name: JourneyListFromKey, Value: filter.From, Raw: map[string]any{"type": "date"}})}),
		LabeledControl(LabeledControlProps{For: "workflow-history-to", Label: view.Locale.Text("workflow_history.to"), Control: html.Tag("input", html.Props{ID: "workflow-history-to", Name: JourneyListToKey, Value: filter.To, Raw: map[string]any{"type": "date"}})}),
		LabeledControl(LabeledControlProps{For: "workflow-history-sort", Label: view.Locale.Text("workflow_history.sort"), Control: html.Select(html.Props{ID: "workflow-history-sort", Name: "history_sort", Value: filter.Sort}, sortOptions...)}),
		html.Div(html.Props{Class: "history-filter-actions"},
			html.Button(html.Props{Class: "button primary", Raw: map[string]any{"type": "submit"}}, ui.Text(view.Locale.Text("workflow_history.apply"))),
			softwareLink(view.Navigate, html.Props{Class: "button secondary"}, statefulHref(view, PageWorkflowHistory), ui.Text(view.Locale.Text("history.clear"))),
		),
	}
	return html.Form(html.Props{Class: "history-filter workflow-history-filters", Action: statefulHref(view, PageWorkflowHistory), Method: "get", Raw: map[string]any{"role": "search"}},
		html.Div(html.Props{Class: "history-filter-controls"}, controls...),
		html.Tag("input", html.Props{Name: "history_dir", Value: filter.Direction, Raw: map[string]any{"type": "hidden"}}),
		html.Tag("input", html.Props{Name: "history_page", Value: "1", Raw: map[string]any{"type": "hidden"}}),
		html.Tag("input", html.Props{Name: "history_page_size", Value: fmt.Sprint(filter.PageSize), Raw: map[string]any{"type": "hidden"}}),
	)
}

func workflowHistoryTableProps(view View, runs []WorkflowHistoryRun, filter WorkflowRunHistoryFilter) DataTableProps {
	columns := []DataTableColumnProps{
		{ID: "workflow", Label: view.Locale.Text("workflow_history.workflow"), Width: "16rem"},
		{ID: "version", Label: view.Locale.Text("workflow_history.version"), Width: "8rem"},
		{ID: "subject", Label: view.Locale.Text("workflow_history.person"), Width: "14rem"},
		{ID: "requester", Label: view.Locale.Text("workflow_history.requester"), Width: "14rem"},
		{ID: "participants", Label: view.Locale.Text("workflow_history.participants"), Width: "14rem"},
		{ID: "stage", Label: view.Locale.Text("workflow_history.stage"), Width: "14rem"},
		{ID: "started", Label: view.Locale.Text("workflow_history.started"), Width: "12rem"},
		{ID: "updated", Label: view.Locale.Text("workflow_history.updated"), Width: "12rem"},
	}
	rows := make([]DataTableRowProps, 0, len(runs))
	for _, run := range runs {
		rows = append(rows, DataTableRowProps{ID: run.ID, Cells: []DataTableCellProps{
			{ColumnID: "workflow", Text: run.Workflow, RowHeader: true}, {ColumnID: "version", Text: run.Version},
			{ColumnID: "subject", Text: valueOrRedacted(run.Subject)}, {ColumnID: "requester", Text: valueOrRedacted(run.Requester)},
			{ColumnID: "participants", Text: valueOrRedacted(run.Participants)}, {ColumnID: "stage", Text: run.Stage + " · " + run.StatusGroup},
			{ColumnID: "started", Text: valueOrUnavailable(run.StartedAt)}, {ColumnID: "updated", Text: valueOrUnavailable(run.UpdatedAt)},
		}})
	}
	return DataTableProps{ID: "workflow-history-table", Caption: view.Locale.Text("workflow_history.table"), AriaLabel: view.Locale.Text("workflow_history.table"), Busy: view.Refreshing, BusyLabel: view.Locale.Text("shell.loading_authorized"), Columns: columns, Rows: rows, StickyFirst: true}
}

func workflowHistoryPagination(view View, window workflowHistoryWindow, filter WorkflowRunHistoryFilter) ui.Node {
	if window.TotalPages < 2 {
		return ui.Text("")
	}
	previous := window.Page - 1
	next := window.Page + 1
	children := []ui.Node{html.Span(html.Props{Class: "muted"}, ui.Text(fmt.Sprintf("%d / %d", window.Page, window.TotalPages)))}
	if previous >= 1 {
		children = append(children, softwareLink(view.Navigate, html.Props{Class: "button secondary"}, workflowHistoryPageHref(view, filter, previous), ui.Text("Previous")))
	}
	if next <= window.TotalPages {
		children = append(children, softwareLink(view.Navigate, html.Props{Class: "button secondary"}, workflowHistoryPageHref(view, filter, next), ui.Text("Next")))
	}
	return html.Nav(html.Props{Class: "workflow-history-pagination", Aria: map[string]string{"label": view.Locale.Text("workflow_history.pagination")}}, children...)
}

func workflowHistoryPageHref(view View, filter WorkflowRunHistoryFilter, page int) string {
	return statefulHref(view, PageWorkflowHistory,
		"history_q", filter.Query, "workflow_q", filter.WorkflowType,
		"outcome", filter.StatusGroup, "history_person", filter.Person,
		"history_requester", filter.Requester, JourneyListFromKey, filter.From,
		JourneyListToKey, filter.To, "history_sort", filter.Sort,
		"history_dir", filter.Direction, "history_page", fmt.Sprint(page),
		"history_page_size", fmt.Sprint(filter.PageSize))
}

func valueOrRedacted(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}
