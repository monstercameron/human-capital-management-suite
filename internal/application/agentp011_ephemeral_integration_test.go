package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatstream"
)

type agentp011PersonaDM struct{ conversationID string }

func (r agentp011PersonaDM) ResolvePersonaDM(context.Context, chatcore.Principal, string) (string, error) {
	return r.conversationID, nil
}

func TestTodo_AGENTP_011_DurableMergedReplayAndRecipientIsolation(t *testing.T) {
	store := streamIntegrationStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	alice := chatcore.Principal{TenantID: "host", SubjectID: "alice"}
	bob := chatcore.Principal{TenantID: "host", SubjectID: "bob"}
	create := func(id string, kind chatcore.ConversationKind, members []chatcore.Membership) {
		t.Helper()
		_, err := store.CreateConversation(ctx, chatcore.Conversation{ID: id, TenantID: "host", Kind: kind, Name: id, OwnerID: "alice", Revision: 1}, members, "")
		if err != nil {
			t.Fatal(err)
		}
	}
	create("shared-room", chatcore.PublicChannel, []chatcore.Membership{
		{TenantID: "host", HomeTenantID: "host", ConversationID: "shared-room", SubjectID: "alice", Role: chatcore.Manager, HistoryVisibility: chatcore.FullHistory},
		{TenantID: "host", HomeTenantID: "host", ConversationID: "shared-room", SubjectID: "bob", Role: chatcore.Member, HistoryVisibility: chatcore.FullHistory},
	})
	create("alice-persona-dm", chatcore.Direct, []chatcore.Membership{
		{TenantID: "host", HomeTenantID: "host", ConversationID: "alice-persona-dm", SubjectID: "alice", Role: chatcore.Manager, HistoryVisibility: chatcore.FullHistory},
	})
	service := chatcore.NewService(store, time.Now)
	service.SetAuthority(streamIntegrationAuthority{store: store})
	service.SetEphemeralStore(store)
	service.SetPersonaDMResolver(agentp011PersonaDM{conversationID: "alice-persona-dm"})
	root, err := store.SendPost(ctx, chatcore.SendPostRequest{Principal: alice, TenantID: "host", ConversationID: "shared-room", IdempotencyKey: "agentp011-root"}, chatcore.Post{AuthorID: "alice", Body: "source thread"})
	if err != nil {
		t.Fatal(err)
	}
	privateAnswer := "private answer must not reach Bob"
	post, err := service.SendEphemeralPost(ctx, chatcore.SendEphemeralPostRequest{Principal: alice, TenantID: "host", ConversationID: "shared-room", ThreadID: root.ID, Body: privateAnswer, IdempotencyKey: "agentp011-private"})
	if err != nil {
		t.Fatal(err)
	}
	if post.Sequence == 0 || post.ThreadLink == "" || !strings.HasPrefix(post.ThreadLink, "/chat/share/") {
		t.Fatalf("ephemeral envelope lacks durable offset or canonical locator: %+v", post)
	}
	dm, err := store.ListPosts(ctx, alice, "host", "alice-persona-dm", 0, chatcore.Page{PageSize: 10}, chatcore.PostWindow{})
	if err != nil || len(dm.Posts) != 1 {
		t.Fatalf("durable persona DM posts=%d err=%v", len(dm.Posts), err)
	}
	if !strings.Contains(dm.Posts[0].Body, privateAnswer) || !strings.Contains(dm.Posts[0].Body, post.ThreadLink) {
		t.Fatalf("durable DM omitted answer or source backlink: %q", dm.Posts[0].Body)
	}
	reader := chatServiceReader{service: service, membership: store, events: store}
	stream, err := chatstream.New(chatstream.Config{Key: []byte("agentp011-integration-key"), Reader: reader,
		Authorizer: chatServiceStreamAuthorizer{service: service, membership: store}, QueueSize: 8, ReplayLimit: 8,
		CursorTTL: time.Minute, Clock: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	bridge := chatstream.Bridge{Stream: stream, Reader: reader, PollInterval: 5 * time.Millisecond, PageLimit: 8}
	watch := func(principal chatcore.Principal, cursor string) (*chatstream.Subscription, error) {
		return bridge.Watch(ctx, chatstream.WatchRequest{TenantID: "host", HomeTenantID: principal.TenantID, SubjectID: principal.SubjectID,
			ConversationID: "shared-room", MembershipEpoch: 1, Cursor: cursor})
	}
	first, err := watch(alice, "")
	if err != nil {
		t.Fatal(err)
	}
	startCursor := first.Cursor()
	first.Close()
	if startCursor == "" {
		t.Fatal("subscription did not issue a signed reconnect cursor")
	}
	other, err := watch(bob, "")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	resumed, err := watch(alice, startCursor)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	for {
		event, nextErr := resumed.Next(ctx)
		if nextErr != nil {
			t.Fatalf("recipient reconnect: %v", nextErr)
		}
		if event.Ephemeral {
			if event.RecipientSubjectID != "alice" || !strings.Contains(string(event.Payload), privateAnswer) {
				t.Fatalf("recipient replay payload=%+v", event)
			}
			break
		}
	}
	otherCtx, stopOther := context.WithTimeout(ctx, 30*time.Millisecond)
	defer stopOther()
	for {
		event, nextErr := other.Next(otherCtx)
		if errors.Is(nextErr, context.DeadlineExceeded) {
			break
		}
		if nextErr != nil {
			t.Fatalf("nonrecipient stream: %v", nextErr)
		}
		if event.Ephemeral || strings.Contains(string(event.Payload), privateAnswer) {
			t.Fatalf("nonrecipient observed private event: %+v", event)
		}
	}
}
