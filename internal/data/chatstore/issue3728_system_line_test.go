package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func TestIssue3728_SystemLineIsStoredForANonMemberAndReadByMembers_Integration(t *testing.T) {
	store, _ := chatFixture(t)
	seedConversation(t, store, "tenant-a")
	adapter := &Adapter{Store: store}
	ctx := context.Background()
	body, err := chat.JourneyStageBody(chat.JourneyStageLine{Outcome: chat.JourneyStageApproved, Decider: "approver"})
	if err != nil {
		t.Fatal(err)
	}
	refs := []chat.Reference{{Kind: chat.JourneyReference, TenantID: "tenant-a", ID: "j1", Display: "Promotion"}}
	req := chat.SendPostRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "approver"}, TenantID: "tenant-a", ConversationID: "c-1", Body: body, IdempotencyKey: "journey-stage:j1:approved", References: refs}
	post := chat.Post{AuthorID: "approver", AuthorHomeTenantID: "tenant-a", TenantID: "tenant-a", ConversationID: "c-1", Body: body, References: refs}
	// The ordinary path still refuses a person who is not in the room.
	if _, err := adapter.SendPost(ctx, req, post); !errors.Is(err, ErrNotMember) {
		t.Fatalf("ordinary send by a non-member: %v", err)
	}
	sent, err := adapter.SendSystemLine(ctx, req, post)
	if err != nil {
		t.Fatalf("system line: %v", err)
	}
	if sent.AuthorID != "approver" || sent.Body != body || len(sent.References) != 1 || sent.References[0].ID != "j1" {
		t.Fatalf("stored %+v", sent)
	}
	// A retry is the same post.
	again, err := adapter.SendSystemLine(ctx, req, post)
	if err != nil || again.ID != sent.ID {
		t.Fatalf("retry %+v %v", again, err)
	}
	got, err := adapter.ListPosts(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: "u-1"}, "tenant-a", "c-1", 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(got.Posts) != 1 || got.Posts[0].Body != body {
		t.Fatalf("member read %+v %v", got.Posts, err)
	}
}

func TestIssue3728_SystemLineStaysInItsTenantAndConversation_Security(t *testing.T) {
	store, _ := chatFixture(t)
	seedConversation(t, store, "tenant-a")
	adapter := &Adapter{Store: store}
	ctx := context.Background()
	body, _ := chat.JourneyStageBody(chat.JourneyStageLine{Outcome: chat.JourneyStageRejected, Decider: "approver"})
	send := func(tenant, conversation, author, home string) error {
		_, err := adapter.SendSystemLine(ctx,
			chat.SendPostRequest{Principal: chat.Principal{TenantID: home, SubjectID: author}, TenantID: tenant, ConversationID: conversation, Body: body, IdempotencyKey: "k-" + tenant + conversation},
			chat.Post{AuthorID: author, AuthorHomeTenantID: home, TenantID: tenant, ConversationID: conversation, Body: body})
		return err
	}
	if err := send("tenant-b", "c-1", "approver", "tenant-b"); err == nil {
		t.Fatal("wrote into another tenant's conversation")
	}
	if err := send("tenant-a", "missing", "approver", "tenant-a"); err == nil {
		t.Fatal("wrote into a conversation that does not exist")
	}
	// An author from another home tenant is not a workspace person.
	if err := send("tenant-a", "c-1", "approver", "tenant-b"); err == nil {
		t.Fatal("attributed a line to another workspace's person")
	}
	// Only a stage line may use the door: an ordinary message is refused.
	if _, err := adapter.SendSystemLine(ctx,
		chat.SendPostRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "approver"}, TenantID: "tenant-a", ConversationID: "c-1", Body: "hello", IdempotencyKey: "plain"},
		chat.Post{AuthorID: "approver", AuthorHomeTenantID: "tenant-a", TenantID: "tenant-a", ConversationID: "c-1", Body: "hello"}); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("plain text through the system door: %v", err)
	}
}
