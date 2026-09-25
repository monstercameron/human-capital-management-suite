package productui

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
)

// The Projects home's Tickets tab: "My tickets" (assigned to the viewer,
// most urgent first) above "All tickets". The filters and sort are links in
// the address; the search box is instant and runs here over the rows the
// address already narrowed, and filters both sections at once.

const (
	projectTicketSearchID   = "project-ticket-search"
	projectTicketsPerPage   = 25
	projectMyTicketsStorage = "hcm.projects.mytickets"
	// projectMyTicketsLimit is how many of "My tickets" show before
	// "Show all".
	projectMyTicketsLimit = 10
)

type projectTicketsProps struct {
	List projectui.TicketList
	Copy projectui.Copy
}

func projectTicketsBoard(props projectTicketsProps) ui.Node {
	query := ui.UseState("")
	page := ui.UseState(1)
	mineOpen := ui.UseState(projectStorageRead(projectMyTicketsStorage) != "closed")
	onInput := ui.UseEvent(func(event ui.InputEvent) {
		query.Set(event.GetValue())
		page.Set(1)
	})
	// The box is the source of truth, as on the projects list: text typed
	// before hydration, or in a burst while a render is in flight, is
	// adopted on mount and after each query change.
	ui.UseEffect(func() func() {
		if value := projectFieldValue(projectTicketSearchID); value != query.Get() && (value != "" || query.Get() != "") {
			query.Set(value)
			page.Set(1)
		}
		return nil
	}, query.Get())
	onKey := ui.UseEvent(func(event ui.KeyboardEvent) {
		if event.GetKey() == "Escape" && query.Get() != "" {
			event.PreventDefault()
			setDocsFieldValue(projectTicketSearchID, "")
			query.Set("")
			page.Set(1)
		}
	})
	toggleMine := ui.UseEvent(func() {
		next := !mineOpen.Get()
		mineOpen.Set(next)
		state := "open"
		if !next {
			state = "closed"
		}
		projectStorageWrite(projectMyTicketsStorage, state)
	})
	// A new page size (from the address) starts again on page one.
	ui.UseEffect(func() func() {
		page.Set(1)
		return nil
	}, props.List.PageSize)
	showAllMine := ui.UseState(false)
	toggleAllMine := ui.UseEvent(func() { showAllMine.Set(!showAllMine.Get()) })
	previous := ui.UseEvent(func() { page.Set(max(1, page.Get()-1)) })
	next := ui.UseEvent(func() { page.Set(page.Get() + 1) })

	list, copy := props.List, props.Copy
	board := copy.Board
	today := list.Today
	if today.IsZero() {
		today = time.Now()
	}
	terms := strings.Fields(strings.ToLower(query.Get()))
	matches := func(ticket projectui.Ticket) bool {
		if len(terms) == 0 {
			return true
		}
		haystack := strings.ToLower(strings.Join(append([]string{ticket.Key, ticket.Title, ticket.ProjectName, ticket.Assignee}, ticket.Labels...), " "))
		for _, term := range terms {
			if !strings.Contains(haystack, term) {
				return false
			}
		}
		return true
	}
	all, mine := []projectui.Ticket{}, []projectui.Ticket{}
	for _, ticket := range list.Tickets {
		if !matches(ticket) {
			continue
		}
		all = append(all, ticket)
		if list.Viewer != "" && ticket.AssigneeID == list.Viewer && ticket.StatusTone != "done" {
			mine = append(mine, ticket)
		}
	}
	// Most urgent first: overdue, then due within a week, then the rest.
	urgency := func(ticket projectui.Ticket) int {
		due, err := time.Parse("2006-01-02", firstN10(ticket.DueDate))
		if err != nil {
			return 2
		}
		day := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
		switch {
		case due.Before(day):
			return 0
		case due.Sub(day) < 7*24*time.Hour:
			return 1
		}
		return 2
	}
	sort.SliceStable(mine, func(i, j int) bool {
		a, b := urgency(mine[i]), urgency(mine[j])
		if a != b {
			return a < b
		}
		return firstN10(mine[i].DueDate) < firstN10(mine[j].DueDate)
	})

	search := html.Div(html.Props{Class: "projectui-filter-search projectui-tickets-search", Role: "search"},
		html.Label(html.Props{For: projectTicketSearchID, Class: "sr-only", Text: board.SearchTickets}),
		html.Span(html.Props{Class: "projectui-filter-search-icon", Aria: map[string]string{"hidden": "true"}}),
		html.Input(html.Props{ID: projectTicketSearchID, Type: "search", Placeholder: board.SearchTickets, AutoComplete: "off", OnInput: onInput, OnKeyDown: onKey, Aria: map[string]string{"controls": "project-all-tickets"}}),
	)
	countText := board.TicketCount(len(all), list.Total)
	toolbar := html.Div(html.Props{Class: "projectui-tickets-toolbar"},
		search,
		projectui.TicketFilters(list, copy),
		html.P(html.Props{Class: "projectui-tickets-count", Role: "status", Aria: map[string]string{"live": "polite"}, Text: countText}),
	)

	// My tickets.
	mineBody := []ui.Node{}
	if len(mine) == 0 {
		mineBody = append(mineBody, html.Div(html.Props{Class: "project-my-tickets-empty"},
			html.P(html.Props{Class: "project-my-tickets-empty-title", Text: board.MyTicketsEmpty}),
			html.P(html.Props{Text: board.MyTicketsEmptyHint})))
	} else {
		mineBody = append(mineBody, projectui.TicketHeader(list, copy))
		shown := mine
		if len(mine) > projectMyTicketsLimit && !showAllMine.Get() {
			shown = mine[:projectMyTicketsLimit]
		}
		groups := [3][]projectui.Ticket{}
		for _, ticket := range shown {
			level := urgency(ticket)
			groups[level] = append(groups[level], ticket)
		}
		titles := [3]string{board.DueOverdue, copy.DueSoon, board.Later}
		for level, tickets := range groups {
			if len(tickets) == 0 {
				continue
			}
			rows := make([]ui.Node, 0, len(tickets))
			for _, ticket := range tickets {
				rows = append(rows, projectui.TicketRow(ticket, copy, today))
			}
			mineBody = append(mineBody, html.Div(html.Props{Class: "project-my-tickets-group", Data: map[string]string{"urgency": strconv.Itoa(level)}},
				html.H3(html.Props{Class: "project-my-tickets-group-title"}, html.Span(html.Props{Text: titles[level]}), html.Span(html.Props{Class: "projectui-count", Text: copy.FormatNumber(len(tickets))})),
				html.Ul(html.Props{Class: "projectui-tickets-rows", Role: "list"}, rows...)))
		}
	}
	if len(mine) > projectMyTicketsLimit {
		label := copy.Board.ShowFewer
		if !showAllMine.Get() {
			label = copy.Board.ShowAll(len(mine))
		}
		mineBody = append(mineBody, html.Div(html.Props{Class: "project-my-tickets-more"},
			html.Button(html.Props{Type: "button", Class: "projectui-link-button", OnClick: toggleAllMine, Aria: map[string]string{"expanded": strconv.FormatBool(showAllMine.Get())}, Text: label})))
	}
	mineSection := html.Section(html.Props{Class: "project-my-tickets", Data: map[string]string{"open": strconv.FormatBool(mineOpen.Get())}, Aria: map[string]string{"labelledby": "project-my-tickets-title"}},
		html.H2(html.Props{ID: "project-my-tickets-title", Class: "project-tickets-section-title"},
			html.Button(html.Props{Type: "button", Class: "project-my-tickets-toggle", OnClick: toggleMine, Aria: map[string]string{"expanded": strconv.FormatBool(mineOpen.Get()), "controls": "project-my-tickets-body"}},
				html.Span(html.Props{Class: "project-my-tickets-chevron", Aria: map[string]string{"hidden": "true"}}),
				html.Span(html.Props{Text: board.MyTickets}),
				html.Span(html.Props{Class: "projectui-count", Text: copy.FormatNumber(len(mine))}))),
		html.Div(html.Props{ID: "project-my-tickets-body", Class: "projectui-tickets-table project-my-tickets-body", Hidden: !mineOpen.Get()}, mineBody...),
	)

	// All tickets, paged here.
	perPage := list.PageSize
	if perPage <= 0 {
		perPage = projectTicketsPerPage
	}
	pages := max(1, (len(all)+perPage-1)/perPage)
	current := min(max(1, page.Get()), pages)
	from := (current - 1) * perPage
	to := min(from+perPage, len(all))
	var empty ui.Node
	switch {
	case list.Loading && len(list.Tickets) == 0:
		empty = projectui.TicketLoading(copy)
	case list.Total == 0:
		empty = html.Div(html.Props{Class: "projectui-tickets-empty"}, html.P(html.Props{Class: "projectui-tickets-empty-title", Text: board.NoTickets}), html.P(html.Props{Text: board.NoTicketsHint}))
	default:
		children := []ui.Node{html.P(html.Props{Class: "projectui-tickets-empty-title", Text: board.NoMatch})}
		if list.ClearHref != "" && list.ActiveCount > 0 {
			children = append(children, html.A(html.Props{Href: list.ClearHref, Class: "projectui-button", Text: board.ClearFilters}))
		}
		empty = html.Div(html.Props{Class: "projectui-tickets-empty"}, children...)
	}
	allSection := []ui.Node{
		html.H2(html.Props{ID: "project-all-tickets-title", Class: "project-tickets-section-title"}, html.Span(html.Props{Text: board.AllTickets}), html.Span(html.Props{Class: "projectui-count", Text: copy.FormatNumber(len(all))})),
		projectui.TicketTable("project-all-tickets", all[from:to], list, copy, empty),
	}
	if len(all) > 0 {
		pager := projectui.Pager{From: from + 1, To: to, Total: len(all), Page: current, Pages: pages, Sizes: list.Sizes}
		allSection = append(allSection, projectui.PagerFooter(pager, copy,
			html.Button(html.Props{Type: "button", Class: "button secondary small", Disabled: current <= 1, OnClick: previous, Text: board.PreviousPage}),
			html.Button(html.Props{Type: "button", Class: "button secondary small", Disabled: current >= pages, OnClick: next, Text: board.NextPage})))
	}
	return html.Section(html.Props{ID: "project-tabpanel-tickets", Class: "projectui-tickets", Role: "tabpanel", Data: map[string]string{"projectui": "tickets"}, Aria: map[string]string{"labelledby": "project-tab-tickets"}},
		toolbar,
		mineSection,
		html.Section(html.Props{Class: "project-all-tickets", Aria: map[string]string{"labelledby": "project-all-tickets-title"}}, allSection...),
	)
}

func firstN10(value string) string {
	if len(value) > 10 {
		return value[:10]
	}
	return value
}
