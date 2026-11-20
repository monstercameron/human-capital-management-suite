//go:build js && wasm

package main

import (
	"context"
	"sync"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

const chatAttentionPageSize = 100

var chatAttentionRead struct {
	sync.Mutex
	pending bool
}

// requestChatAttention reads the viewer's own work once, as the viewer, and
// shows it in the private brief. It only reads: no work item is changed.
func requestChatAttention(cfg journeyclient.Config) {
	active := chatBrowser.config(cfg)
	client := chatWorkers
	if client == nil || active.Tenant == "" || active.Subject == "" {
		chatBrowser.mutate(func(model *chatui.Model) { model.Attention.Phase = chatui.AttentionFailed })
		refreshChatRoute()
		return
	}
	chatAttentionRead.Lock()
	if chatAttentionRead.pending {
		chatAttentionRead.Unlock()
		return
	}
	chatAttentionRead.pending = true
	chatAttentionRead.Unlock()
	chatBrowser.mutate(func(model *chatui.Model) { model.Attention.Phase = chatui.AttentionLoading })
	refreshChatRoute()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		response, err := client.ListJourneys(chatRPCContext(ctx, active), &journeyv1.ListJourneysRequest{Page: &commonv1.PageRequest{PageSize: chatAttentionPageSize}})
		ui.PostAsync(func() {
			chatAttentionRead.Lock()
			chatAttentionRead.pending = false
			chatAttentionRead.Unlock()
			chatBrowser.mutate(func(model *chatui.Model) {
				// The page may have been reset for another tenant or person while
				// this read was out; one identity's work is never shown to another.
				if model.CurrentTenantID != active.Tenant || model.CurrentUser != active.Subject {
					return
				}
				if err != nil {
					// A failed read keeps the previous brief for comparison but never
					// shows an empty list.
					model.Attention.Phase = chatui.AttentionFailed
					return
				}
				next := chatAttentionItems(active.Locale, response.GetJourneys())
				items, gone := chatui.AttentionMerge(model.Attention, next)
				total := len(items)
				if unseen := int(response.GetTotalCount()) - len(response.GetJourneys()); unseen > 0 {
					total += unseen
				}
				model.Attention = chatui.AttentionBrief{Phase: chatui.AttentionReady, Items: items, Gone: gone, LoadedAt: time.Now(), Total: total}
			})
			refreshChatRoute()
		})
	}()
}
