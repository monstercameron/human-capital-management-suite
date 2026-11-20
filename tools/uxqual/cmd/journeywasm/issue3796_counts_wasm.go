//go:build js && wasm

package main

import (
	"context"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// chatCountsNoticed applies one count notice from the person-scoped channel
// (issue 3796): the cache the sidebar repaints from, the model, and the open
// room's rule (nothing is cleared while the page is away or the room is held
// unread). A new post in a room the tab is not looking at also gets one
// immediate sound check, so the slow background poll is not what a person
// waits on.
func chatCountsNoticed(cfg journeyclient.Config, notice chatCountsNotice) {
	chatRecipientBrowser.Lock()
	if chatRecipientBrowser.counts == nil {
		chatRecipientBrowser.counts = map[string]*chatv1.ChatCounts{}
	}
	chatRecipientBrowser.counts[notice.Conversation] = chatCountsNoticeRecord(notice, cfg.Tenant, cfg.Subject)
	chatRecipientBrowser.Unlock()
	var room chatui.Conversation
	selected := ""
	known := false
	chatBrowser.mutate(func(model *chatui.Model) {
		known = applyChatCountsNotice(model, notice)
		clearOpenRoomUnread(model)
		selected = model.SelectedID
		for _, c := range model.Conversations {
			if c.ID == notice.Conversation {
				room = c
			}
		}
	})
	if !known {
		return
	}
	refreshChatRoute()
	if notice.Read || notice.Unread == 0 || notice.Conversation == selected {
		return
	}
	client := chatBrowser.conversationClient()
	if client == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		pollChatSoundConversation(ctx, client, cfg, room, selected)
	}()
}
