package projectui

import (
	"fmt"
	"hash/fnv"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const pageCardLimit = 100
const detailEntryLimit = 50

// Board renders a horizontally scrolling Kanban board over a single page of
// authorized cards. Status and lane controls are native selects so keyboard
// operation is built in. Actions, when given, are placed in the board header
// beside the title (the host's view switch and create/settings popovers).
func Board(model Model, actions ...ui.Node) ui.Node {
	return render(model, ViewBoard, actions)
}

// List renders the same authorized projection as a dense semantic list with
// status and editable-lane controls.
func List(model Model, actions ...ui.Node) ui.Node {
	return render(model, ViewList, actions)
}

// TaskDetail renders an authorized task projection with bounded comments,
// activity, and safe Chat/Docs reference states.
func TaskDetail(model DetailModel) ui.Node {
	model.Copy = localizedCopy(model.Copy)
	if len(model.Comments) > detailEntryLimit {
		model.Comments = model.Comments[:detailEntryLimit]
	}
	if len(model.Activity) > detailEntryLimit {
		model.Activity = model.Activity[:detailEntryLimit]
	}
	if len(model.Links) > detailEntryLimit {
		model.Links = model.Links[:detailEntryLimit]
	}
	head := []ui.Node{html.H2(html.Props{Class: "projectui-detail-title", Text: model.Title})}
	if model.Description != "" {
		head = append(head, html.P(html.Props{Class: "projectui-detail-description", Text: model.Description}))
	}
	children := []ui.Node{
		html.Div(html.Props{Class: "projectui-detail-head"}, head...),
		html.Tag("dl", html.Props{Class: "projectui-facts"}, detailFacts(model)...),
		detailSection(model.Copy.LinkedItems, references(model.Links, model.Copy)),
	}
	comments := []ui.Node{commentList(model.Comments, model.Copy)}
	if model.CommentsMoreHref != "" {
		comments = append(comments, html.A(html.Props{Href: model.CommentsMoreHref, Class: "projectui-more", Text: model.Copy.MoreComments}))
	}
	children = append(children, detailSection(model.Copy.Comments, comments...))
	activity := []ui.Node{activityList(model.Activity, model.Copy)}
	if model.ActivityMoreHref != "" {
		activity = append(activity, html.A(html.Props{Href: model.ActivityMoreHref, Class: "projectui-more", Text: model.Copy.MoreActivity}))
	}
	children = append(children, detailSection(model.Copy.Activity, activity...))
	return html.Section(html.Props{Class: "projectui-detail", Aria: map[string]string{"label": model.Copy.TaskDetails}}, children...)
}

func detailSection(title string, body ...ui.Node) ui.Node {
	return html.Div(html.Props{Class: "projectui-detail-section"}, append([]ui.Node{html.H3(html.Props{Text: title})}, body...)...)
}

func render(model Model, mode ViewMode, actions []ui.Node) ui.Node {
	model.Mode = mode
	if len(model.Cards) > pageCardLimit {
		model.Cards = model.Cards[:pageCardLimit]
	}
	model.Copy = localizedCopy(model.Copy)
	model.Copy.Board = localizedBoardCopy(model.Copy.Board, model.Copy.FormatNumber)
	model.Copy.Workflow = localizedWorkflowCopy(model.Copy.Workflow)
	allCards := model.Cards
	if model.Filters.Active() {
		model.columnTotals = map[string]int{}
		for _, column := range model.Columns {
			for _, card := range allCards {
				if inColumn(card, column) {
					model.columnTotals[column.ID]++
				}
			}
		}
		shown := make([]Card, 0, len(allCards))
		for _, card := range allCards {
			if !card.FilteredOut {
				shown = append(shown, card)
			}
		}
		model.Cards = shown
		model.Lanes = append([]Lane(nil), model.Lanes...)
		for index := range model.Lanes {
			count := 0
			for _, card := range shown {
				if card.LaneID == model.Lanes[index].ID {
					count++
				}
			}
			model.Lanes[index].Count = count
		}
	}
	if model.PendingText == "" {
		model.PendingText = model.Copy.Pending
	}
	if model.Today.IsZero() {
		model.Today = time.Now()
	}
	class := "projectui-board"
	if mode == ViewList {
		class = "projectui-list"
	}
	heading := []ui.Node{}
	if model.ProjectsHref != "" {
		crumbs := []ui.Node{html.Li(html.Props{Class: "projectui-crumb-back"}, html.A(html.Props{Href: model.ProjectsHref, Dir: "auto", Text: model.Copy.Projects}))}
		name := model.ProjectName
		if name == "" {
			name = model.Title
		}
		crumbs = append(crumbs, html.Li(html.Props{Aria: map[string]string{"current": "page"}}, html.Span(html.Props{Dir: "auto", Text: name})))
		heading = append(heading, html.Nav(html.Props{Class: "projectui-breadcrumb", Aria: map[string]string{"label": model.Copy.Projects}}, html.Ol(html.Props{}, crumbs...)))
	}
	heading = append(heading, html.H1(html.Props{Text: model.Title}))
	if model.Description != "" {
		heading = append(heading, html.P(html.Props{Class: "projectui-description", Dir: "auto", Text: model.Description}))
	}
	header := []ui.Node{html.Div(html.Props{Class: "projectui-heading"}, heading...)}
	if mode == ViewBoard && len(model.Lanes) > 0 {
		actions = append([]ui.Node{laneToggle(model)}, actions...)
	}
	if len(model.Workflows) > 0 && len(actions) > 0 {
		actions = append(actions[:len(actions)-1:len(actions)-1], boardWorkflowsPanel(model), actions[len(actions)-1])
	}
	if share := ShareMenu(model.Copy, model.Share); share != nil && len(actions) > 0 {
		// Share sits just before the primary action.
		actions = append(actions[:len(actions)-1:len(actions)-1], share, actions[len(actions)-1])
	}
	if len(actions) > 0 {
		header = append(header, html.Div(html.Props{Class: "projectui-actions"}, actions...))
	}
	children := []ui.Node{html.Header(html.Props{Class: "projectui-header"}, header...)}
	firstRunBoard := len(allCards) == 0 && !model.Filters.Active() && model.Page.Number <= 1 && !model.Page.HasNext && !model.Page.HasPrevious && model.ProjectID != ""
	if model.Filters != nil && !firstRunBoard {
		children = append(children, filterSection(model))
	}
	switch {
	case firstRunBoard:
		children = append(children, firstRun(model))
	case mode == ViewList:
		children = append(children, renderList(model))
	default:
		children = append(children, renderColumns(model))
	}
	if model.Filters != nil {
		children = append(children, shortcutHelp(model))
	}
	if dialog := ShareDialog(model.Copy, model.Share); dialog != nil {
		children = append(children, dialog)
	}
	children = append(children, pagination(model.Page, model.Copy))
	label := model.Title
	if label == "" {
		label = model.Copy.BoardLabel
		if mode == ViewList {
			label = model.Copy.TasksLabel
		}
	}
	return html.Section(html.Props{Class: class, Role: "region", Aria: map[string]string{"label": label}, Data: map[string]string{"projectui": "true", "view": string(mode)}}, children...)
}

// renderColumns lays the board out column-major when there are no lanes and
// lane-major (a header row, then one full-width band per lane) otherwise, so
// a lane reads straight across every status.
func renderColumns(model Model) ui.Node {
	tones := columnTones(model.Columns)
	statusTone := statusTones(model.Columns, tones)
	regionProps := html.Props{Class: "projectui-columns", Role: "region", TabIndex: html.TabIndexZero, Aria: map[string]string{"label": model.Copy.BoardLabel}, Data: map[string]string{"lanes": boolString(len(model.Lanes) > 0), "columns": strconv.Itoa(len(model.Columns))}}
	// Drag and drop is progressive: the host's document listeners read these
	// attributes, and the status/lane selects remain the keyboard path.
	if model.LaneKind != "" && len(model.Lanes) > 0 {
		regionProps.Data["lane-kind"] = model.LaneKind
		if model.LaneFieldID != "" {
			regionProps.Data["lane-field"] = model.LaneFieldID
		}
	}
	if key := LaneStorageKey(model.ProjectID, model.ViewID); key != "" {
		regionProps.Data["lane-store"] = key
	}
	if len(model.Lanes) == 0 {
		cols := make([]ui.Node, 0, len(model.Columns))
		for columnIndex, column := range model.Columns {
			cards := make([]ui.Node, 0)
			for i, card := range model.Cards {
				if inColumn(card, column) {
					cards = append(cards, renderCard(model, card, i, statusTone))
				}
			}
			headID := fmt.Sprintf("projectui-column-%d", columnIndex)
			body := html.Div(html.Props{Class: "projectui-column-body", Role: "list", Aria: map[string]string{"labelledby": headID}, Data: dropData(column, nil)}, cards...)
			if len(cards) == 0 {
				empty := model.Copy.NoTasksColumn
				if model.Filters.Active() {
					empty = model.Copy.Board.NoMatching
				}
				body = html.Div(html.Props{Class: "projectui-column-body", Data: dropData(column, nil)}, emptyColumn(empty, tones[columnIndex]))
			}
			cols = append(cols, html.Section(html.Props{Class: "projectui-column", Data: map[string]string{"column-id": column.ID, "tone": tones[columnIndex]}, Aria: map[string]string{"labelledby": headID}},
				columnHead(column.Label, headID, tones[columnIndex], len(cards), model.columnTotal(column.ID, len(cards)), model.Copy), body))
		}
		// On a board wider than the screen, buttons at the edges scroll one
		// column at a time; the host shows each only when there is more
		// that way.
		scroll := func(dir, label string) ui.Node {
			return html.Button(html.Props{Type: "button", Class: "projectui-scroll-btn", Data: map[string]string{"dir": dir, "projectui-action": "scroll-columns"}, Aria: map[string]string{"label": label}, Title: label},
				html.Span(html.Props{Class: "projectui-scroll-chevron", Aria: map[string]string{"hidden": "true"}}))
		}
		return html.Div(html.Props{Class: "projectui-columns-frame"}, html.Div(regionProps, cols...), scroll("prev", model.Copy.Board.ScrollPrev), scroll("next", model.Copy.Board.ScrollNext))
	}
	children := make([]ui.Node, 0, len(model.Columns)+len(model.Lanes))
	counts := make([]int, len(model.Columns))
	for columnIndex, column := range model.Columns {
		for _, card := range model.Cards {
			if inColumn(card, column) {
				counts[columnIndex]++
			}
		}
	}
	for columnIndex, column := range model.Columns {
		headID := fmt.Sprintf("projectui-column-%d", columnIndex)
		children = append(children, html.Div(html.Props{Class: "projectui-column projectui-column-cap", Data: map[string]string{"column-id": column.ID, "tone": tones[columnIndex]}},
			columnHead(column.Label, headID, tones[columnIndex], counts[columnIndex], model.columnTotal(column.ID, counts[columnIndex]), model.Copy)))
	}
	for laneIndex, lane := range model.Lanes {
		laneID := fmt.Sprintf("projectui-lane-%d", laneIndex)
		cellsID := laneID + "-cells"
		toggle := html.Button(html.Props{Type: "button", Class: "projectui-lane-toggle", Aria: map[string]string{"expanded": boolString(!lane.Collapsed), "controls": cellsID}, Data: map[string]string{"projectui-action": "toggle-lane", "lane-key": lane.ID}},
			html.Span(html.Props{Class: "projectui-lane-chevron", Aria: map[string]string{"hidden": "true"}}),
			html.Span(html.Props{Class: "projectui-lane-label", Text: lane.Label}),
			countBadge(lane.Count, model.Copy),
		)
		cells := []ui.Node{html.H3(html.Props{ID: laneID, Class: "projectui-lane-head"}, toggle)}
		for columnIndex, column := range model.Columns {
			cards := make([]ui.Node, 0)
			for i, card := range model.Cards {
				if inColumn(card, column) && card.LaneID == lane.ID {
					cards = append(cards, renderCard(model, card, i, statusTone))
				}
			}
			laneRef := lane
			cellProps := html.Props{Class: "projectui-cell", Data: dropData(column, &laneRef)}
			cellProps.Data["column-id"], cellProps.Data["tone"] = column.ID, tones[columnIndex]
			if columnIndex == 0 {
				cellProps.ID = cellsID
			}
			// A collapsed lane keeps one slim count per cell, aligned under
			// its column, so the band still reads across the board.
			tally := html.Span(html.Props{Class: "projectui-cell-tally", Aria: map[string]string{"hidden": "true"}, Data: map[string]string{"empty": boolString(len(cards) == 0)}, Text: model.Copy.FormatNumber(len(cards))})
			if len(cards) == 0 {
				note := model.Copy.LaneEmptyHere
				if model.Filters.Active() {
					note = model.Copy.Board.NoMatching
				}
				cells = append(cells, html.Div(cellProps, tally, html.P(html.Props{Class: "projectui-empty projectui-empty-cell"}, html.Span(html.Props{Class: "projectui-sr", Text: model.Copy.NoTasksLane}), html.Span(html.Props{Class: "projectui-empty-cell-note", Aria: map[string]string{"hidden": "true"}, Text: note}))))
				continue
			}
			cellProps.Role = "list"
			cellProps.Aria = map[string]string{"label": column.Label + ", " + lane.Label}
			cells = append(cells, html.Div(cellProps, append([]ui.Node{tally}, cards...)...))
		}
		children = append(children, html.Section(html.Props{Class: "projectui-lane", Data: map[string]string{"lane-id": lane.ID, "collapsed": boolString(lane.Collapsed)}, Aria: map[string]string{"labelledby": laneID}}, cells...))
	}
	return html.Div(html.Props{Class: "projectui-lanes-frame"}, html.Div(regionProps, children...))
}

// laneToggle is one button that collapses every lane while any is open and
// expands them all once every lane is closed. The host flips its label and
// action after each toggle without a re-render.
func laneToggle(model Model) ui.Node {
	anyOpen := false
	for _, lane := range model.Lanes {
		anyOpen = anyOpen || !lane.Collapsed
	}
	action, label, dir := "collapse-lanes", model.Copy.CollapseAll, "collapse"
	if !anyOpen {
		action, label, dir = "expand-lanes", model.Copy.ExpandAll, "expand"
	}
	return html.Button(html.Props{Type: "button", Class: "projectui-lane-tool", Data: map[string]string{"projectui-action": action, "label-collapse": model.Copy.CollapseAll, "label-expand": model.Copy.ExpandAll}},
		html.Span(html.Props{Class: "projectui-lane-tool-icon", Data: map[string]string{"dir": dir}, Aria: map[string]string{"hidden": "true"}}),
		html.Span(html.Props{Class: "projectui-lane-tool-label", Text: label}),
	)
}

// LaneStorageKey names the per-viewer lane collapse memory for one board
// view. It is presentation state only and never leaves the browser.
func LaneStorageKey(projectID, viewID string) string {
	if projectID == "" {
		return ""
	}
	if viewID == "" {
		viewID = "default"
	}
	return "hcm.projectui.lanes." + projectID + "." + viewID
}

// dropData marks a column body or lane cell as a drop zone: the status a
// dropped card moves to (the column's first status), every status the column
// shows, and, in a lane, the lane value and whether it accepts moves.
func dropData(column Column, lane *Lane) map[string]string {
	data := map[string]string{}
	if len(column.Statuses) > 0 {
		data["drop-status"] = column.Statuses[0].ID
		ids := make([]string, 0, len(column.Statuses))
		for _, status := range column.Statuses {
			ids = append(ids, status.ID)
		}
		data["drop-statuses"] = strings.Join(ids, " ")
	}
	if lane != nil {
		data["drop-lane"] = lane.ID
		data["lane-editable"] = boolString(lane.MayEdit)
	}
	return data
}

func columnHead(label, id, tone string, count, total int, copy Copy) ui.Node {
	badge := countBadge(count, copy)
	if total != count {
		text := copy.Board.CountOf(count, total)
		badge = html.Span(html.Props{Class: "projectui-count projectui-count-filtered"}, html.Span(html.Props{Text: text}))
	}
	return html.Header(html.Props{Class: "projectui-column-head"},
		statusGlyph(tone),
		html.H2(html.Props{ID: id, Class: "projectui-column-title"}, html.Span(html.Props{Class: "projectui-column-label", Text: label}), badge),
	)
}

// columnTotal is the unfiltered card count for a column, or shown when
// no filter is active.
func (model Model) columnTotal(columnID string, shown int) int {
	if model.columnTotals == nil {
		return shown
	}
	return model.columnTotals[columnID]
}

func countBadge(count int, copy Copy) ui.Node {
	return html.Span(html.Props{Class: "projectui-count"},
		html.Span(html.Props{Aria: map[string]string{"hidden": "true"}, Text: copy.FormatNumber(count)}),
		html.Span(html.Props{Class: "projectui-sr", Text: ", " + copy.ColumnCount(count)}),
	)
}

func emptyColumn(text, tone string) ui.Node {
	return html.Div(html.Props{Class: "projectui-empty projectui-empty-column", Data: map[string]string{"tone": tone}},
		statusGlyph(tone),
		html.P(html.Props{Text: text}),
	)
}

func statusGlyph(tone string) ui.Node {
	return html.Span(html.Props{Class: "projectui-status-glyph", Data: map[string]string{"tone": tone}, Aria: map[string]string{"hidden": "true"}})
}

func renderList(model Model) ui.Node {
	statusTone := statusTones(model.Columns, columnTones(model.Columns))
	cards := sortedCards(model)
	var footer ui.Node
	if pager := model.ListPager; pager != nil {
		if from, to := pager.From-1, pager.To; from >= 0 && to <= len(cards) && from <= to {
			cards = cards[from:to]
		}
		footer = PagerFooter(*pager, model.Copy, nil, nil)
	}
	items := make([]ui.Node, 0, len(cards))
	for i, card := range cards {
		items = append(items, renderCard(model, card, i, statusTone))
	}
	if len(items) == 0 {
		return html.Div(html.Props{Class: "projectui-list-items"}, html.P(html.Props{Class: "projectui-empty projectui-empty-column", Text: model.Copy.NoTasksPage}))
	}
	// With no explicit sort the list is in workflow (status) order, and
	// the header says so.
	listSort := model.ListSort
	if listSort == "" {
		listSort = "status"
	}
	headCell := func(key, label string) ui.Node {
		href := model.SortHrefs[key]
		if href == "" || !safeHref(href) {
			return html.Span(html.Props{Aria: map[string]string{"hidden": "true"}, Text: label})
		}
		active, descending := strings.TrimPrefix(listSort, "-") == key, strings.HasPrefix(listSort, "-")
		children := []ui.Node{html.Span(html.Props{Text: label})}
		data := map[string]string{"active": boolString(active)}
		if active {
			arrow, state := "\u2191", model.Copy.SortedAscending
			if descending {
				arrow, state = "\u2193", model.Copy.SortedDescending
			}
			children = append(children, html.Span(html.Props{Class: "projectui-sort-arrow", Aria: map[string]string{"hidden": "true"}, Text: arrow}), html.Span(html.Props{Class: "projectui-sr", Text: ", " + state}))
		} else {
			children = append(children, html.Span(html.Props{Class: "projectui-sort-arrow projectui-sort-idle", Aria: map[string]string{"hidden": "true"}, Text: "\u2195"}))
		}
		return html.A(html.Props{Href: href, Class: "projectui-sort", Data: data}, children...)
	}
	head := html.Div(html.Props{Class: "projectui-list-head"},
		headCell("task", model.Copy.TaskField),
		headCell("status", model.Copy.StatusField),
		headCell("priority", model.Copy.PriorityField),
		headCell("assignee", model.Copy.AssigneeField),
		headCell("due", model.Copy.DueField),
	)
	frame := []ui.Node{head, html.Div(html.Props{Class: "projectui-list-items", Role: "list", Aria: map[string]string{"label": model.Copy.TasksLabel}}, items...)}
	if footer != nil {
		frame = append(frame, footer)
	}
	return html.Div(html.Props{Class: "projectui-list-frame"}, frame...)
}

// sortedCards orders the page's cards by the list sort: status by workflow
// column order, priority by level, due and assignee with the empty values
// last in either direction, and the title as the tie-break.
func sortedCards(model Model) []Card {
	listSort := model.ListSort
	if listSort == "" && len(model.SortHrefs) > 0 {
		listSort = "status"
	}
	key := strings.TrimPrefix(listSort, "-")
	if key == "" {
		return model.Cards
	}
	descending := strings.HasPrefix(listSort, "-")
	statusRank := map[string]int{}
	for index, column := range model.Columns {
		for _, status := range column.Statuses {
			if _, seen := statusRank[status.ID]; !seen {
				statusRank[status.ID] = index
			}
		}
	}
	priorityRank := map[string]int{"low": 1, "normal": 2, "high": 3, "urgent": 4}
	cards := append([]Card(nil), model.Cards...)
	sort.SliceStable(cards, func(i, j int) bool {
		a, b := cards[i], cards[j]
		cmp := 0
		emptyA, emptyB := false, false
		switch key {
		case "status":
			cmp = statusRank[a.StatusID] - statusRank[b.StatusID]
		case "priority":
			cmp = priorityRank[cardPriorityLevel(a)] - priorityRank[cardPriorityLevel(b)]
		case "assignee":
			emptyA, emptyB = a.Assignee == "", b.Assignee == ""
			cmp = strings.Compare(strings.ToLower(a.Assignee), strings.ToLower(b.Assignee))
		case "due":
			emptyA, emptyB = a.DueDate == "", b.DueDate == ""
			cmp = strings.Compare(a.DueDate, b.DueDate)
		}
		if emptyA != emptyB {
			return emptyB
		}
		if cmp == 0 {
			cmp = strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
			if key != "task" {
				return cmp < 0
			}
		}
		if descending {
			return cmp > 0
		}
		return cmp < 0
	})
	return cards
}

func cardPriorityLevel(card Card) string {
	if level := priorityLevel(card.PriorityID); level != "unknown" {
		return level
	}
	return priorityLevel(card.Priority)
}

func renderCard(model Model, card Card, index int, statusTone map[string]string) ui.Node {
	cardID := fmt.Sprintf("projectui-card-%d", index)
	tone := statusTone[card.StatusID]
	if tone == "" {
		tone = classifyTone(card.StatusID + " " + card.StatusLabel)
	}
	if tone == "" {
		tone = "todo"
	}
	main := []ui.Node{}
	if card.DetailHref != "" {
		titleProps := html.Props{Href: card.DetailHref, Class: "projectui-card-title", Dir: "auto", Text: card.Title, Raw: map[string]any{"draggable": "false"}}
		if card.Selected {
			titleProps.Aria = map[string]string{"current": "true"}
		}
		main = append(main, html.A(titleProps))
	} else {
		main = append(main, html.H3(html.Props{Class: "projectui-card-title", Dir: "auto", Text: card.Title}))
	}
	if card.Summary != "" {
		main = append(main, html.P(html.Props{Class: "projectui-card-summary", Dir: "auto", Text: card.Summary}))
	}
	content := []ui.Node{html.Div(html.Props{Class: "projectui-card-main"}, main...)}

	meta := []ui.Node{}
	if card.Priority != "" || card.PriorityID != "" {
		meta = append(meta, priorityIndicatorFor(card.PriorityID, card.Priority, model.Copy))
	}
	if card.DueDate != "" {
		// A finished task is never overdue: tone only open work.
		meta = append(meta, dueChip(card.DueDate, model.Today, model.Copy, tone != "done" && tone != "cancelled"))
	}
	if card.Type != "" {
		meta = append(meta, html.Span(html.Props{Class: "projectui-chip projectui-type", Title: model.Copy.TypeField, Text: card.Type}))
	}
	if chip := workflowChip(card.Workflows); chip != nil {
		meta = append(meta, chip)
	}
	if len(card.Labels) > 0 || card.StoryPoints > 0 {
		meta = append(meta, labelChips(card.Labels, card.StoryPoints, model.Copy, 2))
	}
	if card.Assignee != "" {
		meta = append(meta, assigneeBadge(card.Assignee, card.AssigneePhoto, model.Copy))
	} else if model.Mode == ViewList {
		meta = append(meta, html.Span(html.Props{Class: "projectui-assignee projectui-assignee-none"}, html.Span(html.Props{Class: "projectui-avatar projectui-avatar-empty", Aria: map[string]string{"hidden": "true"}}), html.Span(html.Props{Class: "projectui-assignee-name", Text: model.Copy.Unassigned})))
	}
	if card.DueDate == "" && model.Mode == ViewList {
		meta = append(meta, html.Span(html.Props{Class: "projectui-chip projectui-due projectui-due-none", Aria: map[string]string{"label": model.Copy.DueField + ": " + model.Copy.NoDate}, Text: "\u2014"}))
	}
	if len(meta) > 0 {
		content = append(content, html.Div(html.Props{Class: "projectui-card-meta-row"}, meta...))
	}
	conflict := card.Conflict
	if conflict == "" && card.ConflictState {
		conflict = model.Copy.Conflict
	}
	controls := []ui.Node{}
	if card.CanMoveStatus {
		controls = append(controls, moveStatus(card, cardID, tone, conflict != "", card.Pending, model.Copy))
	} else if model.Mode == ViewList {
		statusLabel := card.StatusLabel
		for _, status := range card.StatusOptions {
			if status.ID == card.StatusID {
				statusLabel = status.Label
				break
			}
		}
		if statusLabel != "" {
			controls = append(controls, lockedStatus(cardID+"-lock", tone, statusLabel, model.Copy)...)
		}
	}
	if card.CanMoveLane && len(model.Lanes) > 0 {
		controls = append(controls, moveLane(model.Lanes, card, cardID, conflict != "", card.Pending, model.Copy))
	}
	if len(controls) > 0 || model.Mode != ViewList && card.DetailHref != "" {
		if model.Mode == ViewList {
			content = append(content, html.Div(html.Props{Class: "projectui-card-controls"}, controls...))
		} else {
			// On the board the move controls sit behind a compact menu: the
			// keyboard, screen-reader and touch path for what a pointer does
			// by dragging. A conflict opens it so focus can return to the
			// control whose change failed.
			panel := []ui.Node{html.P(html.Props{Class: "projectui-menu-title", Dir: "auto", Text: card.Title})}
			menuItem := map[string]any{"draggable": "false", "role": "menuitem"}
			if card.DetailHref != "" {
				panel = append(panel, html.A(html.Props{Href: card.DetailHref, Class: "projectui-menu-item", Raw: menuItem}, html.Span(html.Props{Class: "projectui-menu-icon", Data: map[string]string{"icon": "open"}, Aria: map[string]string{"hidden": "true"}}), html.Span(html.Props{Text: model.Copy.OpenTask})))
			}
			panel = append(panel, menuMoveGroups(model, card, cardID, tone, statusTone, conflict != "")...)
			if card.DetailHref != "" {
				panel = append(panel,
					html.A(html.Props{Href: card.DetailHref, Class: "projectui-menu-item", Raw: menuItem}, html.Span(html.Props{Class: "projectui-menu-icon", Data: map[string]string{"icon": "assign"}, Aria: map[string]string{"hidden": "true"}}), html.Span(html.Props{Text: model.Copy.AssignTo})),
				)
				panel = append(panel, html.Div(html.Props{Class: "projectui-menu-group projectui-menu-share"}, shareItems(model.Copy, card.DetailHref, card.Title)...))
			}
			content = append(content, html.Details(html.Props{Class: "projectui-card-menu", Open: conflict != ""},
				html.Summary(html.Props{Class: "projectui-card-menu-trigger", Aria: map[string]string{"label": model.Copy.MoreActions + ": " + card.Title, "haspopup": "menu"}}, html.Span(html.Props{Class: "projectui-card-menu-dots", Aria: map[string]string{"hidden": "true"}})),
				html.Div(html.Props{Class: "projectui-card-menu-panel", Role: "menu", Aria: map[string]string{"label": model.Copy.MoreActions + ": " + card.Title}}, panel...),
			))
		}
	}
	if card.Pending {
		content = append(content, html.P(html.Props{Class: "projectui-pending", Role: "status", Text: model.PendingText}))
	}
	if conflict != "" {
		content = append(content, html.P(html.Props{ID: cardID + "-conflict", Class: "projectui-conflict", Role: "alert", TabIndex: -1, Data: map[string]string{"focus-restore": focusTarget(cardID, card)}, Text: conflict}))
	}
	props := html.Props{Class: "projectui-card", Role: "listitem", Data: map[string]string{"task-id": card.ID, "task-revision": strconv.FormatUint(card.TaskRevision, 10), "workflow-revision": strconv.FormatUint(card.WorkflowRevision, 10), "tone": tone}, Aria: map[string]string{"busy": boolString(card.Pending)}}
	switch {
	case conflict != "":
		props.Data["state"] = "conflict"
	case card.Pending:
		props.Data["state"] = "pending"
	}
	if conflict != "" {
		props.Data["focus-restore"] = focusTarget(cardID, card)
	}
	if card.Selected {
		props.Data["selected"] = "true"
	}
	props.Data["status-id"] = card.StatusID
	if card.LaneID != "" {
		props.Data["lane-id"] = card.LaneID
	}
	if model.Mode != ViewList && (card.CanMoveStatus || card.CanMoveLane) && !card.Pending && conflict == "" {
		targets := []string{card.StatusID}
		if card.CanMoveStatus {
			for _, status := range card.StatusOptions {
				if status.ID != card.StatusID {
					targets = append(targets, status.ID)
				}
			}
		}
		props.Data["move-targets"] = strings.Join(targets, " ")
		props.Data["lane-movable"] = boolString(card.CanMoveLane)
		props.Raw = map[string]any{"draggable": "true"}
	}
	return html.Article(props, html.Div(html.Props{ID: cardID, Class: "projectui-card-content"}, content...))
}

// priorityIndicator draws a three-bar signal whose fill and tint encode the
// level; the level text stays available to assistive technology.
func priorityIndicator(value string, copy Copy) ui.Node {
	return priorityIndicatorFor("", value, copy)
}

// priorityIndicatorFor derives the level from the stable priority ID
// (TASK_PRIORITY_HIGH), never from the label, which is translated.
func priorityIndicatorFor(id, value string, copy Copy) ui.Node {
	level := priorityLevel(id)
	if id == "" {
		level = priorityLevel(value)
	}
	if value == "" {
		value = id
	}
	return html.Span(html.Props{Class: "projectui-priority", Title: value, Data: map[string]string{"level": level}},
		html.Span(html.Props{Class: "projectui-priority-bars", Aria: map[string]string{"hidden": "true"}}, html.Span(html.Props{}), html.Span(html.Props{}), html.Span(html.Props{})),
		html.Span(html.Props{Class: "projectui-priority-text"}, html.Span(html.Props{Class: "projectui-sr", Text: copy.PriorityField + ": "}), html.Span(html.Props{Text: value})),
	)
}

func priorityLevel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "urgent", "critical", "highest", "p0", "task_priority_urgent":
		return "urgent"
	case "high", "p1", "task_priority_high":
		return "high"
	case "normal", "medium", "p2", "task_priority_normal":
		return "normal"
	case "low", "lowest", "p3", "task_priority_low":
		return "low"
	default:
		return "unknown"
	}
}

