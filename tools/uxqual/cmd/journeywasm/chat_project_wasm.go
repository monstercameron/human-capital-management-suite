//go:build js && wasm

package main

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc"
)

var chatProjects projectv1.ProjectServiceClient

func configureChatProjects(conn grpc.ClientConnInterface) {
	chatProjectTaskPreviews.reset()
	chatProjects = projectv1.NewProjectServiceClient(conn)
}

// resolveVisibleChatProjectTasks reads canonical task links as the current
// viewer. Bare task:<id> tokens are intentionally excluded: without a project
// selector there is no authorized lookup route for them.
func resolveVisibleChatProjectTasks(cfg journeyclient.Config) {
	active := chatBrowser.config(cfg)
	if active.Tenant == "" || active.Subject == "" {
		return
	}
	client := chatProjects
	if client == nil {
		armChatProjectTaskRetry(cfg, time.Second)
		return
	}
	identity := active.Tenant + "\x00" + active.Subject
	claims, epoch, shown := chatProjectTaskPreviews.claim(identity, chatProjectTaskPreviewRefs(chatBrowser.snapshot()), time.Now())
	changed := false
	chatBrowser.mutate(func(model *chatui.Model) { changed = mergeChatProjectTaskPreviews(model, shown) })
	if changed {
		refreshChatRoute()
	}
	if len(claims) == 0 {
		return
	}
	armChatProjectTaskRetry(cfg, chatProjectTaskPreviewPendingTimeout+time.Second)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		type result struct {
			key   string
			value chatui.ProjectTaskPreview
		}
		results := make(chan result, len(claims))
		semaphore := make(chan struct{}, 8)
		var workers sync.WaitGroup
		for _, ref := range claims {
			ref := ref
			workers.Add(1)
			go func() {
				defer workers.Done()
				select {
				case semaphore <- struct{}{}:
				case <-ctx.Done():
					results <- result{key: chatui.ProjectTaskPreviewKey(ref.ProjectID, ref.TaskID), value: restrictedChatProjectTaskAnswer(ref)}
					return
				}
				defer func() { <-semaphore }()
				results <- result{key: chatui.ProjectTaskPreviewKey(ref.ProjectID, ref.TaskID), value: resolveChatProjectPreview(ctx, active, client, ref)}
			}()
		}
		workers.Wait()
		close(results)
		answers := make(map[string]chatui.ProjectTaskPreview, len(claims))
		for answer := range results {
			answers[answer.key] = answer.value
		}
		current := chatBrowser.config(cfg)
		if current.Tenant+"\x00"+current.Subject != identity {
			return
		}
		ui.PostAsync(func() {
			if !chatProjectTaskPreviews.finish(identity, epoch, answers, time.Now()) {
				return
			}
			changed := false
			chatBrowser.mutate(func(model *chatui.Model) { changed = mergeChatProjectTaskPreviews(model, answers) })
			if changed {
				refreshChatRoute()
			}
		})
	}()
}

var chatProjectTaskRetryArmed atomic.Bool

func armChatProjectTaskRetry(cfg journeyclient.Config, delay time.Duration) {
	if !chatProjectTaskRetryArmed.CompareAndSwap(false, true) {
		return
	}
	time.AfterFunc(delay, func() {
		ui.PostAsync(func() {
			chatProjectTaskRetryArmed.Store(false)
			resolveVisibleChatProjectTasks(cfg)
		})
	})
}
