package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

type chat024MutationRecorder struct {
	*fakeStore
	reaction Reaction
	pin      Pin
	puts     int
}

func (s *chat024MutationRecorder) PutReaction(_ context.Context, reaction Reaction) (Reaction, error) {
	s.reaction = reaction
	s.puts++
	return reaction, nil
}

func (s *chat024MutationRecorder) PutPin(_ context.Context, pin Pin) (Pin, error) {
	s.pin = pin
	s.puts++
	return pin, nil
}

func TestTodo_CHAT_024_Security(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(100, 0).UTC()
	base := &fakeStore{
		conversation: conversation(),
		membership: Membership{
			TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1",
			HistoryVisibility: FullHistory,
		},
		post: Post{ID: "p1", TenantID: "t1", ConversationID: "c1", AuthorID: "u1", CreatedAt: now.Add(-time.Minute)},
	}
	recorder := &chat024MutationRecorder{fakeStore: base}
	s := NewService(recorder, func() time.Time { return now })
	s.SetAuthority(verifiedAuthority{store: base})

	_, err := s.AddReaction(ctx, AddReactionRequest{
		Principal: principal(),
		Reaction: Reaction{
			TenantID: "t1", ConversationID: "c1", PostID: "p1", Emoji: "+1",
			SubjectID: "forged-subject", HomeTenantID: "forged-tenant", CreatedAt: now.Add(time.Hour),
		},
	})
	if err != nil {
		t.Fatalf("add reaction: %v", err)
	}
	if recorder.reaction.SubjectID != "u1" || recorder.reaction.HomeTenantID != "t1" || !recorder.reaction.CreatedAt.Equal(now) {
		t.Fatalf("reaction attribution = %+v; want authenticated actor and server time", recorder.reaction)
	}

	_, err = s.PinPost(ctx, PinPostRequest{
		Principal: principal(),
		Pin: Pin{
			TenantID: "t1", ConversationID: "c1", PostID: "p1",
			PinnedBy: "forged-subject", PinnedByHomeTenantID: "forged-tenant", CreatedAt: now.Add(time.Hour),
		},
	})
	if err != nil {
		t.Fatalf("pin post: %v", err)
	}
	if recorder.pin.PinnedBy != "u1" || recorder.pin.PinnedByHomeTenantID != "t1" || !recorder.pin.CreatedAt.Equal(now) {
		t.Fatalf("pin attribution = %+v; want authenticated actor and server time", recorder.pin)
	}

	_, err = s.PinPost(ctx, PinPostRequest{
		Principal: Principal{TenantID: "t1", SubjectID: "u2"},
		Pin:       Pin{TenantID: "t1", ConversationID: "c1", PostID: "p1", PinnedBy: "u1"},
	})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("non-owner pin error = %v; want permission denied", err)
	}
	if recorder.puts != 2 {
		t.Fatalf("mutation writes = %d; unauthorized pin must not reach the store", recorder.puts)
	}
}
