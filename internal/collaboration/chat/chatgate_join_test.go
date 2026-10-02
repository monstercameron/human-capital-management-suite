package chat

import (
	"context"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"testing"
	"time"
)

type chatgateDeny struct{ calls int }

type chatgateOpenStore struct{ *fakeStore }

func (f chatgateOpenStore) ReadChannelStatus(_ context.Context, tenant, conversation string) (ChannelStatus, error) {
	return ChannelStatus{TenantID: tenant, ConversationID: conversation, Status: chatpolicy.StatusOpen, Revision: 1}, nil
}
func (chatgateOpenStore) SweepChannelStatuses(context.Context, string, time.Time) (int, error) {
	return 0, ErrUnavailable
}
func (chatgateOpenStore) CommitChannelStatus(context.Context, ChangeChannelStatusRequest, time.Time, func(context.Context, ChannelStatus) error) (ChannelStatus, error) {
	return ChannelStatus{}, ErrUnavailable
}
func chatgateJoinService(f *fakeStore) *Service {
	s := NewService(chatgateOpenStore{f}, time.Now)
	s.SetAuthority(verifiedAuthority{store: f})
	return s
}

func (g *chatgateDeny) CheckMembership(context.Context, Principal, Membership) error {
	g.calls++
	return ErrPermissionDenied
}
func TestTodo_CHATGATE_004(t *testing.T) {
	f := &fakeStore{conversation: Conversation{ID: "room", TenantID: "tenant", Kind: PublicChannel, OwnerID: "owner", Revision: 1}}
	s := chatgateJoinService(f)
	p := Principal{TenantID: "tenant", SubjectID: "worker"}
	r := AddMembershipRequest{Principal: p, Membership: Membership{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: "room", SubjectID: "worker"}}
	g := &chatgateDeny{}
	s.SetChannelGate(g)
	if _, e := s.AddMembership(t.Context(), r); !errors.Is(e, ErrPermissionDenied) || f.mutations != 0 || g.calls != 1 {
		t.Fatalf("gate bypass: %v writes=%d calls=%d", e, f.mutations, g.calls)
	}
	s.SetChannelGate(nil)
	if _, e := s.AddMembership(t.Context(), r); e != nil || f.mutations != 1 {
		t.Fatalf("ungated join %v writes=%d", e, f.mutations)
	}
}
func TestTodo_CHATGATE_004_Security(t *testing.T) {
	f := &fakeStore{conversation: Conversation{ID: "room", TenantID: "tenant", Kind: PublicChannel, OwnerID: "owner", Revision: 1}}
	s := chatgateJoinService(f)
	g := &chatgateDeny{}
	s.SetChannelGate(g)
	r := AddMembershipRequest{Principal: Principal{TenantID: "tenant", SubjectID: "owner"}, Membership: Membership{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: "room", SubjectID: "invitee"}}
	if _, e := s.AddMembership(t.Context(), r); !errors.Is(e, ErrPermissionDenied) || f.mutations != 0 || g.calls != 1 {
		t.Fatalf("direct add bypass %v %d %d", e, f.mutations, g.calls)
	}
}
