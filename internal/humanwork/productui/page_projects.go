package productui

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
)

// ProjectProjectionState is explicit because an empty slice is an authorized
// empty response only when the service said the projection was ready.
type ProjectProjectionState string

const (
	ProjectProjectionLoading     ProjectProjectionState = "loading"
	ProjectProjectionReady       ProjectProjectionState = "ready"
	ProjectProjectionRestricted  ProjectProjectionState = "restricted"
	ProjectProjectionUnavailable ProjectProjectionState = "unavailable"
	ProjectProjectionFailed      ProjectProjectionState = "failed"
)

// ProjectSummaryProjection contains only the fields returned in the current
// authorized project list. OwnerID is a server-provided identity label; this
// page does not resolve project owners through another directory.
type ProjectSummaryProjection struct {
	ID             string
	Name           string
	Description    string
	Status         string
	OwnerID        string
	TaskCount      int
	TaskCountKnown bool
	// DoneCount is how many of TaskCount have a workflow status categorized
	// as done. ProgressPending marks a row whose counts are still loading.
	DoneCount       int
	ProgressPending bool
}

var projectRouteID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]{0,127}$`)

// ProjectBoardHref builds the canonical board selector route. The ID is a
// selector only; the project service still authorizes each read independently.
func ProjectBoardHref(projectID string) string {
	if !projectRouteID.MatchString(projectID) {
		return ""
	}
	return "/workspace/app/project?project=" + url.QueryEscape(projectID)
}

// ProjectViewHref preserves route selectors while switching between board and
// list views. Selectors are bookmarks only; the loader authorizes each read.
func ProjectViewHref(projectID, taskID, boardViewID, cursor string, mode projectui.ViewMode) string {
	if !projectRouteID.MatchString(projectID) || (mode != projectui.ViewBoard && mode != projectui.ViewList && mode != projectui.ViewTask) {
		return ""
	}
	if mode == projectui.ViewTask && !projectRouteID.MatchString(taskID) {
		return ""
	}
	values := url.Values{"project": []string{projectID}, "view": []string{string(mode)}}
	if projectRouteID.MatchString(boardViewID) {
		values.Set("board_view", boardViewID)
	}
	if projectRouteID.MatchString(taskID) {
		values.Set("task", taskID)
	}
	if cursor != "" {
		values.Set("cursor", cursor)
	}
	return "/workspace/app/project?" + values.Encode()
}

// ProjectTaskHref builds a stable project/task detail link. The selector does
// not grant access to either resource.
func ProjectTaskHref(projectID, taskID string) string {
	if !projectRouteID.MatchString(projectID) || !projectRouteID.MatchString(taskID) {
		return ""
	}
	values := url.Values{"project": []string{projectID}, "task": []string{taskID}}
	return "/workspace/app/project?" + values.Encode()
}

// projectRouteHref is the canonical address the project loader settles on
// (projectclient.CanonicalHref): the board is the default view and is left
// out, so a link and the address it lands on are the same string and
// opening a task modal never needs a second history rewrite.
func projectRouteHref(view View, taskID, cursor string, mode projectui.ViewMode) string {
	projectID, boardViewID := view.ProjectID, view.ProjectBoardViewID
	if !projectRouteID.MatchString(projectID) {
		return ""
	}
	values := url.Values{"project": []string{projectID}}
	// The shell's own address state (locale, menu filter, favorites) rides
	// along, exactly as the shell's navigation links carry it.
	setMenuAddressState(values, view)
	if view.NavCollapsed {
		values.Set("nav", "collapsed")
	} else {
		values.Set("nav", "expanded")
	}
	if projectRouteID.MatchString(boardViewID) {
		values.Set("board_view", boardViewID)
	}
	if projectRouteID.MatchString(taskID) {
		values.Set("task", taskID)
	}
	if mode == projectui.ViewList || mode == projectui.ViewTask && projectRouteID.MatchString(taskID) {
		values.Set("view", string(mode))
	}
	if cursor != "" {
		values.Set("cursor", cursor)
	}
	if board := view.ProjectBoard; board != nil {
		if pager := board.ListPager; pager != nil && mode == projectui.ViewList {
			if pager.Page > 1 {
				values.Set("page", strconv.Itoa(pager.Page))
			}
			if pager.SizeParam > 0 {
				values.Set("page_size", strconv.Itoa(pager.SizeParam))
			}
		}
		if board.RouteFilter != "" {
			values.Set("filter", board.RouteFilter)
		}
		if board.RouteQuery != "" {
			values.Set("q", board.RouteQuery)
		}
	}
	return "/workspace/app/project?" + values.Encode()
}

func projectsPage(view View) ui.Node {
	copy := projectPageCopy(view.Locale)
	heading := []ui.Node{html.H1(html.Props{Text: view.Title})}
	if view.Subtitle != "" {
		heading = append(heading, html.P(html.Props{Class: "project-page-description", Text: view.Subtitle}))
	}
	children := []ui.Node{html.Header(html.Props{Class: "project-page-home-header"},
		html.Div(html.Props{Class: "project-page-home-heading"}, heading...),
		projectCreateForm(copy, view.ProjectCreateReady),
	)}
	// Two tabs: the projects themselves and every ticket across them. The
	// tab lives in the address, so Back and Forward move between them.
	board := projectBoardCopy(view.Locale).Board
	ticketsTab := view.ProjectBoard != nil && view.ProjectBoard.HomeTab == "tickets"
	// Counts: projects on this page, and tickets across them once the
	// per-project counts are known.
	ticketTotal, ticketsKnown := 0, len(view.Projects) > 0
	for _, project := range view.Projects {
		ticketTotal += project.TaskCount
		ticketsKnown = ticketsKnown && project.TaskCountKnown
	}
	if ticketsTab && view.ProjectBoard.Tickets != nil && !view.ProjectBoard.Tickets.Loading {
		ticketTotal, ticketsKnown = view.ProjectBoard.Tickets.Total, true
	}
	tab := func(label, href, key string, current bool, count int, known bool) ui.Node {
		props := html.Props{ID: "project-tab-" + key, Class: "project-page-tab", Role: "tab", Data: map[string]string{"tab": key}, Aria: map[string]string{"selected": strconv.FormatBool(current)}}
		if current {
			// Only the current tab's panel is on the page; the other tab
			// navigates, so it controls nothing here.
			if key == "tickets" {
				props.Aria["controls"] = "project-tabpanel-tickets"
			}
			props.Aria["current"] = "page"
		} else {
			props.TabIndex = -1
		}
		children := []ui.Node{html.Span(html.Props{Text: label})}
		if known {
			children = append(children, html.Span(html.Props{Class: "project-page-tab-count", Text: view.Locale.FormatNumber(strconv.Itoa(count), 0)}))
		}
		return softwareLink(view.Navigate, props, href, children...)
	}
	homeHref := projectsHomeHref(view)
	ticketsHref := homeHref + "?tab=tickets"
	if strings.Contains(homeHref, "?") {
		ticketsHref = homeHref + "&tab=tickets"
	}
	children = append(children, html.Div(html.Props{Class: "project-page-tabs", Role: "tablist", Aria: map[string]string{"label": view.Title}},
		tab(board.TabProjects, homeHref, "projects", !ticketsTab, len(view.Projects), view.ProjectsState == ProjectProjectionReady),
		tab(board.TabTickets, ticketsHref, "tickets", ticketsTab, ticketTotal, ticketsKnown)))
	if ticketsTab {
		children = append(children, projectTicketsTab(view, *view.ProjectBoard.Tickets))
		return html.Section(html.Props{Class: "project-page-home", Role: "region", Aria: map[string]string{"label": view.Title}}, children...)
	}
	switch view.ProjectsState {
	case "", ProjectProjectionLoading:
		children = append(children, html.P(html.Props{Role: "status", Class: "project-page-notice", Text: copy.homeLoading}))
	case ProjectProjectionRestricted, ProjectProjectionUnavailable:
		children = append(children, html.P(html.Props{Role: "status", Class: "project-page-notice", Text: copy.projectUnavailable}))
	case ProjectProjectionFailed:
		children = append(children, html.P(html.Props{Role: "alert", Class: "project-page-notice", Text: copy.homeFailed}))
	case ProjectProjectionReady:
		if len(view.Projects) == 0 {
			children = append(children, html.P(html.Props{Class: "project-page-empty", Text: copy.homeEmpty}))
			break
		}
		rows := make([]projectListRow, 0, len(view.Projects))
		for _, project := range view.Projects {
			href := ProjectBoardHref(project.ID)
			name := strings.TrimSpace(project.Name)
			if href == "" || name == "" {
				continue
			}
			row := projectListRow{ID: project.ID, Name: name, Description: strings.TrimSpace(project.Description), Status: strings.TrimSpace(project.Status), Href: href}
			row.StatusKey = strings.ToLower(row.Status)
			if result, err := view.Locale.Resolve("projects.status_" + row.StatusKey); row.StatusKey != "" && err == nil {
				row.Status = result.Text
			}
			if project.OwnerID != "" {
				row.Owner, row.OwnerPhoto = docsOwnerName(view, project.OwnerID), docsOwnerPhoto(view, project.OwnerID)
			}
			if project.TaskCountKnown {
				row.Tasks = view.Locale.FormatNumber(strconv.Itoa(project.TaskCount), 0)
				row.TaskCount, row.TaskKnown = project.TaskCount, true
				row.DoneCount = min(max(project.DoneCount, 0), project.TaskCount)
			}
			row.Pending = project.ProgressPending && !project.TaskCountKnown
			row.Search = strings.ToLower(strings.Join([]string{row.Name, row.Description, row.Status, row.Owner}, " "))
			rows = append(rows, row)
		}
		if len(rows) == 0 {
			children = append(children, html.P(html.Props{Class: "project-page-empty", Text: copy.homeEmpty}))
			break
		}
		children = append(children, ui.CreateElement(projectsList, projectsListProps{Rows: rows, Copy: projectListCopyFor(view.Locale), Navigate: view.Navigate}))
	default:
		children = append(children, html.P(html.Props{Role: "status", Class: "project-page-notice", Text: copy.projectUnavailable}))
	}
	return html.Section(html.Props{Class: "project-page-home", Role: "region", Aria: map[string]string{"label": view.Title}}, children...)
}

func projectPage(view View) ui.Node {
	copy := projectPageCopy(view.Locale)
	if !projectRouteID.MatchString(view.ProjectID) {
		return html.Section(html.Props{Class: "project-page project-page-state", Role: "region", Aria: map[string]string{"label": view.Title}},
			html.H1(html.Props{Text: view.Title}), html.P(html.Props{Text: copy.chooseProject}),
			softwareLink(view.Navigate, html.Props{Class: "project-page-open"}, Path(PageProjects), html.Span(html.Props{Text: copy.openProjects})),
		)
	}
	if view.ProjectBoardState != ProjectProjectionReady {
		return projectStatePanel(view.Title, view.ProjectBoardState, copy.boardLoading, copy.boardFailed, copy.projectUnavailable)
	}
	if view.ProjectBoard == nil {
		return projectStatePanel(view.Title, ProjectProjectionUnavailable, copy.boardLoading, copy.boardFailed, copy.projectUnavailable)
	}
	board := *view.ProjectBoard
	board.Cards = append([]projectui.Card(nil), board.Cards...)
	board.Copy = projectBoardCopy(view.Locale)
	projectLocalizeBoard(view.Locale, &board)
	board.ProjectsHref = projectsHomeHref(view)
	board.Workflows = projectLocalizeWorkflowLinks(view.Locale, board.Workflows)
	if board.Share != nil {
		share := *board.Share
		share.Href, share.Title = ProjectBoardHref(view.ProjectID), board.ProjectName
		if share.Title == "" {
			share.Title = board.Title
		}
		board.Share = &share
	}
	if board.Mode == projectui.ViewTask {
		return projectTaskPage(view, board)
	}
	for index := range board.Cards {
		// A card opens its task in the modal over this board or list; the
		// address carries the task so Back closes it and the link shares.
		if href := projectRouteHref(view, board.Cards[index].ID, view.ProjectCursor, board.Mode); href != "" {
			board.Cards[index].DetailHref = href
		} else {
			board.Cards[index].DetailHref = ""
		}
		board.Cards[index].Selected = view.ProjectDetailSelected && board.Cards[index].ID == view.ProjectTaskID
		names := make([]string, len(board.Cards[index].Workflows))
		for w, name := range board.Cards[index].Workflows {
			names[w] = projectWorkflowName(view.Locale, name)
		}
		board.Cards[index].Workflows = names
		if !view.ProjectMovesReady {
			board.Cards[index].CanMoveStatus = false
			board.Cards[index].CanMoveLane = false
		}
	}
	// The view switch is a segmented control: the current view is marked
	// aria-current and the other is a canonical link that keeps selectors.
	boardHref := projectWithFilter(ProjectViewHref(view.ProjectID, view.ProjectTaskID, view.ProjectBoardViewID, view.ProjectCursor, projectui.ViewBoard), view.ProjectBoard)
	listHref := projectWithFilter(ProjectViewHref(view.ProjectID, view.ProjectTaskID, view.ProjectBoardViewID, view.ProjectCursor, projectui.ViewList), view.ProjectBoard)
	segment := func(mode projectui.ViewMode, href, label string) ui.Node {
		props := html.Props{Class: "project-page-segment", Data: map[string]string{"view": string(mode)}, Aria: map[string]string{"label": copy.switchView(mode)}}
		if board.Mode == mode || board.Mode == "" && mode == projectui.ViewBoard {
			props.Aria = map[string]string{"current": "page"}
		}
		return softwareLink(view.Navigate, props, href, html.Span(html.Props{Class: "project-page-segment-icon", Aria: map[string]string{"hidden": "true"}}), html.Span(html.Props{Text: label}))
	}
	actions := []ui.Node{
		html.Div(html.Props{Class: "project-page-segmented", Role: "group", Aria: map[string]string{"label": copy.viewLabel}},
			segment(projectui.ViewBoard, boardHref, copy.viewBoard), segment(projectui.ViewList, listHref, copy.viewList)),
		projectBoardSettingsForm(copy, view.ProjectID, board, view.ProjectBoardSettingsReady),
		projectTaskCreateForm(copy, view.ProjectID, board, view.ProjectTaskCreateReady),
	}
	var content ui.Node
	if board.Mode == projectui.ViewList {
		content = projectui.List(board, actions...)
	} else {
		content = projectui.Board(board, actions...)
	}
	children := []ui.Node{content}
	props := html.Props{Class: "project-page-board", Role: "region", Aria: map[string]string{"label": view.Title}}
	boardOnly := projectRouteHref(view, "", view.ProjectCursor, board.Mode)
	projectNoteBoardShown(boardOnly, view.ProjectDetailSelected)
	if view.ProjectDetailSelected {
		var body ui.Node
		switch view.ProjectDetailState {
		case ProjectProjectionReady:
			if view.ProjectDetail == nil {
				body = html.P(html.Props{Role: "status", Class: "project-page-notice project-page-detail-unavailable", Text: copy.projectUnavailable})
			} else {
				detail := projectDetailForView(view, *view.ProjectDetail, board)
				body = projectui.TaskModalContent(detail, "project-task-dialog-title")
			}
		case ProjectProjectionRestricted, ProjectProjectionUnavailable:
			body = html.P(html.Props{Role: "status", Class: "project-page-notice project-page-detail-unavailable", Text: copy.projectUnavailable})
		case ProjectProjectionFailed:
			body = html.P(html.Props{Role: "alert", Class: "project-page-notice project-page-detail-failed", Text: copy.detailFailed})
		default:
			body = html.P(html.Props{Role: "status", Class: "project-page-notice project-page-detail-loading", Text: copy.detailLoading})
		}
		closeProject := func() {
			projectCloseTaskDialog(view, boardOnly)
		}
		children = append(children, ui.CreateElement(projectTaskDialog, projectTaskDialogProps{
			TaskID: view.ProjectTaskID, Label: copy.taskDetails, Body: body, Close: closeProject,
		}))
		props.Data = map[string]string{"detail": "open"}
	}
	return html.Section(props, children...)
}

// projectTaskPage renders one task's full page in place of the board.
func projectTaskPage(view View, board projectui.Model) ui.Node {
	copy := projectPageCopy(view.Locale)
	switch {
	case view.ProjectDetailState == ProjectProjectionReady && view.ProjectDetail != nil:
		detail := projectDetailForView(view, *view.ProjectDetail, board)
		return html.Section(html.Props{Class: "project-page-task", Role: "region", Aria: map[string]string{"label": view.Title}}, projectui.TaskPage(detail))
	case view.ProjectDetailState == ProjectProjectionFailed:
		return projectStatePanel(view.Title, ProjectProjectionFailed, copy.detailLoading, copy.detailFailed, copy.projectUnavailable)
	case view.ProjectDetailState == ProjectProjectionLoading || view.ProjectDetailState == "":
		return projectStatePanel(view.Title, ProjectProjectionLoading, copy.detailLoading, copy.detailFailed, copy.projectUnavailable)
	default:
		return projectStatePanel(view.Title, ProjectProjectionUnavailable, copy.detailLoading, copy.detailFailed, copy.projectUnavailable)
	}
}

// projectDetailForView localizes an authorized detail projection and adds
// the hrefs between the board, the modal and the task page.
func projectDetailForView(view View, detail projectui.DetailModel, board projectui.Model) projectui.DetailModel {
	detail.Copy = projectBoardCopy(view.Locale)
	detail.Priority = projectPriorityText(view.Locale, detail.Priority)
	detail.Status = projectStatusText(view.Locale, detail.StatusID, detail.Status)
	detail.StatusOptions = projectLocalizeStatuses(view.Locale, detail.StatusOptions)
	detail.Type = projectTypeText(view.Locale, detail.Type)
	options := make([]projectui.Status, 0, len(detail.PriorityOptions))
	for _, option := range detail.PriorityOptions {
		options = append(options, projectui.Status{ID: option.ID, Label: projectPriorityText(view.Locale, option.Label)})
	}
	detail.PriorityOptions = options
	now := time.Now()
	for index := range detail.Comments {
		detail.Comments[index].Time = projectRelativeTime(view.Locale, detail.Comments[index].DateTime, now)
	}
	// The service reports only the activity kind, not what changed, so a
	// run of identical entries by one person folds into one line with a
	// count instead of repeating "updated the task".
	// Consecutive moves and updates by one person fold into one row
	// ("moved and updated the task · 3 events"); creation and deleted
	// comments always stand alone.
	type activityRun struct {
		entry projectui.Activity
		keys  []string
		count int
	}
	runs := []activityRun{}
	for _, entry := range detail.Activity {
		key := projectActivityKey(entry.Label)
		// New and edited comments are already in the thread beside the
		// activity list, so repeating them there adds nothing.
		if key == "projectui.activity_created" || key == "projectui.activity_corrected" {
			continue
		}
		foldable := projectActivityVerb(key) != ""
		if last := len(runs) - 1; foldable && last >= 0 && runs[last].entry.Actor == entry.Actor && projectActivityVerb(runs[last].keys[0]) != "" {
			runs[last].count++
			if !slices.Contains(runs[last].keys, key) {
				runs[last].keys = append(runs[last].keys, key)
			}
			continue
		}
		runs = append(runs, activityRun{entry: entry, keys: []string{key}, count: 1})
	}
	folded := make([]projectui.Activity, 0, len(runs))
	for _, run := range runs {
		entry := run.entry
		entry.Time = projectRelativeTime(view.Locale, entry.Time, now)
		switch {
		case len(run.keys) == 1:
			entry.Label = view.Locale.Text(run.keys[0], map[string]string{"name": entry.Actor})
			if run.count > 1 {
				entry.Label += " \u00b7 " + detail.Copy.ActivityRepeat(run.count)
			}
		default:
			verbs := make([]string, 0, len(run.keys))
			for _, key := range run.keys {
				verbs = append(verbs, view.Locale.Text(projectActivityVerb(key)))
			}
			entry.Label = view.Locale.Text("projectui.activity_multi", map[string]string{"name": entry.Actor, "verbs": strings.Join(verbs, view.Locale.Text("projectui.activity_and"))})
			entry.Label += " \u00b7 " + view.Locale.Text("projectui.activity_events", map[string]string{"count": view.Locale.FormatNumber(strconv.Itoa(run.count), 0)})
		}
		folded = append(folded, entry)
	}
	detail.Activity = folded
	if detail.ProjectName == "" {
		detail.ProjectName = board.ProjectName
	}
	if detail.ProjectName == "" {
		detail.ProjectName = board.Title
	}
	detail.ProjectsHref = projectsHomeHref(view)
	mode := board.Mode
	if mode == projectui.ViewTask {
		mode = projectui.ViewBoard
	}
	detail.BoardHref = projectRouteHref(view, "", "", mode)
	detail.PageHref = projectRouteHref(view, view.ProjectTaskID, "", projectui.ViewTask)
	if !view.ProjectMovesReady {
		detail.CanMoveStatus, detail.CanEdit, detail.CanComment = false, false, false
	}
	detail.CreatedText = projectRelativeTime(view.Locale, detail.CreatedAt, now)
	detail.UpdatedText = projectRelativeTime(view.Locale, detail.UpdatedAt, now)
	if detail.CreatedAt == "" {
		detail.CreatedText = ""
	}
	if detail.UpdatedAt == "" {
		detail.UpdatedText = ""
	}
	detail.Workflows = projectLocalizeWorkflowLinks(view.Locale, detail.Workflows)
	workflowOptions := make([]projectui.WorkflowOption, len(detail.WorkflowOptions))
	for index, option := range detail.WorkflowOptions {
		option.Workflow, option.Step, option.Status = projectWorkflowName(view.Locale, option.Workflow), projectWorkflowStep(view.Locale, option.Step), projectWorkflowStatus(view.Locale, option.Status)
		option.Group = projectWorkflowStatus(view.Locale, option.Group)
		workflowOptions[index] = option
	}
	detail.WorkflowOptions = workflowOptions
	if board.Share != nil {
		// A shared ticket opens as the modal over its board.
		detail.Share = &projectui.Share{Href: ProjectTaskHref(view.ProjectID, view.ProjectTaskID), Title: detail.Title, Targets: board.Share.Targets}
	}
	return detail
}

// projectWorkflowName, projectWorkflowStep and projectWorkflowStatus turn
// the Work projection's tokens ("promotion", "manager_approval", "open")
// into the reader's words; an unknown step reads as its humanized token.
func projectWorkflowName(locale LocaleContext, token string) string {
	if result, err := locale.Resolve("projectui.workflow_" + strings.ToLower(token)); err == nil && token != "" {
		return result.Text
	}
	return projectHumanize(token)
}

func projectWorkflowStep(locale LocaleContext, token string) string {
	if result, err := locale.Resolve("projectui.step_" + strings.ToLower(token)); err == nil && token != "" {
		return result.Text
	}
	return projectHumanize(token)
}

func projectWorkflowStatus(locale LocaleContext, token string) string {
	switch {
	case strings.HasPrefix(token, "work:"):
		return locale.Text("projectui.work_status_" + strings.TrimPrefix(token, "work:"))
	case strings.HasPrefix(token, "JOURNEY_STAGE_"):
		if result, err := locale.Resolve("projectui.stage_" + strings.ToLower(strings.TrimPrefix(token, "JOURNEY_STAGE_"))); err == nil {
			return result.Text
		}
		return locale.Text("projectui.stage_in_progress")
	case strings.HasPrefix(token, "group:"):
		return locale.Text("projectui.stage_group_" + strings.TrimPrefix(token, "group:"))
	}
	return token
}

// projectLocalizeWorkflowLinks localizes linked workflow rows in place.
func projectLocalizeWorkflowLinks(locale LocaleContext, links []projectui.WorkflowLink) []projectui.WorkflowLink {
	out := make([]projectui.WorkflowLink, len(links))
	for index, link := range links {
		link.Workflow, link.Step, link.Status = projectWorkflowName(locale, link.Workflow), projectWorkflowStep(locale, link.Step), projectWorkflowStatus(locale, link.Status)
		out[index] = link
	}
	return out
}

func projectHumanize(token string) string {
	words := strings.Fields(strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(strings.ToLower(token)))
	if len(words) == 0 {
		return ""
	}
	words[0] = strings.ToUpper(words[0][:1]) + words[0][1:]
	return strings.Join(words, " ")
}

// projectTicketsTab localizes the cross-project ticket list and renders it.
func projectTicketsTab(view View, list projectui.TicketList) ui.Node {
	locale := view.Locale
	now := time.Now()
	tickets := make([]projectui.Ticket, len(list.Tickets))
	for index, ticket := range list.Tickets {
		ticket.StatusLabel = projectStatusText(locale, ticket.StatusLabel, ticket.StatusLabel)
		ticket.PriorityLabel = projectPriorityText(locale, ticket.PriorityID)
		if ticket.UpdateAt != "" {
			ticket.Updated = projectRelativeTime(locale, ticket.UpdateAt, now)
		}
		names := make([]string, len(ticket.Workflows))
		for w, name := range ticket.Workflows {
			names[w] = projectWorkflowName(locale, name)
		}
		ticket.Workflows = names
		tickets[index] = ticket
	}
	list.Tickets = tickets
	copy := projectui.TicketCopy(projectBoardCopy(locale))
	groups := make([]projectui.TicketFilterGroup, len(list.Groups))
	for index, group := range list.Groups {
		choices := append([]projectui.FilterChoice(nil), group.Choices...)
		for c := range choices {
			switch group.Key {
			case "status":
				choices[c].Label = map[string]string{"todo": copy.Board.StatusTodo, "active": copy.Board.StatusActive, "done": copy.Board.StatusDone}[choices[c].ID]
			case "priority":
				choices[c].Label = projectPriorityText(locale, choices[c].ID)
			case "due":
				choices[c].Label = map[string]string{"overdue": copy.Board.DueOverdue, "week": copy.Board.DueThisWeek}[choices[c].ID]
			case "assignee":
				if choices[c].ID == "none" {
					choices[c].Label = copy.Unassigned
				}
			case "linked":
				choices[c].Label = locale.Text("projectui.linked_to_workflow")
			}
		}
		group.Choices = choices
		group.Label = map[string]string{"project": copy.Board.FilterProject, "status": copy.Board.FilterStatus, "assignee": copy.AssigneeField, "priority": copy.PriorityField, "due": copy.Board.FilterDue, "label": copy.Board.FilterLabels, "linked": locale.Text("projectui.workflows")}[group.Key]
		groups[index] = group
	}
	list.Groups = groups
	return ui.CreateElement(projectTicketsBoard, projectTicketsProps{List: list, Copy: copy})
}

// projectsHomeHref is the Projects list address with the shell's own state.
func projectsHomeHref(view View) string {
	values := url.Values{}
	setMenuAddressState(values, view)
	if view.NavCollapsed {
		values.Set("nav", "collapsed")
	}
	if len(values) == 0 {
		return Path(PageProjects)
	}
	return Path(PageProjects) + "?" + values.Encode()
}

// projectDefaultStatusNames are the seeded English names of the starter
// workflow's statuses. A status or column still carrying one of them is
// shown in the reader's language; a name a project renamed stays as typed.
var projectDefaultStatusNames = map[string]string{
	"todo": "To do", "doing": "In progress", "done": "Done", "backlog": "Backlog", "review": "In review", "blocked": "Blocked",
}

func projectStatusText(locale LocaleContext, id, label string) string {
	if name, ok := projectDefaultStatusNames[id]; ok && (label == name || label == id || label == "") {
		return locale.Text("projectui.status_" + id)
	}
	return label
}

func projectLocalizeStatuses(locale LocaleContext, statuses []projectui.Status) []projectui.Status {
	out := make([]projectui.Status, len(statuses))
	for index, status := range statuses {
		out[index] = projectui.Status{ID: status.ID, Label: projectStatusText(locale, status.ID, status.Label)}
	}
	return out
}

// projectTypeText localizes the starter task type's name.
func projectTypeText(locale LocaleContext, label string) string {
	if label == "Task" || label == "task_default" {
		return locale.Text("projectui.type_task")
	}
	return label
}

// projectLocalizeBoard translates the board's data labels: column and
// status names that still carry their seeded defaults, priorities and the
// starter task type. Logic keeps using the IDs.
func projectLocalizeBoard(locale LocaleContext, board *projectui.Model) {
	if board.Filters != nil {
		filters := *board.Filters
		filters.Priorities = append([]projectui.FilterChoice(nil), filters.Priorities...)
		for index := range filters.Priorities {
			filters.Priorities[index].Label = projectPriorityText(locale, filters.Priorities[index].ID)
		}
		filters.Assignees = append([]projectui.FilterChoice(nil), filters.Assignees...)
		for index := range filters.Assignees {
			if filters.Assignees[index].ID == "none" {
				filters.Assignees[index].Label = locale.Text("projectui.unassigned")
			}
		}
		filters.DueSoon.Label = locale.Text("projectui.filter_due_soon")
		board.Filters = &filters
	}
	columns := make([]projectui.Column, len(board.Columns))
	for index, column := range board.Columns {
		column.Label = projectStatusText(locale, column.ID, column.Label)
		column.Statuses = projectLocalizeStatuses(locale, column.Statuses)
		columns[index] = column
	}
	board.Columns = columns
	for index := range board.Cards {
		card := &board.Cards[index]
		card.Priority = projectPriorityText(locale, card.Priority)
		card.StatusLabel = projectStatusText(locale, card.StatusID, card.StatusLabel)
		card.StatusOptions = projectLocalizeStatuses(locale, card.StatusOptions)
		card.Type = projectTypeText(locale, card.Type)
	}
	if board.LaneKind == projectui.LaneKindPriority {
		board.Lanes = append([]projectui.Lane(nil), board.Lanes...)
		for index := range board.Lanes {
			board.Lanes[index].Label = projectPriorityText(locale, board.Lanes[index].ID)
		}
	}
}

// projectActivityVerb is the past-tense verb for an activity that folds
// with its neighbours, or "" for one that stands alone.
func projectActivityVerb(key string) string {
	switch key {
	case "projectui.activity_task_moved":
		return "projectui.activity_verb_moved"
	case "projectui.activity_task_revised":
		return "projectui.activity_verb_updated"
	case "projectui.activity_task_archived":
		return "projectui.activity_verb_archived"
	case "projectui.activity_task_restored":
		return "projectui.activity_verb_restored"
	}
	return ""
}

// projectWithFilter carries the board's quick filter and search into a
// board, list or task address, so moving between views keeps them.
func projectWithFilter(href string, board *projectui.Model) string {
	if href == "" || board == nil || board.RouteFilter == "" && board.RouteQuery == "" {
		return href
	}
	values := url.Values{}
	if board.RouteFilter != "" {
		values.Set("filter", board.RouteFilter)
	}
	if board.RouteQuery != "" {
		values.Set("q", board.RouteQuery)
	}
	return href + "&" + values.Encode()
}

// projectActivityKey maps a service activity kind to its sentence. Unknown
// kinds read as a general update rather than exposing the kind name.
func projectActivityKey(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "comment_created", "task.comment.created":
		return "projectui.activity_created"
	case "comment_corrected", "task.comment.corrected":
		return "projectui.activity_corrected"
	case "comment_tombstoned", "task.comment.tombstoned":
		return "projectui.activity_tombstoned"
	case "task.created":
		return "projectui.activity_task_created"
	case "task.moved":
		return "projectui.activity_task_moved"
	case "task.archived":
		return "projectui.activity_task_archived"
	case "task.restored":
		return "projectui.activity_task_restored"
	default:
		return "projectui.activity_task_revised"
	}
}

// projectPriorityText localizes the service's priority label ("High") or
// enum name ("TASK_PRIORITY_HIGH"); anything else passes through.
func projectPriorityText(locale LocaleContext, value string) string {
	key := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(value), "TASK_PRIORITY_"))
	switch key {
	case "low", "normal", "high", "urgent":
		return locale.Text("projectui.priority_" + key)
	}
	return value
}

// projectRelativeTime turns an RFC 3339 timestamp into "5 minutes ago"
// style text; within a week, and a short date after that.
func projectRelativeTime(locale LocaleContext, value string, now time.Time) string {
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return value
	}
	elapsed := now.Sub(at)
	count := func(key string, n int) string {
		return locale.Text(key, map[string]string{"count": locale.FormatNumber(strconv.Itoa(n), 0)})
	}
	switch {
	case elapsed < time.Minute:
		return locale.Text("projectui.time_now")
	case elapsed < time.Hour:
		return count("projectui.time_minutes", int(elapsed/time.Minute))
	case elapsed < 24*time.Hour:
		return count("projectui.time_hours", int(elapsed/time.Hour))
	case elapsed < 7*24*time.Hour:
		return count("projectui.time_days", int(elapsed/(24*time.Hour)))
	}
	return projectShortDate(locale, at)
}

func projectStatePanel(title string, state ProjectProjectionState, loading, failed, unavailable string) ui.Node {
	message, role := unavailable, "status"
	switch state {
	case "", ProjectProjectionLoading:
		message = loading
	case ProjectProjectionFailed:
		message, role = failed, "alert"
	}
	return html.Section(html.Props{Class: "project-page-state", Role: "region", Aria: map[string]string{"label": title}}, html.H1(html.Props{Text: title}), html.P(html.Props{Role: role, Class: "project-page-notice", Text: message}))
}

func projectActionUnavailable(label string) ui.Node {
	return html.Button(html.Props{Class: "project-page-action-unavailable", Type: "button", Disabled: true, Aria: map[string]string{"disabled": "true"}, Text: label})
}

// projectField pairs a visible label with its control so the popover forms
// read as a single column of labelled fields.
func projectField(children ...ui.Node) ui.Node {
	return html.Div(html.Props{Class: "project-page-field"}, children...)
}

func projectFormActions(children ...ui.Node) ui.Node {
	return html.Div(html.Props{Class: "project-page-form-actions"}, children...)
}

// projectPopoverHead gives every create/settings popover a title and a close
// button, so the phone bottom sheet reads as a sheet and every size offers a
// way out besides Escape.
func projectPopoverHead(title, closeLabel string) ui.Node {
	return html.Div(html.Props{Class: "project-page-popover-head"},
		html.Span(html.Props{Class: "project-page-sheet-handle", Aria: map[string]string{"hidden": "true"}}),
		html.H2(html.Props{Class: "project-page-popover-title", Text: title}),
		html.Button(html.Props{Type: "button", Class: "project-page-popover-close", Data: map[string]string{"projectui-close-popover": "true"}, Aria: map[string]string{"label": closeLabel}}, html.Span(html.Props{Class: "project-page-detail-close-icon", Aria: map[string]string{"hidden": "true"}})),
	)
}

func projectCancelButton(label string) ui.Node {
	return html.Button(html.Props{Type: "button", Class: "button secondary", Data: map[string]string{"projectui-close-popover": "true"}, Text: label})
}

// projectTimeZones is the curated IANA list the create form offers. The
// workspace default zone is not exposed to the page yet, so UTC leads.
var projectTimeZones = []string{
	"UTC", "America/New_York", "America/Chicago", "America/Denver", "America/Phoenix", "America/Los_Angeles", "America/Anchorage", "Pacific/Honolulu",
	"America/Toronto", "America/Mexico_City", "America/Sao_Paulo", "Europe/London", "Europe/Dublin", "Europe/Lisbon", "Europe/Paris", "Europe/Berlin",
	"Europe/Madrid", "Europe/Warsaw", "Europe/Istanbul", "Africa/Cairo", "Africa/Johannesburg", "Africa/Lagos", "Asia/Riyadh", "Asia/Dubai",
	"Asia/Karachi", "Asia/Kolkata", "Asia/Singapore", "Asia/Shanghai", "Asia/Tokyo", "Australia/Sydney", "Pacific/Auckland",
}

func projectCreateForm(copy projectPageCopyText, ready bool) ui.Node {
	zones := make([]ui.Node, 0, len(projectTimeZones))
	for _, zone := range projectTimeZones {
		zones = append(zones, html.Option(html.Props{Value: zone, Selected: zone == "UTC", Text: strings.ReplaceAll(zone, "_", " ")}))
	}
	children := []ui.Node{
		projectPopoverHead(copy.newProject, copy.closeDetail),
		projectField(
			html.Label(html.Props{For: "project-create-name", Text: copy.projectName}),
			html.Input(html.Props{ID: "project-create-name", Name: "name", Type: "text", Required: true, MaxLength: 200, Disabled: !ready, Data: map[string]string{"autofocus": "true"}, Aria: map[string]string{"label": copy.projectName}}),
		),
		projectField(
			html.Label(html.Props{For: "project-create-timezone", Text: copy.timezone}),
			html.Select(html.Props{ID: "project-create-timezone", Name: "timezone", Required: true, Disabled: !ready, Aria: map[string]string{"label": copy.timezone}}, zones...),
		),
		projectFormActions(projectCancelButton(copy.cancel), html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: !ready, Text: copy.createProject})),
	}
	if !ready {
		children = append([]ui.Node{html.P(html.Props{Role: "status", Class: "project-page-form-note", Text: copy.createProjectUnavailable})}, children...)
	}
	label := copy.newProject
	if !ready {
		label = copy.createProjectUnavailableShort
	}
	return html.Details(html.Props{Class: "project-page-disclosure project-page-create-disclosure"},
		html.Summary(html.Props{Text: label}),
		html.Form(html.Props{Class: "project-page-create-form", Data: map[string]string{"projectui-action": "create-project"}}, children...),
	)
}

func projectTaskCreateForm(copy projectPageCopyText, projectID string, board projectui.Model, ready bool) ui.Node {
	children := []ui.Node{
		projectPopoverHead(copy.newTask, copy.closeDetail),
		projectField(
			html.Label(html.Props{For: "project-task-create-title", Text: copy.taskTitle}),
			html.Input(html.Props{ID: "project-task-create-title", Name: "title", Type: "text", Required: true, MaxLength: 200, Disabled: !ready, Data: map[string]string{"autofocus": "true"}, Aria: map[string]string{"label": copy.taskTitle}}),
		),
		projectField(
			html.Label(html.Props{For: "project-task-create-description", Text: copy.taskDescription}),
			html.Textarea(html.Props{ID: "project-task-create-description", Name: "description", MaxLength: 8000, Rows: 3, Disabled: !ready, Aria: map[string]string{"label": copy.taskDescription}}),
		),
	}
	statuses := make([]projectui.Status, 0)
	seen := map[string]bool{}
	for _, column := range board.Columns {
		for _, status := range column.Statuses {
			if status.ID != "" && !seen[status.ID] {
				statuses = append(statuses, status)
				seen[status.ID] = true
			}
		}
	}
	pair := []ui.Node{}
	if len(statuses) > 0 {
		options := make([]ui.Node, 0, len(statuses))
		for i, status := range statuses {
			options = append(options, html.Option(html.Props{Value: status.ID, Selected: i == 0, Text: status.Label}))
		}
		pair = append(pair, projectField(html.Label(html.Props{For: "project-task-create-status", Text: copy.initialStatus}), html.Select(html.Props{ID: "project-task-create-status", Name: "initial-status-id", Disabled: !ready, Aria: map[string]string{"label": copy.initialStatus}}, options...)))
	}
	assignees := []ui.Node{html.Option(html.Props{Raw: map[string]any{"value": ""}, Selected: true, Text: copy.unassigned})}
	for _, person := range board.Members {
		if person.ID != "" && person.Name != "" {
			assignees = append(assignees, html.Option(html.Props{Value: person.ID, Text: person.Name}))
		}
	}
	pair = append(pair, projectField(html.Label(html.Props{For: "project-task-create-assignee", Text: copy.assignee}), html.Select(html.Props{ID: "project-task-create-assignee", Name: "assignee-id", Disabled: !ready}, assignees...)))
	priorities := []ui.Node{}
	for _, level := range []string{"LOW", "NORMAL", "HIGH", "URGENT"} {
		priorities = append(priorities, html.Option(html.Props{Value: "TASK_PRIORITY_" + level, Selected: level == "NORMAL", Text: copy.priority(level)}))
	}
	pair = append(pair,
		projectField(html.Label(html.Props{For: "project-task-create-priority", Text: copy.priorityLabel}), html.Select(html.Props{ID: "project-task-create-priority", Name: "priority", Disabled: !ready}, priorities...)),
		projectField(html.Label(html.Props{For: "project-task-create-due", Text: copy.dueDate}), html.Input(html.Props{ID: "project-task-create-due", Name: "due-date", Type: "date", Disabled: !ready})),
	)
	children = append(children, html.Div(html.Props{Class: "project-page-field-pair"}, pair...))
	children = append(children, projectFormActions(projectCancelButton(copy.cancel), html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: !ready, Text: copy.createTask})))
	if !ready {
		children = append([]ui.Node{html.P(html.Props{Role: "status", Class: "project-page-form-note", Text: copy.createTaskUnavailable})}, children...)
	}
	label := copy.newTask
	if !ready {
		label = copy.createTaskUnavailableShort
	}
	summary := html.Props{Text: label}
	if ready {
		summary.Title = label + " (C)"
	}
	return html.Details(html.Props{Class: "project-page-disclosure project-page-create-disclosure"},
		html.Summary(summary),
		html.Form(html.Props{Class: "project-page-create-form project-page-task-form", Data: map[string]string{"projectui-action": "create-task", "project-id": projectID}}, children...),
	)
}

func projectBoardSettingsForm(copy projectPageCopyText, projectID string, board projectui.Model, ready bool) ui.Node {
	disabled := !ready || board.ViewID == "" || board.ViewRevision == 0
	children := []ui.Node{projectPopoverHead(copy.boardSettingsShort, copy.closeDetail)}
	if disabled {
		children = append(children, html.P(html.Props{Role: "status", Class: "project-page-form-note", Text: copy.boardSettingsUnavailable}))
	}
	groupings := []struct{ id, label string }{{"BOARD_SWIMLANE_GROUPING_NONE", copy.swimlaneNone}, {"BOARD_SWIMLANE_GROUPING_ASSIGNEE", copy.swimlaneAssignee}, {"BOARD_SWIMLANE_GROUPING_PRIORITY", copy.swimlanePriority}, {"BOARD_SWIMLANE_GROUPING_ENUM_FIELD", copy.swimlaneField}}
	options := make([]ui.Node, 0, len(groupings))
	for _, grouping := range groupings {
		options = append(options, html.Option(html.Props{Value: grouping.id, Selected: board.SwimlaneGrouping == grouping.id, Text: grouping.label}))
	}
	// The custom-field choice is shown only while "Custom field" is the
	// grouping (CSS :has on the grouping select); it lists the project's
	// enum fields by name instead of asking for an ID.
	fieldOptions := []ui.Node{html.Option(html.Props{Raw: map[string]any{"value": ""}, Selected: board.SwimlaneFieldID == "", Text: copy.chooseField})}
	for _, field := range board.EnumFields {
		fieldOptions = append(fieldOptions, html.Option(html.Props{Value: field.ID, Selected: field.ID == board.SwimlaneFieldID, Text: field.Label}))
	}
	fieldControl := projectField(
		html.Label(html.Props{For: "project-board-swimlane-field", Text: copy.swimlaneField}),
		html.Select(html.Props{ID: "project-board-swimlane-field", Name: "swimlane-field-id", Disabled: disabled || len(board.EnumFields) == 0, Aria: map[string]string{"label": copy.swimlaneFieldID}}, fieldOptions...),
	)
	if len(board.EnumFields) == 0 {
		fieldControl = html.Div(html.Props{Class: "project-page-field"}, html.P(html.Props{Class: "project-page-form-note", Text: copy.noEnumFields}), html.Input(html.Props{ID: "project-board-swimlane-field", Name: "swimlane-field-id", Type: "hidden", Raw: map[string]any{"value": board.SwimlaneFieldID}}))
	}
	children = append(children,
		projectField(
			html.Label(html.Props{For: "project-board-view-name", Text: copy.boardName}),
			html.Input(html.Props{ID: "project-board-view-name", Name: "name", Type: "text", Value: board.Title, Required: true, MaxLength: 200, Disabled: disabled, Data: map[string]string{"autofocus": "true"}, Aria: map[string]string{"label": copy.boardName}}),
		),
		projectField(
			html.Label(html.Props{For: "project-board-swimlane-grouping", Text: copy.swimlaneGrouping}),
			html.Select(html.Props{ID: "project-board-swimlane-grouping", Name: "swimlane-grouping", Disabled: disabled, Aria: map[string]string{"label": copy.swimlaneGrouping}}, options...),
		),
		html.Div(html.Props{Class: "project-page-field-custom"}, fieldControl),
		html.H3(html.Props{Class: "project-page-form-section", Text: copy.columns}),
	)
	// Each column is a name plus the workflow statuses it shows, chosen by
	// label from checkboxes; the stable IDs travel only as values.
	statuses := make([]projectui.Status, 0)
	seen := map[string]bool{}
	for _, column := range board.Columns {
		for _, status := range column.Statuses {
			if status.ID != "" && !seen[status.ID] {
				statuses = append(statuses, status)
				seen[status.ID] = true
			}
		}
	}
	rows := make([]ui.Node, 0, len(board.Columns))
	for index, column := range board.Columns {
		mapped := map[string]bool{}
		for _, status := range column.Statuses {
			mapped[status.ID] = true
		}
		boxes := make([]ui.Node, 0, len(statuses))
		for statusIndex, status := range statuses {
			id := fmt.Sprintf("project-board-column-status-%d-%d", index, statusIndex)
			boxes = append(boxes, html.Label(html.Props{For: id, Class: "project-page-check"},
				html.Input(html.Props{ID: id, Type: "checkbox", Name: fmt.Sprintf("column-status-%d", index), Value: status.ID, Checked: mapped[status.ID], Disabled: disabled}),
				html.Span(html.Props{Text: status.Label}),
			))
		}
		rows = append(rows, html.Fieldset(html.Props{Class: "project-page-column-row"},
			html.Legend(html.Props{Class: "projectui-sr", Text: copy.columnName(index + 1)}),
			projectField(
				html.Label(html.Props{For: fmt.Sprintf("project-board-column-name-%d", index), Text: copy.columnName(index + 1)}),
				html.Input(html.Props{ID: fmt.Sprintf("project-board-column-name-%d", index), Name: fmt.Sprintf("column-name-%d", index), Type: "text", Value: column.Label, Required: true, MaxLength: 100, Disabled: disabled}),
			),
			html.Div(html.Props{Class: "project-page-checks", Role: "group", Aria: map[string]string{"label": copy.columnStatuses(index + 1)}}, boxes...),
		))
	}
	children = append(children, html.Div(html.Props{Class: "project-page-column-rows"}, rows...))
	children = append(children, projectFormActions(projectCancelButton(copy.cancel), html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: disabled, Text: copy.saveBoardSettings})))
	label := copy.boardSettingsShort
	if disabled {
		label = copy.boardSettingsUnavailable
	}
	return html.Details(html.Props{Class: "project-page-disclosure project-page-settings-disclosure"},
		html.Summary(html.Props{Text: label}),
		html.Form(html.Props{Class: "project-page-create-form project-page-board-settings", Data: map[string]string{"projectui-action": "save-board-view", "project-id": projectID, "view-id": board.ViewID, "view-revision": strconv.FormatUint(board.ViewRevision, 10), "msg-unmapped": copy.statusUnmapped, "msg-failed": copy.boardSaveFailed}}, children...),
	)
}

type projectPageCopyText struct {
	homeLoading, homeFailed, homeEmpty                                                                                                        string
	projectUnavailable, boardLoading, boardFailed                                                                                             string
	detailLoading, detailFailed, chooseProject, openProjects                                                                                  string
	openProject, owner, projectList                                                                                                           string
	createProject, createTask, boardSettingsUnavailable, editTask, backToBoard                                                                string
	createProjectUnavailableShort, createTaskUnavailableShort, boardSettingsSummary                                                           string
	createProjectUnavailable, createTaskUnavailable, projectName, timezone, taskTitle, taskDescription, initialStatus, defaultStatus          string
	boardName, swimlaneGrouping, swimlaneNone, swimlaneAssignee, swimlanePriority, swimlaneField, swimlaneFieldID, columns, saveBoardSettings string
	newProject, newTask, boardSettingsShort, viewBoard, viewList, viewLabel, closeDetail, taskDetails, statusMapping                          string
	cancel, unassigned, assignee, priorityLabel, dueDate, chooseField, noEnumFields                                                           string
	statusUnmapped, boardSaveFailed                                                                                                           string
	priority                                                                                                                                  func(string) string
	openProjectNamed                                                                                                                          func(string) string
	columnName                                                                                                                                func(int) string
	columnStatuses                                                                                                                            func(int) string
	switchView                                                                                                                                func(projectui.ViewMode) string
	tasks                                                                                                                                     func(int) string
}

func projectPageCopy(locale LocaleContext) projectPageCopyText {
	tasks := func(count int) string {
		return locale.Text("projects.task_count", map[string]string{"count": locale.FormatNumber(strconv.Itoa(count), 0)})
	}
	return projectPageCopyText{
		homeLoading: locale.Text("projects.home_loading"), homeFailed: locale.Text("projects.home_failed"), homeEmpty: locale.Text("projects.home_empty"),
		projectUnavailable: locale.Text("projects.unavailable"), boardLoading: locale.Text("projects.board_loading"), boardFailed: locale.Text("projects.board_failed"),
		detailLoading: locale.Text("projects.detail_loading"), detailFailed: locale.Text("projects.detail_failed"), chooseProject: locale.Text("projects.choose_project"),
		openProjects: locale.Text("projects.open_projects"), openProject: locale.Text("projects.open_project"), owner: locale.Text("projects.owner"), projectList: locale.Text("projects.list_label"), tasks: tasks,
		createProject: locale.Text("projects.create_project"), createTask: locale.Text("projects.create_task"), boardSettingsUnavailable: locale.Text("projects.board_settings_unavailable"), editTask: locale.Text("projects.edit_task_unavailable"), backToBoard: locale.Text("projects.back_to_board"),
		createProjectUnavailable: locale.Text("projects.create_unavailable_help"), createTaskUnavailable: locale.Text("projects.create_task_unavailable_help"), projectName: locale.Text("projects.project_name"), timezone: locale.Text("projects.timezone"), taskTitle: locale.Text("projects.task_title"), taskDescription: locale.Text("projects.task_description"), initialStatus: locale.Text("projects.initial_status"), defaultStatus: locale.Text("projects.default_status"),
		createProjectUnavailableShort: locale.Text("projects.create_unavailable"), createTaskUnavailableShort: locale.Text("projects.create_task_unavailable"), boardSettingsSummary: locale.Text("projects.board_settings_summary"),
		boardName: locale.Text("projects.board_name"), swimlaneGrouping: locale.Text("projects.swimlane_grouping"), swimlaneNone: locale.Text("projects.swimlane_none"), swimlaneAssignee: locale.Text("projects.swimlane_assignee"), swimlanePriority: locale.Text("projects.swimlane_priority"), swimlaneField: locale.Text("projects.swimlane_field"), swimlaneFieldID: locale.Text("projects.swimlane_field_id"), columns: locale.Text("projects.columns"), saveBoardSettings: locale.Text("projects.save_board_settings"),
		newProject: locale.Text("projects.new_project"), newTask: locale.Text("projects.new_task"), boardSettingsShort: locale.Text("projects.board_settings_short"),
		viewBoard: locale.Text("projects.view_board"), viewList: locale.Text("projects.view_list"), viewLabel: locale.Text("projects.view_label"), closeDetail: locale.Text("projects.close_detail"), taskDetails: locale.Text("projectui.task_details"), statusMapping: locale.Text("projects.status_mapping"),
		cancel: locale.Text("projectui.cancel"), unassigned: locale.Text("projectui.unassigned"), assignee: locale.Text("projectui.assignee_field"),
		priorityLabel: locale.Text("projectui.priority_field"), dueDate: locale.Text("projectui.due_field"), chooseField: locale.Text("projects.choose_field"), noEnumFields: locale.Text("projects.no_enum_fields"),
		statusUnmapped: locale.Text("projects.board_status_unmapped"), boardSaveFailed: locale.Text("projects.board_save_failed"),
		priority: func(level string) string { return projectPriorityText(locale, level) },
		openProjectNamed: func(name string) string {
			return locale.Text("projects.open_project_named", map[string]string{"name": name})
		},
		columnName: func(number int) string {
			return locale.Text("projects.column_name", map[string]string{"number": locale.FormatNumber(strconv.Itoa(number), 0)})
		}, columnStatuses: func(number int) string {
			return locale.Text("projects.column_statuses", map[string]string{"number": locale.FormatNumber(strconv.Itoa(number), 0)})
		},
		switchView: func(mode projectui.ViewMode) string {
			if mode == projectui.ViewList {
				return locale.Text("projects.switch_to_list")
			}
			return locale.Text("projects.switch_to_board")
		},
	}
}

func projectBoardCopy(locale LocaleContext) projectui.Copy {
	text := locale.Text
	return projectui.Copy{
		Pending: text("projectui.pending"), Conflict: text("projectui.conflict"), Status: text("projectui.status"), Lane: text("projectui.lane"),
		StatusFor: text("projectui.status_for"), LaneFor: text("projectui.lane_for"), BoardLabel: text("projectui.board_label"), TasksLabel: text("projectui.tasks_label"),
		NoTasksLane: text("projectui.no_tasks_lane"), NoTasksColumn: text("projectui.no_tasks_column"), NoTasksPage: text("projectui.no_tasks_page"),
		PreviousPage: text("projectui.previous_page"), NextPage: text("projectui.next_page"), TaskPages: text("projectui.task_pages"), MoreInLane: text("projectui.more_in_lane"),
		TaskDetails: text("projectui.task_details"), LinkedItems: text("projectui.linked_items"), NoLinkedItems: text("projectui.no_linked_items"),
		Comments: text("projectui.comments"), NoComments: text("projectui.no_comments"), MoreComments: text("projectui.more_comments"),
		Activity: text("projectui.activity"), NoActivity: text("projectui.no_activity"), MoreActivity: text("projectui.more_activity"),
		Loading: text("projectui.loading"), Restricted: text("projectui.restricted"), Unavailable: text("projectui.unavailable"),
		LinkedItem: text("projectui.linked_item"), LinkedChatItem: text("projectui.linked_chat"), LinkedDocsItem: text("projectui.linked_docs"),
		StatusField: text("projectui.status_field"), AssigneeField: text("projectui.assignee_field"), DueField: text("projectui.due_field"),
		PriorityField: text("projectui.priority_field"), TypeField: text("projectui.type_field"), TaskField: text("projectui.task_field"),
		DueToday: text("projectui.due_today"), Overdue: text("projectui.overdue"), DueSoon: text("projectui.due_soon"),
		CollapseAll: text("projectui.collapse_all"), ExpandAll: text("projectui.expand_all"), ToggleLane: text("projectui.toggle_lane"), MoveTask: text("projectui.move_task"),
		LaneField: text("projectui.lane"), Unassigned: text("projectui.unassigned"), OpenTaskPage: text("projectui.open_task_page"), Close: text("projects.close_detail"),
		Description: text("projects.task_description"), NoDescription: text("projectui.no_description"), AddDescription: text("projectui.add_description"),
		Save: text("projectui.save"), Cancel: text("projectui.cancel"), Edit: text("projectui.edit"), Delete: text("projectui.delete"), AddComment: text("projectui.add_comment"),
		CommentPlaceholder: text("projectui.comment_placeholder"), Edited: text("projectui.edited"), Saving: text("projectui.saving"), Saved: text("projectui.saved"),
		SaveFailed: text("projectui.save_failed"), Projects: text("page.projects.title"), TaskKey: text("projectui.task_field"), Details: text("projectui.details"),
		LatestComments: text("projectui.latest_comments"), AllComments: text("projectui.all_comments"),
		OpenTask: text("projectui.open_task"), MoveTo: text("projectui.move_to"), AssignTo: text("projectui.assign_to"), CopyLink: text("projectui.copy_link"),
		LinkCopied: text("projectui.link_copied"), MoreActions: text("projectui.more_actions"), NoDate: text("projectui.no_date"), NoCommentsHint: text("projectui.no_comments_hint"),
		StatusLocked: text("projectui.status_locked"), StatusReadOnly: text("projectui.status_readonly"),
		SortedAscending: text("projectui.sorted_ascending"), SortedDescending: text("projectui.sorted_descending"),
		Board: projectui.BoardCopy{
			Filters: text("projectui.filters"), FilterAssignee: text("projectui.assignee_field"), FilterPriority: text("projectui.priority_field"), FilterDueSoon: text("projectui.filter_due_soon"),
			SearchTasks: text("projectui.search_tasks"), ClearFilters: text("projectui.clear_filters"), NoMatching: text("projectui.no_matching"), FiltersDone: text("projectui.filters_done"),
			FirstRunTitle: text("projectui.first_run_title"), FirstRunBody: text("projectui.first_run_body"), CreateFirstTask: text("projectui.create_first_task"),
			ShortcutsTitle: text("projectui.shortcuts_title"), ShortcutNewTask: text("projectui.shortcut_new_task"), ShortcutSearch: text("projectui.search_tasks"),
			ShortcutFilters: text("projectui.shortcut_filters"), ShortcutMove: text("projectui.shortcut_move"), ShortcutOpen: text("projectui.shortcut_open"), ShortcutHelp: text("projectui.shortcut_help"),
			Share:            text("projectui.share"),
			SendToChat:       text("projectui.send_to_chat"),
			CopyForDocs:      text("projectui.copy_for_docs"),
			CopiedForDocs:    text("projectui.copied_for_docs"),
			SendTitle:        text("projectui.send_title"),
			Conversation:     text("projectui.conversation"),
			Channels:         text("projectui.channels"),
			DirectMessages:   text("projectui.direct_messages"),
			ShareNote:        text("projectui.share_note"),
			Send:             text("projectui.send"),
			OpenConversation: text("projectui.open_conversation"),
			ShareFailed:      text("projectui.share_failed"),
			NoConversations:  text("projectui.no_conversations"),
			People:           text("projectui.people"),
			Planning:         text("projectui.planning"),
			Dates:            text("projectui.dates"),
			Reporter:         text("projectui.reporter"),
			ReporterUnknown:  text("projectui.reporter_unknown"),
			StartDate:        text("projectui.start_date"),
			StoryPoints:      text("projectui.story_points"),
			Labels:           text("projectui.labels"),
			AddLabel:         text("projectui.add_label"),
			RemoveLabel:      text("projectui.remove_label"),
			AddStartDate:     text("projectui.add_start_date"),
			AddEstimate:      text("projectui.add_estimate"),
			NoLabels:         text("projectui.no_labels"),
			Project:          text("projectui.project"),
			TabProjects:      text("projectui.tab_projects"),
			TabTickets:       text("projectui.tab_tickets"),
			TicketsLabel:     text("projectui.tickets_label"),
			AssignedToMe:     text("projectui.assigned_to_me"),
			SearchTickets:    text("projectui.search_tickets"),
			FilterProject:    text("projectui.filter_project"),
			FilterStatus:     text("projectui.filter_status"),
			FilterDue:        text("projectui.filter_due"),
			FilterLabels:     text("projectui.filter_labels"),
			StatusTodo:       text("projectui.category_todo"),
			StatusActive:     text("projectui.category_active"),
			StatusDone:       text("projectui.category_done"),
			DueOverdue:       text("projectui.due_overdue"),
			DueThisWeek:      text("projectui.due_this_week"),
			ColKey:           text("projectui.col_key"),
			ColTitle:         text("projectui.col_title"),
			ColUpdated:       text("projectui.col_updated"),
			NoTickets:        text("projectui.no_tickets"),
			NoTicketsHint:    text("projectui.no_tickets_hint"),
			NoMatch:          text("projectui.no_ticket_match"),
			LoadingTickets:   text("projectui.loading_tickets"),
			ScrollPrev:       text("projectui.scroll_prev"), ScrollNext: text("projectui.scroll_next"),
			PageSize: text("projects.page_size"), ShowFewer: text("projectui.show_fewer"),
			ShowAll: func(n int) string {
				return locale.Text("projectui.show_all", map[string]string{"count": locale.FormatNumber(strconv.Itoa(n), 0)})
			},
			Range: func(from, to, total int) string {
				return locale.Text("projects.page_range", map[string]string{"from": locale.FormatNumber(strconv.Itoa(from), 0), "to": locale.FormatNumber(strconv.Itoa(to), 0), "count": locale.FormatNumber(strconv.Itoa(total), 0)})
			},
			MyTickets: text("projectui.my_tickets"), AllTickets: text("projectui.all_tickets"), MyTicketsEmpty: text("projectui.my_tickets_empty"), MyTicketsEmptyHint: text("projectui.my_tickets_empty_hint"), Later: text("projectui.later"),
			Points: func(n int) string {
				return locale.Text("projectui.points_short", map[string]string{"count": locale.FormatNumber(strconv.Itoa(n), 0)})
			},
			PreviousPage: text("projectui.tickets_previous"),
			NextPage:     text("projectui.tickets_next"),
			SentTo:       func(name string) string { return locale.Text("projectui.sent_to", map[string]string{"name": name}) },
			Created: func(when string) string {
				return locale.Text("projectui.created_when", map[string]string{"when": when})
			},
			Updated: func(when string) string {
				return locale.Text("projectui.updated_when", map[string]string{"when": when})
			},
			TicketCount: func(shown, total int) string {
				key := "projectui.ticket_count"
				if shown != total {
					key = "projectui.ticket_count_of"
				}
				return locale.Text(key, map[string]string{"shown": locale.FormatNumber(strconv.Itoa(shown), 0), "total": locale.FormatNumber(strconv.Itoa(total), 0)})
			},
			PageOf: func(page, pages int) string {
				return locale.Text("projectui.page_of", map[string]string{"page": locale.FormatNumber(strconv.Itoa(page), 0), "pages": locale.FormatNumber(strconv.Itoa(pages), 0)})
			},
			CountOf: func(shown, total int) string {
				return locale.Text("projectui.count_of", map[string]string{"shown": locale.FormatNumber(strconv.Itoa(shown), 0), "total": locale.FormatNumber(strconv.Itoa(total), 0)})
			},
		},
		Workflow: projectui.WorkflowCopy{
			Workflows:      text("projectui.wf_workflows"),
			LinkWorkflow:   text("projectui.wf_link_workflow"),
			NoWorkflows:    text("projectui.wf_no_workflows"),
			Open:           text("projectui.wf_open"),
			Unlink:         text("projectui.wf_unlink"),
			Restricted:     text("projectui.wf_restricted"),
			Unavailable:    text("projectui.wf_unavailable"),
			Loading:        text("projectui.wf_loading"),
			PickerTitle:    text("projectui.wf_picker_title"),
			PickerSearch:   text("projectui.wf_picker_search"),
			PickerOpenOnly: text("projectui.wf_picker_open_only"),
			PickerAll:      text("projectui.wf_picker_all"),
			PickerEmpty:    text("projectui.wf_picker_empty"),
			PickerLoading:  text("projectui.wf_picker_loading"),
			Linked:         text("projectui.wf_linked"),
			FilterLinked:   text("projectui.wf_filter_linked"),
			LinkFailed:     text("projectui.wf_link_failed"),
			UnlinkFrom:     text("projectui.wf_unlink_from"),
			BoardTitle:     text("projectui.board_workflows"), BoardHint: text("projectui.board_workflows_hint"),
		},
		StatusLockedShort: text("projectui.status_locked_short"),
		// (Board copy is extended below by projectBoardCopyExtras.) MoveNotAllowed: text("projectui.move_not_allowed"), LaneEmptyHere: text("projectui.lane_empty_here"), AddDueDate: text("projectui.add_due_date"),
		FormatNumber: func(n int) string { return locale.FormatNumber(strconv.Itoa(n), 0) },
		ActivityRepeat: func(n int) string {
			return locale.Text("projectui.activity_repeat", map[string]string{"count": locale.FormatNumber(strconv.Itoa(n), 0)})
		},
		ColumnCount: func(count int) string {
			return locale.Text("projectui.column_count", map[string]string{"count": locale.FormatNumber(strconv.Itoa(count), 0)})
		},
		FormatDate: func(date time.Time) string { return projectShortDate(locale, date) },
		PageSummary: func(page, total int) string {
			return locale.Text("projectui.page_summary", map[string]string{"page": locale.FormatNumber(strconv.Itoa(page), 0), "count": locale.FormatNumber(strconv.Itoa(total), 0)})
		},
	}
}

func projectPageStylesheet() string {
	return projectPageStyles + projectListStylesheet
}

// projectShortDate renders a civil due date compactly for board chips. It
// keeps the year only when the date falls outside the current year.
func projectShortDate(locale LocaleContext, date time.Time) string {
	sameYear := date.Year() == time.Now().Year()
	switch {
	case strings.HasPrefix(locale.Resolved, "de"):
		months := []string{"Jan.", "Feb.", "März", "Apr.", "Mai", "Juni", "Juli", "Aug.", "Sept.", "Okt.", "Nov.", "Dez."}
		text := strconv.Itoa(date.Day()) + ". " + months[date.Month()-1]
		if !sameYear {
			text += " " + strconv.Itoa(date.Year())
		}
		return text
	case strings.HasPrefix(locale.Resolved, "ar"):
		months := []string{"يناير", "فبراير", "مارس", "أبريل", "مايو", "يونيو", "يوليو", "أغسطس", "سبتمبر", "أكتوبر", "نوفمبر", "ديسمبر"}
		text := locale.FormatNumber(strconv.Itoa(date.Day()), 0) + " " + months[date.Month()-1]
		if !sameYear {
			text += " " + locale.FormatNumber(strconv.Itoa(date.Year()), 0)
		}
		return text
	default:
		if sameYear {
			return date.Format("Jan 2")
		}
		return date.Format("Jan 2, 2006")
	}
}

// projectPageStyles owns the page chrome around projectui: header actions,
// the segmented view switch, popover forms, the detail side panel and the
// Projects home. The board bleeds into the page gutter so columns scroll edge
// to edge; --pu-bleed tracks the shell .main padding at each breakpoint.
const projectPageStyles = `
.project-page-board .projectui-board,.project-page-board .projectui-list{--pu-bleed:34px}
@media (max-width:1190px){.project-page-board .projectui-board,.project-page-board .projectui-list{--pu-bleed:22px}}
@media (max-width:760px){.project-page-board .projectui-board,.project-page-board .projectui-list{--pu-bleed:16px}}
@media (max-width:520px){.project-page-board .projectui-board,.project-page-board .projectui-list{--pu-bleed:12px}}
.project-page-board{display:grid;gap:var(--hcm-space-3);min-inline-size:0;align-items:start}
.project-page-board>*{min-inline-size:0}
.project-page-board :focus-visible,.project-page-home :focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:2px}

