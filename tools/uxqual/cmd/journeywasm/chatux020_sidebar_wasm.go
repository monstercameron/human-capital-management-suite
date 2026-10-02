//go:build js && wasm

package main

import (
	"context"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// withChatSidebarRowCallbacks wires the sidebar row menu's Mark as read, Mark as
// unread and Leave (CHATUX-020).
func withChatSidebarRowCallbacks(callbacks chatui.Callbacks, cfg journeyclient.Config) chatui.Callbacks {
	callbacks.MarkConversationRead = func(id string) { go markChatConversationRead(cfg, id) }
	callbacks.MarkConversationUnread = func(id string) { go markChatConversationUnread(cfg, id) }
	callbacks.LeaveConversation = func(id string) { go leaveChatConversation(cfg, id) }
	return callbacks
}

// chatConversationHost is the tenant that hosts a conversation: its own when
// the listing named one, the viewer's otherwise.
func chatConversationHost(cfg journeyclient.Config, id string) string {
	for _, room := range chatBrowser.snapshot().Conversations {
		if room.ID == id && room.HostTenantID != "" {
			return room.HostTenantID
		}
	}
	return cfg.Tenant
}

// markChatConversationRead moves the read position to the newest message and
// drops the badge at once.
func markChatConversationRead(cfg journeyclient.Config, id string) {
	client := chatBrowser.conversationClient()
	if client == nil || id == "" {
		return
	}
	active := chatBrowser.config(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := client.ListPosts(chatRPCContext(ctx, active), &chatv1.ListPostsRequest{TenantId: chatConversationHost(active, id), ConversationId: id, PageSize: 1, Descending: true})
	if chatActionFailed("mark this conversation as read", err) {
		return
	}
	setManualUnread(id, false)
	setChatReadHold(id, false)
	chatBrowser.mutate(func(model *chatui.Model) { markConversationUnreadInModel(model, id, false) })
	markChatRead(active, id, result.GetPosts())
	refreshChatRoute()
}

// markChatConversationUnread shows a conversation as unread until it is
// opened. The page's own mark (see manualUnread) shows it at once; the server's
// read position then goes back to before the newest message, the same call the
// message menu's "Mark unread from here" makes (CHATUX-022), so the mark is
// still there after a reload and on the person's other devices.
func markChatConversationUnread(cfg journeyclient.Config, id string) {
	if id == "" {
		return
	}
	setManualUnread(id, true)
	if chatBrowser.selectedID() == id {
		setChatReadHold(id, true)
	}
	chatBrowser.mutate(func(model *chatui.Model) { markConversationUnreadInModel(model, id, true) })
	refreshChatRoute()
	client := chatBrowser.conversationClient()
	if client == nil {
		return
	}
	active := chatBrowser.config(cfg)
	host := chatConversationHost(active, id)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := client.ListPosts(chatRPCContext(ctx, active), &chatv1.ListPostsRequest{TenantId: host, ConversationId: id, PageSize: 1, Descending: true})
	if err == nil && len(result.GetPosts()) > 0 {
		err = rewindChatRead(active, host, id, result.GetPosts()[0].GetSequence())
	}
	if chatActionFailed(chatMarkUnreadAction, err) {
		return
	}
	refreshChatRoute()
}

// leaveChatConversation removes the viewer's own membership of a channel and
// takes it out of the sidebar; when it was the open one, the first conversation
// that is left opens instead.
func leaveChatConversation(cfg journeyclient.Config, id string) {
	client := chatBrowser.conversationClient()
	if client == nil || id == "" {
		return
	}
	active := chatBrowser.config(cfg)
	host := chatConversationHost(active, id)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	callCtx := chatRPCContext(ctx, active)
	revision, cursor := uint64(0), ""
	for page := 0; page < 20 && revision == 0; page++ {
		result, err := client.ListMemberships(callCtx, &chatv1.ListMembershipsRequest{TenantId: host, ConversationId: id, Cursor: cursor, PageSize: 100})
		if chatActionFailed("leave this channel", err) {
			return
		}
		for _, member := range result.GetMemberships() {
			if member.GetSubjectId() == active.Subject && (member.GetHomeTenantId() == "" || member.GetHomeTenantId() == active.Tenant) {
				revision = member.GetRevision()
				break
			}
		}
		if cursor = result.GetNextCursor(); cursor == "" {
			break
		}
	}
	if revision == 0 {
		chatActionSucceeded("You are not in that channel")
		return
	}
	_, err := client.RemoveMembership(callCtx, &chatv1.RemoveMembershipRequest{TenantId: host, ConversationId: id, SubjectId: active.Subject, HomeTenantId: active.Tenant, ExpectedRevision: revision})
	if chatActionFailed("leave this channel", err) {
		return
	}
	next := ""
	wasOpen := false
	chatBrowser.mutate(func(model *chatui.Model) {
		wasOpen = model.SelectedID == id
		model.Conversations = withoutConversation(model.Conversations, id)
		for s := range model.Sections {
			model.Sections[s].Chats = withoutConversation(model.Sections[s].Chats, id)
		}
		for _, room := range model.Conversations {
			if room.ID != "" {
				next = room.ID
				break
			}
		}
	})
	setManualUnread(id, false)
	chatActionSucceeded("You left the channel")
	invalidateChatRecipientProjection()
	if wasOpen && next != "" {
		pushChatHistoryForSelection(next)
		openChatConversation(active, next)
		return
	}
	refreshChatRoute()
}

// withoutConversation is rooms without the one named.
func withoutConversation(rooms []chatui.Conversation, id string) []chatui.Conversation {
	kept := make([]chatui.Conversation, 0, len(rooms))
	for _, room := range rooms {
		if room.ID != id {
			kept = append(kept, room)
		}
	}
	return kept
}
