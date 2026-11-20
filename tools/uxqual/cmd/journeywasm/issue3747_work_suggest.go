package main

import (
	"sort"
	"strings"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
)

// Composer pickers for tickets and boards (issue 3747). The project service
// only returns projects and tickets the viewer can read, so everything here
// is a projection of readable rows; archived work is left out.

const (
	workSuggestLimit = 8
	// workTicketProjectCap bounds the fan-out of one ticket search: the
	// first projects by name are searched, one request each.
	workTicketProjectCap = 6
	workTicketPageSize   = 40
)

func workProjectOpen(project *projectv1.Project) bool {
	return project != nil && project.GetProjectId() != "" && strings.TrimSpace(project.GetName()) != "" &&
		project.GetLifecycle() == projectv1.ProjectLifecycle_PROJECT_LIFECYCLE_ACTIVE
}

// workTicketProjects is the projects one ticket search visits.
func workTicketProjects(projects []*projectv1.Project) []*projectv1.Project {
	open := make([]*projectv1.Project, 0, len(projects))
	for _, project := range projects {
		if workProjectOpen(project) {
			open = append(open, project)
		}
	}
	sort.SliceStable(open, func(i, j int) bool { return strings.ToLower(open[i].GetName()) < strings.ToLower(open[j].GetName()) })
	if len(open) > workTicketProjectCap {
		open = open[:workTicketProjectCap]
	}
	return open
}

// workSuggestBoards lists boards whose project name contains the query.
func workSuggestBoards(projects []*projectv1.Project, query string, limit int) []chatui.DocSuggestion {
	needle := strings.ToLower(strings.TrimSpace(query))
	open := make([]*projectv1.Project, 0, len(projects))
	for _, project := range projects {
		if workProjectOpen(project) && strings.Contains(strings.ToLower(project.GetName()), needle) {
			open = append(open, project)
		}
	}
	sort.SliceStable(open, func(i, j int) bool { return strings.ToLower(open[i].GetName()) < strings.ToLower(open[j].GetName()) })
	out := []chatui.DocSuggestion{}
	for _, project := range open {
		if len(out) >= limit {
			break
		}
		out = append(out, chatui.WorkSuggestion(chatui.SuggestKindBoard, project.GetProjectId(), "", strings.TrimSpace(project.GetName()), ""))
	}
	return out
}

// workQueryIsKey reports whether the query is shaped like a ticket key
// ("T-AB12"), which the search index does not hold.
func workQueryIsKey(query string) bool {
	q := strings.ToUpper(strings.TrimSpace(query))
	return strings.HasPrefix(q, "T-") && !strings.ContainsAny(q, " \t")
}

// workTicketServerText is the title text sent to the server: keys are matched
// here against the page the server returns, so they are not sent.
func workTicketServerText(query string) string {
	if workQueryIsKey(query) {
		return ""
	}
	return strings.TrimSpace(query)
}

// workSuggestTickets lists one project's tickets whose key or title contains
// the query.
func workSuggestTickets(project *projectv1.Project, tasks []*projectv1.ProjectTask, query string) []chatui.DocSuggestion {
	needle := strings.ToLower(strings.TrimSpace(query))
	keyNeedle := strings.TrimPrefix(needle, "t-")
	out := []chatui.DocSuggestion{}
	for _, task := range tasks {
		if task == nil {
			continue
		}
		title := strings.TrimSpace(task.GetTitle())
		if task.GetTaskId() == "" || title == "" || task.GetArchived() {
			continue
		}
		key := projectui.TaskKey(task.GetTaskId())
		keyHit := keyNeedle != "" && strings.Contains(strings.ToLower(strings.TrimPrefix(key, "T-")), keyNeedle)
		if needle != "" && !keyHit && !strings.Contains(strings.ToLower(title), needle) {
			continue
		}
		out = append(out, chatui.WorkSuggestion(chatui.SuggestKindTicket, project.GetProjectId(), task.GetTaskId(), title, key+" · "+strings.TrimSpace(project.GetName())))
	}
	return out
}
