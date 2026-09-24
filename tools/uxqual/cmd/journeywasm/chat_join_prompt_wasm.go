//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

const chatJoinPromptHistoryLimit = 128

func chatDismissedJoinPromptIDs(seen map[string]bool) []string {
	ids := make([]string, 0, len(seen))
	for id, dismissed := range seen {
		if dismissed && strings.TrimSpace(id) != "" && len(id) <= 256 {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > chatJoinPromptHistoryLimit {
		ids = ids[len(ids)-chatJoinPromptHistoryLimit:]
	}
	return ids
}

func chatJoinPromptSeen(ids []string) map[string]bool {
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) != "" && len(id) <= 256 {
			seen[id] = true
		}
	}
	return seen
}

// chatChannelJoinPromptDismissed reads from the account's existing sidebar
// storage. That keeps a declined first-visit prompt quiet across reloads
// without introducing a separate browser-storage path.
func chatChannelJoinPromptDismissed(ctx context.Context, cfg journeyclient.Config, conversationID string) bool {
	if conversationID == "" {
		return false
	}
	chatRecipientBrowser.Lock()
	if !chatRecipientBrowser.loadedAt.IsZero() {
		for _, id := range chatRecipientBrowser.layout.DismissedJoinPrompts {
			if id == conversationID {
				chatRecipientBrowser.Unlock()
				return true
			}
		}
		chatRecipientBrowser.Unlock()
		return false
	}
	client := chatRecipientBrowser.client
	generation := chatRecipientBrowser.generation
	chatRecipientBrowser.Unlock()
	if client == nil {
		return false
	}
	result, err := client.GetSidebar(chatRPCContext(ctx, cfg), &chatv1.GetSidebarRequest{})
	if err != nil || result == nil || result.GetSidebar() == nil {
		return false
	}
	layout := recipientLayout{}
	if json.Unmarshal([]byte(result.GetSidebar().GetLayoutJson()), &layout) != nil {
		return false
	}
	chatRecipientBrowser.Lock()
	if generation == chatRecipientBrowser.generation && chatRecipientBrowser.sidebarEdit == chatRecipientBrowser.sidebarSaved {
		chatRecipientBrowser.layout = layout
		chatRecipientBrowser.sidebarRevision = result.GetSidebar().GetRevision()
	}
	chatRecipientBrowser.Unlock()
	for _, id := range layout.DismissedJoinPrompts {
		if id == conversationID {
			return true
		}
	}
	return false
}

func promptChatChannelAccess(cfg journeyclient.Config, conversation chatui.Conversation) {
	if conversation.ID == "" || conversation.Kind != chatui.PublicChannel || conversation.Joined {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		dismissed := chatChannelJoinPromptDismissed(ctx, cfg, conversation.ID)
		current := chatBrowser.config(cfg)
		if current.Tenant != cfg.Tenant || current.Subject != cfg.Subject || chatBrowser.selectedID() != conversation.ID {
			return
		}
		chatBrowser.mutate(func(model *chatui.Model) {
			if model.SelectedID != conversation.ID {
				return
			}
			if model.JoinPromptSeen == nil {
				model.JoinPromptSeen = make(map[string]bool)
			}
			if dismissed {
				model.JoinPromptSeen[conversation.ID] = true
				return
			}
			preview := conversation
			model.PreviewConversation = &preview
			model.JoinPromptID = conversation.ID
		})
		refreshChatRoute()
	}()
}
