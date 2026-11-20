package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestIssue3923_EditCannotMakeAnItemNobodyCanComplete_Security: the author of
// a list that anyone may complete cannot edit it into one that only assignees
// may complete while an item has no assignee; keeping the policy still edits.
func TestIssue3923_EditCannotMakeAnItemNobodyCanComplete_Security(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "cards", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", Role: "manager", State: "active"}, {MemberID: "worker", Role: "member", State: "active"}})
	card := chat.Chatcmd002Card{Kind: "todo", Title: "Launch", Todo: &chat.Chatcmd002Todo{Tick: "anyone", Items: []chat.Chatcmd002Task{
		{ChannelTodoItem: chat.ChannelTodoItem{ID: "task-1", Text: "Book the room", CreatedBy: "owner", CreatedByHomeTenantID: "tenant-a"}, AssigneeID: "worker", AssigneeHomeTenantID: "tenant-a", AssigneeName: "Worker"},
		{ChannelTodoItem: chat.ChannelTodoItem{ID: "task-2", Text: "Send invites", CreatedBy: "owner", CreatedByHomeTenantID: "tenant-a"}},
	}}}
	body, err := card.Body()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	raw, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "cards", AuthorID: "owner", ClientKey: "card-3923", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	post, err := chatPost(raw)
	if err != nil {
		t.Fatal(err)
	}
	request := chat.Chatcmd002Request{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "owner"}, TenantID: "tenant-a", ConversationID: "cards", PostID: post.ID, ExpectedRevision: post.Revision}
	edited := card
	todo := *card.Todo
	todo.Tick = "assignee"
	edited.Todo = &todo
	request.Mutation = chat.Chatcmd002Mutation{Operation: "EDIT", Card: &edited}
	if _, err := s.Chatcmd002Mutate(ctx, request, todoAllow); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("edit to assignee-only with an unassigned item: %v", err)
	}
	// The card is as it was and an edit that keeps the policy still works.
	todo.Tick = "anyone"
	todo.Items = append([]chat.Chatcmd002Task(nil), card.Todo.Items...)
	todo.Items[1].Text = "Send the invites"
	request.Mutation = chat.Chatcmd002Mutation{Operation: "EDIT", Card: &edited}
	updated, err := s.Chatcmd002Mutate(ctx, request, todoAllow)
	if err != nil {
		t.Fatalf("plain edit refused: %v", err)
	}
	if now, ok := chat.Chatcmd002Decode(updated.Body); !ok || now.Todo.Tick != "anyone" || now.Todo.Items[1].Text != "Send the invites" {
		t.Fatalf("edit not stored: %+v %v", now, ok)
	}
}
