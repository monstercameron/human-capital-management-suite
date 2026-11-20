//go:build js && wasm

package main

import (
	"context"
	"sync"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// withChatWorkCallbacks gives the composer its "ticket:" and "board:"
// autocomplete over the projects the viewer can read (issue 3747).
func withChatWorkCallbacks(callbacks chatui.Callbacks, cfg journeyclient.Config) chatui.Callbacks {
	run := func(search func(ctx context.Context, client projectv1.ProjectServiceClient, query string) ([]chatui.DocSuggestion, error)) func(string, func([]chatui.DocSuggestion, chatui.DocSuggestStatus)) {
		return func(query string, done func([]chatui.DocSuggestion, chatui.DocSuggestStatus)) {
			client := chatProjects
			active := chatBrowser.config(cfg)
			if client == nil {
				done(nil, chatui.DocSuggestUnavailable)
				return
			}
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				items, err := search(chatRPCContext(ctx, active), client, query)
				cancel()
				status := chatui.DocSuggestOK
				if err != nil {
					items, status = nil, chatui.DocSuggestFailed
				}
				ui.PostAsync(func() { done(items, status) })
			}()
		}
	}
	callbacks.SuggestProjects = run(searchChatBoards)
	callbacks.SuggestTasks = run(searchChatTickets)
	return callbacks
}

func searchChatBoards(ctx context.Context, client projectv1.ProjectServiceClient, query string) ([]chatui.DocSuggestion, error) {
	response, err := client.ListProjects(ctx, &projectv1.ListProjectsRequest{Page: &commonv1.PageRequest{PageSize: 100}})
	if err != nil {
		return nil, err
	}
	return workSuggestBoards(response.GetProjects(), query, workSuggestLimit), nil
}

// searchChatTickets asks each of the first few readable projects for its
// matching tickets. It fails only when every project's search fails, so one
// unreachable project does not hide the others' tickets.
func searchChatTickets(ctx context.Context, client projectv1.ProjectServiceClient, query string) ([]chatui.DocSuggestion, error) {
	response, err := client.ListProjects(ctx, &projectv1.ListProjectsRequest{Page: &commonv1.PageRequest{PageSize: 100}})
	if err != nil {
		return nil, err
	}
	projects := workTicketProjects(response.GetProjects())
	found := make([][]chatui.DocSuggestion, len(projects))
	failures := make([]error, len(projects))
	var wait sync.WaitGroup
	for i, project := range projects {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, searchErr := client.SearchTasks(ctx, &projectv1.SearchTasksRequest{ProjectId: project.GetProjectId(), Text: workTicketServerText(query), Page: &commonv1.PageRequest{PageSize: workTicketPageSize}})
			if searchErr != nil {
				failures[i] = searchErr
				return
			}
			found[i] = workSuggestTickets(project, result.GetTasks(), query)
		}()
	}
	wait.Wait()
	out := []chatui.DocSuggestion{}
	failed := 0
	for i := range projects {
		if failures[i] != nil {
			failed++
		}
		out = append(out, found[i]...)
	}
	if len(projects) > 0 && failed == len(projects) {
		return nil, failures[0]
	}
	if len(out) > workSuggestLimit {
		out = out[:workSuggestLimit]
	}
	return out, nil
}
