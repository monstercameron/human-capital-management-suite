package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTodo_CHAT_024_ReactionReadAuthorizationAndHistory(t *testing.T) {
	ctx := context.Background()
	joined := time.Unix(20, 0).UTC()
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", HistoryVisibility: FromJoin, JoinedAt: &joined}, post: Post{ID: "p1", TenantID: "t1", ConversationID: "c1", CreatedAt: time.Unix(10, 0).UTC()}}
	s := newTestService(f, nil)
	r := ListReactionsRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", PostID: "p1"}
	if _, err := s.ListReactions(ctx, r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddReaction(ctx, AddReactionRequest{Principal: principal(), Reaction: Reaction{TenantID: "t1", ConversationID: "c1", PostID: "p1", Emoji: "+1"}}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("prejoin add: %v", err)
	}
	if err := s.RemoveReaction(ctx, RemoveReactionRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", PostID: "p1", Emoji: "+1"}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("prejoin remove: %v", err)
	}
	if f.mutations != 0 {
		t.Fatalf("unauthorized mutations: %d", f.mutations)
	}
	f.membership.LeftAt = &joined
	if _, err := s.ListReactions(ctx, r); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("revoked read: %v", err)
	}
	if _, err := s.ListReactions(ctx, ListReactionsRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", PostID: "p1", Page: Page{PageSize: 201}}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("unbounded page: %v", err)
	}
}
