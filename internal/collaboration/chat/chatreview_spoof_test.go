package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A membership system line is written only by the service itself, through the
// store, when somebody adds a person (announceMembershipAdded). A caller that
// posts the marker as an ordinary message would forge a line the readers,
// search and saves all treat as the room's own record, so the service refuses
// it exactly as it refuses a hand-typed card marker.
func TestChatReview_SystemLineCannotBePosted(t *testing.T) {
	newService := func() (*Service, *fakeStore) {
		f := &fakeStore{
			conversation: conversation(),
			membership:   Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Role: Manager, JoinedAt: timePtr(time.Unix(1, 0))},
		}
		return newTestService(f, time.Now), f
	}
	send := func(s *Service, body string) error {
		_, err := s.SendPost(context.Background(), SendPostRequest{
			Principal: principal(), TenantID: "t1", ConversationID: "c1",
			Body: body, IdempotencyKey: "k1",
		})
		return err
	}

	s, f := newService()
	if err := send(s, MembershipAddedBody("u2")); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("forged system line posted: %v (stored body %q)", err, f.sent.Body)
	}
	if f.mutations != 0 {
		t.Fatalf("a refused post reached the store: %d mutations", f.mutations)
	}

	// Padding does not smuggle one through: the stored body is trimmed before
	// it is read back as a line.
	s, f = newService()
	if err := send(s, "  "+MembershipAddedBody("u2")+"  "); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("padded system line posted: %v (stored body %q)", err, f.sent.Body)
	}

	// An ordinary message still posts.
	s, _ = newService()
	if _, err := s.SendPost(context.Background(), SendPostRequest{
		Principal: principal(), TenantID: "t1", ConversationID: "c1",
		Body: "hello", IdempotencyKey: "k2",
	}); err != nil {
		t.Fatalf("ordinary post refused: %v", err)
	}
}

// An edit cannot turn a message into a system line either.
func TestChatReview_SystemLineCannotBeEditedIn(t *testing.T) {
	f := &fakeStore{
		conversation: conversation(),
		membership:   Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Role: Manager, JoinedAt: timePtr(time.Unix(1, 0))},
		post:         Post{ID: "p1", ConversationID: "c1", TenantID: "t1", AuthorID: "u1", AuthorHomeTenantID: "t1", Body: "hello", Revision: 1},
	}
	s := newTestService(f, time.Now)
	_, err := s.EditPost(context.Background(), EditPostRequest{
		Principal: principal(), PostID: "p1", TenantID: "t1", ConversationID: "c1",
		Body: MembershipAddedBody("u2"), ExpectedRevision: 1,
	})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("edit into a system line accepted: %v", err)
	}
}