// dueChip parses YYYY-MM-DD (tolerating a trailing time) and tones the chip
// overdue, today or soon (within three days) against the model's today.
func dueChip(raw string, today time.Time, copy Copy, open bool) ui.Node {
	trimmed := strings.TrimSpace(raw)
	date, err := time.Parse("2006-01-02", firstN(trimmed, 10))
	if err != nil {
		return html.Span(html.Props{Class: "projectui-chip projectui-due"}, html.Span(html.Props{Class: "projectui-sr", Text: copy.DueField + ": "}), html.Span(html.Props{Text: trimmed}))
	}
	civilToday := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	days := int(date.Sub(civilToday).Hours() / 24)
	tone, prefix := "", copy.DueField
	text := copy.FormatDate(date)
	switch {
	case !open:
	case days < 0:
		tone, prefix = "overdue", copy.Overdue
	case days == 0:
		tone, text = "today", copy.DueToday
	case days <= 3:
		tone, prefix = "soon", copy.DueSoon
	}
	props := html.Props{Class: "projectui-chip projectui-due", Title: copy.DueField + ": " + date.Format("2006-01-02")}
	if tone != "" {
		props.Data = map[string]string{"tone": tone}
	}
	return html.Span(props,
		html.Span(html.Props{Class: "projectui-due-icon", Data: map[string]string{"tone": tone}, Aria: map[string]string{"hidden": "true"}}),
		html.Span(html.Props{Class: "projectui-sr", Text: prefix + ": "}),
		html.Tag("time", html.Props{Raw: map[string]any{"dateTime": date.Format("2006-01-02")}, Text: text}),
	)
}