.project-page-segmented{display:inline-flex;align-items:stretch;gap:2px;padding:2px;border:1px solid color-mix(in srgb,var(--ink) 11%,var(--surface));border-radius:var(--hcm-radius-control);background:color-mix(in srgb,var(--ink) 5%,var(--surface))}
.project-page-segment{display:inline-flex;align-items:center;gap:.4rem;min-block-size:2rem;padding-inline:.75rem;border-radius:calc(var(--hcm-radius-control) - 2px);color:var(--muted);font-size:.875rem;font-weight:600;line-height:1;text-decoration:none;white-space:nowrap;transition:background-color var(--hcm-motion-fast) var(--hcm-motion-easing),color var(--hcm-motion-fast) var(--hcm-motion-easing)}
.project-page-segment:hover{color:var(--ink)}
.project-page-segment[aria-current="page"]{background:var(--surface);color:var(--ink);box-shadow:0 1px 2px color-mix(in srgb,var(--ink) 16%,transparent)}
.project-page-segment-icon{display:inline-block;inline-size:.875rem;block-size:.75rem}
.project-page-segment[data-view="board"] .project-page-segment-icon{background:linear-gradient(currentColor,currentColor) 0 0/28% 100% no-repeat,linear-gradient(currentColor,currentColor) 50% 0/28% 70% no-repeat,linear-gradient(currentColor,currentColor) 100% 0/28% 85% no-repeat;opacity:.85}
.project-page-segment[data-view="list"] .project-page-segment-icon{background:linear-gradient(currentColor,currentColor) 0 0/100% 2px no-repeat,linear-gradient(currentColor,currentColor) 0 50%/100% 2px no-repeat,linear-gradient(currentColor,currentColor) 0 100%/100% 2px no-repeat;opacity:.85}

