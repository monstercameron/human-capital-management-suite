//go:build js && wasm

package main

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	projectclient "github.com/monstercameron/human-capital-management-suite/tools/uxqual/projectclient"
)

// The tickets tab lists every task across the projects the viewer can see.
// There is no cross-project search RPC, so it reuses the per-project reads
// the home progress already fans out (ListTasks plus the workflow), cached
// per project; while any project is still loading the list shows what it
// has and a quiet loading state, and the route revalidates when the batch
// settles.

var projectPriorityRank = map[string]int{"low": 1, "normal": 2, "high": 3, "urgent": 4}

type projectTicketRow struct {
	ticket   projectui.Ticket
	filter   projectclient.Ticket
	category string
	order    int
	level    string
	updated  time.Time
}

func loadProjectTickets(ctx context.Context, cfg journeyclient.Config, service projectv1.ProjectServiceClient, state projectclient.State, projects []*projectv1.Project) projectui.TicketList {
	names, photos := projectDirectory(ctx, cfg)
	filter := projectclient.ParseTicketFilter(state.Filter)
	today := time.Now()
	list := projectui.TicketList{Sort: state.Sort, Today: today, Viewer: cfg.Subject}
	if list.Sort == "" {
		list.Sort = "-updated"
	}
	rows := []projectTicketRow{}
	projectNames := map[string]string{}
	people := map[string]string{}
	labels := map[string]string{}
	unassigned := false
	for index, project := range projects {
		if project == nil || project.GetProjectId() == "" || index >= projectProgressLimit {
			continue
		}
		projectID := project.GetProjectId()
		projectNames[projectID] = project.GetName()
		progress, ok := projectProgressFor(cfg, service, projectID)
		if !ok || progress.tasks == nil && progress.known && progress.total > 0 {
			list.Loading = true
			continue
		}
		for _, task := range progress.tasks {
			if task == nil || task.GetTaskId() == "" {
				continue
			}
			status := progress.statuses[task.GetStatusId()]
			if status.category == "" {
				status = projectTicketStatus{name: task.GetStatusId(), category: projectclient.CategoryActive, order: 1}
			}
			level := strings.ToLower(strings.TrimPrefix(task.GetPriority().String(), "TASK_PRIORITY_"))
			if projectPriorityRank[level] == 0 {
				level = ""
			}
			assignee := task.GetAssigneeId()
			assigneeName := ""
			if assignee != "" {
				assigneeName = projectclient.PersonName(assignee, names)
				people[assignee] = assigneeName
			} else {
				unassigned = true
			}
			for _, label := range task.GetLabels() {
				labels[strings.ToLower(label)] = label
			}
			tone := "active"
			switch status.category {
			case projectclient.CategoryTodo:
				tone = "todo"
			case projectclient.CategoryDone:
				tone = "done"
			}
			updated := time.Time{}
			updatedText := ""
			if stamp := task.GetUpdatedAt(); stamp != nil && stamp.IsValid() && stamp.AsTime().Unix() > 0 {
				updated = stamp.AsTime()
				updatedText = updated.UTC().Format(time.RFC3339)
			}
			key := projectui.TaskKey(task.GetTaskId())
			links := projectTaskWorkflows(cfg, service, projectID, task.GetTaskId())
			linkedWorkflow := links.workflows
			if links.at.IsZero() && filter.Workflow {
				// The filter needs every ticket's links; until they are all
				// read the list says it is still loading.
				list.Loading = true
			}
			workflows := []string(nil)
			if linkedWorkflow {
				workflows = []string{projectWorkflowPromotion}
			}
			rows = append(rows, projectTicketRow{
				ticket: projectui.Ticket{
					ID: task.GetTaskId(), Key: key, Title: task.GetTitle(),
					Href:      projectclient.CanonicalHref(projectclient.State{Route: projectclient.RouteProject, ProjectID: projectID, TaskID: task.GetTaskId(), BoardViewID: "default", View: projectclient.ViewTask, Shell: state.Shell}),
					ProjectID: projectID, ProjectName: project.GetName(),
					StatusLabel: status.name, StatusTone: tone,
					AssigneeID: assignee, Assignee: assigneeName, AssigneePhoto: photos[assignee],
					PriorityID: task.GetPriority().String(), DueDate: task.GetDueDate(), UpdateAt: updatedText,
					Labels: append([]string(nil), task.GetLabels()...), StoryPoints: int(task.GetStoryPoints()), Workflows: workflows,
				},
				filter:   projectclient.Ticket{ProjectID: projectID, Title: task.GetTitle(), Key: key, AssigneeID: assignee, Category: status.category, PriorityLevel: level, DueDate: task.GetDueDate(), Labels: task.GetLabels(), HasWorkflow: linkedWorkflow},
				category: status.category, order: status.order, level: level, updated: updated,
			})
		}
	}
	list.Total = len(rows)
	// The address's filters narrow the rows here; the search box and paging
	// run in the page over what is left, so typing is instant.
	matched := rows[:0:0]
	for _, row := range rows {
		if filter.Matches(row.filter, "", today) {
			matched = append(matched, row)
		}
	}
	sortProjectTickets(matched, list.Sort)
	for _, row := range matched {
		list.Tickets = append(list.Tickets, row.ticket)
	}

	// Addresses: every filter, sort and page link is the current address
	// with one thing changed, and any change returns to page one.
	href := func(next projectclient.TicketFilter, query, sortKey string, page int) string {
		target := state
		target.Filter, target.Query, target.Sort, target.PageNumber = next.String(), query, sortKey, page
		return projectclient.CanonicalHref(target)
	}
	list.ClearHref = href(projectclient.TicketFilter{}, "", state.Sort, 0)
	list.PageSize = state.PageSize
	if list.PageSize == 0 {
		list.PageSize = projectclient.DefaultPageSize
	}
	for _, size := range projectclient.PageSizes {
		target := state
		target.PageNumber, target.PageSize = 0, size
		list.Sizes = append(list.Sizes, projectui.FilterChoice{ID: strconv.Itoa(size), Label: strconv.Itoa(size), Href: projectclient.CanonicalHref(target), Active: size == list.PageSize})
	}
	list.SortHrefs = map[string]string{}
	for _, key := range projectclient.TicketSortKeys {
		next := key
		if list.Sort == key {
			next = "-" + key
		}
		list.SortHrefs[key] = href(filter, state.Query, next, 0)
	}
	if cfg.Subject != "" {
		list.Mine = projectui.FilterChoice{ID: cfg.Subject, Href: href(filter.Toggle("assignee", cfg.Subject), state.Query, state.Sort, 0), Active: filter.Has("assignee", cfg.Subject)}
	}
	choice := func(kind, id, label string) projectui.FilterChoice {
		return projectui.FilterChoice{ID: id, Label: label, Href: href(filter.Toggle(kind, id), state.Query, state.Sort, 0), Active: filter.Has(kind, id)}
	}
	group := func(key string, choices []projectui.FilterChoice) projectui.TicketFilterGroup {
		return projectui.TicketFilterGroup{Key: key, Choices: choices}
	}
	projectChoices := []projectui.FilterChoice{}
	for _, project := range projects {
		if project != nil && projectNames[project.GetProjectId()] != "" {
			projectChoices = append(projectChoices, choice("project", project.GetProjectId(), project.GetName()))
		}
	}
	sort.SliceStable(projectChoices, func(i, j int) bool {
		return strings.ToLower(projectChoices[i].Label) < strings.ToLower(projectChoices[j].Label)
	})
	statusChoices := []projectui.FilterChoice{choice("status", projectclient.CategoryTodo, ""), choice("status", projectclient.CategoryActive, ""), choice("status", projectclient.CategoryDone, "")}
	peopleChoices := []projectui.FilterChoice{}
	for id, name := range people {
		peopleChoices = append(peopleChoices, choice("assignee", id, name))
	}
	sort.SliceStable(peopleChoices, func(i, j int) bool {
		return strings.ToLower(peopleChoices[i].Label) < strings.ToLower(peopleChoices[j].Label)
	})
	if unassigned {
		peopleChoices = append(peopleChoices, choice("assignee", projectclient.FilterUnassigned, ""))
	}
	priorityChoices := []projectui.FilterChoice{}
	for _, level := range projectclient.FilterPriorities {
		priorityChoices = append(priorityChoices, choice("priority", level, level))
	}
	dueChoices := []projectui.FilterChoice{choice("due", projectclient.DueOverdue, ""), choice("due", projectclient.DueThisWeek, "")}
	labelChoices := []projectui.FilterChoice{}
	for key, label := range labels {
		labelChoices = append(labelChoices, choice("label", key, label))
	}
	sort.SliceStable(labelChoices, func(i, j int) bool { return labelChoices[i].ID < labelChoices[j].ID })
	list.Groups = []projectui.TicketFilterGroup{
		group("project", projectChoices), group("status", statusChoices), group("assignee", peopleChoices),
		group("priority", priorityChoices), group("due", dueChoices), group("label", labelChoices),
		group("linked", []projectui.FilterChoice{choice("linked", "workflow", "")}),
	}
	list.ActiveCount = len(filter.Projects) + len(filter.Statuses) + len(filter.Assignees) + len(filter.Priorities) + len(filter.Labels)
	if filter.Workflow {
		list.ActiveCount++
	}
	if filter.Due != "" {
		list.ActiveCount++
	}
	return list
}