func assigneeBadge(name, photo string, copy Copy) ui.Node {
	return html.Span(html.Props{Class: "projectui-assignee", Title: copy.AssigneeField + ": " + name},
		avatarWithPhoto(name, photo),
		html.Span(html.Props{Class: "projectui-assignee-name"}, html.Span(html.Props{Class: "projectui-sr", Text: copy.AssigneeField + ": "}), html.Span(html.Props{Text: name})),
	)
}

// avatar renders initials on a tint chosen by a stable hash of the name, so a
// person keeps one colour across cards, lanes and sessions.
func avatar(name string) ui.Node { return avatarWithPhoto(name, "") }

// avatarWithPhoto layers a same-origin portrait over the initials; the
// initials stay underneath so a slow or missing image never leaves a hole.
func avatarWithPhoto(name, photo string) ui.Node {
	props := html.Props{Class: "projectui-avatar", Data: map[string]string{"hue": strconv.Itoa(hueBucket(name))}, Aria: map[string]string{"hidden": "true"}}
	if photo == "" || !safeHref(photo) {
		props.Text = initials(name)
		return html.Span(props)
	}
	return html.Span(props, html.Span(html.Props{Text: initials(name)}), html.Img(html.Props{Src: photo, Class: "projectui-avatar-photo", Loading: "lazy", Raw: map[string]any{"alt": ""}}))
}

