//go:build js && wasm

package main

import (
	"context"
	"strings"
	"syscall/js"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func resetChatChannelFragment() {
	chatBrowser.claimChannelFragment("")
}

func currentChatChannelFragment() (id, hash string) {
	location := js.Global().Get("location")
	if !location.Truthy() {
		return "", ""
	}
	hash = location.Get("hash").String()
	if !strings.HasPrefix(hash, "#channel=") {
		return "", hash
	}
	refs := chatui.ChannelReferences("/workspace/app/chat"+hash, location.Get("origin").String())
	if len(refs) != 1 || chatui.ChannelReferenceURL(refs[0].ID) != "/workspace/app/chat"+hash {
		return "", hash
	}
	// NAV-01: the address bar names a channel by its readable name when it
	// can (chatChannelFragmentValue); map it back to the room id here so
	// every reader of the fragment keeps working with ids. An id fragment
	// (older entries, pasted links) resolves to itself.
	resolved, _ := resolveChatChannelFragment(chatFragmentRooms(), refs[0].ID)
	return resolved, hash
}

// A pasted link names a room, not an admission grant. GetConversation is an
// authorized read for rooms beyond the first ListConversations page; every
// post read and stream opened afterward still applies server authorization.
func openChatChannelFragment(cfg journeyclient.Config) {
	id, hash := currentChatChannelFragment()
	if id == "" {
		resetChatChannelFragment()
		return
	}
	active := chatBrowser.config(cfg)
	claim := active.Tenant + "\x00" + active.Subject + "\x00" + hash
	if !chatBrowser.claimChannelFragment(claim) {
		return
	}

	for _, room := range chatBrowser.snapshot().Conversations {
		if room.ID == id && room.Joined {
			navigation := chatBrowser.snapshot()
			navigation.SelectedID = id
			navigation.ShowThread, navigation.ThreadParentID = false, ""
			navigation.FocusMessageID = ""
			chatHistory.replace(navigation)
			openChatConversation(active, id)
			return
		}
	}
	// The URL already names this room. While authorization is pending, do not
	// leave the last room's messages visible under the new address.
	cancelChatSubscription()
	clearChatMediaCache()
	chatBrowser.releaseReactionRead()
	pending, _ := chatBrowser.selectChatConversation(id)
	chatHistory.replace(pending)
	refreshChatRoute()
	go func() {
		client := chatBrowser.conversationClient()
		if client == nil {
			current := chatBrowser.config(cfg)
			currentID, currentHash := currentChatChannelFragment()
			if current.Tenant == active.Tenant && current.Subject == active.Subject && currentID == id && currentHash == hash {
				showUnavailableChatChannelFragment(active, id)
			}
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		result, err := client.GetConversation(chatRPCContext(ctx, active), &chatv1.GetConversationRequest{TenantId: active.Tenant, ConversationId: id})
		current := chatBrowser.config(cfg)
		currentID, currentHash := currentChatChannelFragment()
		if current.Tenant != active.Tenant || current.Subject != active.Subject || currentID != id || currentHash != hash {
			return
		}
		if err != nil || result.GetConversation() == nil || result.GetConversation().GetId() != id || result.GetConversation().GetTenantId() != active.Tenant {
			showUnavailableChatChannelFragment(active, id)
			return
		}
		room := chatConversation(result.GetConversation())
		joined := false
		for _, currentRoom := range chatBrowser.snapshot().Conversations {
			if currentRoom.ID == id && currentRoom.Joined {
				joined = true
				room = currentRoom
				break
			}
		}
		if !joined {
			joined, _ = chatChannelMembership(ctx, client, active, id)
			room.Joined = joined
		}
		chatBrowser.mutate(func(model *chatui.Model) {
			if room.Joined {
				for _, existing := range model.Conversations {
					if existing.ID == id {
						return
					}
				}
				model.Conversations = append(model.Conversations, room)
				// CHAT-02: a channel reference can discover a membership the
				// rail never rendered (joined from another session); give it
				// a section too, for the same reason CreateConversation does.
				ensureRecipientSections(model)
				return
			}
			if room.Kind == chatui.PublicChannel {
				preview := room
				model.PreviewConversation = &preview
				model.JoinPromptID = ""
			}
		})
		if room.Kind == chatui.PublicChannel && !room.Joined {
			dismissed := chatChannelJoinPromptDismissed(ctx, active, id)
			chatBrowser.mutate(func(model *chatui.Model) {
				if dismissed {
					if model.JoinPromptSeen == nil {
						model.JoinPromptSeen = make(map[string]bool)
					}
					model.JoinPromptSeen[id] = true
					return
				}
				requestChatJoinPrompt(model, id)
			})
		}
		navigation := chatBrowser.snapshot()
		navigation.SelectedID = id
		navigation.ShowThread, navigation.ThreadParentID = false, ""
		navigation.FocusMessageID = ""
		chatHistory.replace(navigation)
		queued := chatBrowser.finishChatOpen(id, chatBrowser.currentGeneration())
		openChatConversation(active, id)
		for _, body := range queued {
			sendChatMessage(active, id, body)
		}
	}()
}

func showUnavailableChatChannelFragment(cfg journeyclient.Config, id string) {
	_, generation := chatBrowser.selectChatConversation(id)
	chatBrowser.commit(generation, func(model *chatui.Model) {
		model.State = chatui.StateError
		model.Error = productui.ResolveProductLocale(cfg.Locale).Text(chatui.KeySearchChannelUnavailable)
	})
	queued := chatBrowser.finishChatOpen(id, generation)
	if len(queued) > 0 {
		chatBrowser.restoreDraft(id, strings.Join(queued, "\n"))
	}
	chatHistory.replace(chatBrowser.snapshot())
	refreshChatRoute()
}

// chatChannelMembership asks the policy-filtered discoverable listing for the
// authenticated reader's joined bit. GetConversation is an authorized read
// but does not carry a reader-specific membership projection.
func chatChannelMembership(ctx context.Context, client chatv1.ConversationServiceClient, cfg journeyclient.Config, target string) (bool, bool) {
	cursor := ""
	for page := 0; page < 100; page++ {
		result, err := client.ListConversations(chatRPCContext(ctx, cfg), &chatv1.ListConversationsRequest{
			TenantId: cfg.Tenant, Cursor: cursor, PageSize: 100, IncludeDiscoverable: true,
		})
		if err != nil || result == nil {
			return false, false
		}
		for _, conversation := range result.GetConversations() {
			if conversation != nil && conversation.GetId() == target {
				return conversation.GetJoined(), true
			}
		}
		if result.GetNextCursor() == "" {
			return false, false
		}
		cursor = result.GetNextCursor()
	}
	return false, false
}
