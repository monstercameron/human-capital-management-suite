package projectclient

import (
	"sort"
	"strings"
	"time"
)

// TabTickets is the Projects home tab that lists tickets across projects.
const TabTickets = "tickets"

// Ticket status categories: every workflow status falls into one.
const (
	CategoryTodo   = "todo"
	CategoryActive = "active"
	CategoryDone   = "done"
)

// Ticket due windows.
const (
	DueOverdue  = "overdue"
	DueThisWeek = "week"
)

// TicketSortKeys are the sortable ticket columns; a leading "-" reverses.
var TicketSortKeys = []string{"key", "title", "project", "status", "assignee", "priority", "due", "updated"}

// ValidTicketSort reports whether value names a ticket column.
func ValidTicketSort(value string) bool {
	return contains(TicketSortKeys, strings.TrimPrefix(value, "-"))
}

// TicketFilter narrows the cross-project ticket list. It travels in the
// "filter" key of the tickets tab as
// "project:ID;status:done;assignee:ID,none;priority:high;due:overdue;label:x".
type TicketFilter struct {
	Projects, Statuses, Assignees, Priorities, Labels []string
	Due                                               string
	// Workflow keeps only tickets linked to a workflow item.
	Workflow bool
}

// ParseTicketFilter reads a filter value, dropping unknown or unsafe parts.
func ParseTicketFilter(raw string) TicketFilter {
	filter := TicketFilter{}
	for _, part := range strings.Split(raw, ";") {
		kind, values, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok {
			continue
		}
		for _, value := range strings.Split(values, ",") {
			value = strings.TrimSpace(value)
			switch kind {
			case "project":
				if validOpaqueID(value) {
					filter.Projects = appendUnique(filter.Projects, value)
				}
			case "status":
				if value == CategoryTodo || value == CategoryActive || value == CategoryDone {
					filter.Statuses = appendUnique(filter.Statuses, value)
				}
			case "assignee":
				if value == FilterUnassigned || validOpaqueID(value) {
					filter.Assignees = appendUnique(filter.Assignees, value)
				}
			case "priority":
				if contains(FilterPriorities, value) {
					filter.Priorities = appendUnique(filter.Priorities, value)
				}
			case "due":
				if value == DueOverdue || value == DueThisWeek {
					filter.Due = value
				}
			case "linked":
				if value == "workflow" {
					filter.Workflow = true
				}
			case "label":
				if label := strings.ToLower(value); label != "" && len(label) <= 40 && safeValue(label) && !strings.ContainsAny(label, ";,") {
					filter.Labels = appendUnique(filter.Labels, label)
				}
			}
		}
	}
	for _, list := range [][]string{filter.Projects, filter.Statuses, filter.Assignees, filter.Priorities, filter.Labels} {
		sort.Strings(list)
	}
	return filter
}

// String is the canonical address value; an empty filter is "".
func (f TicketFilter) String() string {
	parts := []string{}
	add := func(kind string, values []string) {
		if len(values) == 0 {
			return
		}
		sorted := append([]string(nil), values...)
		sort.Strings(sorted)
		parts = append(parts, kind+":"+strings.Join(sorted, ","))
	}
	add("project", f.Projects)
	add("status", f.Statuses)
	add("assignee", f.Assignees)
	add("priority", f.Priorities)
	if f.Due != "" {
		parts = append(parts, "due:"+f.Due)
	}
	add("label", f.Labels)
	if f.Workflow {
		parts = append(parts, "linked:workflow")
	}
	return strings.Join(parts, ";")
}

// Empty reports whether the filter narrows nothing.
func (f TicketFilter) Empty() bool { return f.String() == "" }

// Toggle returns a copy with one value of one kind flipped. Due is a single
// choice: toggling the current window clears it.
func (f TicketFilter) Toggle(kind, value string) TicketFilter {
	switch kind {
	case "project":
		f.Projects = toggle(f.Projects, value)
	case "status":
		f.Statuses = toggle(f.Statuses, value)
	case "assignee":
		f.Assignees = toggle(f.Assignees, value)
	case "priority":
		f.Priorities = toggle(f.Priorities, value)
	case "label":
		f.Labels = toggle(f.Labels, strings.ToLower(value))
	case "due":
		if f.Due == value {
			f.Due = ""
		} else {
			f.Due = value
		}
	case "linked":
		f.Workflow = !f.Workflow
	}
	return f
}

// Has reports whether a value of a kind is selected.
func (f TicketFilter) Has(kind, value string) bool {
	switch kind {
	case "project":
		return contains(f.Projects, value)
	case "status":
		return contains(f.Statuses, value)
	case "assignee":
		return contains(f.Assignees, value)
	case "priority":
		return contains(f.Priorities, value)
	case "label":
		return contains(f.Labels, strings.ToLower(value))
	case "due":
		return f.Due == value
	case "linked":
		return f.Workflow
	}
	return false
}

// Ticket is what the ticket filter sees of one task.
type Ticket struct {
	ProjectID, Title, Key, AssigneeID string
	Category, PriorityLevel, DueDate  string
	Labels                            []string
	// HasWorkflow is true when the ticket links a workflow item.
	HasWorkflow bool
}

// Matches reports whether a ticket passes the filter and the search. The
// search matches the title, the key and labels.
func (f TicketFilter) Matches(ticket Ticket, query string, today time.Time) bool {
	if f.Workflow && !ticket.HasWorkflow {
		return false
	}
	if len(f.Projects) > 0 && !contains(f.Projects, ticket.ProjectID) {
		return false
	}
	if len(f.Statuses) > 0 && !contains(f.Statuses, ticket.Category) {
		return false
	}
	if len(f.Assignees) > 0 {
		id := ticket.AssigneeID
		if id == "" {
			id = FilterUnassigned
		}
		if !contains(f.Assignees, id) {
			return false
		}
	}
	if len(f.Priorities) > 0 && !contains(f.Priorities, ticket.PriorityLevel) {
		return false
	}
	if len(f.Labels) > 0 {
		found := false
		for _, label := range ticket.Labels {
			if contains(f.Labels, strings.ToLower(label)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if f.Due != "" {
		raw := ticket.DueDate
		if len(raw) > 10 {
			raw = raw[:10]
		}
		due, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return false
		}
		day := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
		switch f.Due {
		case DueOverdue:
			if !due.Before(day) || ticket.Category == CategoryDone {
				return false
			}
		case DueThisWeek:
			if due.Before(day) || due.Sub(day) >= 7*24*time.Hour {
				return false
			}
		}
	}
	if terms := strings.Fields(strings.ToLower(query)); len(terms) > 0 {
		haystack := strings.ToLower(ticket.Title + " " + ticket.Key + " " + strings.Join(ticket.Labels, " "))
		for _, term := range terms {
			if !strings.Contains(haystack, term) {
				return false
			}
		}
	}
	return true
}
