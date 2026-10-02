package main

import (
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// CHATUX-022, "Mark unread from here". The server now takes the read position
// back (UpdateReadState with rewind), so the mark survives a reload and
// reaches the person's other devices. The page's part is to not undo it: an
// open conversation is marked read every time its messages are fetched or one
// arrives, which would move the position forward again a moment later.
//
// chatReadHold is the conversations whose reading is held: marked unread while
// open. A held conversation is not marked read and keeps its unread count in
// the sidebar although it is open. Opening it again, or Mark as read, lets go.
var chatReadHold struct {
	sync.Mutex
	ids map[string]bool
}

func setChatReadHold(id string, on bool) {
	chatReadHold.Lock()
	defer chatReadHold.Unlock()
	if !on {
		delete(chatReadHold.ids, id)
		return
	}
	if chatReadHold.ids == nil {
		chatReadHold.ids = map[string]bool{}
	}
	chatReadHold.ids[id] = true
}

func chatReadHeld(id string) bool {
	chatReadHold.Lock()
	defer chatReadHold.Unlock()
	return chatReadHold.ids[id]
}

// chatUnreadFromMessage finds the message a mark starts at and counts what the
// mark leaves unread: that message and every later one somebody else wrote,
// which is how the server counts. The person's own messages are never unread,
// so a mark on the last thing they said counts one: the sidebar still has to
// show that they put the conversation aside.
func chatUnreadFromMessage(messages []chatui.Message, viewer, postID string) (sequence uint64, unread int, found bool) {
	for i, message := range messages {
		if message.ID != postID {
			continue
		}
		for _, later := range messages[i:] {
			if later.AuthorID != viewer {
				unread++
			}
		}
		return message.Sequence, max(unread, 1), message.Sequence > 0
	}
	return 0, 0, false
}

// setConversationUnread shows a conversation with an unread count in the
// model, including the copy each sidebar section holds.
func setConversationUnread(model *chatui.Model, id string, unread int) {
	for i := range model.Conversations {
		if model.Conversations[i].ID == id {
			model.Conversations[i].Unread = unread
		}
	}
	for s := range model.Sections {
		for i := range model.Sections[s].Chats {
			if model.Sections[s].Chats[i].ID == id {
				model.Sections[s].Chats[i].Unread = unread
			}
		}
	}
}

// chatux022OpenedRoomRead takes the unread state off the open conversation's
// sidebar row when its messages are on screen. The sidebar draws the copy each
// section holds, and the open only cleared the listing's own entry, so a
// conversation opened again after "Mark unread from here" kept its unread row
// until the next read of the sidebar, seconds later. A conversation whose
// reading is held keeps its count.
func chatux022OpenedRoomRead(model *chatui.Model) {
	if model.SelectedID == "" || chatReadHeld(model.SelectedID) {
		return
	}
	markConversationUnreadInModel(model, model.SelectedID, false)
}

// chatux022LastSequence is the newest numbered message on screen: the position
// reading a conversation moves to.
func chatux022LastSequence(messages []chatui.Message) uint64 {
	var last uint64
	for _, message := range messages {
		last = max(last, message.Sequence)
	}
	return last
}

// keepHeldRoomUnread is what the open conversation's count becomes when its
// reading is held: the server's count, or one when the server counts none
// (every later message is the person's own).
func keepHeldRoomUnread(conversation *chatui.Conversation) {
	if conversation.Unread == 0 && conversation.Mentions == 0 {
		conversation.Unread = 1
	}
}