.project-page-disclosure{position:relative;min-inline-size:0;border:0;background:none}
.project-page-disclosure>summary{display:inline-flex;align-items:center;gap:.45rem;min-block-size:2.25rem;padding-inline:.875rem;border:1px solid var(--control-border);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font-size:.875rem;font-weight:600;line-height:1.1;white-space:nowrap;list-style:none;cursor:pointer;user-select:none;transition:background-color var(--hcm-motion-fast) var(--hcm-motion-easing),border-color var(--hcm-motion-fast) var(--hcm-motion-easing)}
.project-page-disclosure>summary::-webkit-details-marker{display:none}
.project-page-disclosure>summary:hover{background:var(--hcm-hover-surface)}
.project-page-disclosure>summary::after{content:"";inline-size:.375rem;block-size:.375rem;margin-inline-start:.125rem;border-right:1.5px solid currentColor;border-bottom:1.5px solid currentColor;transform:translateY(-.1rem) rotate(45deg);opacity:.7;transition:transform var(--hcm-motion-fast) var(--hcm-motion-easing)}
.project-page-disclosure[open]>summary::after{transform:translateY(.1rem) rotate(225deg)}
.project-page-create-disclosure>summary{border-color:transparent;background:var(--accent);color:var(--on-brand)}
.project-page-create-disclosure>summary:hover{background:var(--accent-hover)}
.project-page-create-disclosure>summary::before{content:"+";font-size:1.125rem;font-weight:500;line-height:0;margin-inline-end:-.1rem}
.project-page-disclosure:has(form :disabled)>summary{border-color:color-mix(in srgb,var(--ink) 14%,var(--surface));background:color-mix(in srgb,var(--ink) 4%,var(--surface));color:var(--muted)}
.project-page-disclosure[open]>summary{position:relative;z-index:39}
.project-page-disclosure[open]::before{content:"";position:fixed;inset:0;z-index:38;margin:0;background:transparent;cursor:default}
.project-page-create-disclosure>summary::after{display:none}
.project-page-disclosure>.project-page-create-form{display:none}
.project-page-disclosure[open]>.project-page-create-form{position:absolute;z-index:40;inset-block-start:calc(100% + .5rem);inset-inline-end:0;display:grid;gap:.875rem;inline-size:min(24rem,calc(100vw - 2rem));max-block-size:min(72dvh,40rem);overflow:auto;margin:0;padding:1rem 1.125rem 1.125rem;border:1px solid color-mix(in srgb,var(--ink) 12%,var(--surface));border-radius:var(--hcm-radius-surface);background:var(--surface);box-shadow:var(--hcm-shadow-raised);color:var(--ink);text-align:start;animation:project-page-pop var(--hcm-motion-normal) var(--hcm-motion-easing)}
.project-page-settings-disclosure[open]>.project-page-create-form{inline-size:min(32rem,calc(100vw - 2rem))}
@keyframes project-page-pop{from{opacity:0;transform:translateY(-4px)}to{opacity:1;transform:none}}
.project-page-field{display:grid;gap:.3rem;min-inline-size:0}
.project-page-field>label{font-size:.8125rem;font-weight:600;color:var(--ink);max-inline-size:none}
.project-page-create-form .project-page-field :is(input,select,textarea){inline-size:100%;max-inline-size:100%;min-block-size:2.5rem;min-height:2.5rem;block-size:auto;box-sizing:border-box;padding-inline:.75rem;font-size:.9375rem}
.project-page-create-form .project-page-field textarea{min-block-size:5.5rem;padding-block:.5rem;resize:vertical;line-height:1.45}
.project-page-field-pair{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:.75rem;align-items:end}
.project-page-field-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(min(12rem,100%),1fr));gap:.625rem .75rem}
.project-page-advanced{display:grid;gap:.625rem;padding:.5rem .75rem;border-radius:var(--hcm-radius-control);background:color-mix(in srgb,var(--ink) 4%,var(--surface))}
.project-page-advanced[open]{padding-block-end:.75rem}
.project-page-advanced>summary{display:flex;align-items:center;gap:.4rem;min-block-size:2rem;color:var(--muted);font-size:.8125rem;font-weight:600;list-style:none;cursor:pointer}
.project-page-advanced>summary::-webkit-details-marker{display:none}
.project-page-advanced>summary::before{content:"";inline-size:.35rem;block-size:.35rem;border-inline-end:1.5px solid currentColor;border-block-end:1.5px solid currentColor;transform:rotate(-45deg);transition:transform var(--hcm-motion-fast) var(--hcm-motion-easing)}
[dir="rtl"] .project-page-advanced>summary::before{transform:rotate(135deg)}
.project-page-advanced[open]>summary::before{transform:rotate(45deg)}
.project-page-advanced .project-page-field>label{font-size:.75rem;color:var(--muted)}
.project-page-form-section{margin:.25rem 0 0;font-size:.875rem;font-weight:650;max-inline-size:none}
.project-page-form-actions{display:flex;justify-content:flex-end;gap:.5rem;padding-block-start:.25rem}
.project-page-form-actions .button{min-block-size:2.5rem;padding-inline:1.125rem}
.project-page-form-note,[data-projectui-result]{margin:0;padding:.625rem .75rem;border-radius:var(--hcm-radius-control);background:var(--hcm-color-info-surface);color:var(--ink);font-size:.8125rem;line-height:1.45;max-inline-size:none}
[data-projectui-result][role="alert"]{background:var(--hcm-color-danger-surface);color:var(--hcm-color-danger)}

