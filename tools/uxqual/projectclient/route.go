// Package projectclient contains pure address-bar state for the project UI.
// It does not authorize project or task access; route IDs are only selectors
// passed to the owning service, which must authorize each read independently.
package projectclient

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	ProjectsPath       = "/workspace/app/projects"
	ProjectPath        = "/workspace/app/project"
	maxRouteQueryBytes = 4096
	maxOpaqueIDBytes   = 128
)

var opaqueIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]{0,127}$`)

// Route identifies the project home or one selected project.
type Route uint8

const (
	RouteProjects Route = iota + 1
	RouteProject
)

// View is the selected project presentation. On the board and list a task ID
// opens that task in a modal over the retained board/list context; ViewTask
// is the task's own full page and requires a task ID.
type View string

const (
	ViewBoard View = "board"
	ViewList  View = "list"
	ViewTask  View = "task"
)

// State contains only validated, presentation-level selectors. It never
// carries authorization, task content, or project names.
type State struct {
	Route       Route
	ProjectID   string
	BoardViewID string
	TaskID      string
	View        View
	Lane        string
	Filter      string
	Query       string
	Cursor      string
	// Sort orders the list view by one column; a leading "-" reverses it.
	Sort string
	// Tab selects the Projects home tab: "" (projects) or TabTickets. On
	// the tickets tab Filter is a TicketFilter, Query the search, Sort a
	// ticket column and PageNumber the 1-based page.
	Tab        string
	PageNumber int
	// PageSize is the rows per page of a paged table (the tickets tab or
	// the board's list view): one of PageSizes, or 0 for the default.
	PageSize int
	// Shell carries the product shell's own presentation keys (locale,
	// navigation state, menu filter, favorites) through a project address
	// unchanged, so canonicalizing a project route never resets them.
	Shell url.Values
}

// ShellKeys are the product shell's route keys a project address keeps.
var ShellKeys = []string{"locale", "nav", "menu_q", "favorites"}

// ParseState accepts only the two project routes. Unknown query keys are
// discarded, while repeated or unsafe recognized values are rejected.
func ParseState(pathname, rawQuery string) (State, error) {
	if len(rawQuery) > maxRouteQueryBytes {
		return State{}, errors.New("projectclient: route state is too large")
	}
	state := State{View: ViewBoard}
	switch pathname {
	case ProjectsPath:
		state.Route = RouteProjects
	case ProjectPath:
		state.Route = RouteProject
	default:
		return State{}, errors.New("projectclient: unknown project route")
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return State{}, errors.New("projectclient: route state is malformed")
	}
	allowed := map[string]bool{}
	for _, key := range ShellKeys {
		allowed[key] = true
	}
	if state.Route == RouteProjects {
		for _, key := range []string{"tab", "filter", "q", "sort", "page", "page_size"} {
			allowed[key] = true
		}
	}
	if state.Route == RouteProject {
		for _, key := range []string{"project", "board_view", "task", "view", "lane", "filter", "q", "cursor", "sort", "page", "page_size"} {
			allowed[key] = true
		}
	}
	for key, entries := range values {
		if !allowed[key] {
			values.Del(key)
			continue
		}
		if len(entries) != 1 || !safeValue(entries[0]) {
			return State{}, errors.New("projectclient: route state is malformed")
		}
	}
	for _, key := range ShellKeys {
		if value := strings.TrimSpace(values.Get(key)); value != "" {
			if state.Shell == nil {
				state.Shell = url.Values{}
			}
			state.Shell.Set(key, value)
		}
	}
	if state.Route == RouteProjects {
		if values.Get("tab") == TabTickets {
			state.Tab = TabTickets
			state.Filter = ParseTicketFilter(values.Get("filter")).String()
			state.Query = strings.TrimSpace(values.Get("q"))
			if value := strings.TrimSpace(values.Get("sort")); ValidTicketSort(value) {
				state.Sort = value
			}
			state.PageNumber, state.PageSize = parsePaging(values)
		}
		return state, nil
	}
	state.ProjectID = strings.TrimSpace(values.Get("project"))
	state.BoardViewID = strings.TrimSpace(values.Get("board_view"))
	state.TaskID = strings.TrimSpace(values.Get("task"))
	if !validOpaqueID(state.ProjectID) || state.BoardViewID != "" && !validOpaqueID(state.BoardViewID) || state.TaskID != "" && !validOpaqueID(state.TaskID) {
		return State{}, errors.New("projectclient: route state is malformed")
	}
	if value := values.Get("view"); value != "" {
		state.View = View(value)
		if state.View != ViewBoard && state.View != ViewList && state.View != ViewTask {
			return State{}, errors.New("projectclient: route state is malformed")
		}
	}
	// A task page without a task has nothing to show; it is the board.
	if state.View == ViewTask && state.TaskID == "" {
		state.View = ViewBoard
	}
	state.Lane = strings.TrimSpace(values.Get("lane"))
	// The quick filter is canonicalized: unknown or unsafe parts drop out.
	state.Filter = ParseBoardFilter(values.Get("filter")).String()
	state.Query = strings.TrimSpace(values.Get("q"))
	state.Cursor = strings.TrimSpace(values.Get("cursor"))
	if state.Lane != "" && !validOpaqueID(state.Lane) {
		return State{}, errors.New("projectclient: route state is malformed")
	}
	// An unknown sort is dropped rather than rejected: it is presentation.
	if value := strings.TrimSpace(values.Get("sort")); ValidSort(value) {
		state.Sort = value
	}
	if state.View == ViewList {
		state.PageNumber, state.PageSize = parsePaging(values)
	}
	return state, nil
}

// CanonicalHref serializes only selectors supported by the chosen route.
func CanonicalHref(state State) string {
	if state.Route == RouteProjects {
		shell := shellValues(state.Shell)
		if state.Tab == TabTickets {
			shell.Set("tab", TabTickets)
			if value := ParseTicketFilter(state.Filter).String(); value != "" {
				shell.Set("filter", value)
			}
			if value := canonicalText(state.Query); value != "" {
				shell.Set("q", value)
			}
			if ValidTicketSort(state.Sort) {
				shell.Set("sort", state.Sort)
			}
			setPaging(shell, state)
		}
		if len(shell) > 0 {
			return ProjectsPath + "?" + shell.Encode()
		}
		return ProjectsPath
	}
	if state.Route != RouteProject || !validOpaqueID(state.ProjectID) || state.BoardViewID != "" && !validOpaqueID(state.BoardViewID) || state.TaskID != "" && !validOpaqueID(state.TaskID) {
		return ProjectsPath
	}
	values := shellValues(state.Shell)
	values.Set("project", state.ProjectID)
	if state.BoardViewID != "" {
		values.Set("board_view", state.BoardViewID)
	}
	if state.TaskID != "" {
		values.Set("task", state.TaskID)
	}
	if state.View == ViewList || state.View == ViewTask && state.TaskID != "" {
		values.Set("view", string(state.View))
	}
	if state.Lane != "" && validOpaqueID(state.Lane) {
		values.Set("lane", state.Lane)
	}
	if value := ParseBoardFilter(state.Filter).String(); value != "" {
		values.Set("filter", value)
	}
	if value := canonicalText(state.Query); value != "" {
		values.Set("q", value)
	}
	if value := canonicalText(state.Cursor); value != "" {
		values.Set("cursor", value)
	}
	if state.View == ViewList && ValidSort(state.Sort) {
		values.Set("sort", state.Sort)
	}
	if state.View == ViewList {
		setPaging(values, state)
	}
	return ProjectPath + "?" + values.Encode()
}

// SortKeys are the list columns a project list can be ordered by.
var SortKeys = []string{"task", "status", "priority", "assignee", "due"}

// ValidSort reports whether value is a sort key, optionally prefixed with
// "-" for descending order.
func ValidSort(value string) bool {
	key := strings.TrimPrefix(value, "-")
	for _, candidate := range SortKeys {
		if key == candidate {
			return true
		}
	}
	return false
}

// shellValues copies only recognized, safe shell keys.
func shellValues(shell url.Values) url.Values {
	out := url.Values{}
	for _, key := range ShellKeys {
		if value := canonicalText(shell.Get(key)); value != "" {
			out.Set(key, value)
		}
	}
	return out
}

func validOpaqueID(value string) bool {
	return len(value) <= maxOpaqueIDBytes && opaqueIDPattern.MatchString(value)
}

func safeValue(value string) bool {
	return len(value) <= maxRouteQueryBytes && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}

func canonicalText(value string) string {
	if !safeValue(value) {
		return ""
	}
	return strings.TrimSpace(value)
}

// PageSizes are the rows-per-page choices of a paged table.
var PageSizes = []int{10, 25, 50, 100}

// DefaultPageSize is used when the address names none.
const DefaultPageSize = 25

// ValidPageSize reports whether n is one of PageSizes.
func ValidPageSize(n int) bool {
	for _, size := range PageSizes {
		if n == size {
			return true
		}
	}
	return false
}

func parsePaging(values url.Values) (page, size int) {
	if n, err := strconv.Atoi(values.Get("page")); err == nil && n > 1 && n <= 10000 {
		page = n
	}
	if n, err := strconv.Atoi(values.Get("page_size")); err == nil && ValidPageSize(n) {
		size = n
	}
	return page, size
}

func setPaging(values url.Values, state State) {
	if state.PageNumber > 1 {
		values.Set("page", strconv.Itoa(state.PageNumber))
	}
	if ValidPageSize(state.PageSize) {
		values.Set("page_size", strconv.Itoa(state.PageSize))
	}
}
