package projectui

import (
	"fmt"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// FilterChoice is one toggle in the board's quick-filter bar. Href is the
// address with this choice flipped, so every toggle is a plain link the host
// router follows; the filter lives in the URL and survives a reload.
type FilterChoice struct {
	ID, Label string
	// Photo is an optional same-origin portrait for an assignee choice.
	Photo  string
	Href   string
	Active bool
}

// FilterBar is the quick filter above the columns. The host computes which
// cards match (Card.FilteredOut) and every choice's address.
type FilterBar struct {
	Assignees  []FilterChoice
	Priorities []FilterChoice
	DueSoon    FilterChoice
	// Query is the title search; SearchHref is the current address without
	// it, to which the host appends the typed text.
	Query      string
	SearchHref string
	ClearHref  string
	// ActiveCount is how many choices and searches narrow the board.
	ActiveCount int
}

// Active reports whether anything narrows the board.
func (f *FilterBar) Active() bool { return f != nil && f.ActiveCount > 0 }

// BoardCopy holds the strings for the filter bar, the first-run board and
// the keyboard shortcut help.
type BoardCopy struct {
	Filters, FilterAssignee, FilterPriority, FilterDueSoon string
	SearchTasks, ClearFilters, NoMatching, FiltersDone     string
	FirstRunTitle, FirstRunBody, CreateFirstTask           string
	ShortcutsTitle, ShortcutNewTask, ShortcutSearch        string
	ShortcutFilters, ShortcutMove, ShortcutOpen            string
	ShortcutHelp                                           string
	// Sharing.
	Share, SendToChat, CopyForDocs, CopiedForDocs, SendTitle string
	Conversation, Channels, DirectMessages, ShareNote        string
	Send, OpenConversation, ShareFailed, NoConversations     string
	// SentTo is the toast after a send ("Sent to #general").
	SentTo func(name string) string
	// Task page structure and planning fields.
	People, Planning, Dates, Reporter, StartDate, StoryPoints string
	Labels, AddLabel, RemoveLabel, AddStartDate, AddEstimate  string
	NoLabels, Project                                         string
	ReporterUnknown                                           string
	// Created and Updated are "Created 3 h ago" style lines.
	Created, Updated func(when string) string
	// Tickets tab.
	TabProjects, TabTickets, TicketsLabel, AssignedToMe, SearchTickets string
	FilterProject, FilterStatus, FilterDue, FilterLabels               string
	StatusTodo, StatusActive, StatusDone, DueOverdue, DueThisWeek      string
	ColKey, ColTitle, ColUpdated, NoTickets, NoTicketsHint, NoMatch    string
	LoadingTickets, PreviousPage, NextPage                             string
	MyTickets, AllTickets, MyTicketsEmpty, MyTicketsEmptyHint, Later   string
	ScrollPrev, ScrollNext                                             string
	// PageSize labels the rows-per-page select; Range is "1–25 of 110".
	PageSize string
	Range    func(from, to, total int) string
	// ShowAll and ShowFewer expand and shorten "My tickets".
	ShowAll   func(n int) string
	ShowFewer string
	// Points is the story points badge text ("2 pts").
	Points      func(n int) string
	TicketCount func(shown, total int) string
	PageOf      func(page, pages int) string
	// CountOf is a filtered column count ("3 of 7").
	CountOf func(shown, total int) string
}

func localizedBoardCopy(copy BoardCopy, format func(int) string) BoardCopy {
	defaults := BoardCopy{
		Filters: "Filters", FilterAssignee: "Assignee", FilterPriority: "Priority", FilterDueSoon: "Due soon",
		SearchTasks: "Search tasks", ClearFilters: "Clear filters", NoMatching: "No matching tasks", FiltersDone: "Done",
		FirstRunTitle: "Start this board", FirstRunBody: "Add the first task. It lands in the first column, and you can drag it along as work moves.",
		CreateFirstTask: "Create the first task",
		ShortcutsTitle:  "Keyboard shortcuts", ShortcutNewTask: "New task", ShortcutSearch: "Search tasks",
		ShortcutFilters: "Show or hide filters", ShortcutMove: "Move between cards", ShortcutOpen: "Open the focused card",
		ShortcutHelp: "Show this help",
		Share:        "Share", SendToChat: "Send to chat…", CopyForDocs: "Copy for Docs", CopiedForDocs: "Copied for Docs", SendTitle: "Send to chat",
		Conversation: "Conversation", Channels: "Channels", DirectMessages: "Direct messages", ShareNote: "Add a note (optional)",
		Send: "Send", OpenConversation: "Open conversation", ShareFailed: "The message could not be sent. Try again.", NoConversations: "You are not in any conversations yet.",
		People: "People", Planning: "Planning", Dates: "Dates", Reporter: "Reporter", StartDate: "Start date", StoryPoints: "Story points",
		Labels: "Labels", AddLabel: "Add label", RemoveLabel: "Remove label", AddStartDate: "Add start date", AddEstimate: "Add estimate",
		NoLabels: "No labels", Project: "Project", ReporterUnknown: "Unknown",
		TabProjects: "Projects", TabTickets: "Tickets", TicketsLabel: "Tickets", AssignedToMe: "Assigned to me", SearchTickets: "Search tickets",
		FilterProject: "Project", FilterStatus: "Status", FilterDue: "Due", FilterLabels: "Labels",
		StatusTodo: "To do", StatusActive: "In progress", StatusDone: "Done", DueOverdue: "Overdue", DueThisWeek: "Due this week",
		ColKey: "Key", ColTitle: "Title", ColUpdated: "Updated", NoTickets: "No tickets yet", NoTicketsHint: "Tickets from every project you can see appear here.",
		NoMatch: "No tickets match these filters.", LoadingTickets: "Loading tickets", PreviousPage: "Previous", NextPage: "Next",
		PageSize: "Rows per page", ShowFewer: "Show fewer",
		ScrollPrev: "Scroll to earlier columns", ScrollNext: "Scroll to later columns",
		MyTickets: "My tickets", AllTickets: "All tickets", MyTicketsEmpty: "Nothing is assigned to you right now.", MyTicketsEmptyHint: "Tickets assigned to you show up here first, most urgent on top.", Later: "Later",
	}
	for _, pair := range []struct {
		value    *string
		fallback string
	}{
		{&copy.Filters, defaults.Filters}, {&copy.FilterAssignee, defaults.FilterAssignee}, {&copy.FilterPriority, defaults.FilterPriority},
		{&copy.FilterDueSoon, defaults.FilterDueSoon}, {&copy.SearchTasks, defaults.SearchTasks}, {&copy.ClearFilters, defaults.ClearFilters},
		{&copy.NoMatching, defaults.NoMatching}, {&copy.FiltersDone, defaults.FiltersDone}, {&copy.FirstRunTitle, defaults.FirstRunTitle},
		{&copy.FirstRunBody, defaults.FirstRunBody}, {&copy.CreateFirstTask, defaults.CreateFirstTask}, {&copy.ShortcutsTitle, defaults.ShortcutsTitle},
		{&copy.ShortcutNewTask, defaults.ShortcutNewTask}, {&copy.ShortcutSearch, defaults.ShortcutSearch}, {&copy.ShortcutFilters, defaults.ShortcutFilters},
		{&copy.ShortcutMove, defaults.ShortcutMove}, {&copy.ShortcutOpen, defaults.ShortcutOpen}, {&copy.ShortcutHelp, defaults.ShortcutHelp},
		{&copy.Share, defaults.Share},
		{&copy.SendToChat, defaults.SendToChat},
		{&copy.CopyForDocs, defaults.CopyForDocs},
		{&copy.CopiedForDocs, defaults.CopiedForDocs},
		{&copy.SendTitle, defaults.SendTitle},
		{&copy.Conversation, defaults.Conversation},
		{&copy.Channels, defaults.Channels},
		{&copy.DirectMessages, defaults.DirectMessages},
		{&copy.ShareNote, defaults.ShareNote},
		{&copy.Send, defaults.Send},
		{&copy.OpenConversation, defaults.OpenConversation},
		{&copy.ShareFailed, defaults.ShareFailed},
		{&copy.NoConversations, defaults.NoConversations},
		{&copy.People, defaults.People},
		{&copy.Planning, defaults.Planning},
		{&copy.Dates, defaults.Dates},
		{&copy.Reporter, defaults.Reporter},
		{&copy.StartDate, defaults.StartDate},
		{&copy.StoryPoints, defaults.StoryPoints},
		{&copy.Labels, defaults.Labels},
		{&copy.AddLabel, defaults.AddLabel},
		{&copy.RemoveLabel, defaults.RemoveLabel},
		{&copy.AddStartDate, defaults.AddStartDate},
		{&copy.AddEstimate, defaults.AddEstimate},
		{&copy.NoLabels, defaults.NoLabels},
		{&copy.Project, defaults.Project},
		{&copy.TabProjects, defaults.TabProjects},
		{&copy.TabTickets, defaults.TabTickets},
		{&copy.TicketsLabel, defaults.TicketsLabel},
		{&copy.AssignedToMe, defaults.AssignedToMe},
		{&copy.SearchTickets, defaults.SearchTickets},
		{&copy.FilterProject, defaults.FilterProject},
		{&copy.FilterStatus, defaults.FilterStatus},
		{&copy.FilterDue, defaults.FilterDue},
		{&copy.FilterLabels, defaults.FilterLabels},
		{&copy.StatusTodo, defaults.StatusTodo},
		{&copy.StatusActive, defaults.StatusActive},
		{&copy.StatusDone, defaults.StatusDone},
		{&copy.DueOverdue, defaults.DueOverdue},
		{&copy.DueThisWeek, defaults.DueThisWeek},
		{&copy.ColKey, defaults.ColKey},
		{&copy.ColTitle, defaults.ColTitle},
		{&copy.ColUpdated, defaults.ColUpdated},
		{&copy.NoTickets, defaults.NoTickets},
		{&copy.NoTicketsHint, defaults.NoTicketsHint},
		{&copy.NoMatch, defaults.NoMatch},
		{&copy.LoadingTickets, defaults.LoadingTickets},
		{&copy.PreviousPage, defaults.PreviousPage},
		{&copy.NextPage, defaults.NextPage},
		{&copy.PageSize, defaults.PageSize}, {&copy.ShowFewer, defaults.ShowFewer}, {&copy.ReporterUnknown, defaults.ReporterUnknown},
		{&copy.ScrollPrev, defaults.ScrollPrev}, {&copy.ScrollNext, defaults.ScrollNext},
		{&copy.MyTickets, defaults.MyTickets}, {&copy.AllTickets, defaults.AllTickets}, {&copy.MyTicketsEmpty, defaults.MyTicketsEmpty}, {&copy.MyTicketsEmptyHint, defaults.MyTicketsEmptyHint}, {&copy.Later, defaults.Later},
	} {
		if *pair.value == "" {
			*pair.value = pair.fallback
		}
	}
	if copy.ShowAll == nil {
		copy.ShowAll = func(n int) string { return "Show all " + format(n) }
	}
	if copy.Range == nil {
		copy.Range = func(from, to, total int) string { return format(from) + "–" + format(to) + " of " + format(total) }
	}
	if copy.Points == nil {
		copy.Points = func(n int) string { return format(n) + " pts" }
	}
	if copy.SentTo == nil {
		copy.SentTo = func(name string) string { return "Sent to " + name }
	}
	if copy.Created == nil {
		copy.Created = func(when string) string { return "Created " + when }
	}
	if copy.Updated == nil {
		copy.Updated = func(when string) string { return "Updated " + when }
	}
	if copy.TicketCount == nil {
		copy.TicketCount = func(shown, total int) string {
			if shown == total {
				return fmt.Sprintf("%s tickets", format(total))
			}
			return fmt.Sprintf("%s of %s tickets", format(shown), format(total))
		}
	}
	if copy.PageOf == nil {
		copy.PageOf = func(page, pages int) string { return fmt.Sprintf("Page %s of %s", format(page), format(pages)) }
	}
	if copy.CountOf == nil {
		copy.CountOf = func(shown, total int) string { return fmt.Sprintf("%s of %s", format(shown), format(total)) }
	}
	return copy
}

// keyHint appends a shortcut to a tooltip: "Search tasks (/)".
func keyHint(label, key string) string { return label + " (" + key + ")" }

// filterSection renders the bar and, for phones and for a bar hidden with
// the F shortcut, the button that shows it. The host flips
// data-filters-toggled on the section; CSS reads it per breakpoint.
func filterSection(model Model) ui.Node {
	bar := model.Filters
	copy := model.Copy.Board
	toggleLabel := []ui.Node{
		html.Span(html.Props{Class: "projectui-filter-toggle-icon", Aria: map[string]string{"hidden": "true"}}),
		html.Span(html.Props{Text: copy.Filters}),
	}
	if bar.ActiveCount > 0 {
		toggleLabel = append(toggleLabel, html.Span(html.Props{Class: "projectui-filter-count", Text: model.Copy.FormatNumber(bar.ActiveCount)}))
	}
	toggle := html.Button(html.Props{Type: "button", Class: "projectui-filter-toggle", Title: keyHint(copy.Filters, "F"), Data: map[string]string{"projectui-action": "toggle-filters"}, Aria: map[string]string{"controls": "projectui-filterbar", "expanded": "false"}}, toggleLabel...)

	search := html.Form(html.Props{Class: "projectui-filter-search", Role: "search", Data: map[string]string{"projectui-action": "board-search", "href": bar.SearchHref}},
		html.Label(html.Props{For: "projectui-board-search", Class: "projectui-sr", Text: copy.SearchTasks}),
		html.Span(html.Props{Class: "projectui-filter-search-icon", Aria: map[string]string{"hidden": "true"}}),
		html.Input(html.Props{ID: "projectui-board-search", Type: "search", Name: "q", AutoComplete: "off", Placeholder: copy.SearchTasks, Title: keyHint(copy.SearchTasks, "/"), Raw: map[string]any{"value": bar.Query}}),
	)
	chip := func(choice FilterChoice, class string, lead ui.Node) ui.Node {
		props := html.Props{Href: choice.Href, Class: class, Role: "button", Title: choice.Label, Data: map[string]string{"active": boolString(choice.Active), "filter-id": choice.ID}, Aria: map[string]string{"pressed": boolString(choice.Active)}}
		children := []ui.Node{}
		if lead != nil {
			children = append(children, lead)
		}
		if class != "projectui-filter-person" {
			children = append(children, html.Span(html.Props{Text: choice.Label}))
		} else {
			props.Aria["label"] = choice.Label
		}
		return html.A(props, children...)
	}
	people := []ui.Node{}
	for _, choice := range bar.Assignees {
		if choice.Label == "" && choice.ID == "none" {
			choice.Label = model.Copy.Unassigned
		}
		lead := avatarWithPhoto(choice.Label, choice.Photo)
		if choice.ID == "none" {
			lead = html.Span(html.Props{Class: "projectui-avatar projectui-avatar-empty", Aria: map[string]string{"hidden": "true"}})
		}
		people = append(people, chip(choice, "projectui-filter-person", lead))
	}
	priorities := []ui.Node{}
	for _, choice := range bar.Priorities {
		priorities = append(priorities, chip(choice, "projectui-filter-chip", priorityIndicatorBars(choice.ID)))
	}
	groups := []ui.Node{search}
	if len(people) > 0 {
		groups = append(groups, html.Div(html.Props{Class: "projectui-filter-group projectui-filter-people", Role: "group", Aria: map[string]string{"label": copy.FilterAssignee}}, people...))
	}
	if len(priorities) > 0 {
		groups = append(groups, html.Div(html.Props{Class: "projectui-filter-group", Role: "group", Aria: map[string]string{"label": copy.FilterPriority}}, priorities...))
	}
	if bar.DueSoon.Href != "" {
		if bar.DueSoon.Label == "" {
			bar.DueSoon.Label = copy.FilterDueSoon
		}
		groups = append(groups, chip(bar.DueSoon, "projectui-filter-chip", html.Span(html.Props{Class: "projectui-due-icon", Data: map[string]string{"tone": "soon"}, Aria: map[string]string{"hidden": "true"}})))
	}
	if bar.ActiveCount > 0 && bar.ClearHref != "" {
		groups = append(groups, html.A(html.Props{Href: bar.ClearHref, Class: "projectui-filter-clear", Text: copy.ClearFilters}))
	}
	groups = append(groups, html.Button(html.Props{Type: "button", Class: "projectui-button projectui-button-primary projectui-filter-done", Data: map[string]string{"projectui-action": "toggle-filters"}, Text: copy.FiltersDone}))
	return html.Div(html.Props{Class: "projectui-filters", Data: map[string]string{"active": boolString(bar.ActiveCount > 0)}},
		toggle,
		html.Div(html.Props{ID: "projectui-filterbar", Class: "projectui-filterbar", Role: "region", Aria: map[string]string{"label": copy.Filters}}, groups...),
	)
}

// firstRun is the empty board: one clear next step instead of empty
// columns. The button opens the page's New task form through the host.
func firstRun(model Model) ui.Node {
	copy := model.Copy.Board
	return html.Div(html.Props{Class: "projectui-firstrun"},
		html.Div(html.Props{Class: "projectui-firstrun-art", Aria: map[string]string{"hidden": "true"}},
			html.Span(html.Props{}), html.Span(html.Props{}), html.Span(html.Props{})),
		html.H2(html.Props{Class: "projectui-firstrun-title", Text: copy.FirstRunTitle}),
		html.P(html.Props{Class: "projectui-firstrun-body", Text: copy.FirstRunBody}),
		html.Button(html.Props{Type: "button", Class: "projectui-button projectui-button-primary", Title: keyHint(copy.CreateFirstTask, "C"), Data: map[string]string{"projectui-action": "open-create-task"}, Text: copy.CreateFirstTask}),
	)
}

// shortcutHelp is the hidden keyboard-shortcut sheet the ? key opens.
func shortcutHelp(model Model) ui.Node {
	copy := model.Copy.Board
	row := func(label string, keys ...string) ui.Node {
		kbd := make([]ui.Node, 0, len(keys))
		for _, key := range keys {
			kbd = append(kbd, html.Kbd(html.Props{Text: key}))
		}
		return html.Div(html.Props{Class: "projectui-shortcut"}, html.Tag("dt", html.Props{Text: label}), html.Tag("dd", html.Props{}, kbd...))
	}
	return html.Div(html.Props{ID: "projectui-shortcuts", Class: "projectui-shortcuts", Role: "dialog", Hidden: true, TabIndex: -1, Aria: map[string]string{"labelledby": "projectui-shortcuts-title"}},
		html.Div(html.Props{Class: "projectui-shortcuts-head"},
			html.H2(html.Props{ID: "projectui-shortcuts-title", Text: copy.ShortcutsTitle}),
			html.Button(html.Props{Type: "button", Class: "projectui-modal-close", Data: map[string]string{"projectui-action": "close-shortcuts"}, Aria: map[string]string{"label": model.Copy.Close}}, html.Span(html.Props{Class: "projectui-modal-close-icon", Aria: map[string]string{"hidden": "true"}})),
		),
		html.Tag("dl", html.Props{},
			row(copy.ShortcutNewTask, "C"),
			row(copy.ShortcutSearch, "/"),
			row(copy.ShortcutFilters, "F"),
			row(copy.ShortcutMove, "J", "K", "←", "→"),
			row(copy.ShortcutOpen, "Enter"),
			row(copy.ShortcutHelp, "?"),
		),
	)
}