// sortProjectTickets orders tickets by one column; empty values sort last
// either way, and the key breaks ties so pages never reshuffle.
func sortProjectTickets(rows []projectTicketRow, order string) {
	key := strings.TrimPrefix(order, "-")
	descending := strings.HasPrefix(order, "-")
	categoryRank := map[string]int{projectclient.CategoryTodo: 0, projectclient.CategoryActive: 1, projectclient.CategoryDone: 2}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		cmp, emptyA, emptyB := 0, false, false
		switch key {
		case "key":
			cmp = strings.Compare(a.ticket.Key, b.ticket.Key)
		case "title":
			cmp = strings.Compare(strings.ToLower(a.ticket.Title), strings.ToLower(b.ticket.Title))
		case "project":
			cmp = strings.Compare(strings.ToLower(a.ticket.ProjectName), strings.ToLower(b.ticket.ProjectName))
		case "status":
			cmp = categoryRank[a.category] - categoryRank[b.category]
			if cmp == 0 {
				cmp = a.order - b.order
			}
		case "assignee":
			emptyA, emptyB = a.ticket.Assignee == "", b.ticket.Assignee == ""
			cmp = strings.Compare(strings.ToLower(a.ticket.Assignee), strings.ToLower(b.ticket.Assignee))
		case "priority":
			cmp = projectPriorityRank[a.level] - projectPriorityRank[b.level]
		case "due":
			emptyA, emptyB = a.ticket.DueDate == "", b.ticket.DueDate == ""
			cmp = strings.Compare(a.ticket.DueDate, b.ticket.DueDate)
		case "updated":
			emptyA, emptyB = a.updated.IsZero(), b.updated.IsZero()
			switch {
			case a.updated.Before(b.updated):
				cmp = -1
			case a.updated.After(b.updated):
				cmp = 1
			}
		}
		if emptyA != emptyB {
			return emptyB
		}
		if cmp == 0 {
			return a.ticket.Key < b.ticket.Key
		}
		if descending {
			return cmp > 0
		}
		return cmp < 0
	})
}