func hueBucket(name string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(name))))
	return int(h.Sum32() % 12)
}

func initials(name string) string {
	fields := strings.FieldsFunc(name, func(r rune) bool { return unicode.IsSpace(r) || r == '-' || r == '_' || r == '.' })
	letters := make([]rune, 0, 2)
	for _, field := range fields {
		r, _ := utf8.DecodeRuneInString(field)
		if unicode.IsLetter(r) {
			letters = append(letters, unicode.ToUpper(r))
		}
	}
	switch len(letters) {
	case 0:
		return "?"
	case 1:
		return string(letters)
	default:
		return string([]rune{letters[0], letters[len(letters)-1]})
	}
}

func firstN(value string, n int) string {
	if len(value) <= n {
		return value
	}
	return value[:n]
}

func focusTarget(id string, card Card) string {
	if card.FailedAction == "lane" && card.CanMoveLane || !card.CanMoveStatus && card.CanMoveLane {
		return id + "-lane"
	}
	return id + "-status"
}

func moveStatus(card Card, id, tone string, failed, pending bool, copy Copy) ui.Node {
	options := make([]ui.Node, 0)
	for _, status := range card.StatusOptions {
		options = append(options, html.Option(html.Props{Value: status.ID, Selected: status.ID == card.StatusID, Text: status.Label}))
	}
	if len(options) == 0 {
		return html.Span(html.Props{Class: "projectui-status-readonly"}, statusGlyph(tone), html.Span(html.Props{Text: card.StatusLabel}))
	}
	props := html.Props{ID: id + "-status", Name: "status", Class: "projectui-status-menu", Data: map[string]string{"projectui-action": "move-status", "task-id": card.ID, "task-revision": strconv.FormatUint(card.TaskRevision, 10), "workflow-revision": strconv.FormatUint(card.WorkflowRevision, 10)}, Aria: map[string]string{"label": copy.StatusFor + card.Title}}
	if failed {
		props.Aria["describedby"] = id + "-conflict"
	}
	props.Disabled = pending
	return html.Label(html.Props{Class: "projectui-control projectui-control-status"}, html.Span(html.Props{Class: "projectui-sr", Text: copy.Status}), statusGlyph(tone), html.Select(props, options...))
}

