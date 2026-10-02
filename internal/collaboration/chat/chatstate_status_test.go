package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type chatstateFixture struct {
	fakeStore
	status ChannelStatus
	writes int
}

func (f *chatstateFixture) ReadChannelStatus(context.Context, string, string) (ChannelStatus, error) {
	return f.status, nil
}
func (f *chatstateFixture) SweepChannelStatuses(context.Context, string, time.Time) (int, error) {
	return 0, nil
}
func (f *chatstateFixture) CommitChannelStatus(ctx context.Context, r ChangeChannelStatusRequest, at time.Time, check func(context.Context, ChannelStatus) error) (ChannelStatus, error) {
	if f.status.Revision != r.ExpectedRevision {
		return ChannelStatus{}, ErrConflict
	}
	if err := check(ctx, f.status); err != nil {
		return ChannelStatus{}, err
	}
	f.writes++
	f.status.Status, f.status.Revision, f.status.Until = r.Status, f.status.Revision+1, r.Until
	return f.status, nil
}

type chatstateAuthority struct {
	verifiedAuthority
	permissions chatpolicy.StatusPermissions
	revoked     bool
}

func (a *chatstateAuthority) ChannelStatusPermissions(context.Context, Principal, Conversation, time.Time) (chatpolicy.StatusPermissions, error) {
	if a.revoked {
		return chatpolicy.StatusPermissions{}, nil
	}
	return a.permissions, nil
}
func chatstateService() (*Service, *chatstateFixture, *chatstateAuthority, time.Time) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	f := &chatstateFixture{fakeStore: fakeStore{conversation: Conversation{ID: "room", TenantID: "tenant", Kind: PublicChannel, Revision: 1}}, status: ChannelStatus{TenantID: "tenant", ConversationID: "room", Status: chatpolicy.StatusOpen, Revision: 1}}
	a := &chatstateAuthority{verifiedAuthority: verifiedAuthority{store: &f.fakeStore}, permissions: chatpolicy.StatusPermissions{WorkspaceAdmin: true}}
	s := NewService(f, func() time.Time { return at })
	s.SetAuthority(a)
	return s, f, a, at
}

func TestTodo_CHATSTATE_001(t *testing.T) {
	s, f, _, at := chatstateService()
	ctx := t.Context()
	p := Principal{TenantID: "tenant", SubjectID: "admin"}
	r := ChangeChannelStatusRequest{Principal: p, TenantID: "tenant", ConversationID: "room", Status: chatpolicy.StatusLocked, ExpectedRevision: 1, Reason: "incident"}
	until := at.Add(time.Hour)
	r.Until = &until
	status, err := s.ChangeChannelStatus(ctx, r)
	if err != nil || status.Revision != 2 || f.writes != 1 {
		t.Fatalf("change = %+v %v", status, err)
	}
	if err = s.authorize(ctx, p, f.conversation, chatpolicy.ActionPost); !errors.Is(err, ErrChannelStatus) {
		t.Fatalf("locked post = %v", err)
	}
	if err = s.authorize(ctx, p, f.conversation, chatpolicy.ActionRead); err != nil {
		t.Fatalf("locked read = %v", err)
	}
	f.conversation.Archived = true
	if _, err = s.GetConversation(ctx, GetConversationRequest{Principal: p, TenantID: "tenant", ConversationID: "room"}); err != nil {
		t.Fatalf("legacy archived projection locked read = %v", err)
	}
	snapshot, err := s.GetChannelStatusSnapshot(ctx, GetConversationRequest{Principal: p, TenantID: "tenant", ConversationID: "room"})
	if err != nil || snapshot.CanPost || len(snapshot.Transitions) == 0 {
		t.Fatalf("locked snapshot = %+v %v", snapshot, err)
	}
	s.clock = func() time.Time { return until }
	if err = s.authorize(ctx, p, f.conversation, chatpolicy.ActionPost); err != nil {
		t.Fatalf("expired lock = %v", err)
	}
	read, err := s.GetChannelStatus(ctx, GetConversationRequest{Principal: p, TenantID: "tenant", ConversationID: "room"})
	if err != nil || read.Status != chatpolicy.StatusOpen {
		t.Fatalf("read expiry = %+v %v", read, err)
	}
}

func TestTodo_CHATSTATE_001_Security(t *testing.T) {
	s, f, a, _ := chatstateService()
	p := Principal{TenantID: "tenant", SubjectID: "admin"}
	r := ChangeChannelStatusRequest{Principal: p, TenantID: "tenant", ConversationID: "room", Status: chatpolicy.StatusLocked, ExpectedRevision: 1, Reason: "incident"}
	a.revoked = true
	if _, err := s.ChangeChannelStatus(t.Context(), r); !errors.Is(err, ErrPermissionDenied) || f.writes != 0 {
		t.Fatalf("revoked change = %v writes %d", err, f.writes)
	}
	a.revoked = false
	r.ExpectedRevision = 2
	if _, err := s.ChangeChannelStatus(t.Context(), r); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale change = %v", err)
	}
	if err := s.PostAllowed(t.Context(), f.conversation, trust.SubjectKindAgent); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("unverified scheduled post = %v", err)
	}
	r.ExpectedRevision = 1
	r.Reason = "  "
	if _, err := s.ChangeChannelStatus(t.Context(), r); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("empty reason = %v", err)
	}
}

func TestTodo_CHATSTATE_001_Property(t *testing.T) {
	s, f, a, at := chatstateService()
	p := Principal{TenantID: "tenant", SubjectID: "admin"}
	for _, rule := range chatpolicy.StatusRegistry() {
		f.status.Status = rule.Status
		for action := chatpolicy.StatusPost; action <= chatpolicy.StatusRename; action++ {
			err := s.CheckChannelStatusAction(t.Context(), p, f.conversation, action)
			if (err == nil) != chatpolicy.StatusAllows(rule.Status, action, false, false) {
				t.Fatalf("%s/%d = %v", rule.Status, action, err)
			}
		}
	}
	f.status.Status = chatpolicy.StatusOpen
	a.permissions = chatpolicy.StatusPermissions{ChannelAdmin: true}
	transitions, err := s.AllowedStatusTransitions(t.Context(), GetConversationRequest{Principal: p, TenantID: "tenant", ConversationID: "room"})
	if err != nil || len(transitions) != 1 || transitions[0].Status != chatpolicy.StatusAnnouncements {
		t.Fatalf("transitions = %+v %v", transitions, err)
	}
	until := at.Add(-time.Second)
	if (ChannelStatus{Status: chatpolicy.StatusArchived, Until: &until}).Effective(at) != chatpolicy.StatusArchived {
		t.Fatal("archive expired")
	}
}
