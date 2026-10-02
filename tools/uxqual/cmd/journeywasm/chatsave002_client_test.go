package main

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// TestTodo_CHATSAVE_002 covers the client side of the redesigned Saved panel:
// the row carries what the conversation's own renderer needs (author identity,
// time, mentions, attachments), a reminder and a note show at once, the counts
// are the whole list's, and a removed item comes back with its note, reminder
// and state.
func TestTodo_CHATSAVE_002(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "host", Subject: "walt", Locale: "en-US"}
	sent := time.Date(2026, time.October, 1, 7, 0, 0, 0, time.UTC)
	due := time.Date(2026, time.October, 2, 9, 0, 0, 0, time.UTC)
	post := &chat.Post{ID: "p1", ConversationID: "room", AuthorID: "ben", AuthorHomeTenantID: "host", Body: "@Ben Whitaker see this", Sequence: 4, Revision: 2, CreatedAt: sent,
		References: []chat.Reference{
			{Kind: chat.PersonMention, TenantID: "host", ID: "ben", Display: "Ben Whitaker"},
			{Kind: chat.AgentMention, TenantID: "host", ID: "helper", Display: "Policy Helper"},
			{Kind: chat.ConversationMention, ID: "room"},
			{Kind: chat.MediaAttachment, ID: "file-1"}, {Kind: chat.MediaAttachment, ID: "file-2"},
		}}
	item := chat.SavedItem{TenantID: "host", HomeTenantID: "host", PersonID: "walt", ConversationID: "room", PostID: "p1", State: chat.SavedDone, Note: "ask Ben", DueAt: &due, UpdatedAt: sent.Add(time.Hour), Availability: "readable", Post: post, Channel: "random"}
	model := chatui.Model{Conversations: []chatui.Conversation{{ID: "room", Name: "random", Kind: chatui.PublicChannel}}, Members: []chatui.Member{{ID: "ben", HomeTenantID: "host", Name: "Ben Whitaker"}}}
	rows := chatsaveRows(chat.SavedPage{Items: []chat.SavedItem{item}}, cfg, model)
	if len(rows) != 1 {
		t.Fatalf("rows: %+v", rows)
	}
	row := rows[0]
	if row.AuthorID != "ben" || !row.SentAt.Equal(sent) || row.Revision != 2 || row.Attachments != 2 || len(row.References) != 2 || row.References[0].Kind != "PERSON_MENTION" || row.References[1].Display != "Policy Helper" {
		t.Fatalf("the row lacks what the conversation's renderer needs: %+v", row)
	}
	if !row.InChannel || row.Channel != "random" || !row.DueAt.Equal(due) || !row.DoneAt.Equal(sent.Add(time.Hour)) || !row.Done || row.Note != "ask Ben" {
		t.Fatalf("the row lacks its channel, reminder, done time or note: %+v", row)
	}
	// A direct conversation is not written "#name".
	direct := chatsaveRows(chat.SavedPage{Items: []chat.SavedItem{item}}, cfg, chatui.Model{Conversations: []chatui.Conversation{{ID: "room", Name: "Loretta Haynes", Kind: chatui.DirectMessage}}})
	if direct[0].InChannel {
		t.Fatalf("a direct conversation was drawn as a channel")
	}

	// A reminder and a note show at once, and clear.
	page := chat.SavedPage{Items: []chat.SavedItem{{TenantID: "host", ConversationID: "room", PostID: "p1", State: chat.SavedTodo}}}
	note := "call back"
	page = chatsaveApplyLocal(page, "host", chatsaveCommand{Action: "note", ConversationID: "room", PostID: "p1", Note: &note}, nil)
	page = chatsaveApplyLocal(page, "host", chatsaveCommand{Action: "due", ConversationID: "room", PostID: "p1", SetDue: true, DueAt: &due}, nil)
	if page.Items[0].Note != "call back" || page.Items[0].DueAt == nil || !page.Items[0].DueAt.Equal(due) {
		t.Fatalf("a note and a reminder are not shown at once: %+v", page.Items[0])
	}
	page = chatsaveApplyLocal(page, "host", chatsaveCommand{Action: "due", ConversationID: "room", PostID: "p1", SetDue: true}, nil)
	if page.Items[0].DueAt != nil || page.Items[0].Note != "call back" {
		t.Fatalf("a cleared reminder: %+v", page.Items[0])
	}
	// A command for another message or another host changes nothing.
	other := chatsaveApplyLocal(page, "elsewhere", chatsaveCommand{Action: "note", ConversationID: "room", PostID: "p1", Note: &note}, nil)
	if other.Items[0].Note != "call back" {
		t.Fatal("a command for another host changed this item")
	}

	// Counts are the whole list's; the heading counts what is still to do.
	todo, done, all := chatsaveCounts(chat.SavedPage{Items: []chat.SavedItem{{State: chat.SavedTodo}, {State: chat.SavedTodo}, {State: chat.SavedDone}}})
	if todo != 2 || done != 1 || all != 3 {
		t.Fatalf("counts %d %d %d", todo, done, all)
	}

	// An item taken off the list is put back with everything it had.
	commands := chatsaveRestore(item)
	if len(commands) != 4 || commands[0].Action != "save" || commands[1].Action != "note" || *commands[1].Note != "ask Ben" || commands[2].Action != "due" || !commands[2].SetDue || !commands[2].DueAt.Equal(due) || commands[3].Action != "done" {
		t.Fatalf("restore: %+v", commands)
	}
	plain := chatsaveRestore(chat.SavedItem{ConversationID: "room", PostID: "p9"})
	if len(plain) != 1 || plain[0].Action != "save" || plain[0].PostID != "p9" {
		t.Fatalf("a plain item is restored by saving it: %+v", plain)
	}
	if found, ok := chatsaveFind(chat.SavedPage{Items: []chat.SavedItem{item}}, "host", "room", "p1"); !ok || found.Note != "ask Ben" {
		t.Fatal("the item is not found")
	}
	if _, ok := chatsaveFind(chat.SavedPage{Items: []chat.SavedItem{item}}, "host", "room", "nope"); ok {
		t.Fatal("an item that is not there was found")
	}

	// A message saved from the conversation carries its mentions and attachments
	// into the row before the service has answered.
	live := chatui.Model{SelectedID: "room", Conversations: []chatui.Conversation{{ID: "room", Name: "random"}},
		Messages: []chatui.Message{{ID: "p1", AuthorID: "ben", Author: "Ben Whitaker", Body: "@Ben Whitaker hi", Revision: 3, SentAt: sent, Attachments: []chatui.Attachment{{ID: "f"}}, PersonaReferences: []chatui.ChatReference{{Kind: "PERSON_MENTION", ID: "ben", Display: "Ben Whitaker"}}}}}
	added := chatsaveOptimisticItem(cfg, "host", chatsaveCommand{Action: "save", ConversationID: "room", PostID: "p1"}, live, sent)
	if added == nil || added.Post.Revision != 3 {
		t.Fatalf("optimistic item: %+v", added)
	}
	if refs, files := chatsaveReferences(added.Post.References); len(refs) != 1 || files != 1 {
		t.Fatalf("the optimistic item lost its mention or attachment: %+v", added.Post.References)
	}
}
