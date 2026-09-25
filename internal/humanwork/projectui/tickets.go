package projectui

import (
	"strconv"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Ticket is one row of the cross-project ticket list, already authorized
// and localized by the host.
type Ticket struct {
	ID, Key, Title, Href       string
	ProjectID, ProjectName     string
	StatusLabel, StatusTone    string
	AssigneeID                 string
	Assignee, AssigneePhoto    string
	PriorityID, PriorityLabel  string
	DueDate, Updated, UpdateAt string
	Labels                     []string
	StoryPoints                int
	Workflows                  []string
}

// TicketFilterGroup is one filter dropdown ("Project", "Status", ...).
type TicketFilterGroup struct {
	Key, Label string
	Choices    []FilterChoice
}

// TicketList is the tickets tab: every ticket the address's filters keep,
// sorted, with the filters as links and sort links per column. Search and
// paging run in the page over these rows.
type TicketList struct {
	Tickets []Ticket
	// Total counts every ticket before filtering.
	Total int
	// Viewer is the signed-in subject, for "My tickets".
	Viewer      string
	ClearHref   string
	Mine        FilterChoice
	Groups      []TicketFilterGroup
	ActiveCount int
	Sort        string
	SortHrefs   map[string]string
	// Loading is true while the per-project reads are still arriving.
	Loading bool
	Today   time.Time
	// PageSize is the rows per page of "All tickets"; Sizes are the
	// rows-per-page choices as addresses.
	PageSize int
	Sizes    []FilterChoice
}

// TicketCopy completes a copy for the ticket views.
func TicketCopy(copy Copy) Copy {
	copy = localizedCopy(copy)
	copy.Board = localizedBoardCopy(copy.Board, copy.FormatNumber)
	return copy
}

// TicketFilters renders "Assigned to me", the filter dropdowns and "Clear
// filters". Every choice is a link to the address with that choice flipped.
func TicketFilters(list TicketList, copy Copy) ui.Node {
	board := copy.Board
	filters := []ui.Node{}
	if list.Mine.Href != "" {
		filters = append(filters, html.A(html.Props{Href: list.Mine.Href, Class: "projectui-filter-chip projectui-tickets-mine", Role: "button", Data: map[string]string{"active": boolString(list.Mine.Active)}, Aria: map[string]string{"pressed": boolString(list.Mine.Active)}},
			html.Span(html.Props{Class: "projectui-menu-icon", Data: map[string]string{"icon": "assign"}, Aria: map[string]string{"hidden": "true"}}), html.Span(html.Props{Text: board.AssignedToMe})))
	}
	for _, group := range list.Groups {
		if len(group.Choices) == 0 {
			continue
		}
		active := 0
		items := make([]ui.Node, 0, len(group.Choices))
		for _, choice := range group.Choices {
			if choice.Active {
				active++
			}
			items = append(items, html.A(html.Props{Href: choice.Href, Class: "projectui-menu-item projectui-menu-radio", Role: "menuitemcheckbox", Aria: map[string]string{"checked": boolString(choice.Active)}, Data: map[string]string{"filter-id": choice.ID}},
				html.Span(html.Props{Class: "projectui-menu-item-label", Dir: "auto", Text: choice.Label}),
				html.Span(html.Props{Class: "projectui-menu-check", Aria: map[string]string{"hidden": "true"}})))
		}
		summary := []ui.Node{html.Span(html.Props{Text: group.Label})}
		if active > 0 {
			summary = append(summary, html.Span(html.Props{Class: "projectui-filter-count", Text: copy.FormatNumber(active)}))
		}
		summary = append(summary, html.Span(html.Props{Class: "projectui-tickets-chevron", Aria: map[string]string{"hidden": "true"}}))
		filters = append(filters, html.Details(html.Props{Class: "projectui-tickets-filter", Data: map[string]string{"group": group.Key, "active": boolString(active > 0)}},
			html.Summary(html.Props{Class: "projectui-filter-chip", Aria: map[string]string{"haspopup": "menu"}}, summary...),
			html.Div(html.Props{Class: "projectui-card-menu-panel projectui-tickets-panel", Role: "menu", Aria: map[string]string{"label": group.Label}}, items...),
		))
	}
	if list.ActiveCount > 0 && list.ClearHref != "" {
		filters = append(filters, html.A(html.Props{Href: list.ClearHref, Class: "projectui-filter-clear", Text: board.ClearFilters}))
	}
	return html.Div(html.Props{Class: "projectui-filterbar projectui-tickets-filters", Role: "region", Aria: map[string]string{"label": board.Filters}}, filters...)
}

// TicketTable renders the column head (with sort links) and the rows, or
// the given empty state when there are none.
// TicketHeader is the ticket columns' head row, with sort links.
func TicketHeader(list TicketList, copy Copy) ui.Node {
	board := copy.Board
	head := func(key, label string) ui.Node {
		href := list.SortHrefs[key]
		if href == "" {
			return html.Span(html.Props{Class: "projectui-tickets-th", Data: map[string]string{"col": key}, Text: label})
		}
		active, descending := list.Sort == key || list.Sort == "-"+key, len(list.Sort) > 0 && list.Sort[0] == '-'
		children := []ui.Node{html.Span(html.Props{Text: label})}
		sortState := "none"
		if active {
			arrow, state := "↑", copy.SortedAscending
			sortState = "ascending"
			if descending {
				arrow, state, sortState = "↓", copy.SortedDescending, "descending"
			}
			children = append(children, html.Span(html.Props{Class: "projectui-sort-arrow", Aria: map[string]string{"hidden": "true"}, Text: arrow}), html.Span(html.Props{Class: "projectui-sr", Text: ", " + state}))
		} else {
			children = append(children, html.Span(html.Props{Class: "projectui-sort-arrow projectui-sort-idle", Aria: map[string]string{"hidden": "true"}, Text: "↕"}))
		}
		return html.Span(html.Props{Class: "projectui-tickets-th", Role: "columnheader", Data: map[string]string{"col": key}, Aria: map[string]string{"sort": sortState}},
			html.A(html.Props{Href: href, Class: "projectui-sort", Data: map[string]string{"active": boolString(active)}}, children...))
	}
	return html.Div(html.Props{Class: "projectui-tickets-head", Role: "row"},
		head("key", board.ColKey), head("title", board.ColTitle), head("project", board.Project), head("status", copy.StatusField),
		head("assignee", copy.AssigneeField), head("priority", copy.PriorityField), head("due", copy.DueField), head("updated", board.ColUpdated),
	)
}

func TicketTable(id string, tickets []Ticket, list TicketList, copy Copy, empty ui.Node) ui.Node {
	today := list.Today
	board := copy.Board
	if today.IsZero() {
		today = time.Now()
	}
	header := TicketHeader(list, copy)
	var body ui.Node
	if len(tickets) == 0 {
		body = empty
	} else {
		rows := make([]ui.Node, 0, len(tickets))
		for _, ticket := range tickets {
			rows = append(rows, TicketRow(ticket, copy, today))
		}
		body = html.Ul(html.Props{ID: id, Class: "projectui-tickets-rows", Role: "list", Aria: map[string]string{"label": board.TicketsLabel}}, rows...)
	}
	return html.Div(html.Props{Class: "projectui-tickets-table"}, header, body)
}

// PagerFooter renders a table footer: the range, a "Rows per page" select
// whose options are addresses, and the page controls. prev and next, when
// given, replace the page links (a page that pages in place).
func PagerFooter(pager Pager, copy Copy, prev, next ui.Node) ui.Node {
	board := copy.Board
	children := []ui.Node{}
	if pager.Total > 0 {
		children = append(children, html.P(html.Props{Class: "projectui-pager-range", Aria: map[string]string{"live": "polite"}, Text: board.Range(pager.From, pager.To, pager.Total)}))
	}
	if len(pager.Sizes) > 0 {
		options := make([]ui.Node, 0, len(pager.Sizes))
		for _, size := range pager.Sizes {
			options = append(options, html.Option(html.Props{Value: size.Href, Selected: size.Active, Text: size.Label}))
		}
		children = append(children, html.Label(html.Props{Class: "projectui-page-size"},
			html.Span(html.Props{Text: board.PageSize}),
			html.Select(html.Props{Data: map[string]string{"projectui-action": "page-size"}}, options...)))
	}
	if pager.Pages > 1 {
		if prev == nil {
			prev = html.Span(html.Props{Class: "projectui-button", Aria: map[string]string{"disabled": "true"}, Text: board.PreviousPage})
			if pager.PrevHref != "" {
				prev = html.A(html.Props{Href: pager.PrevHref, Class: "projectui-button", Rel: "prev", Text: board.PreviousPage})
			}
		}
		if next == nil {
			next = html.Span(html.Props{Class: "projectui-button", Aria: map[string]string{"disabled": "true"}, Text: board.NextPage})
			if pager.NextHref != "" {
				next = html.A(html.Props{Href: pager.NextHref, Class: "projectui-button", Rel: "next", Text: board.NextPage})
			}
		}
		children = append(children, html.Div(html.Props{Class: "projectui-pager-pages"},
			prev, html.Span(html.Props{Class: "projectui-tickets-page", Text: board.PageOf(pager.Page, pager.Pages)}), next))
	}
	return html.Nav(html.Props{Class: "projectui-pager-footer", Aria: map[string]string{"label": copy.TaskPages}}, children...)
}

// TicketLoading is the quiet placeholder while tickets are still loading.
func TicketLoading(copy Copy) ui.Node {
	rows := []ui.Node{}
	for index := 0; index < 5; index++ {
		rows = append(rows, html.Li(html.Props{Class: "projectui-tickets-row projectui-tickets-shimmer", Aria: map[string]string{"hidden": "true"}}, html.Span(html.Props{}), html.Span(html.Props{}), html.Span(html.Props{})))
	}
	return html.Div(html.Props{Class: "projectui-tickets-loading", Role: "status", Aria: map[string]string{"label": copy.Board.LoadingTickets}}, html.Ul(html.Props{Class: "projectui-tickets-rows"}, rows...))
}

// TicketRow is one ticket as a link row.
func TicketRow(ticket Ticket, copy Copy, today time.Time) ui.Node {
	tone := ticket.StatusTone
	if tone == "" {
		tone = "todo"
	}
	title := []ui.Node{html.Span(html.Props{Class: "projectui-tickets-title-text", Dir: "auto", Text: ticket.Title})}
	if len(ticket.Labels) > 0 || ticket.StoryPoints > 0 || len(ticket.Workflows) > 0 {
		chips := []ui.Node{}
		if chip := workflowChip(ticket.Workflows); chip != nil {
			chips = append(chips, chip)
		}
		chips = append(chips, labelChips(ticket.Labels, ticket.StoryPoints, copy, 3))
		title = append(title, html.Span(html.Props{Class: "projectui-tickets-chips"}, chips...))
	}
	assignee := ui.Node(html.Span(html.Props{Class: "projectui-assignee projectui-assignee-none"}, html.Span(html.Props{Class: "projectui-avatar projectui-avatar-empty", Aria: map[string]string{"hidden": "true"}}), html.Span(html.Props{Class: "projectui-assignee-name", Text: copy.Unassigned})))
	if ticket.Assignee != "" {
		assignee = assigneeBadge(ticket.Assignee, ticket.AssigneePhoto, copy)
	}
	priority := ui.Node(html.Span(html.Props{Class: "projectui-muted", Text: "—"}))
	if ticket.PriorityID != "" || ticket.PriorityLabel != "" {
		priority = priorityIndicatorFor(ticket.PriorityID, ticket.PriorityLabel, copy)
	}
	due := ui.Node(html.Span(html.Props{Class: "projectui-muted", Text: "—"}))
	if ticket.DueDate != "" {
		due = dueChip(ticket.DueDate, today, copy, tone != "done" && tone != "cancelled")
	}
	updated := html.Span(html.Props{Class: "projectui-muted", Text: ticket.Updated})
	if ticket.UpdateAt != "" {
		updated = html.Time(html.Props{Class: "projectui-muted", Title: ticket.UpdateAt, Raw: map[string]any{"dateTime": ticket.UpdateAt}, Text: ticket.Updated})
	}
	cell := func(col string, children ...ui.Node) ui.Node {
		return html.Span(html.Props{Class: "projectui-tickets-td", Data: map[string]string{"col": col}}, children...)
	}
	return html.WithKey(html.Li(html.Props{Class: "projectui-tickets-item", Data: map[string]string{"task-id": ticket.ID, "tone": tone}},
		html.A(html.Props{Href: ticket.Href, Class: "projectui-tickets-row"},
			cell("key", html.Span(html.Props{Class: "projectui-task-key", Text: ticket.Key})),
			cell("title", title...),
			cell("project", html.Span(html.Props{Class: "projectui-tickets-project", Dir: "auto", Text: ticket.ProjectName})),
			cell("status", html.Span(html.Props{Class: "projectui-status-pill", Data: map[string]string{"tone": tone}}, statusGlyph(tone), html.Span(html.Props{Text: ticket.StatusLabel}))),
			cell("assignee", assignee),
			cell("priority", priority),
			cell("due", due),
			cell("updated", updated),
		)), ticket.ID)
}

// labelChips renders up to limit labels plus a "+n" chip, and the story
// points badge, compactly.
func labelChips(labels []string, points int, copy Copy, limit int) ui.Node {
	chips := []ui.Node{}
	for index, label := range labels {
		if index == limit {
			chips = append(chips, html.Span(html.Props{Class: "projectui-label projectui-label-more", Text: "+" + copy.FormatNumber(len(labels)-limit)}))
			break
		}
		chips = append(chips, html.Span(html.Props{Class: "projectui-label", Dir: "auto", Data: map[string]string{"hue": strconv.Itoa(hueBucket(label))}, Text: label}))
	}
	if points > 0 {
		chips = append(chips, html.Span(html.Props{Class: "projectui-points", Title: copy.Board.StoryPoints, Aria: map[string]string{"label": copy.Board.StoryPoints + ": " + copy.FormatNumber(points)}, Text: copy.Board.Points(points)}))
	}
	return html.Span(html.Props{Class: "projectui-labels"}, chips...)
}
