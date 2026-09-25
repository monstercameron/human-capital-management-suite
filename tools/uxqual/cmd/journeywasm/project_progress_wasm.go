//go:build js && wasm

package main

import (
	"context"
	"errors"
	"sync"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	projectclient "github.com/monstercameron/human-capital-management-suite/tools/uxqual/projectclient"
)

// Projects home progress. The home list renders at once from ListProjects;
// each row's done/total counts are read afterwards, one project per
// goroutine, from the authorized ListTasks and GetWorkflowConfiguration
// calls, cached briefly per tenant, subject and project. When a batch of
// reads settles the route revalidates, and the rows fill in.

const (
	// projectProgressLimit caps the fan-out: at most this many projects
	// are counted per home load.
	projectProgressLimit = 50
	// projectProgressTTL keeps counts fresh after board edits elsewhere.
	projectProgressTTL = 60 * time.Second
	// projectProgressPages prevents an unbounded read if a provider keeps
	// returning cursors. Reaching it is an error, never a partial count.
	projectProgressPages = 1000
)

type projectProgress struct {
	total, done int
	known       bool
	at          time.Time
	// tasks and statuses feed the tickets tab from the same reads.
	tasks    []*projectv1.ProjectTask
	statuses map[string]projectTicketStatus
}

// projectTicketStatus is a workflow status's name and category.
type projectTicketStatus struct {
	name, category string
	order          int
}

var (
	projectRevalidate      func()
	projectProgressMu      sync.Mutex
	projectProgressCache   = map[string]projectProgress{}
	projectProgressLoading = map[string]bool{}
	projectProgressWaiting int
)

// projectProgressFor returns cached counts for the project, or starts a read
// and reports false so the row shows its placeholder.
func projectProgressFor(cfg journeyclient.Config, service projectv1.ProjectServiceClient, projectID string) (projectProgress, bool) {
	key := projectActionKey(cfg.Tenant, cfg.Subject, projectID, "")
	projectProgressMu.Lock()
	defer projectProgressMu.Unlock()
	cached, ok := projectProgressCache[key]
	if ok && time.Since(cached.at) < projectProgressTTL {
		return cached, true
	}
	if !projectProgressLoading[key] {
		projectProgressLoading[key] = true
		projectProgressWaiting++
		go readProjectProgress(cfg, service, projectID, key)
	}
	// A stale count is still better than a placeholder while it refreshes.
	return cached, ok
}

func readProjectProgress(cfg journeyclient.Config, service projectv1.ProjectServiceClient, projectID, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result := projectProgress{at: time.Now()}
	if counted, err := countProjectProgress(ctx, cfg, service, projectID); err == nil {
		result = counted
	}
	projectProgressMu.Lock()
	projectProgressCache[key] = result
	delete(projectProgressLoading, key)
	projectProgressWaiting--
	settled := projectProgressWaiting == 0
	projectProgressMu.Unlock()
	// One revalidate per settled batch, while a projects page is shown.
	if path := currentPath(); settled && projectRevalidate != nil && (path == projectclient.ProjectsPath || path == projectclient.ProjectPath) {
		projectRevalidate()
	}
}

// countProjectProgress counts live tasks and those whose workflow status is
// explicitly categorized as done, including statuses that allow reopening.
func countProjectProgress(ctx context.Context, cfg journeyclient.Config, service projectv1.ProjectServiceClient, projectID string) (projectProgress, error) {
	callCtx := chatRPCContext(ctx, cfg)
	configuration, err := service.GetWorkflowConfiguration(callCtx, &projectv1.GetWorkflowConfigurationRequest{ProjectId: projectID})
	if err != nil {
		return projectProgress{}, err
	}
	statuses := map[string]projectTicketStatus{}
	for index, status := range configuration.GetConfiguration().GetStatuses() {
		if status == nil || status.GetStatusId() == "" {
			continue
		}
		category := projectclient.StatusCategory(status, index)
		name := status.GetName()
		if name == "" {
			name = status.GetStatusId()
		}
		statuses[status.GetStatusId()] = projectTicketStatus{name: name, category: category, order: index}
	}
	result := projectProgress{known: true, at: time.Now(), statuses: statuses}
	cursor := ""
	for page := 0; page < projectProgressPages; page++ {
		response, err := service.ListTasks(callCtx, &projectv1.ListTasksRequest{ProjectId: projectID, Page: &commonv1.PageRequest{PageSize: 100, Cursor: cursor}})
		if err != nil {
			return projectProgress{}, err
		}
		for _, task := range response.GetTasks() {
			if task == nil || task.GetArchived() {
				continue
			}
			result.total++
			result.tasks = append(result.tasks, task)
			if statuses[task.GetStatusId()].category == projectclient.CategoryDone {
				result.done++
			}
		}
		cursor = response.GetPage().GetNextCursor()
		if cursor == "" {
			break
		}
		if page == projectProgressPages-1 {
			return projectProgress{}, errors.New("project progress exceeds task page limit")
		}
	}
	return result, nil
}

// projectProgressForget drops a project's cached counts and tasks after a
// write, so the home, the tickets tab and board cards read them again.
func projectProgressForget(cfg journeyclient.Config, projectID string) {
	key := projectActionKey(cfg.Tenant, cfg.Subject, projectID, "")
	projectProgressMu.Lock()
	delete(projectProgressCache, key)
	projectProgressMu.Unlock()
}
