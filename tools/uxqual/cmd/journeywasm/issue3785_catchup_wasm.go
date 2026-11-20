//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var chatCatchUpRead struct {
	sync.Mutex
	generation uint64
}

// requestChatCatchUp asks for the private brief of the open conversation, or of
// one thread, once per press. It only reads: the read position and the
// conversation are untouched. A newer press, another conversation or another
// person leaves an older answer unshown.
func requestChatCatchUp(cfg journeyclient.Config, threadRootID, sinceID string) {
	active := chatBrowser.config(cfg)
	conversation := chatBrowser.selectedID()
	if conversation == "" || active.Tenant == "" || active.Subject == "" {
		return
	}
	chatCatchUpRead.Lock()
	chatCatchUpRead.generation++
	generation := chatCatchUpRead.generation
	chatCatchUpRead.Unlock()
	chatBrowser.mutate(func(model *chatui.Model) {
		model.CatchUp = chatui.CatchUpBrief{Phase: chatui.CatchUpLoading, ConversationID: conversation, ThreadRootID: threadRootID}
	})
	refreshChatRoute()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		reply, err := chatCatchUpRequest(ctx, http.DefaultClient, active, conversation, threadRootID, sinceID)
		ui.PostAsync(func() {
			chatCatchUpRead.Lock()
			current := generation == chatCatchUpRead.generation
			chatCatchUpRead.Unlock()
			if !current {
				return
			}
			chatBrowser.mutate(func(model *chatui.Model) {
				// The page may have moved on while this was out; one person's
				// brief is never shown to another, nor in another conversation.
				if model.CurrentTenantID != active.Tenant || model.CurrentUser != active.Subject || model.SelectedID != conversation {
					model.CatchUp = chatui.CatchUpBrief{}
					return
				}
				if err != nil {
					model.CatchUp = chatCatchUpFailed(conversation, threadRootID, chatCatchUpFailure(err))
					return
				}
				brief := chatCatchUpView(reply)
				brief.ConversationID, brief.ThreadRootID = conversation, threadRootID
				model.CatchUp = brief
			})
			refreshChatRoute()
		})
	}()
}

// dismissChatCatchUp closes the card of one scope and drops its content.
func dismissChatCatchUp(threadRootID string) {
	chatCatchUpRead.Lock()
	chatCatchUpRead.generation++
	chatCatchUpRead.Unlock()
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.CatchUp.ThreadRootID == threadRootID {
			model.CatchUp = chatui.CatchUpBrief{}
		}
	})
	refreshChatRoute()
}
