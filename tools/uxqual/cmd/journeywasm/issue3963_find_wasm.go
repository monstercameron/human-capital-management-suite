//go:build js && wasm

package main

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// chatFindLoad numbers the contact finder's reads so a slow one that finishes
// after a newer open cannot overwrite the newer answer (issue 3963).
var chatFindLoad atomic.Uint64

// startChatFindChannels is what opening "Find the right contact or channel"
// calls. It marks the finder's data as not ready and reads it in the background.
func startChatFindChannels(cfg journeyclient.Config) {
	generation := chatFindLoad.Add(1)
	chatBrowser.mutate(func(model *chatui.Model) {
		model.FindChannels, model.FindChannelsReady, model.FindChannelsFailed = nil, false, false
	})
	go loadChatFindChannels(cfg, generation)
}

// loadChatFindChannels reads the public channels this person may browse, as the
// server admits them, and the listed purpose of those and of every channel in
// their list, for the finder only. It does not open the Browse dialog and it
// joins nothing. When the read fails the finder searches the channels already in
// the list and says so.
func loadChatFindChannels(cfg journeyclient.Config, generation uint64) {
	active := chatBrowser.config(cfg)
	finish := func(rooms []chatui.Conversation, purposes map[string]string, failed bool) {
		if chatFindLoad.Load() != generation {
			return
		}
		chatBrowser.mutate(func(model *chatui.Model) {
			for i := range rooms {
				if purpose, ok := purposes[rooms[i].ID]; ok {
					rooms[i].Topic = purpose
				}
			}
			for i := range model.Conversations {
				if purpose, ok := purposes[model.Conversations[i].ID]; ok {
					model.Conversations[i].Topic = purpose
				}
			}
			model.FindChannels, model.FindChannelsReady, model.FindChannelsFailed = rooms, true, failed
		})
		refreshChatRoute()
	}
	own := []chatui.Conversation{}
	for _, room := range chatBrowser.snapshot().Conversations {
		if room.Kind == chatui.PublicChannel || room.Kind == chatui.PrivateChannel {
			room.Topic = ""
			own = append(own, room)
		}
	}
	client := chatBrowser.conversationClient()
	if client == nil {
		finish(nil, readChatPurposes(active, own), true)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := client.ListConversations(chatRPCContext(ctx, active), &chatv1.ListConversationsRequest{
		TenantId: active.Tenant, PageSize: 100, IncludeDiscoverable: true,
	})
	if err != nil {
		finish(nil, readChatPurposes(active, own), true)
		return
	}
	rooms := []chatui.Conversation{}
	for _, room := range joinableChatConversations(result.GetConversations()) {
		if room.Kind != chatui.PublicChannel || (room.HostTenantID != "" && room.HostTenantID != active.Tenant) {
			continue
		}
		room.Topic = ""
		rooms = append(rooms, room)
	}
	directory := chatDirectorySnapshot()
	for i := range rooms {
		if name, ok := directory[rooms[i].Name]; ok && name != "" {
			rooms[i].Name = strings.TrimSpace(name)
		}
	}
	finish(rooms, readChatPurposes(active, append(append([]chatui.Conversation(nil), rooms...), own...)), false)
}
