//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"syscall/js"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func shareCopy(key string) string {
	m := chatBrowser.snapshot()
	if m.Text != nil {
		if value := m.Text(key); value != "" && value != key {
			return value
		}
	}
	return chatui.EnglishCopy()[key]
}

// A fast RPC can update share state while the initial dialog render is still
// committing. Verify the rendered revision on animation frames and retry the
// local component invalidation only while the DOM is behind.
var shareRenderEpoch uint64

func refreshChatShareRoute() {
	refreshChatRoute()
	frameAPI := js.Global().Get("requestAnimationFrame")
	if frameAPI.Type() != js.TypeFunction {
		return
	}
	shareRenderEpoch++
	epoch := shareRenderEpoch
	attempts := 0
	var frame js.Func
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		if epoch != shareRenderEpoch {
			frame.Release()
			return nil
		}
		model := chatBrowser.snapshot()
		dialog := js.Global().Get("document").Call("querySelector", ".share-dialog")
		matched := model.SharePostID == "" && !dialog.Truthy()
		if model.SharePostID != "" && dialog.Truthy() {
			matched = domDataset(dialog, "shareVersion") == fmt.Sprint(model.ShareVersion)
		}
		if matched {
			if dialog.Truthy() {
				chatui.EnsureShareFocus()
			}
			frame.Release()
			return nil
		}
		attempts++
		if attempts >= 90 {
			frame.Release()
			return nil
		}
		refreshChatRoute()
		frameAPI.Invoke(frame)
		return nil
	})
	frameAPI.Invoke(frame)
}

func openChatShare(cfg journeyclient.Config, postID string) {
	generation, ok := chatBrowser.openShare(postID)
	if !ok {
		return
	}
	refreshChatShareRoute()
	go func() {
		active := chatBrowser.config(cfg)
		client := chatBrowser.conversationClient()
		if client == nil {
			if chatBrowser.shareListing(generation, nil, shareCopy(chatui.KeyShareUnavailable)) {
				refreshChatShareRoute()
			}
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cursor := ""
		seenCursors := map[string]bool{}
		snapshot := chatBrowser.snapshot()
		source := snapshot.ShareSourceRoomID
		directory := chatDirectorySnapshot()
		peers := snapshot.PeerIDs
		rooms := []chatui.Conversation{}
		seen := map[string]bool{}
		for {
			result, err := client.ListConversations(chatRPCContext(ctx, active), &chatv1.ListConversationsRequest{TenantId: active.Tenant, Cursor: cursor, PageSize: 100})
			if err != nil {
				if chatBrowser.shareListing(generation, nil, shareCopy(chatui.KeyShareLoadError)) {
					refreshChatShareRoute()
				}
				return
			}
			peers = chatux047PeerIDs(peers, result.GetConversations())
			for _, c := range result.GetConversations() {
				if c == nil || c.GetTenantId() != active.Tenant || c.GetArchived() || seen[c.GetId()] {
					continue
				}
				seen[c.GetId()] = true
				// Issue 3919: channels, and the direct and group conversations
				// the caller is a current member of (the listing is the server's
				// answer for this caller). A direct conversation is named by its
				// peer, and one with an agent is not a destination.
				room := chatux047Peer(chatConversation(c), c)
				room = chatDirectoryNamedDirect(room, peers, directory)
				if !chatui.ShareDestinationEligible(room, source) {
					continue
				}
				if room.Kind == chatui.DirectMessage && room.Name == room.ID {
					room.Name = shareCopy(chatui.KeyKindDirect)
				}
				rooms = append(rooms, room)
			}
			next := result.GetNextCursor()
			if next == "" || seenCursors[next] {
				break
			}
			seenCursors[next] = true
			cursor = next
		}
		filtered := rooms[:0]
		for _, c := range rooms {
			if c.ID != source {
				filtered = append(filtered, c)
			}
		}
		if active.Tenant != chatBrowser.config(cfg).Tenant || active.Subject != chatBrowser.config(cfg).Subject {
			return
		}
		if chatBrowser.shareListing(generation, filtered, "") {
			refreshChatShareRoute()
		}
	}()
}

func shareChatMessage(cfg journeyclient.Config) {
	attempt, ok := chatBrowser.beginShare(func() string { return fmt.Sprintf("wasm-forward-%d", time.Now().UnixNano()) })
	if !ok {
		return
	}
	refreshChatShareRoute()
	go func() {
		client := chatBrowser.conversationClient()
		if client == nil {
			if chatBrowser.finishShare(attempt, shareCopy(chatui.KeyShareUnavailable)) {
				refreshChatShareRoute()
			}
			return
		}
		active := chatBrowser.config(cfg)
		if active.Tenant != attempt.Config.Tenant || active.Subject != attempt.Config.Subject || active.Bearer == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := client.ForwardPost(chatRPCContext(ctx, active), &chatv1.ForwardPostRequest{
			SourceTenantId: active.Tenant, SourceConversationId: attempt.SourceRoom, SourcePostId: attempt.SourcePost,
			DestinationTenantId: active.Tenant, DestinationConversationId: attempt.Destination, IdempotencyKey: attempt.Key,
			Note: attempt.Note,
		})
		if err != nil {
			if chatBrowser.finishShare(attempt, shareCopy(chatui.KeyShareError)) {
				refreshChatShareRoute()
			}
			return
		}
		if chatBrowser.finishShare(attempt, "") {
			noteChatAction(shareCopy(chatui.KeyShareSuccess))
			refreshChatShareRoute()
			chatui.RestoreShareFocus()
		}
	}()
}
