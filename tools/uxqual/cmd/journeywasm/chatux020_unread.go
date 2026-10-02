package main

import (
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// manualUnread is the conversations the person marked "unread" from the row
// menu (CHATUX-020). This mark is the page's own: it shows the conversation as
// unread at once and until it is opened, also when the server counts nothing
// unread there (the newest message is the person's own). The server's read
// position is taken back as well (CHATUX-022), which is what lasts.
var manualUnread struct {
	sync.Mutex
	ids map[string]bool
}

func setManualUnread(id string, on bool) {
	manualUnread.Lock()
	defer manualUnread.Unlock()
	if !on {
		delete(manualUnread.ids, id)
		return
	}
	if manualUnread.ids == nil {
		manualUnread.ids = map[string]bool{}
	}
	manualUnread.ids[id] = true
}

// applyManualUnread shows each marked conversation as unread, except the one
// that is open: opening a conversation reads it and drops its mark.
func applyManualUnread(model *chatui.Model) {
	manualUnread.Lock()
	defer manualUnread.Unlock()
	for i := range model.Conversations {
		id := model.Conversations[i].ID
		if !manualUnread.ids[id] {
			continue
		}
		if id == model.SelectedID {
			delete(manualUnread.ids, id)
			continue
		}
		if model.Conversations[i].Unread == 0 && model.Conversations[i].Mentions == 0 {
			model.Conversations[i].Unread = 1
		}
	}
}

// markConversationUnreadInModel shows a conversation as unread in the model,
// including the copy each sidebar section holds.
func markConversationUnreadInModel(model *chatui.Model, id string, unread bool) {
	set := func(c *chatui.Conversation) {
		if c.ID != id {
			return
		}
		if unread {
			if c.Unread == 0 && c.Mentions == 0 {
				c.Unread = 1
			}
			return
		}
		c.Unread, c.Mentions = 0, 0
	}
	for i := range model.Conversations {
		set(&model.Conversations[i])
	}
	for s := range model.Sections {
		for i := range model.Sections[s].Chats {
			set(&model.Sections[s].Chats[i])
		}
	}
}