.project-page-detail{display:grid;gap:.625rem;min-inline-size:0;align-content:start}
.project-page-detail-bar{display:flex;justify-content:flex-end}
.project-page-detail-close{display:inline-flex;align-items:center;gap:.4rem;min-block-size:2.25rem;padding-inline:.75rem;border:1px solid color-mix(in srgb,var(--ink) 12%,var(--surface));border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font-size:.875rem;font-weight:600;text-decoration:none}
.project-page-detail-close:hover{background:var(--hcm-hover-surface)}
.project-page-detail-close-icon{position:relative;inline-size:.75rem;block-size:.75rem}
.project-page-detail-close-icon::before,.project-page-detail-close-icon::after{content:"";position:absolute;inset:0;margin:auto;inline-size:100%;block-size:1.5px;background:currentColor;transform:rotate(45deg)}
.project-page-detail-close-icon::after{transform:rotate(-45deg)}
.project-page-detail .project-page-action-unavailable{justify-self:start}
.project-page-notice{margin:0;padding:.875rem 1rem;border-radius:var(--hcm-radius-control);background:var(--hcm-color-info-surface);color:var(--ink);font-size:.9375rem;max-inline-size:none}
.project-page-notice[role="alert"]{background:var(--hcm-color-danger-surface);color:var(--hcm-color-danger)}
.project-page-state{display:grid;gap:var(--hcm-space-2)}
@media (min-width:75rem){.project-page-board[data-detail="open"]{grid-template-columns:minmax(0,1fr) minmax(20rem,25rem)}.project-page-board[data-detail="open"] .projectui-columns{margin-inline-end:0;padding-inline-end:0}.project-page-detail{position:sticky;inset-block-start:0}.project-page-detail .projectui-detail{box-shadow:var(--hcm-shadow-raised)}}
@media (max-width:74.99rem){.project-page-detail{order:-1}}
.project-page-board[data-detail="open"]:has(dialog.project-task-dialog){grid-template-columns:minmax(0,1fr)}

