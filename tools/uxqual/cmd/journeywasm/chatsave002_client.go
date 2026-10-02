package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// CHATSAVE-002. The client's side of the redesigned Saved panel that needs no
// browser, so it is tested natively.

// chatsaveReferences is what a saved post carries that the conversation draws:
// the people and agents it mentions (the mention chips), and how many files it
// has (the attachment indicator).
func chatsaveReferences(references []chat.Reference) ([]chatui.ChatReference, int) {
	var mentions []chatui.ChatReference
	files := 0
	for _, reference := range references {
		switch reference.Kind {
		case chat.PersonMention, chat.AgentMention:
			mentions = append(mentions, chatui.ChatReference{Kind: string(reference.Kind), TenantID: reference.TenantID, ID: reference.ID, Display: reference.Display, ConversationID: reference.ConversationID})
		case chat.MediaAttachment:
			files++
		}
	}
	return mentions, files
}

// chatsaveIsChannel is true when the person's own list of conversations says
// the saved message is in a channel, which is written "#name".
func chatsaveIsChannel(conversationID string, model chatui.Model) bool {
	for _, conversation := range model.Conversations {
		if conversation.ID == conversationID {
			return conversation.Kind == chatui.PublicChannel || conversation.Kind == chatui.PrivateChannel
		}
	}
	return false
}

// chatsaveCounts are the whole list's counts for the segmented control and the
// heading: still to do, done, and all.
func chatsaveCounts(page chat.SavedPage) (todo, done, all int) {
	for _, item := range page.Items {
		if item.State == chat.SavedDone {
			done++
		} else {
			todo++
		}
	}
	return todo, done, todo + done
}

// chatsaveOpenCount is the number the sidebar's Saved row shows: the items
// still to do, and whether it is known yet. It is known only once a whole read
// of the list has landed (CHATBUG-041): a list the person changed before that
// read returned holds only what they touched, and counting it showed a number
// that then changed by itself.
func chatsaveOpenCount(page chat.SavedPage, loaded bool) (int, bool) {
	if !loaded {
		return 0, false
	}
	todo, _, _ := chatsaveCounts(page)
	return todo, true
}

// chatsaveFind returns a copy of the saved item for one message, or false.
func chatsaveFind(page chat.SavedPage, host, conversationID, postID string) (chat.SavedItem, bool) {
	for _, item := range page.Items {
		if item.TenantID == host && item.ConversationID == conversationID && item.PostID == postID {
			return item, true
		}
	}
	return chat.SavedItem{}, false
}

// chatsaveRestore is the commands that put a removed item back the way it was:
// save it, then give it back its note, its reminder and its done state. The
// service keeps none of them once the item is removed.
func chatsaveRestore(item chat.SavedItem) []chatsaveCommand {
	base := chatsaveCommand{ConversationID: item.ConversationID, PostID: item.PostID}
	commands := []chatsaveCommand{{Action: "save", ConversationID: base.ConversationID, PostID: base.PostID}}
	if item.Note != "" {
		note := item.Note
		commands = append(commands, chatsaveCommand{Action: "note", ConversationID: base.ConversationID, PostID: base.PostID, Note: &note})
	}
	if item.DueAt != nil {
		commands = append(commands, chatsaveCommand{Action: "due", ConversationID: base.ConversationID, PostID: base.PostID, SetDue: true, DueAt: item.DueAt})
	}
	if item.State == chat.SavedDone {
		commands = append(commands, chatsaveCommand{Action: "done", ConversationID: base.ConversationID, PostID: base.PostID})
	}
	return commands
}