// lockedStatus is the read-only status with its reason: a lock glyph, a
// tooltip, and the same sentence for assistive technology.
func lockedStatus(reasonID, tone, label string, copy Copy) []ui.Node {
	reason := copy.StatusReadOnly
	if tone == "done" || tone == "cancelled" {
		reason = copy.StatusLocked
	}
	caption := copy.StatusReadOnly
	if tone == "done" || tone == "cancelled" {
		caption = copy.StatusLockedShort
	}
	return []ui.Node{
		html.Span(html.Props{Class: "projectui-status-readonly", Title: reason, Data: map[string]string{"tone": tone, "locked": "true"}, Aria: map[string]string{"describedby": reasonID}},
			statusGlyph(tone), html.Span(html.Props{Text: label}), html.Span(html.Props{Class: "projectui-lock-icon", Aria: map[string]string{"hidden": "true"}})),
		html.Span(html.Props{ID: reasonID, Class: "projectui-sr", Text: reason}),
		// The visible caption is one line; the full sentence is the tooltip
		// and the description above.
		html.Span(html.Props{Class: "projectui-lock-caption", Title: reason, Aria: map[string]string{"hidden": "true"}, Text: caption}),
	}
}

// menuMoveGroups renders the card menu's move targets as menuitemradio rows
// (one group for status, one for the lane), each carrying the same identity
// and revisions the list view's selects carry, so one write path serves both.
func menuMoveGroups(model Model, card Card, cardID, tone string, statusTone map[string]string, failed bool) []ui.Node {
	copy := model.Copy
	identity := func(action, value string) map[string]string {
		return map[string]string{"projectui-action": action, "value": value, "task-id": card.ID, "task-revision": strconv.FormatUint(card.TaskRevision, 10), "workflow-revision": strconv.FormatUint(card.WorkflowRevision, 10)}
	}
	check := html.Span(html.Props{Class: "projectui-menu-check", Aria: map[string]string{"hidden": "true"}})
	groups := []ui.Node{}
	group := func(id, label string, rows []ui.Node) {
		props := html.Props{ID: id, Class: "projectui-menu-group", Role: "group", Aria: map[string]string{"label": label}}
		if failed {
			props.Aria["describedby"] = cardID + "-conflict"
		}
		groups = append(groups, html.Div(props, append([]ui.Node{html.Span(html.Props{Class: "projectui-menu-label", Aria: map[string]string{"hidden": "true"}, Text: copy.MoveTo})}, rows...)...))
	}
	if card.CanMoveStatus && len(card.StatusOptions) > 0 {
		// Every workflow status is listed; the ones the workflow does not
		// offer from here stay visible, disabled, with the reason, so the
		// menu never looks as if a column went missing.
		allowed := map[string]bool{}
		for _, status := range card.StatusOptions {
			allowed[status.ID] = true
		}
		all := append([]Status(nil), card.StatusOptions...)
		for _, column := range model.Columns {
			for _, status := range column.Statuses {
				if status.ID != "" && !allowed[status.ID] {
					all = append(all, status)
					allowed[status.ID] = false
				}
			}
		}
		order := map[string]int{}
		for index, column := range model.Columns {
			for _, status := range column.Statuses {
				if _, seen := order[status.ID]; !seen {
					order[status.ID] = index
				}
			}
		}
		sort.SliceStable(all, func(i, j int) bool { return order[all[i].ID] < order[all[j].ID] })
		rows := make([]ui.Node, 0, len(all))
		for _, status := range all {
			optionTone := statusTone[status.ID]
			if optionTone == "" {
				optionTone = classifyTone(status.ID + " " + status.Label)
			}
			if optionTone == "" {
				optionTone = "todo"
			}
			current := status.ID == card.StatusID
			label := []ui.Node{statusGlyph(optionTone), html.Span(html.Props{Class: "projectui-menu-item-label", Text: status.Label})}
			props := html.Props{Type: "button", Class: "projectui-menu-item projectui-menu-radio", Role: "menuitemradio", Disabled: card.Pending, Data: identity("move-status", status.ID), Aria: map[string]string{"checked": boolString(current)}}
			if !allowed[status.ID] {
				props.Aria["disabled"] = "true"
				props.Title = copy.MoveNotAllowed
				label = append(label, html.Span(html.Props{Class: "projectui-menu-reason", Text: copy.MoveNotAllowed}))
			} else {
				label = append(label, check)
			}
			rows = append(rows, html.Button(props, label...))
		}
		group(cardID+"-status", copy.StatusFor+card.Title, rows)
	} else if label := card.StatusLabel; label != "" {
		groups = append(groups, html.Div(html.Props{Class: "projectui-menu-group projectui-menu-locked"}, lockedStatus(cardID+"-lock", tone, label, copy)...))
	}
	if card.CanMoveLane && len(model.Lanes) > 0 {
		rows := []ui.Node{}
		for _, lane := range model.Lanes {
			if !lane.MayEdit {
				continue
			}
			rows = append(rows, html.Button(html.Props{Type: "button", Class: "projectui-menu-item projectui-menu-radio", Role: "menuitemradio", Disabled: card.Pending, Data: identity("move-lane", lane.ID), Aria: map[string]string{"checked": boolString(lane.ID == card.LaneID)}},
				html.Span(html.Props{Class: "projectui-lane-glyph", Aria: map[string]string{"hidden": "true"}}), html.Span(html.Props{Class: "projectui-menu-item-label", Text: lane.Label}), check))
		}
		if len(rows) > 0 {
			group(cardID+"-lane", copy.LaneFor+card.Title, rows)
		}
	}
	return groups
}

