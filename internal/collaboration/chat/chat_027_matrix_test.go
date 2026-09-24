package chat

import (
	"context"
	"testing"
	"time"
)

type chat027ReferenceStore struct {
	*fakeStore
	resolved Membership
	lookups  int
}

func (s *chat027ReferenceStore) GetMembership(_ context.Context, _, _, _, _ string) (Membership, error) {
	s.lookups++
	if s.resolved.SubjectID == "" {
		return Membership{}, ErrNotFound
	}
	return s.resolved, nil
}

func TestTodo_CHAT_027(t *testing.T) {
	ctx := context.Background()
	base := &fakeStore{conversation: conversation()}
	store := &chat027ReferenceStore{fakeStore: base, resolved: Membership{TenantID: "t1", ConversationID: "c1", HomeTenantID: "t1", SubjectID: "u2"}}
	s := NewService(store, func() time.Time { return time.Unix(20, 0).UTC() })
	s.SetAuthority(referenceAuthority{})
	s.SetReferenceDirectory(referenceDirectory{candidates: []ReferenceCandidate{{Reference: Reference{Kind: PersonMention, TenantID: "t1", ID: "u2", Display: "User Two"}, Eligible: true}}})

	post, err := s.SendPostWithReferences(ctx, SendPostWithReferencesRequest{
		SendPostRequest: SendPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Body: "Hello @user-two", IdempotencyKey: "chat027-primary"},
		References:      []Reference{{Kind: PersonMention, TenantID: "t1", ID: "u2", Display: "User Two"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if post.ID == "" || base.mutations != 1 || len(base.sent.References) != 1 || base.sent.References[0].ID != "u2" || base.sent.References[0].Display != "User Two" {
		t.Fatalf("committed post = %+v, stored = %+v; want canonical ID and display snapshot", post, base.sent)
	}
	if store.lookups == 0 {
		t.Fatal("commit did not resolve the selected person against current membership")
	}
}

func TestTodo_CHAT_027_Security(t *testing.T) {
	ctx := context.Background()
	for name, current := range map[string]Membership{
		"wrong host tenant":  {TenantID: "other", ConversationID: "c1", HomeTenantID: "t1", SubjectID: "u2"},
		"wrong conversation": {TenantID: "t1", ConversationID: "other", HomeTenantID: "t1", SubjectID: "u2"},
		"wrong home tenant":  {TenantID: "t1", ConversationID: "c1", HomeTenantID: "other", SubjectID: "u2"},
		"wrong subject":      {TenantID: "t1", ConversationID: "c1", HomeTenantID: "t1", SubjectID: "u1"},
		"left audience":      {TenantID: "t1", ConversationID: "c1", HomeTenantID: "t1", SubjectID: "u2", LeftAt: timePtr(time.Unix(10, 0))},
	} {
		t.Run(name, func(t *testing.T) {
			base := &fakeStore{conversation: conversation()}
			store := &chat027ReferenceStore{fakeStore: base, resolved: current}
			s := NewService(store, func() time.Time { return time.Unix(20, 0).UTC() })
			s.SetAuthority(referenceAuthority{})
			_, err := s.SendPostWithReferences(ctx, SendPostWithReferencesRequest{
				SendPostRequest: SendPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Body: "forged @u1", IdempotencyKey: "chat027-security-" + name},
				References:      []Reference{{Kind: PersonMention, TenantID: "t1", ID: "u2", Display: "u1"}},
			})
			if err != ErrPermissionDenied || base.mutations != 0 {
				t.Fatalf("send err=%v, mutations=%d; want denied before persistence", err, base.mutations)
			}
		})
	}

	// A membership-shaped row must not stand in for a currently installed
	// agent. Agent mentions require the installation authority port at commit.
	base := &fakeStore{conversation: conversation()}
	store := &chat027ReferenceStore{fakeStore: base, resolved: Membership{TenantID: "t1", ConversationID: "c1", HomeTenantID: "t1", SubjectID: "agent-1"}}
	s := NewService(store, func() time.Time { return time.Unix(20, 0).UTC() })
	s.SetAuthority(referenceAuthority{})
	s.SetReferenceDirectory(referenceDirectory{})
	_, err := s.SendPostWithReferences(ctx, SendPostWithReferencesRequest{
		SendPostRequest: SendPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Body: "hello @agent", IdempotencyKey: "chat027-agent-security"},
		References:      []Reference{{Kind: AgentMention, TenantID: "t1", ID: "agent-1", Display: "Agent"}},
	})
	if err != ErrPermissionDenied || base.mutations != 0 {
		t.Fatalf("agent mention without installation authority err=%v mutations=%d; want denied before persistence", err, base.mutations)
	}
}

func TestTodo_CHAT_027_Property(t *testing.T) {
	ctx := context.Background()
	// Arbitrary display strings, including text naming a real audience member,
	// cannot convert a different canonical ID into an eligible recipient.
	for _, display := range []string{"u1", "@u1", "User One", "<@u1>", ""} {
		base := &fakeStore{conversation: conversation()}
		store := &chat027ReferenceStore{fakeStore: base, resolved: Membership{TenantID: "t1", ConversationID: "c1", HomeTenantID: "t1", SubjectID: "u1"}}
		s := NewService(store, func() time.Time { return time.Unix(20, 0).UTC() })
		s.SetAuthority(referenceAuthority{})
		_, err := s.SendPostWithReferences(ctx, SendPostWithReferencesRequest{
			SendPostRequest: SendPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Body: "mention", IdempotencyKey: "chat027-property-" + display},
			References:      []Reference{{Kind: PersonMention, TenantID: "t1", ID: "u2", Display: display}},
		})
		if err != ErrPermissionDenied || base.mutations != 0 {
			t.Fatalf("display %q authorized a different subject: err=%v mutations=%d", display, err, base.mutations)
		}
	}
}
