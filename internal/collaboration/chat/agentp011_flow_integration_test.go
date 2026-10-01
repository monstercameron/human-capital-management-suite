package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatstream"
)

type agentp011StreamReader struct{ store EphemeralStore }

func (r agentp011StreamReader) Read(ctx context.Context, q chatstream.ReadRequest) (chatstream.Page, error) {
	posts, next, err := r.store.ListEphemeral(ctx, Principal{TenantID: q.HomeTenantID, SubjectID: q.SubjectID}, q.TenantID, q.ConversationID, q.AfterSequence, q.Limit)
	if err != nil {
		return chatstream.Page{}, err
	}
	page := chatstream.Page{NextSequence: next, Complete: true}
	for _, post := range posts {
		page.Events = append(page.Events, chatstream.Event{
			TenantID: post.TenantID, ConversationID: post.ConversationID,
			Sequence: post.Sequence, MembershipEpoch: 1,
			RecipientSubjectID: post.RecipientSubjectID, RecipientHomeTenantID: post.RecipientHomeTenantID, Ephemeral: true,
			ExpiresAt: post.ExpiresAt, Payload: []byte(post.Body),
		})
	}
	return page, nil
}

type agentp011StreamAuth struct{}

func (agentp011StreamAuth) Authorize(context.Context, chatstream.Access) error { return nil }

func TestTodo_AGENTP_011_ServiceBridgeIntegration(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", SubjectID: "u1", ConversationID: "c1"}}
	service := newTestService(store, func() time.Time { return now })
	ephemeral := NewMemoryEphemeralStore(func() time.Time { return now })
	service.SetEphemeralStore(ephemeral)
	service.SetPersonaDMResolver(testPersonaDMResolver{conversationID: "c1"})
	post, err := service.SendEphemeralPost(context.Background(), SendEphemeralPostRequest{
		Principal: principal(), TenantID: "t1", ConversationID: "c1", ThreadID: "c1",
		Body: "private answer", IdempotencyKey: "flow-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !post.OnlyVisibleToYou || post.ThreadLink == "" || post.DurableCopyPostID == "" {
		t.Fatalf("incomplete recipient envelope or durable-copy link: %+v", post)
	}
	if store.sent.ID != post.DurableCopyPostID || store.sent.ConversationID != post.DurableCopyConversationID {
		t.Fatalf("DM copy was not durably submitted: stored=%+v ephemeral=%+v", store.sent, post)
	}
	if !strings.Contains(store.sent.Body, post.Body) || !strings.Contains(store.sent.Body, post.ThreadLink) || !strings.Contains(post.ThreadLink, "/chat/share/") {
		t.Fatalf("durable DM copy lacks canonical thread backlink: body=%q link=%q", store.sent.Body, post.ThreadLink)
	}

	reader := agentp011StreamReader{store: ephemeral}
	stream, err := chatstream.New(chatstream.Config{
		Key: []byte("agentp011-test-key"), Reader: reader, Authorizer: agentp011StreamAuth{},
		QueueSize: 8, ReplayLimit: 4, CursorTTL: time.Hour, Clock: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	bridge := chatstream.Bridge{Stream: stream, Reader: reader, PollInterval: time.Hour, PageLimit: 8}
	request := func(subject, cursor string) chatstream.WatchRequest {
		return chatstream.WatchRequest{TenantID: "t1", HomeTenantID: "t1", SubjectID: subject, ConversationID: "c1", MembershipEpoch: 1, Cursor: cursor}
	}

	// Simulate a dropped connection before the first delivery, then resume from
	// its signed cursor. The pending record must replay to its original recipient.
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	first, err := bridge.Watch(firstCtx, request("u1", ""))
	if err != nil {
		cancelFirst()
		t.Fatal(err)
	}
	cursor := first.Cursor()
	first.Close()
	cancelFirst()
	if cursor == "" {
		t.Fatal("initial subscription did not provide a signed cursor")
	}

	otherCtx, cancelOther := context.WithCancel(context.Background())
	defer cancelOther()
	other, err := bridge.Watch(otherCtx, request("u2", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()

	recipientCtx, cancelRecipient := context.WithCancel(context.Background())
	defer cancelRecipient()
	recipient, err := bridge.Watch(recipientCtx, request("u1", cursor))
	if err != nil {
		t.Fatal(err)
	}
	defer recipient.Close()
	recipientNextCtx, cancelRecipientNext := context.WithTimeout(context.Background(), time.Second)
	defer cancelRecipientNext()
	got, err := recipient.Next(recipientNextCtx)
	if err != nil || !got.Ephemeral || got.RecipientSubjectID != "u1" || string(got.Payload) != post.Body {
		t.Fatalf("resumed recipient event=%+v err=%v", got, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := other.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("non-recipient stream observed private event: %v", err)
	}
}