.project-page-home{display:grid;gap:var(--hcm-space-3);min-inline-size:0}
.project-page-home-header{display:flex;flex-wrap:wrap;align-items:flex-end;justify-content:space-between;gap:var(--hcm-space-2) var(--hcm-space-3)}
.project-page-home-heading{flex:1 1 20rem;min-inline-size:0;display:grid;gap:.375rem}
.project-page-home-heading h1{margin:0;color:var(--ink);font-size:clamp(1.5rem,1.15rem + 1.1vw,2rem);line-height:1.15;letter-spacing:-.02em}
.project-page-home-heading .project-page-description{margin:0;color:var(--muted);font-size:.9375rem;max-inline-size:68ch}
.project-page-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(min(19rem,100%),1fr));gap:var(--hcm-space-2)}
.project-page-card{position:relative;display:flex;flex-direction:column;gap:.625rem;min-inline-size:0;padding:1.125rem 1.25rem 1rem;border:1px solid color-mix(in srgb,var(--ink) 11%,var(--surface));border-radius:var(--hcm-radius-surface);background:var(--surface);box-shadow:var(--hcm-shadow-resting);transition:border-color var(--hcm-motion-fast) var(--hcm-motion-easing),box-shadow var(--hcm-motion-normal) var(--hcm-motion-easing),transform var(--hcm-motion-normal) var(--hcm-motion-easing)}
.project-page-card:hover{border-color:color-mix(in srgb,var(--ink) 24%,var(--surface));box-shadow:0 1px 2px color-mix(in srgb,var(--ink) 8%,transparent),0 10px 24px -12px color-mix(in srgb,var(--ink) 34%,transparent);transform:translateY(-1px)}
.project-page-card-top{display:flex;align-items:center;justify-content:space-between;gap:.5rem;font-size:.8125rem}
.project-page-status{display:inline-flex;align-items:center;gap:.375rem;padding:.125rem .5rem;border-radius:999px;background:color-mix(in srgb,var(--ink) 6%,var(--surface));color:var(--muted);font-weight:600}
.project-page-status-dot{inline-size:.5rem;block-size:.5rem;border-radius:50%;background:currentColor}
.project-page-status[data-state="active"]{background:var(--hcm-color-success-surface);color:var(--hcm-color-success)}
.project-page-status[data-state="suspended"]{background:var(--hcm-color-warning-surface);color:var(--hcm-color-warning)}
.project-page-count{color:var(--muted);font-variant-numeric:tabular-nums}
.project-page-card h2{margin:0;font-size:1.0625rem;font-weight:650;line-height:1.3;letter-spacing:-.01em;max-inline-size:none}
.project-page-card-description{display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;overflow:hidden;margin:0;color:var(--muted);font-size:.875rem;line-height:1.5;max-inline-size:none}
.project-page-facts{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:.5rem 1rem;margin-block-start:auto;padding-block-start:.625rem;border-block-start:1px solid color-mix(in srgb,var(--ink) 9%,var(--surface));color:var(--muted);font-size:.8125rem}
.project-page-fact{display:inline-flex;gap:.375rem;min-inline-size:0}
.project-page-fact-label{font-weight:600}
.project-page-fact-label::after{content:":"}
.project-page-open{display:inline-flex;align-items:center;min-block-size:2rem;margin-inline-start:auto;color:var(--accent);font-weight:650;text-decoration:none}
.project-page-open::after{content:"";position:absolute;inset:0;border-radius:inherit}
.project-page-open:focus-visible{outline:2px solid transparent}
.project-page-open:focus-visible::after{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:2px}
.project-page-empty{margin:0;padding:2rem 1.25rem;border:1.5px dashed color-mix(in srgb,var(--ink) 16%,transparent);border-radius:var(--hcm-radius-surface);color:var(--muted);text-align:center}
@media (pointer:coarse){.project-page-disclosure>summary{min-block-size:2.75rem}.project-page-segment{min-block-size:2.5rem}.project-page-detail-close{min-block-size:2.75rem}}
@media (max-width:40rem){
.projectui-header .projectui-actions{flex:1 1 100%}
.projectui-actions .project-page-segmented{flex:1 1 100%}
.projectui-actions>.project-page-disclosure{flex:1 1 0}
.projectui-actions>.project-page-disclosure>summary{inline-size:100%;justify-content:center}
.projectui-actions .project-page-segment{flex:1;justify-content:center}
.project-page-disclosure[open]::before{background:color-mix(in srgb,#000 42%,transparent)}
.project-page-sheet-handle{display:block}
.project-page-disclosure[open]>.project-page-create-form,.project-page-settings-disclosure[open]>.project-page-create-form{position:fixed;inset:auto 0 0 0;inline-size:auto;max-block-size:86dvh;border-end-start-radius:0;border-end-end-radius:0;padding-block-end:max(1.125rem,env(safe-area-inset-bottom));animation-name:project-page-sheet}
.project-page-field-pair{grid-template-columns:minmax(0,1fr)}
.project-page-home-header .project-page-disclosure{flex:1 1 100%}
}
@keyframes project-page-sheet{from{transform:translateY(16px);opacity:.4}to{transform:none;opacity:1}}
@media (prefers-reduced-motion:reduce){.project-page-card,.project-page-card:hover,.project-page-disclosure[open]>.project-page-create-form{transition:none;transform:none;animation:none}}

.project-page-popover-head{display:flex;align-items:center;justify-content:space-between;gap:.75rem;margin-block-end:.125rem}
.project-page-popover-title{margin:0;font-size:1rem;font-weight:650;color:var(--ink);max-inline-size:none}
.project-page-popover-close{display:grid;place-items:center;inline-size:2.25rem;block-size:2.25rem;min-height:2.25rem;padding:0;border:0;border-radius:var(--hcm-radius-control);background:transparent;color:var(--muted);cursor:pointer}
.project-page-popover-close:hover{background:var(--hcm-hover-surface);color:var(--ink)}
.project-page-sheet-handle{display:none;position:absolute;inset-block-start:.4rem;inset-inline-start:calc(50% - 1.125rem);inline-size:2.25rem;block-size:.25rem;border-radius:999px;background:color-mix(in srgb,var(--ink) 22%,transparent)}
.project-page-create-form{position:relative}
.project-page-task-form{inline-size:min(28rem,calc(100vw - 2rem))!important}
.project-page-board-settings:not(:has(#project-board-swimlane-grouping option[value="BOARD_SWIMLANE_GROUPING_ENUM_FIELD"]:checked)) .project-page-field-custom{display:none}
.project-page-column-rows{display:grid;gap:.5rem}
.project-page-column-row{display:grid;grid-template-columns:minmax(0,1fr);gap:.5rem;margin:0;padding:0;border:0;background:none}
.project-page-column-row+.project-page-column-row{padding-block-start:.75rem;border-block-start:1px solid color-mix(in srgb,var(--ink) 8%,var(--surface))}
.project-page-column-row .project-page-field :is(input):not(#project-none){min-block-size:2.25rem;min-height:2.25rem}
.project-page-checks{display:flex;flex-wrap:nowrap;gap:.375rem;min-block-size:2.25rem;overflow-x:auto}
.project-page-check{display:inline-flex;align-items:center;gap:.375rem;min-block-size:2.25rem;padding-inline:.5rem .625rem;border:1px solid var(--control-border);border-radius:999px;background:var(--surface);font-size:.8125rem;font-weight:500;color:var(--ink);cursor:pointer;white-space:nowrap}
.project-page-check:hover{border-color:color-mix(in srgb,var(--ink) 30%,var(--surface))}
.project-page-check:has(input:checked){border-color:color-mix(in srgb,var(--accent) 55%,var(--surface));background:color-mix(in srgb,var(--accent) 12%,var(--surface));font-weight:600}
.project-page-check:has(input:focus-visible){outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:2px}
.project-page-check input:not(#project-none){inline-size:.875rem;block-size:.875rem;min-height:0;min-block-size:0;margin:0;accent-color:var(--accent)}
.project-page-board-settings .project-page-form-actions{position:sticky;inset-block-end:-1.125rem;z-index:1;margin:0 -1.125rem -1.125rem;padding:.75rem 1.125rem;border-block-start:1px solid color-mix(in srgb,var(--ink) 10%,var(--surface));background:var(--surface)}
.project-page-create-form:has(input[required]:invalid) button[type="submit"]{opacity:.5;cursor:not-allowed}
:root[data-hcm-color-mode="dark"] :is(.project-page-create-disclosure>summary,.project-page-create-form .button.primary){border-color:transparent;background:var(--pu-primary-dark);color:#fff}
:root[data-hcm-color-mode="dark"] :is(.project-page-create-disclosure>summary,.project-page-create-form .button.primary):hover{background:var(--pu-primary-dark-hover)}
@media (prefers-color-scheme:dark){:root[data-hcm-color-mode="system"] :is(.project-page-create-disclosure>summary,.project-page-create-form .button.primary){border-color:transparent;background:var(--pu-primary-dark);color:#fff}:root[data-hcm-color-mode="system"] :is(.project-page-create-disclosure>summary,.project-page-create-form .button.primary):hover{background:var(--pu-primary-dark-hover)}}
.project-page-form-actions{align-items:center}
.project-page-form-actions .button.secondary{min-block-size:2.5rem;padding-inline:1rem}
@media (pointer:coarse){.project-page-popover-close{inline-size:2.75rem;block-size:2.75rem}.project-page-check{min-block-size:2.75rem}}
@media (max-width:40rem){.project-page-task-form{inline-size:auto!important}.project-page-popover-head{padding-block-start:.5rem}.project-page-column-row{grid-template-columns:minmax(0,1fr)}.project-page-board-settings .project-page-form-actions{inset-block-end:calc(-1 * max(1.125rem,env(safe-area-inset-bottom)));margin-block-end:calc(-1 * max(1.125rem,env(safe-area-inset-bottom)));padding-block-end:max(.75rem,env(safe-area-inset-bottom))}}
`
