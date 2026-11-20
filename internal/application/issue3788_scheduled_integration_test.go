package application

import (
	"context"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// Issue 3788: the delivery job against the real Send service and a real store.
// The unit tests use a fake sender; this proves the principal the job mints is
// accepted by the ordinary service, the post is authored by the person, a
// second pass posts nothing new, and a conversation the person cannot post in
// posts nothing and keeps the row for its author only.
func TestIssue3788_Delivery_ThroughTheRealSendService_Integration(t *testing.T) {
	store := streamIntegrationStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	create := func(id string, members ...string) {
		t.Helper()
		memberships := make([]chatcore.Membership, 0, len(members))
		for _, subject := range members {
			memberships = append(memberships, chatcore.Membership{TenantID: "host", HomeTenantID: "host", ConversationID: id, SubjectID: subject, Role: chatcore.Member, HistoryVisibility: chatcore.FullHistory})
		}
		if _, err := store.CreateConversation(ctx, chatcore.Conversation{ID: id, TenantID: "host", Kind: chatcore.PrivateChannel, Name: id, OwnerID: members[0], Revision: 1}, memberships, ""); err != nil {
			t.Fatal(err)
		}
	}
	create("team-room", "alice", "bob")
	create("bobs-room", "bob")
	service := chatcore.NewService(store, time.Now)
	service.SetAuthority(streamIntegrationAuthority{store: store})

	rows := chatstore.NewRecipientStateStore(store.Store)
	past := time.Now().Add(-time.Minute)
	keep := func(conversation, body string) chatrecipient.ScheduledPost {
		t.Helper()
		row, err := rows.InsertScheduled(ctx, chatrecipient.ScheduledPost{HostTenantID: "host", HomeTenantID: "host", AuthorID: "alice", ConversationID: conversation, Body: body, SendAt: past, Zone: "UTC"}, 10)
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	sent := keep("team-room", "Reminder: compliance training at 09:00")
	refused := keep("bobs-room", "should never appear")

	delivery := ScheduledPostDelivery{Store: rows, Send: service}
	if n, err := delivery.Deliver(ctx, "host"); err != nil || n != 1 {
		t.Fatalf("Deliver = %d, %v; want 1 message posted", n, err)
	}
	alice := chatcore.Principal{TenantID: "host", SubjectID: "alice"}
	posted, err := store.ListPosts(ctx, alice, "host", "team-room", 0, chatcore.Page{PageSize: 10}, chatcore.PostWindow{})
	if err != nil || len(posted.Posts) != 1 || posted.Posts[0].AuthorID != "alice" || posted.Posts[0].Body != "Reminder: compliance training at 09:00" {
		t.Fatalf("posts in the room = %+v, err %v; want one message by alice", posted.Posts, err)
	}
	bob := chatcore.Principal{TenantID: "host", SubjectID: "bob"}
	elsewhere, err := store.ListPosts(ctx, bob, "host", "bobs-room", 0, chatcore.Page{PageSize: 10}, chatcore.PostWindow{})
	if err != nil || len(elsewhere.Posts) != 0 {
		t.Fatalf("a message reached a conversation its author is not in: %+v, err %v", elsewhere.Posts, err)
	}

	left, err := rows.ListScheduled(ctx, "host", "host", "alice")
	if err != nil || len(left) != 1 || left[0].ID != refused.ID || left[0].State != chatrecipient.ScheduledFailed || left[0].Refusal != chatrecipient.ScheduledRefusedAccess {
		t.Fatalf("rows left = %+v, err %v; want only the refused one, kept for alice with the access reason", left, err)
	}
	if other, err := rows.ListScheduled(ctx, "host", "host", "bob"); err != nil || len(other) != 0 {
		t.Fatalf("bob can list alice's scheduled rows: %+v, %v", other, err)
	}

	// A second pass neither re-sends the posted message nor retries the refusal.
	if n, err := delivery.Deliver(ctx, "host"); err != nil || n != 0 {
		t.Fatalf("second Deliver = %d, %v; want nothing", n, err)
	}
	again, err := store.ListPosts(ctx, alice, "host", "team-room", 0, chatcore.Page{PageSize: 10}, chatcore.PostWindow{})
	if err != nil || len(again.Posts) != 1 {
		t.Fatalf("posts after the second pass = %d, %v; want still 1", len(again.Posts), err)
	}
	// A crash between the post and the delete is replayed by the same key.
	replay := ScheduledPostDelivery{Store: rows, Send: service}
	if _, err := replay.Send.SendPost(mustScheduledContext(t, sent), chatcore.SendPostRequest{Principal: alice, TenantID: "host", ConversationID: "team-room", Body: sent.Body, IdempotencyKey: "scheduled:" + sent.ID}); err != nil {
		t.Fatalf("replay with the row's key: %v", err)
	}
	final, err := store.ListPosts(ctx, alice, "host", "team-room", 0, chatcore.Page{PageSize: 10}, chatcore.PostWindow{})
	if err != nil || len(final.Posts) != 1 {
		t.Fatalf("a replay with the same key posted again: %d, %v", len(final.Posts), err)
	}
}

func mustScheduledContext(t *testing.T, post chatrecipient.ScheduledPost) context.Context {
	t.Helper()
	ctx, err := scheduledAuthorContext(context.Background(), post, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}
