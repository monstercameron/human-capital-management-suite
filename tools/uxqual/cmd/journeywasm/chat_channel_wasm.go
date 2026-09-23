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
	return refs[0].ID, hash
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
			openChatConversation(active, id)
			return
		}
	}
	generation := chatBrowser.currentGeneration()
	go func() {
		client := chatBrowser.conversationClient()
		if client == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		result, err := client.GetConversation(chatRPCContext(ctx, active), &chatv1.GetConversationRequest{TenantId: active.Tenant, ConversationId: id})
		current := chatBrowser.config(cfg)
		currentID, currentHash := currentChatChannelFragment()
		if current.Tenant != active.Tenant || current.Subject != active.Subject || !chatBrowser.generationActive(generation) || currentID != id || currentHash != hash {
			return
		}
		if err != nil || result.GetConversation() == nil || result.GetConversation().GetId() != id || result.GetConversation().GetTenantId() != active.Tenant {
			noteChatAction(productui.ResolveProductLocale(active.Locale).Text(chatui.KeyConversationLinkUnavailable))
			return
		}
		chatBrowser.mutate(func(model *chatui.Model) {
			for _, room := range model.Conversations {
				if room.ID == id {
					return
				}
			}
			model.Conversations = append(model.Conversations, chatConversation(result.GetConversation()))
		})
		openChatConversation(active, id)
	}()
}
