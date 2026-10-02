package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATUX_022 is the page's half of "Mark unread from here": where the
// mark starts, what it counts, and that a held conversation keeps its count.
func TestTodo_CHATUX_022(t *testing.T) {
	messages := []chatui.Message{
		{ID: "a", AuthorID: "loretta", Sequence: 4},
		{ID: "b", AuthorID: "walt", Sequence: 5},
		{ID: "c", AuthorID: "loretta", Sequence: 6},
		{ID: "d", AuthorID: "walt", Sequence: 7},
		{ID: "unsent", AuthorID: "walt"},
	}
	for _, tc := range []struct {
		post     string
		sequence uint64
		unread   int
		found    bool
	}{
		{"a", 4, 2, true},
		{"b", 5, 1, true},
		// Only the person's own messages follow: still one, so the sidebar shows it.
		{"d", 7, 1, true},
		// A message the server has not numbered yet has no position to go back to.
		{"unsent", 0, 1, false},
		{"missing", 0, 0, false},
	} {
		sequence, unread, found := chatUnreadFromMessage(messages, "walt", tc.post)
		if sequence != tc.sequence || unread != tc.unread || found != tc.found {
			t.Errorf("mark from %q = sequence %d, unread %d, found %v; want %d, %d, %v", tc.post, sequence, unread, found, tc.sequence, tc.unread, tc.found)
		}
	}

	model := chatui.Model{
		SelectedID:    "room",
		Conversations: []chatui.Conversation{{ID: "room"}, {ID: "other", Unread: 3}},
		Sections:      []chatui.SidebarSection{{ID: "s", Chats: []chatui.Conversation{{ID: "room"}}}},
	}
	setConversationUnread(&model, "room", 2)
	if model.Conversations[0].Unread != 2 || model.Sections[0].Chats[0].Unread != 2 || model.Conversations[1].Unread != 3 {
		t.Fatalf("the unread count did not reach the conversation and its section copy only: %+v %+v", model.Conversations, model.Sections)
	}

	if chatReadHeld("room") {
		t.Fatal("a conversation is held before anything marked it")
	}
	setChatReadHold("room", true)
	t.Cleanup(func() { setChatReadHold("room", false) })
	if !chatReadHeld("room") || chatReadHeld("other") {
		t.Fatal("the hold is not on the marked conversation alone")
	}
	read := chatui.Conversation{ID: "room"}
	keepHeldRoomUnread(&read)
	counted := chatui.Conversation{ID: "room", Unread: 4, Mentions: 1}
	keepHeldRoomUnread(&counted)
	if read.Unread != 1 || counted.Unread != 4 || counted.Mentions != 1 {
		t.Fatalf("a held conversation shows %d (want 1) and %d/%d (want the server's 4/1)", read.Unread, counted.Unread, counted.Mentions)
	}
	setChatReadHold("room", false)
	if chatReadHeld("room") {
		t.Fatal("opening the conversation again did not let the hold go")
	}
}

// TestTodo_CHATUX_022_Reopen: a conversation marked unread and opened again
// stops reading as unread in the sidebar as soon as its messages are on screen.
// The sidebar draws each section's own copy of the conversation, which the open
// used to leave unread until the sidebar was next read from the server.
func TestTodo_CHATUX_022_Reopen(t *testing.T) {
	model := chatui.Model{
		SelectedID:    "room",
		Conversations: []chatui.Conversation{{ID: "room"}, {ID: "other", Unread: 3, Mentions: 1}},
		Sections: []chatui.SidebarSection{
			{ID: "starred", Chats: []chatui.Conversation{{ID: "room"}}},
			{ID: "channels", Chats: []chatui.Conversation{{ID: "room"}, {ID: "other", Unread: 3, Mentions: 1}}},
		},
		Messages: []chatui.Message{{ID: "a", AuthorID: "loretta", Sequence: 4}, {ID: "b", AuthorID: "loretta", Sequence: 9}, {ID: "unsent", AuthorID: "walt"}},
	}
	unreadRows := func() int {
		rows := 0
		for _, section := range model.Sections {
			for _, chat := range section.Chats {
				if chat.ID == "room" && (chat.Unread > 0 || chat.Mentions > 0) {
					rows++
				}
			}
		}
		return rows
	}

	// Mark unread from here, in the open conversation: its reading is held.
	setChatReadHold("room", true)
	t.Cleanup(func() { setChatReadHold("room", false) })
	setConversationUnread(&model, "room", 2)
	chatux022OpenedRoomRead(&model)
	if unreadRows() != 2 || model.Conversations[0].Unread != 2 {
		t.Fatalf("a held conversation lost its unread row while it was still held: %+v", model.Sections)
	}

	// Opening it again lets the hold go, and the commit of its messages clears
	// every copy of the row.
	setChatReadHold("room", false)
	chatux022OpenedRoomRead(&model)
	if unreadRows() != 0 || model.Conversations[0].Unread != 0 || model.Conversations[0].Mentions != 0 {
		t.Fatalf("the row still reads as unread after the conversation was opened again: %+v %+v", model.Conversations, model.Sections)
	}
	if model.Conversations[1].Unread != 3 || model.Sections[1].Chats[1].Unread != 3 || model.Sections[1].Chats[1].Mentions != 1 {
		t.Fatalf("opening one conversation read another: %+v %+v", model.Conversations, model.Sections)
	}

	// Reading moves to the newest numbered message; an unsent one has no number.
	if last := chatux022LastSequence(model.Messages); last != 9 {
		t.Fatalf("reading moves to sequence %d, want 9", last)
	}
	if last := chatux022LastSequence(nil); last != 0 {
		t.Fatalf("an empty conversation reads to %d, want nothing", last)
	}

	// No conversation open: nothing to clear, nothing touched.
	model.SelectedID = ""
	chatux022OpenedRoomRead(&model)
	if model.Conversations[1].Unread != 3 {
		t.Fatal("with nothing open a conversation was read")
	}
}
