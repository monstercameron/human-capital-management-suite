package chatstore

import (
	"context"
	"testing"
	"time"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func TestTodo_CHAT_029_Integration(t *testing.T) {
	store := adapterDB(t)
	ctx := context.Background()
	const tenant = "tenant-chat-029"
	alice := chat.Principal{TenantID: tenant, SubjectID: "alice"}
	bob := chat.Principal{TenantID: tenant, SubjectID: "bob"}

	create := func(id, name string, members ...chat.Membership) {
		t.Helper()
		conversation := chat.Conversation{ID: id, TenantID: tenant, Kind: chat.PrivateChannel, Name: name, OwnerID: "alice", Revision: 1}
		if _, err := store.CreateConversation(ctx, conversation, members, ""); err != nil {
			t.Fatalf("create conversation %s: %v", id, err)
		}
	}
	member := func(conversation, subject string, role chat.MembershipRole) chat.Membership {
		return chat.Membership{ConversationID: conversation, TenantID: tenant, HomeTenantID: tenant, SubjectID: subject, Role: role, HistoryVisibility: chat.FullHistory}
	}
	create("source", "Source", member("source", "alice", chat.Manager), member("source", "bob", chat.Member))
	create("secret", "Layoff Planning", member("secret", "alice", chat.Manager))
	create("visible", "Visible Room", member("visible", "alice", chat.Manager), member("visible", "bob", chat.Member))

	service := chat.NewService(store, func() time.Time { return time.Now().UTC() })
	service.SetAuthority(forwardingAuthority{store: store})
	refs := []chat.Reference{
		{Kind: chat.ConversationMention, TenantID: tenant, ID: "secret", ConversationID: "secret", Display: "Layoff Planning"},
		{Kind: chat.ConversationMention, TenantID: tenant, ID: "visible", ConversationID: "visible", Display: "Visible Room"},
	}
	post, err := service.SendPostWithReferences(ctx, chat.SendPostWithReferencesRequest{
		SendPostRequest: chat.SendPostRequest{Principal: alice, TenantID: tenant, ConversationID: "source", Body: "confidential staffing plan", IdempotencyKey: "chat-029-projection"},
		References:      refs,
	})
	if err != nil {
		t.Fatalf("send referenced post: %v", err)
	}
	if _, err := service.PinPost(ctx, chat.PinPostRequest{Principal: alice, Pin: chat.Pin{TenantID: tenant, ConversationID: "source", PostID: post.ID}}); err != nil {
		t.Fatalf("pin referenced post: %v", err)
	}

	assertProjected := func(surface string, got chat.Post) {
		t.Helper()
		if len(got.References) != 2 {
			t.Fatalf("%s references = %+v, want restricted and visible references", surface, got.References)
		}
		if got.References[0] != (chat.Reference{Kind: chat.ConversationMention, Display: "Restricted conversation"}) {
			t.Fatalf("%s leaked restricted target: %+v", surface, got.References[0])
		}
		if got.References[1].ID != "visible" || got.References[1].TenantID != tenant || got.References[1].ConversationID != "visible" || got.References[1].Display != "Visible Room" {
			t.Fatalf("%s lost authorized target: %+v", surface, got.References[1])
		}
	}

	listed, err := service.ListPosts(ctx, chat.ListPostsRequest{Principal: bob, TenantID: tenant, ConversationID: "source", Page: chat.Page{PageSize: 10}})
	if err != nil || len(listed.Posts) != 1 {
		t.Fatalf("ListPosts = %+v, %v", listed, err)
	}
	assertProjected("ListPosts", listed.Posts[0])

	search, err := service.Search(ctx, chat.SearchRequest{Principal: bob, TenantID: tenant, Query: "confidential", Page: chat.Page{PageSize: 10}})
	if err != nil || len(search.Results) != 1 {
		t.Fatalf("Search = %+v, %v", search, err)
	}
	assertProjected("Search", search.Results[0].Post)

	pins, err := service.ListPins(ctx, chat.ListPinsRequest{Principal: bob, TenantID: tenant, ConversationID: "source"})
	if err != nil || len(pins) != 1 || pins[0].Post == nil {
		t.Fatalf("ListPins = %+v, %v", pins, err)
	}
	assertProjected("ListPins", *pins[0].Post)

	watchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	events, err := service.WatchConversation(watchCtx, chat.WatchConversationRequest{Principal: bob, TenantID: tenant, ConversationID: "source"})
	if err != nil {
		t.Fatalf("WatchConversation: %v", err)
	}
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("watch closed before referenced post event")
			}
			if event.Event.Post == nil || event.Event.Post.ID != post.ID {
				continue
			}
			assertProjected("WatchConversation", *event.Event.Post)
			return
		case <-watchCtx.Done():
			t.Fatalf("timed out waiting for referenced post event: %v", watchCtx.Err())
		}
	}
}
