//go:build js && wasm

package main

import (
	"context"
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
	refs := chatProjectTaskPreviewRefs(chatBrowser.snapshot())
	claims, epoch, shown := chatProjectTaskPreviews.claim(identity, refs, time.Now())
	changed := false
	chatBrowser.mutate(func(model *chatui.Model) { changed = mergeChatProjectTaskPreviews(model, shown) })
	if changed {
		refreshChatRoute()
	}
	if len(claims) == 0 {
		// Issue 3753: wake up when the newest answer on screen goes stale, so a
		// rename, archive, delete or revoked grant reaches a quiet channel.
		if due, ok := chatProjectTaskPreviews.revalidateIn(identity, refs, time.Now()); ok {
			armChatProjectTaskRetry(cfg, due+time.Millisecond)
		}
		return
	}
	armChatProjectTaskRetry(cfg, chatProjectTaskPreviewPendingTimeout+time.Second)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// One batched, viewer-authorized read for every claimed link (#3751).
		answers := resolveChatProjectPreviews(ctx, active, client, claims)
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
