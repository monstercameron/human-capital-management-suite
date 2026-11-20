package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

func issue3838Service(post Post) (*Service, *chat024MutationRecorder) {
	now := time.Unix(100, 0).UTC()
	conv := Conversation{ID: "c1", TenantID: "t1", Kind: PublicChannel, OwnerID: "owner", Revision: 1}
	base := &fakeStore{
		conversation: conv,
		membership: Membership{
			TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u2",
			HistoryVisibility: FullHistory,
		},
		post: post,
	}
	rec := &chat024MutationRecorder{fakeStore: base}
	s := NewService(rec, func() time.Time { return now })
	s.SetAuthority(verifiedAuthority{store: base})
	return s, rec
}

// A member who may post in a channel they did not create can pin their own message.
func TestIssue3838_MemberPinsOwnMessageInChannelTheyDoNotOwn(t *testing.T) {
	s, rec := issue3838Service(Post{ID: "p1", TenantID: "t1", ConversationID: "c1", AuthorID: "u2", AuthorHomeTenantID: "t1", CreatedAt: time.Unix(50, 0).UTC()})
	_, err := s.PinPost(context.Background(), PinPostRequest{
		Principal: Principal{TenantID: "t1", SubjectID: "u2"},
		Pin:       Pin{TenantID: "t1", ConversationID: "c1", PostID: "p1"},
	})
	if err != nil {
		t.Fatalf("member pinning own message: %v", err)
	}
	if rec.pin.PinnedBy != "u2" || rec.puts != 1 {
		t.Fatalf("pin = %+v puts=%d; want pinned by u2 once", rec.pin, rec.puts)
	}
}

// Someone else's message in a channel they do not own stays owner-only.
func TestIssue3838_MemberCannotPinSomeoneElsesMessage(t *testing.T) {
	s, rec := issue3838Service(Post{ID: "p1", TenantID: "t1", ConversationID: "c1", AuthorID: "other", AuthorHomeTenantID: "t1", CreatedAt: time.Unix(50, 0).UTC()})
	_, err := s.PinPost(context.Background(), PinPostRequest{
		Principal: Principal{TenantID: "t1", SubjectID: "u2"},
		Pin:       Pin{TenantID: "t1", ConversationID: "c1", PostID: "p1"},
	})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("err = %v; want permission denied", err)
	}
	if rec.puts != 0 {
		t.Fatalf("denied pin reached the store")
	}
}

// A deleted own message cannot be pinned.
func TestIssue3838_DeletedOwnMessageCannotBePinned(t *testing.T) {
	s, rec := issue3838Service(Post{ID: "p1", TenantID: "t1", ConversationID: "c1", AuthorID: "u2", AuthorHomeTenantID: "t1", Deleted: true})
	_, err := s.PinPost(context.Background(), PinPostRequest{
		Principal: Principal{TenantID: "t1", SubjectID: "u2"},
		Pin:       Pin{TenantID: "t1", ConversationID: "c1", PostID: "p1"},
	})
	if !errors.Is(err, ErrPermissionDenied) || rec.puts != 0 {
		t.Fatalf("err = %v puts=%d; want permission denied and no write", err, rec.puts)
	}
}

// A caller outside the room gets the same refusal whether or not the message
// exists, so pinning cannot be used to probe for message ids.
func TestIssue3838_NonMemberLearnsNothingAboutTheMessage(t *testing.T) {
	for name, post := range map[string]Post{
		"existing": {ID: "p1", TenantID: "t1", ConversationID: "c1", AuthorID: "other", AuthorHomeTenantID: "t1"},
		"missing":  {},
	} {
		s, rec := issue3838Service(post)
		rec.fakeStore.membership = Membership{}
		rec.fakeStore.conversation.Kind = PrivateChannel
		_, err := s.PinPost(context.Background(), PinPostRequest{
			Principal: Principal{TenantID: "t1", SubjectID: "stranger"},
			Pin:       Pin{TenantID: "t1", ConversationID: "c1", PostID: "p1"},
		})
		if !errors.Is(err, ErrPermissionDenied) || rec.puts != 0 {
			t.Fatalf("%s message: err = %v puts=%d; want permission denied and no write", name, err, rec.puts)
		}
	}
}

// issue3838ReadCounter counts the message and pin reads a pin decision makes.
type issue3838ReadCounter struct {
	*chat024MutationRecorder
	reads int
}

func (s *issue3838ReadCounter) GetPost(ctx context.Context, tenantID, conversationID, postID string) (Post, error) {
	s.reads++
	return s.chat024MutationRecorder.GetPost(ctx, tenantID, conversationID, postID)
}

func (s *issue3838ReadCounter) ListPins(ctx context.Context, tenantID, conversationID string) ([]Pin, error) {
	s.reads++
	return s.chat024MutationRecorder.ListPins(ctx, tenantID, conversationID)
}

// The room's rule is asked before the message or the pin list is read: a
// caller the room refuses causes no read at all, for pinning and unpinning.
func TestIssue3838_RefusedCallerCausesNoRead(t *testing.T) {
	_, rec := issue3838Service(Post{ID: "p1", TenantID: "t1", ConversationID: "c1", AuthorID: "stranger", AuthorHomeTenantID: "t1"})
	rec.fakeStore.membership = Membership{}
	rec.fakeStore.conversation.Kind = PrivateChannel
	counter := &issue3838ReadCounter{chat024MutationRecorder: rec}
	s := NewService(counter, func() time.Time { return time.Unix(100, 0).UTC() })
	s.SetAuthority(verifiedAuthority{store: rec.fakeStore})
	stranger := Principal{TenantID: "t1", SubjectID: "stranger"}
	if _, err := s.PinPost(context.Background(), PinPostRequest{Principal: stranger, Pin: Pin{TenantID: "t1", ConversationID: "c1", PostID: "p1"}}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("pin: err = %v; want permission denied", err)
	}
	if err := s.UnpinPost(context.Background(), UnpinPostRequest{Principal: stranger, TenantID: "t1", ConversationID: "c1", PostID: "p1", ExpectedRevision: 1}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("unpin: err = %v; want permission denied", err)
	}
	if counter.reads != 0 || rec.puts != 0 {
		t.Fatalf("refused caller caused %d reads and %d writes; want none", counter.reads, rec.puts)
	}
}
