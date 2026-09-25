//go:build js && wasm

package main

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
	projectclient "github.com/monstercameron/human-capital-management-suite/tools/uxqual/projectclient"
)

// applyProjectFilter marks the cards the address's quick filter hides and
// builds the filter bar: one link per choice, each the current address with
// that choice flipped. It narrows the authorized board; it never widens it.
func applyProjectFilter(board *projectui.Model, state projectclient.State, assignees, photos map[string]string) {
	filter := projectclient.ParseBoardFilter(state.Filter)
	board.RouteFilter, board.RouteQuery = filter.String(), state.Query
	base := state
	base.TaskID, base.Cursor = "", ""
	href := func(next projectclient.BoardFilter, query string) string {
		target := base
		target.Filter, target.Query = next.String(), query
		return projectclient.CanonicalHref(target)
	}
	today := time.Now()
	people := map[string]bool{}
	unassigned := false
	for index := range board.Cards {
		card := &board.Cards[index]
		assignee := assignees[card.ID]
		if assignee == "" {
			unassigned = true
		} else {
			people[assignee] = true
		}
		level := strings.ToLower(strings.TrimPrefix(card.PriorityID, "TASK_PRIORITY_"))
		card.FilteredOut = !filter.Matches(projectclient.FilterTask{Title: card.Title, Summary: card.Summary, AssigneeID: assignee, PriorityLevel: level, DueDate: card.DueDate}, state.Query, today)
	}
	bar := &projectui.FilterBar{Query: state.Query, SearchHref: href(filter, ""), ClearHref: href(projectclient.BoardFilter{}, "")}
	// People who hold a card on this board, plus any already selected, by
	// name; the members list supplies names and photos.
	names, memberPhotos := map[string]string{}, map[string]string{}
	for _, member := range board.Members {
		names[member.ID], memberPhotos[member.ID] = member.Name, member.PhotoURL
	}
	for _, id := range filter.Assignees {
		if id != projectclient.FilterUnassigned {
			people[id] = true
		}
	}
	for index := range board.Cards {
		if name := board.Cards[index].Assignee; name != "" {
			if id := assignees[board.Cards[index].ID]; id != "" && names[id] == "" {
				names[id] = name
			}
		}
	}
	ids := make([]string, 0, len(people))
	for id := range people {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return strings.ToLower(names[ids[i]]+ids[i]) < strings.ToLower(names[ids[j]]+ids[j])
	})
	for _, id := range ids {
		label := names[id]
		if label == "" {
			label = id
		}
		photo := photos[id]
		if photo == "" {
			photo = memberPhotos[id]
		}
		bar.Assignees = append(bar.Assignees, projectui.FilterChoice{ID: id, Label: label, Photo: photo, Href: href(filter.ToggleAssignee(id), state.Query), Active: filter.HasAssignee(id)})
	}
	if unassigned || filter.HasAssignee(projectclient.FilterUnassigned) {
		bar.Assignees = append(bar.Assignees, projectui.FilterChoice{ID: projectclient.FilterUnassigned, Href: href(filter.ToggleAssignee(projectclient.FilterUnassigned), state.Query), Active: filter.HasAssignee(projectclient.FilterUnassigned)})
	}
	for _, level := range projectclient.FilterPriorities {
		bar.Priorities = append(bar.Priorities, projectui.FilterChoice{ID: level, Label: level, Href: href(filter.TogglePriority(level), state.Query), Active: filter.HasPriority(level)})
	}
	bar.DueSoon = projectui.FilterChoice{ID: "due-soon", Href: href(filter.ToggleDueSoon(), state.Query), Active: filter.DueSoon}
	bar.ActiveCount = len(filter.Assignees) + len(filter.Priorities)
	if filter.DueSoon {
		bar.ActiveCount++
	}
	if strings.TrimSpace(state.Query) != "" {
		bar.ActiveCount++
	}
	board.Filters = bar
}

// projectListPager pages the list view over the cards the filter keeps;
// every link is the current address with one thing changed.
func projectListPager(board projectui.Model, state projectclient.State) *projectui.Pager {
	shown := 0
	for _, card := range board.Cards {
		if !card.FilteredOut {
			shown++
		}
	}
	size := state.PageSize
	if size == 0 {
		size = projectclient.DefaultPageSize
	}
	pager := &projectui.Pager{Total: shown, Pages: max(1, (shown+size-1)/size), SizeParam: state.PageSize}
	pager.Page = min(max(1, state.PageNumber), pager.Pages)
	pager.From = (pager.Page-1)*size + 1
	pager.To = min(pager.Page*size, shown)
	if shown == 0 {
		pager.From, pager.To = 0, 0
	}
	href := func(page, pageSize int) string {
		target := state
		target.TaskID, target.PageNumber, target.PageSize = "", page, pageSize
		return projectclient.CanonicalHref(target)
	}
	if pager.Page > 1 {
		pager.PrevHref = href(pager.Page-1, state.PageSize)
	}
	if pager.Page < pager.Pages {
		pager.NextHref = href(pager.Page+1, state.PageSize)
	}
	for _, option := range projectclient.PageSizes {
		pager.Sizes = append(pager.Sizes, projectui.FilterChoice{ID: strconv.Itoa(option), Label: strconv.Itoa(option), Href: href(0, option), Active: option == size})
	}
	return pager
}
