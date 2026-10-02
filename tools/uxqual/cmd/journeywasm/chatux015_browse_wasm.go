//go:build js && wasm

package main

import (
	"context"
	"strings"
	"sync"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// browsePurposeFanout bounds how many channel purposes are read at once.
const browsePurposeFanout = 4

// fillChatBrowsePurposes reads each browsed channel's purpose and shows it on
// its row (CHATUX-015). The listing does not carry purposes, so each is read
// from the channel's team widget; a channel whose purpose cannot be read keeps
// a row without a purpose line, and nothing else is affected.
func fillChatBrowsePurposes(active journeyclient.Config, rooms []chatui.Conversation) {
	client := channelTodoClient()
	if client == nil || len(rooms) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	callCtx := chatRPCContext(ctx, active)
	purposes := make(map[string]string, len(rooms))
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, browsePurposeFanout)
	for _, room := range rooms {
		if room.ID == "" || strings.TrimSpace(room.Topic) != "" {
			continue
		}
		wg.Add(1)
		go func(id, host string) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			result, err := client.GetChannelWidgets(callCtx, &chatv1.GetChannelWidgetsRequest{ConversationId: id, HostTenantId: host})
			if err != nil {
				return
			}
			if purpose := strings.TrimSpace(result.GetTeam().GetPurpose()); purpose != "" {
				mu.Lock()
				purposes[id] = purpose
				mu.Unlock()
			}
		}(room.ID, channelTodoHost(room.ID, active.Tenant))
	}
	wg.Wait()
	if len(purposes) == 0 {
		return
	}
	chatBrowser.mutate(func(model *chatui.Model) {
		for i := range model.Browse {
			if purpose, ok := purposes[model.Browse[i].ID]; ok {
				model.Browse[i].Topic = purpose
			}
		}
		for i := range model.Conversations {
			if purpose, ok := purposes[model.Conversations[i].ID]; ok && strings.TrimSpace(model.Conversations[i].Topic) == "" {
				model.Conversations[i].Topic = purpose
			}
		}
	})
	refreshChatRoute()
}
