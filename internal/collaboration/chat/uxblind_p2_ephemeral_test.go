package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTodo_AGENTP_011(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", SubjectID: "u1", ConversationID: "c1"}}
	s := newTestService(f, func() time.Time { return now })
	eph := NewMemoryEphemeralStore(func() time.Time { return now })
	s.SetEphemeralStore(eph)
	s.SetPersonaDMResolver(testPersonaDMResolver{conversationID: "c1"})

	got, err := s.SendEphemeralPost(context.Background(), SendEphemeralPostRequest{
		Principal: principal(), TenantID: "t1", ConversationID: "c1", ThreadID: "c1",
		Body: "Only you can see this answer", DurableCopyConversationID: "c1", IdempotencyKey: "ep-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.OnlyVisibleToYou || got.RecipientSubjectID != "u1" || got.DurableCopyPostID == "" || got.ThreadLink == "" || !got.ExpiresAt.Equal(now.Add(EphemeralLifetime)) {
		t.Fatalf("ephemeral delivery lost its privacy/durable link: %+v", got)
	}
	visible, next, err := eph.ListEphemeral(context.Background(), principal(), "t1", "c1", 0, 10)
	if err != nil || len(visible) != 1 || next != got.Sequence {
		t.Fatalf("recipient list=%+v next=%d err=%v", visible, next, err)
	}
}

type testPersonaDMResolver struct{ conversationID string }

func (r testPersonaDMResolver) ResolvePersonaDM(context.Context, Principal, string) (string, error) {
	return r.conversationID, nil
}

func TestTodo_AGENTP_011_Security(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	eph := NewMemoryEphemeralStore(func() time.Time { return now })
	p := EphemeralPost{ID: "p", TenantID: "t1", ConversationID: "c1", RecipientHomeTenantID: "t1", RecipientSubjectID: "alice", Body: "secret", OnlyVisibleToYou: true, CreatedAt: now, ExpiresAt: now.Add(time.Hour), Sequence: 1}
	if _, err := eph.PutEphemeral(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if got, _, err := eph.ListEphemeral(context.Background(), Principal{TenantID: "t1", SubjectID: "bob"}, "t1", "c1", 0, 10); err != nil || len(got) != 0 {
		t.Fatalf("other member received ephemeral post: posts=%+v err=%v", got, err)
	}
	now = now.Add(25 * time.Hour)
	if got, _, err := eph.ListEphemeral(context.Background(), Principal{TenantID: "t1", SubjectID: "alice"}, "t1", "c1", 0, 10); err != nil || len(got) != 0 {
		t.Fatalf("expired post remained visible: posts=%+v err=%v", got, err)
	}
}

func TestTodo_AGENTP_011_Fault(t *testing.T) {
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", SubjectID: "u1", ConversationID: "c1"}}
	s := newTestService(f, time.Now)
	if _, err := s.SendEphemeralPost(context.Background(), SendEphemeralPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", ThreadID: "c1", Body: "x", DurableCopyConversationID: "c1", IdempotencyKey: "fault"}); !errors.Is(err, ErrEphemeralUnavailable) {
		t.Fatalf("missing ephemeral store error=%v", err)
	}
}