func moveLane(lanes []Lane, card Card, id string, failed, pending bool, copy Copy) ui.Node {
	options := make([]ui.Node, 0, len(lanes))
	for _, lane := range lanes {
		if !lane.MayEdit {
			continue
		}
		options = append(options, html.Option(html.Props{Value: lane.ID, Selected: lane.ID == card.LaneID, Text: lane.Label}))
	}
	if len(options) == 0 {
		return html.Span(html.Props{Class: "projectui-readonly-lane", Text: card.LaneLabel})
	}
	props := html.Props{ID: id + "-lane", Name: "lane", Class: "projectui-lane-menu", Data: map[string]string{"projectui-action": "move-lane", "task-id": card.ID, "task-revision": strconv.FormatUint(card.TaskRevision, 10), "workflow-revision": strconv.FormatUint(card.WorkflowRevision, 10)}, Aria: map[string]string{"label": copy.LaneFor + card.Title}}
	if failed {
		props.Aria["describedby"] = id + "-conflict"
	}
	props.Disabled = pending
	return html.Label(html.Props{Class: "projectui-control projectui-control-lane"}, html.Span(html.Props{Class: "projectui-sr", Text: copy.Lane}), html.Span(html.Props{Class: "projectui-lane-glyph", Aria: map[string]string{"hidden": "true"}}), html.Select(props, options...))
}

// columnTones resolves each column's workflow category: an explicit Tone,
// else keywords in its stable IDs, else its position in the flow.
func columnTones(columns []Column) []string {
	tones := make([]string, len(columns))
	for i, column := range columns {
		tone := strings.TrimSpace(column.Tone)
		if tone == "" {
			tone = classifyTone(column.ID)
		}
		for _, status := range column.Statuses {
			if tone != "" {
				break
			}
			tone = classifyTone(status.ID)
		}
		if tone == "" {
			tone = classifyTone(column.Label)
		}
		if tone == "" {
			switch {
			case i == 0:
				tone = "todo"
			case i == len(columns)-1 && len(columns) > 2:
				tone = "done"
			default:
				tone = "active"
			}
		}
		tones[i] = tone
	}
	return tones
}

func statusTones(columns []Column, tones []string) map[string]string {
	out := map[string]string{}
	for i, column := range columns {
		for _, status := range column.Statuses {
			tone := classifyTone(status.ID)
			if tone == "" || strings.TrimSpace(column.Tone) != "" {
				tone = tones[i]
			}
			out[status.ID] = tone
		}
	}
	return out
}

