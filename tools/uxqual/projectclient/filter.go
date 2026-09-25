package projectclient

import (
	"sort"
	"strings"
	"time"
)

// BoardFilter is the board's quick filter as it travels in the address bar
// ("filter" key): assignees, priority levels and a due-soon toggle. It only
// narrows what the authorized board already returned; it never widens it.
//
// The canonical text is "assignee:ID,ID;priority:high,urgent;due:soon" with
// each list sorted, so one filter always has one address.
type BoardFilter struct {
	Assignees  []string
	Priorities []string
	DueSoon    bool
}

// FilterUnassigned is the assignee value that matches tasks with nobody.
const FilterUnassigned = "none"

// FilterPriorities are the priority levels a filter may name, highest first.
var FilterPriorities = []string{"urgent", "high", "normal", "low"}

// ParseBoardFilter reads a filter value. Unknown parts and unsafe values are
// dropped, so a hand-edited address degrades to a narrower filter or none.
func ParseBoardFilter(raw string) BoardFilter {
	filter := BoardFilter{}
	for _, part := range strings.Split(raw, ";") {
		kind, values, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok {
			continue
		}
		for _, value := range strings.Split(values, ",") {
			value = strings.TrimSpace(value)
			switch kind {
			case "assignee":
				if value == FilterUnassigned || validOpaqueID(value) {
					filter.Assignees = appendUnique(filter.Assignees, value)
				}
			case "priority":
				for _, level := range FilterPriorities {
					if value == level {
						filter.Priorities = appendUnique(filter.Priorities, value)
					}
				}
			case "due":
				if value == "soon" {
					filter.DueSoon = true
				}
			}
		}
	}
	sort.Strings(filter.Assignees)
	sort.Strings(filter.Priorities)
	return filter
}

// String is the canonical address value; an empty filter is "".
func (f BoardFilter) String() string {
	parts := []string{}
	if len(f.Assignees) > 0 {
		values := append([]string(nil), f.Assignees...)
		sort.Strings(values)
		parts = append(parts, "assignee:"+strings.Join(values, ","))
	}
	if len(f.Priorities) > 0 {
		values := append([]string(nil), f.Priorities...)
		sort.Strings(values)
		parts = append(parts, "priority:"+strings.Join(values, ","))
	}
	if f.DueSoon {
		parts = append(parts, "due:soon")
	}
	return strings.Join(parts, ";")
}

// Empty reports whether the filter narrows nothing.
func (f BoardFilter) Empty() bool {
	return len(f.Assignees) == 0 && len(f.Priorities) == 0 && !f.DueSoon
}

// HasAssignee and HasPriority report whether a value is selected.
func (f BoardFilter) HasAssignee(id string) bool    { return contains(f.Assignees, id) }
func (f BoardFilter) HasPriority(level string) bool { return contains(f.Priorities, level) }

// ToggleAssignee, TogglePriority and ToggleDueSoon return a copy with one
// value flipped; the receiver is unchanged.
func (f BoardFilter) ToggleAssignee(id string) BoardFilter {
	f.Assignees = toggle(f.Assignees, id)
	return f
}

func (f BoardFilter) TogglePriority(level string) BoardFilter {
	f.Priorities = toggle(f.Priorities, level)
	return f
}

func (f BoardFilter) ToggleDueSoon() BoardFilter {
	f.DueSoon = !f.DueSoon
	return f
}

// FilterTask is what a filter can see of one task.
type FilterTask struct {
	Title, Summary string
	AssigneeID     string
	// PriorityLevel is "urgent", "high", "normal", "low" or "".
	PriorityLevel string
	// DueDate is YYYY-MM-DD, possibly with a trailing time, or "".
	DueDate string
}

// DueSoonDays is how far ahead "due soon" looks; overdue always counts.
const DueSoonDays = 3

// Matches reports whether the task passes the filter and the title search.
// Each selected group narrows (AND); values inside a group widen (OR).
func (f BoardFilter) Matches(task FilterTask, query string, today time.Time) bool {
	if len(f.Assignees) > 0 {
		id := task.AssigneeID
		if id == "" {
			id = FilterUnassigned
		}
		if !contains(f.Assignees, id) {
			return false
		}
	}
	if len(f.Priorities) > 0 && !contains(f.Priorities, task.PriorityLevel) {
		return false
	}
	if f.DueSoon {
		raw := task.DueDate
		if len(raw) > 10 {
			raw = raw[:10]
		}
		due, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return false
		}
		day := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
		if due.Sub(day) > DueSoonDays*24*time.Hour {
			return false
		}
	}
	if terms := strings.Fields(strings.ToLower(query)); len(terms) > 0 {
		haystack := strings.ToLower(task.Title + " " + task.Summary)
		for _, term := range terms {
			if !strings.Contains(haystack, term) {
				return false
			}
		}
	}
	return true
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func appendUnique(values []string, value string) []string {
	if contains(values, value) {
		return values
	}
	return append(values, value)
}

func toggle(values []string, value string) []string {
	out := make([]string, 0, len(values)+1)
	found := false
	for _, candidate := range values {
		if candidate == value {
			found = true
			continue
		}
		out = append(out, candidate)
	}
	if !found {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
