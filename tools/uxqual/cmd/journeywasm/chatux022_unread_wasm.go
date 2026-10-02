//go:build js && wasm

package main

import (
	"context"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

const chatMarkUnreadAction = "mark this conversation as unread"

// markChatUnreadFrom is the message menu's "Mark unread from here": the "New"
// line moves above the message, the conversation shows as unread in the
// sidebar, and the server's read position goes back to the message before it.
func markChatUnreadFrom(cfg journeyclient.Config, postID string) {
	model := chatBrowser.snapshot()
	conversation := model.SelectedID
	sequence, unread, found := chatUnreadFromMessage(model.Messages, model.CurrentUser, postID)
	if conversation == "" || !found {
		return
	}
	setChatReadHold(conversation, true)
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.SelectedID != conversation {
			return
		}
		model.UnreadFromID, model.MenuID = postID, ""
		setConversationUnread(model, conversation, unread)
	})
	refreshChatRoute()
	active := chatBrowser.config(cfg)
	if err := rewindChatRead(active, chatConversationHost(active, conversation), conversation, sequence); chatActionFailed(chatMarkUnreadAction, err) {
		// Nothing was taken back: let reading go on as before.
		setChatReadHold(conversation, false)
		chatBrowser.mutate(func(model *chatui.Model) {
			if model.SelectedID == conversation && model.UnreadFromID == postID {
				model.UnreadFromID = ""
			}
			setConversationUnread(model, conversation, 0)
		})
		refreshChatRoute()
		return
	}
	refreshChatRoute()
}

// chatux022Reselect is the person choosing the row of the conversation that is
// already open, after marking it unread: that is opening it again. Nothing is
// loaded, because it is all on screen, so the hold is let go here: the row
// reads as read at once, the "New" line stays where the person put it, and the
// server's read position moves to the newest message.
func chatux022Reselect(cfg journeyclient.Config, id string) {
	if id == "" || chatBrowser.selectedID() != id || !chatReadHeld(id) {
		return
	}
	setChatReadHold(id, false)
	setManualUnread(id, false)
	var last uint64
	chatBrowser.mutate(func(model *chatui.Model) {
		last = chatux022LastSequence(model.Messages)
		markConversationUnreadInModel(model, id, false)
	})
	refreshChatRoute()
	active := chatBrowser.config(cfg)
	markChatRead(active, id, []*chatv1.Post{{TenantId: chatConversationHost(active, id), Sequence: last}})
}

// rewindChatRead moves the server's read position of a conversation back to
// just before sequence. A position already at or before that point is left
// alone. The write names the revision it read, so one that lost to a read
// receipt in flight is read again and tried once more.
func rewindChatRead(cfg journeyclient.Config, host, conversation string, sequence uint64) error {
	client := chatBrowser.conversationClient()
	if client == nil || conversation == "" || sequence == 0 {
		return nil
	}
	target := sequence - 1
	var err error
	for range 2 {
		if err = rewindChatReadOnce(cfg, client, host, conversation, target); err == nil {
			break
		}
	}
	if err != nil {
		return err
	}
	chatRecipientBrowser.Lock()
	if chatRecipientBrowser.readMarked != nil {
		// What markChatRead remembers as read is no longer read.
		chatRecipientBrowser.readMarked[host+"\x00"+conversation] = target
	}
	chatRecipientBrowser.loadedAt = time.Time{}
	chatRecipientBrowser.Unlock()
	return nil
}

func rewindChatReadOnce(cfg journeyclient.Config, client chatv1.ConversationServiceClient, host, conversation string, target uint64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = chatRPCContext(ctx, cfg)
	current, err := client.GetReadState(ctx, &chatv1.GetReadStateRequest{TenantId: host, ConversationId: conversation})
	if err != nil {
		return err
	}
	state := current.GetState()
	if state == nil || state.GetLastReadSequence() <= target {
		return nil
	}
	_, err = client.UpdateReadState(ctx, &chatv1.UpdateReadStateRequest{
		State:            &chatv1.ReadState{TenantId: host, ConversationId: conversation, LastReadSequence: target},
		ExpectedRevision: state.GetRevision(), Rewind: true,
	})
	return err
}