func classifyTone(text string) string {
	value := strings.ToLower(text)
	has := func(words ...string) bool {
		for _, word := range words {
			if strings.Contains(value, word) {
				return true
			}
		}
		return false
	}
	switch {
	case has("cancel", "wontfix", "won't", "declin", "abandon", "reject"):
		return "cancelled"
	case has("done", "complete", "closed", "resolved", "shipped", "finished", "released"):
		return "done"
	case has("block", "hold", "stuck", "waiting"):
		return "blocked"
	case has("review", "qa", "verif", "approv", "testing"):
		return "review"
	case has("progress", "doing", "active", "wip", "started", "working"):
		return "active"
	case has("backlog", "icebox", "later", "someday", "triage"):
		return "planned"
	case has("todo", "to do", "to_do", "to-do", "open", "ready", "new", "queued"):
		return "todo"
	}
	return ""
}

func pagination(page Page, copy Copy) ui.Node {
	children := []ui.Node{}
	if page.HasPrevious && page.PreviousHref != "" {
		children = append(children, html.A(html.Props{Href: page.PreviousHref, Class: "projectui-page-link", Text: copy.PreviousPage}))
	}
	if page.Total > 0 {
		children = append(children, html.Span(html.Props{Class: "projectui-page-summary", Text: copy.PageSummary(max(1, page.Number), page.Total)}))
	}
	if page.HasNext && page.NextHref != "" {
		children = append(children, html.A(html.Props{Href: page.NextHref, Class: "projectui-page-link", Text: copy.NextPage}))
	}
	if page.MoreInLane {
		children = append(children, html.Span(html.Props{Class: "projectui-more-in-lane", Role: "status", Text: copy.MoreInLane}))
	}
	return html.Nav(html.Props{Class: "projectui-pagination", Aria: map[string]string{"label": copy.TaskPages}}, children...)
}

func references(items []Reference, copy Copy) ui.Node {
	if len(items) == 0 {
		return html.P(html.Props{Class: "projectui-empty", Text: copy.NoLinkedItems})
	}
	children := make([]ui.Node, 0, len(items))
	for _, ref := range items {
		kind := strings.TrimSpace(ref.Kind)
		glyph := html.Span(html.Props{Class: "projectui-reference-kind", Data: map[string]string{"kind": strings.ToLower(kind)}, Aria: map[string]string{"hidden": "true"}})
		if ref.State == ReferenceReady && ref.Title != "" && safeHref(ref.Href) {
			children = append(children, html.Li(html.Props{Class: "projectui-reference"}, glyph, html.A(html.Props{Href: ref.Href, Text: ref.Title})))
			continue
		}
		stateText := copy.Unavailable
		switch ref.State {
		case ReferenceLoading:
			stateText = copy.Loading
		case ReferenceRestricted:
			stateText = copy.Restricted
		case ReferenceUnavailable:
			stateText = copy.Unavailable
		}
		label := copy.LinkedItem
		if kind == "Chat" || kind == "Docs" {
			if kind == "Chat" {
				label = copy.LinkedChatItem
			} else {
				label = copy.LinkedDocsItem
			}
		}
		children = append(children, html.Li(html.Props{Class: "projectui-reference projectui-reference-neutral"}, glyph, html.Span(html.Props{Text: label}), html.Span(html.Props{Class: "projectui-reference-state", Text: stateText})))
	}
	return html.Ul(html.Props{Class: "projectui-references"}, children...)
}

func commentList(items []Comment, copy Copy) ui.Node {
	children := make([]ui.Node, 0, len(items))
	for _, comment := range items {
		byline := []ui.Node{html.Strong(html.Props{Text: comment.Author})}
		if comment.Time != "" {
			byline = append(byline, html.Time(html.Props{Text: comment.Time}))
		}
		children = append(children, html.Li(html.Props{Class: "projectui-comment"},
			avatar(comment.Author),
			html.Div(html.Props{Class: "projectui-comment-body"}, html.Div(html.Props{Class: "projectui-comment-byline"}, byline...), html.P(html.Props{Text: comment.Body})),
		))
	}
	if len(children) == 0 {
		children = append(children, html.Li(html.Props{Class: "projectui-empty", Text: copy.NoComments}))
	}
	return html.Ul(html.Props{Class: "projectui-comments"}, children...)
}

func activityList(items []Activity, copy Copy) ui.Node {
	children := make([]ui.Node, 0, len(items))
	for _, activity := range items {
		children = append(children, html.Li(html.Props{Class: "projectui-activity-item"}, html.Span(html.Props{Text: activity.Label}), html.Time(html.Props{Text: activity.Time})))
	}
	if len(children) == 0 {
		children = append(children, html.Li(html.Props{Class: "projectui-empty", Text: copy.NoActivity}))
	}
	return html.Ul(html.Props{Class: "projectui-activity"}, children...)
}

func detailFacts(model DetailModel) []ui.Node {
	due := strings.TrimSpace(model.DueDate)
	if date, err := time.Parse("2006-01-02", firstN(due, 10)); err == nil {
		due = model.Copy.FormatDate(date)
	}
	facts := []Fact{{Label: model.Copy.StatusField, Value: model.Status}, {Label: model.Copy.AssigneeField, Value: model.Assignee}, {Label: model.Copy.DueField, Value: due}}
	facts = append(facts, model.Fields...)
	out := make([]ui.Node, 0, len(facts))
	for _, fact := range facts {
		if fact.Label == "" || fact.Value == "" {
			continue
		}
		out = append(out, html.Div(html.Props{Class: "projectui-fact"}, html.Tag("dt", html.Props{Text: fact.Label}), html.Tag("dd", html.Props{Text: fact.Value})))
	}
	return out
}

