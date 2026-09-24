package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

// chat025EditStore records the edit the service hands to the store.
type chat025EditStore struct {
	*chat027ReferenceStore
	edited []EditPostRequest
}

func (s *chat025EditStore) EditPost(_ context.Context, r EditPostRequest) (Post, error) {
	s.mutations++
	s.edited = append(s.edited, r)
	return Post{ID: r.PostID, ConversationID: r.ConversationID, TenantID: r.TenantID, AuthorID: r.Principal.SubjectID, Body: r.Body, References: r.References, Revision: 2}, nil
}

// chat025Policy refuses any body containing blocked and records every check.
type chat025Policy struct {
	blocked string
	seen    []ContentInput
}

var errChat025Blocked = errors.New("content refused by policy")

func (p *chat025Policy) CheckContent(_ context.Context, in ContentInput) error {
	p.seen = append(p.seen, in)
	if p.blocked != "" && in.Body == p.blocked {
		return errChat025Blocked
	}
	return nil
}

// TestTodo_CHAT_025_Revalidation proves an edit is checked the way a new post
// is: the content policy sees the new body (and every send), supplied mentions
// must resolve for the current audience, and a carried-over mention whose
// subject has left is dropped instead of being committed and re-notified.
func TestTodo_CHAT_025_Revalidation(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(20, 0).UTC()
	mention := Reference{Kind: PersonMention, TenantID: "t1", ID: "u2", Display: "User Two"}
	current := Membership{TenantID: "t1", ConversationID: "c1", HomeTenantID: "t1", SubjectID: "u2"}
	left := current
	left.LeftAt = timePtr(time.Unix(10, 0))

	fixture := func(member Membership) (*Service, *chat025EditStore, *chat025Policy) {
		base := &fakeStore{conversation: conversation(), post: Post{ID: "p1", ConversationID: "c1", TenantID: "t1", AuthorID: "u1", AuthorHomeTenantID: "t1", Body: "old", Revision: 1, References: []Reference{mention}}}
		store := &chat025EditStore{chat027ReferenceStore: &chat027ReferenceStore{fakeStore: base, resolved: member}}
		policy := &chat025Policy{blocked: "leak the salary band"}
		s := NewService(store, func() time.Time { return now })
		s.SetAuthority(referenceAuthority{})
		s.SetContentPolicy(policy)
		return s, store, policy
	}
	edit := func(body string, refs []Reference) EditPostRequest {
		return EditPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", PostID: "p1", Body: body, ExpectedRevision: 1, References: refs}
	}

	// A clean edit reaches the policy as an edit and keeps a current mention.
	s, store, policy := fixture(current)
	post, err := s.EditPost(ctx, edit("  new text @user-two ", nil))
	if err != nil {
		t.Fatalf("clean edit: %v", err)
	}
	if len(policy.seen) != 1 || !policy.seen[0].Edit || policy.seen[0].Body != "new text @user-two" || policy.seen[0].Conversation.ID != "c1" {
		t.Fatalf("policy saw %+v, want one edit check of the trimmed body", policy.seen)
	}
	if len(store.edited) != 1 || store.edited[0].Body != "new text @user-two" || len(store.edited[0].References) != 1 || post.References[0].ID != "u2" {
		t.Fatalf("stored edit = %+v, want trimmed body with the still-current mention", store.edited)
	}

	// A blocked body is refused before the store is touched.
	s, store, _ = fixture(current)
	if _, err := s.EditPost(ctx, edit("leak the salary band", nil)); !errors.Is(err, errChat025Blocked) {
		t.Fatalf("blocked edit = %v, want the policy refusal", err)
	}
	if store.mutations != 0 {
		t.Fatalf("a refused edit reached the store (%d mutations)", store.mutations)
	}

	// A carried-over mention of someone who left is dropped, not re-committed.
	s, store, _ = fixture(left)
	if _, err := s.EditPost(ctx, edit("new text", nil)); err != nil {
		t.Fatalf("edit after the mentioned person left: %v", err)
	}
	if len(store.edited) != 1 || store.edited[0].References == nil || len(store.edited[0].References) != 0 {
		t.Fatalf("stored references = %#v, want an explicit empty set", store.edited[0].References)
	}

	// A newly supplied mention outside the audience refuses the edit.
	s, store, _ = fixture(left)
	if _, err := s.EditPost(ctx, edit("new text @user-two", []Reference{mention})); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("edit adding an ineligible mention = %v, want permission denied", err)
	}
	if store.mutations != 0 {
		t.Fatalf("an edit with an ineligible mention reached the store (%d mutations)", store.mutations)
	}

	// A new post faces the same content policy.
	s, store, policy = fixture(current)
	if _, err := s.SendPost(ctx, SendPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Body: "leak the salary band", IdempotencyKey: "chat025-send"}); !errors.Is(err, errChat025Blocked) {
		t.Fatalf("blocked send = %v, want the policy refusal", err)
	}
	if len(policy.seen) != 1 || policy.seen[0].Edit || store.mutations != 0 {
		t.Fatalf("send check = %+v mutations=%d, want one non-edit check and no write", policy.seen, store.mutations)
	}
}
