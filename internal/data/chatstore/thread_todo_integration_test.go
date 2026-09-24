package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
)

func TestTodo_CHAT_023_Integration(t *testing.T) {
	store := adapterDB(t)
	ctx := context.Background()
	principal := chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}
	conversation := chat.Conversation{ID: "thread-room", TenantID: principal.TenantID, Kind: chat.PrivateChannel, Name: "thread", OwnerID: principal.SubjectID, Revision: 1}
	membership := chat.Membership{ConversationID: conversation.ID, TenantID: conversation.TenantID, HomeTenantID: principal.TenantID, SubjectID: principal.SubjectID, Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := store.CreateConversation(ctx, conversation, []chat.Membership{membership}, ""); err != nil {
		t.Fatal(err)
	}

	root, err := store.SendPost(ctx, chat.SendPostRequest{Principal: principal, TenantID: conversation.TenantID, ConversationID: conversation.ID, IdempotencyKey: "thread-root"}, chat.Post{AuthorID: principal.SubjectID, Body: "root context"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.SendPost(ctx, chat.SendPostRequest{Principal: principal, TenantID: conversation.TenantID, ConversationID: conversation.ID, IdempotencyKey: "thread-reply-1", ParentID: root.ID}, chat.Post{AuthorID: principal.SubjectID, Body: "first reply", ParentID: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.SendPost(ctx, chat.SendPostRequest{Principal: principal, TenantID: conversation.TenantID, ConversationID: conversation.ID, IdempotencyKey: "thread-reply-2", ParentID: root.ID}, chat.Post{AuthorID: principal.SubjectID, Body: "second reply", ParentID: root.ID})
	if err != nil {
		t.Fatal(err)
	}

	follows := NewRecipientStateStore(store.Store)
	identity := chatrecipient.Identity{HostTenantID: conversation.TenantID, HomeTenantID: principal.TenantID, SubjectID: principal.SubjectID, ConversationID: conversation.ID}
	follow, err := follows.PutFollow(ctx, identity, chatrecipient.Follow{RootPostID: root.ID, Followed: true}, 1)
	if err != nil || !follow.Followed || follow.Revision != 2 {
		t.Fatalf("follow=%+v err=%v", follow, err)
	}
	if _, err = store.DeletePost(ctx, chat.DeletePostRequest{Principal: principal, TenantID: conversation.TenantID, ConversationID: conversation.ID, PostID: root.ID, ExpectedRevision: root.Revision}); err != nil {
		t.Fatal(err)
	}

	page, err := store.ListPosts(ctx, principal, conversation.TenantID, conversation.ID, 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(page.Posts) != 3 {
		t.Fatalf("thread context posts=%+v err=%v", page.Posts, err)
	}
	if page.Posts[0].ID != root.ID || !page.Posts[0].Deleted || page.Posts[0].Body != "" {
		t.Fatalf("root tombstone lost or exposed body: %+v", page.Posts[0])
	}
	if page.Posts[1].ID != first.ID || page.Posts[1].ParentID != root.ID || page.Posts[2].ID != second.ID || page.Posts[2].ParentID != root.ID || page.Posts[1].Sequence >= page.Posts[2].Sequence {
		t.Fatalf("replies lost parent identity or order: %+v", page.Posts)
	}
	current, err := follows.Follow(ctx, identity, root.ID)
	if err != nil || !current.Followed || current.Revision != follow.Revision {
		t.Fatalf("follow after root tombstone=%+v err=%v", current, err)
	}
}

func TestTodo_CHAT_023_Security(t *testing.T) {
	store := adapterDB(t)
	ctx := context.Background()
	owner := chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}
	conversation := chat.Conversation{ID: "thread-room", TenantID: owner.TenantID, Kind: chat.PrivateChannel, Name: "thread", OwnerID: owner.SubjectID, Revision: 1}
	membership := chat.Membership{ConversationID: conversation.ID, TenantID: conversation.TenantID, HomeTenantID: owner.TenantID, SubjectID: owner.SubjectID, Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := store.CreateConversation(ctx, conversation, []chat.Membership{membership}, ""); err != nil {
		t.Fatal(err)
	}
	root, err := store.SendPost(ctx, chat.SendPostRequest{Principal: owner, TenantID: conversation.TenantID, ConversationID: conversation.ID, IdempotencyKey: "thread-root"}, chat.Post{AuthorID: owner.SubjectID, Body: "root"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.RemoveMembership(ctx, owner, conversation.TenantID, conversation.ID, owner.TenantID, owner.SubjectID, 1); err != nil {
		t.Fatal(err)
	}
	follows := NewRecipientStateStore(store.Store)
	identity := chatrecipient.Identity{HostTenantID: conversation.TenantID, HomeTenantID: owner.TenantID, SubjectID: owner.SubjectID, ConversationID: conversation.ID}
	if _, err = follows.Follow(ctx, identity, root.ID); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("revoked member read thread follow: %v", err)
	}
	if _, err = follows.PutFollow(ctx, identity, chatrecipient.Follow{RootPostID: root.ID, Followed: true}, 1); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("revoked member changed thread follow: %v", err)
	}
	page, err := store.ListPosts(ctx, owner, conversation.TenantID, conversation.ID, 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(page.Posts) != 0 {
		t.Fatalf("revoked member read thread context: posts=%+v err=%v", page.Posts, err)
	}
}