func inColumn(card Card, column Column) bool {
	if len(column.Statuses) == 0 {
		return card.ColumnID == column.ID
	}
	for _, status := range column.Statuses {
		if status.ID == card.StatusID {
			return true
		}
	}
	return false
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func localizedCopy(copy Copy) Copy {
	defaults := Copy{
		Pending: "Saving change…", Conflict: "This task changed elsewhere. Your change was not applied.",
		Status: "Status", Lane: "Lane", StatusFor: "Status for ", LaneFor: "Lane for ",
		NoTasksLane: "No tasks in this lane", NoTasksColumn: "No tasks in this column", NoTasksPage: "No tasks on this page",
		PreviousPage: "Previous page", NextPage: "Next page", TaskPages: "Task pages", MoreInLane: "More authorized tasks may be in a lane on another page.",
		TaskDetails: "Task details", LinkedItems: "Linked conversations and documents", NoLinkedItems: "No linked conversations or documents",
		Comments: "Comments", NoComments: "No comments", MoreComments: "More comments",
		Activity: "Activity", NoActivity: "No activity", MoreActivity: "More activity",
		Loading: "Loading", Restricted: "Access restricted", Unavailable: "Unavailable",
		LinkedItem: "Linked item", LinkedChatItem: "Linked Chat item", LinkedDocsItem: "Linked Docs item",
		BoardLabel: "Project board", TasksLabel: "Project tasks",
		StatusField: "Status", AssigneeField: "Assignee", DueField: "Due",
		PriorityField: "Priority", TypeField: "Type", TaskField: "Task",
		DueToday: "Today", Overdue: "Overdue", DueSoon: "Due soon",
		CollapseAll: "Collapse all", ExpandAll: "Expand all", ToggleLane: "Show or hide lane", MoveTask: "Move",
		LaneField: "Lane", Unassigned: "Unassigned", OpenTaskPage: "Open task page", Close: "Close",
		Description: "Description", NoDescription: "No description yet.", AddDescription: "Add a description",
		Save: "Save", Cancel: "Cancel", Edit: "Edit", Delete: "Delete", AddComment: "Comment",
		CommentPlaceholder: "Add a follow-up", Edited: "edited", Saving: "Saving", Saved: "Saved",
		SaveFailed: "Not saved. The task changed; review it and try again.", Projects: "Projects", TaskKey: "Task", Details: "Details",
		TouchMoveHint: "", LatestComments: "Latest comments", AllComments: "All comments",
		OpenTask: "Open task", MoveTo: "Move to", AssignTo: "Assign", CopyLink: "Copy link",
		LinkCopied: "Link copied", MoreActions: "More actions", NoDate: "No date", NoCommentsHint: "Ask a question or leave a follow-up for the team.",
		SortedAscending: "sorted ascending", SortedDescending: "sorted descending",
		StatusLocked:   "Done is the last step in this project's workflow, so the task can't be reopened.",
		StatusReadOnly: "You can't change this task's status.", StatusLockedShort: "Final step. Can't be reopened.",
		MoveNotAllowed: "Not a next step", LaneEmptyHere: "No tasks here", AddDueDate: "Add due date",
	}
	fill := func(value *string, fallback string) {
		if *value == "" {
			*value = fallback
		}
	}
	fill(&copy.Pending, defaults.Pending)
	fill(&copy.Conflict, defaults.Conflict)
	fill(&copy.Status, defaults.Status)
	fill(&copy.Lane, defaults.Lane)
	fill(&copy.StatusFor, defaults.StatusFor)
	fill(&copy.LaneFor, defaults.LaneFor)
	fill(&copy.NoTasksLane, defaults.NoTasksLane)
	fill(&copy.NoTasksColumn, defaults.NoTasksColumn)
	fill(&copy.NoTasksPage, defaults.NoTasksPage)
	fill(&copy.PreviousPage, defaults.PreviousPage)
	fill(&copy.NextPage, defaults.NextPage)
	fill(&copy.TaskPages, defaults.TaskPages)
	fill(&copy.MoreInLane, defaults.MoreInLane)
	fill(&copy.TaskDetails, defaults.TaskDetails)
	fill(&copy.LinkedItems, defaults.LinkedItems)
	fill(&copy.NoLinkedItems, defaults.NoLinkedItems)
	fill(&copy.Comments, defaults.Comments)
	fill(&copy.NoComments, defaults.NoComments)
	fill(&copy.MoreComments, defaults.MoreComments)
	fill(&copy.Activity, defaults.Activity)
	fill(&copy.NoActivity, defaults.NoActivity)
	fill(&copy.MoreActivity, defaults.MoreActivity)
	fill(&copy.Loading, defaults.Loading)
	fill(&copy.Restricted, defaults.Restricted)
	fill(&copy.Unavailable, defaults.Unavailable)
	fill(&copy.LinkedItem, defaults.LinkedItem)
	fill(&copy.LinkedChatItem, defaults.LinkedChatItem)
	fill(&copy.LinkedDocsItem, defaults.LinkedDocsItem)
	fill(&copy.BoardLabel, defaults.BoardLabel)
	fill(&copy.TasksLabel, defaults.TasksLabel)
	fill(&copy.StatusField, defaults.StatusField)
	fill(&copy.AssigneeField, defaults.AssigneeField)
	fill(&copy.DueField, defaults.DueField)
	fill(&copy.PriorityField, defaults.PriorityField)
	fill(&copy.TypeField, defaults.TypeField)
	fill(&copy.TaskField, defaults.TaskField)
	fill(&copy.DueToday, defaults.DueToday)
	fill(&copy.Overdue, defaults.Overdue)
	fill(&copy.DueSoon, defaults.DueSoon)
	for _, pair := range []struct {
		value    *string
		fallback string
	}{
		{&copy.CollapseAll, defaults.CollapseAll}, {&copy.ExpandAll, defaults.ExpandAll}, {&copy.ToggleLane, defaults.ToggleLane}, {&copy.MoveTask, defaults.MoveTask},
		{&copy.LaneField, defaults.LaneField}, {&copy.Unassigned, defaults.Unassigned}, {&copy.OpenTaskPage, defaults.OpenTaskPage}, {&copy.Close, defaults.Close},
		{&copy.Description, defaults.Description}, {&copy.NoDescription, defaults.NoDescription}, {&copy.AddDescription, defaults.AddDescription},
		{&copy.Save, defaults.Save}, {&copy.Cancel, defaults.Cancel}, {&copy.Edit, defaults.Edit}, {&copy.Delete, defaults.Delete}, {&copy.AddComment, defaults.AddComment},
		{&copy.CommentPlaceholder, defaults.CommentPlaceholder}, {&copy.Edited, defaults.Edited}, {&copy.Saving, defaults.Saving}, {&copy.Saved, defaults.Saved},
		{&copy.SaveFailed, defaults.SaveFailed}, {&copy.Projects, defaults.Projects}, {&copy.TaskKey, defaults.TaskKey}, {&copy.Details, defaults.Details},
		{&copy.LatestComments, defaults.LatestComments}, {&copy.AllComments, defaults.AllComments},
		{&copy.OpenTask, defaults.OpenTask}, {&copy.MoveTo, defaults.MoveTo}, {&copy.AssignTo, defaults.AssignTo}, {&copy.CopyLink, defaults.CopyLink},
		{&copy.LinkCopied, defaults.LinkCopied}, {&copy.MoreActions, defaults.MoreActions}, {&copy.NoDate, defaults.NoDate}, {&copy.NoCommentsHint, defaults.NoCommentsHint},
		{&copy.StatusLocked, defaults.StatusLocked}, {&copy.StatusReadOnly, defaults.StatusReadOnly},
		{&copy.SortedAscending, defaults.SortedAscending}, {&copy.SortedDescending, defaults.SortedDescending},
		{&copy.StatusLockedShort, defaults.StatusLockedShort}, {&copy.MoveNotAllowed, defaults.MoveNotAllowed}, {&copy.LaneEmptyHere, defaults.LaneEmptyHere}, {&copy.AddDueDate, defaults.AddDueDate},
	} {
		fill(pair.value, pair.fallback)
	}
	if copy.PageSummary == nil {
		copy.PageSummary = func(page, total int) string { return fmt.Sprintf("Page %d · %d tasks", page, total) }
	}
	if copy.ColumnCount == nil {
		copy.ColumnCount = func(count int) string {
			if count == 1 {
				return "1 task"
			}
			return fmt.Sprintf("%d tasks", count)
		}
	}
	if copy.FormatNumber == nil {
		copy.FormatNumber = strconv.Itoa
	}
	if copy.ActivityRepeat == nil {
		copy.ActivityRepeat = func(n int) string { return strconv.Itoa(n) + " times" }
	}
	if copy.FormatDate == nil {
		copy.FormatDate = func(date time.Time) string { return date.Format("Jan 2") }
	}
	return copy
}

func safeHref(href string) bool {
	trimmed := strings.TrimSpace(href)
	parsed, err := url.Parse(trimmed)
	if err != nil || trimmed == "" || strings.ContainsAny(trimmed, "\\\r\n") {
		return false
	}
	if parsed.Scheme != "" {
		return strings.EqualFold(parsed.Scheme, "https") && parsed.Host != ""
	}
	return !strings.HasPrefix(trimmed, "//")
}
