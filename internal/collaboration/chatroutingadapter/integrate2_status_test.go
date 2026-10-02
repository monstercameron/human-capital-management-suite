package chatroutingadapter

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
)

type integrate2StatusFixture struct {
	calls  int
	action chatpolicy.StatusAction
	lease  bool
	err    error
}

func (f *integrate2StatusFixture) GetChannelStatus(context.Context, chat.GetConversationRequest) (chat.ChannelStatus, error) {
	f.calls++
	return chat.ChannelStatus{Revision: 1}, f.err
}
func (f *integrate2StatusFixture) AllowedStatusTransitions(context.Context, chat.GetConversationRequest) ([]chat.StatusTransition, error) {
	f.calls++
	return []chat.StatusTransition{{Status: chatpolicy.StatusLocked}}, f.err
}
func (f *integrate2StatusFixture) GetChannelStatusSnapshot(context.Context, chat.GetConversationRequest) (chat.ChannelStatusSnapshot, error) {
	f.calls++
	return chat.ChannelStatusSnapshot{}, f.err
}
func (f *integrate2StatusFixture) ChangeChannelStatus(ctx context.Context, r chat.ChangeChannelStatusRequest) (chat.ChannelStatus, error) {
	_, f.lease = chatrouting.WriteLeaseFromContext(ctx)
	f.calls++
	return chat.ChannelStatus{Status: r.Status, Revision: 2}, f.err
}
func (f *integrate2StatusFixture) CheckChannelStatusAction(_ context.Context, _ chat.Principal, _ chat.Conversation, a chatpolicy.StatusAction) error {
	f.action = a
	f.calls++
	return f.err
}
func (f *integrate2StatusFixture) CheckChannelReopenerRemoval(context.Context, chat.Principal, chat.Conversation) error {
	f.calls++
	return f.err
}

type integrate2ConversationFixture struct{ *fakeService }

func (f *integrate2ConversationFixture) GetConversation(_ context.Context, r chat.GetConversationRequest) (chat.Conversation, error) {
	return chat.Conversation{TenantID: r.TenantID, ID: r.ConversationID, Kind: chat.PublicChannel}, nil
}
func (f *integrate2ConversationFixture) SendPost(ctx context.Context, r chat.SendPostRequest) (chat.Post, error) {
	if err := chat.RecheckChannelMutation(ctx); err != nil {
		return chat.Post{}, err
	}
	return f.fakeService.SendPost(ctx, r)
}

func TestIntegrate2StatusLeaseAndAction(t *testing.T) {
	f := &integrate2ConversationFixture{fakeService: &fakeService{}}
	s, d := newAdapter(t, f)
	status := &integrate2StatusFixture{}
	s.status = status
	route, err := d.Reserve(t.Context(), chatrouting.ReserveRequest{ConversationID: "room", HostTenantID: "t1", ShardID: "s1", IdempotencyKey: "route"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Activate(t.Context(), "room", "t1", route.Epoch); err != nil {
		t.Fatal(err)
	}
	p := chat.Principal{TenantID: "t1", SubjectID: "owner"}
	r := chat.GetConversationRequest{Principal: p, TenantID: p.TenantID, ConversationID: "room"}
	if v, err := s.GetChannelStatus(t.Context(), r); err != nil || v.Revision != 1 {
		t.Fatal(v, err)
	}
	if v, err := s.AllowedStatusTransitions(t.Context(), r); err != nil || len(v) != 1 {
		t.Fatal(v, err)
	}
	if _, err = s.GetChannelStatusSnapshot(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	if v, err := s.ChangeChannelStatus(t.Context(), chat.ChangeChannelStatusRequest{Principal: p, TenantID: p.TenantID, ConversationID: "room", Status: chatpolicy.StatusLocked}); err != nil || v.Revision != 2 || !status.lease {
		t.Fatal("unleased status", v, status.lease, err)
	}
	if _, err = s.ChatWriteContext(t.Context(), p.TenantID, "room"); err != nil {
		t.Fatal(err)
	}
	for _, a := range []chatpolicy.StatusAction{chatpolicy.StatusPost, chatpolicy.StatusReply, chatpolicy.StatusEdit, chatpolicy.StatusReact, chatpolicy.StatusPin, chatpolicy.StatusRename, chatpolicy.StatusManageMembers} {
		ctx := s.statusMutation(t.Context(), p, p.TenantID, "room", a, nil)
		if err := chat.RecheckChannelMutation(ctx); err != nil || status.action != a {
			t.Fatal("wrong action", a, status.action, err)
		}
	}
	status.err = chat.ErrChannelStatus
	_, err = s.SendPost(t.Context(), chat.SendPostRequest{Principal: p, TenantID: p.TenantID, ConversationID: "room", ParentID: "root", Body: "reply"})
	if !errors.Is(err, chat.ErrChannelStatus) || status.action != chatpolicy.StatusReply || f.sendCalls != 0 {
		t.Fatal("reply bypassed precise fenced status", status.action, err)
	}
	status.err = nil
	if err := chat.RecheckChannelMutation(s.statusMutation(t.Context(), p, p.TenantID, "room", chatpolicy.StatusManageMembers, &p)); err != nil {
		t.Fatal(err)
	}
	s.status = nil
	if _, err = s.GetChannelStatus(t.Context(), r); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err = s.AllowedStatusTransitions(t.Context(), r); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err = s.GetChannelStatusSnapshot(t.Context(), r); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err = s.ChangeChannelStatus(t.Context(), chat.ChangeChannelStatusRequest{}); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatal(err)
	}
}

func (f *integrate2StatusFixture) CheckChannelMembershipAdmissionByID(_ context.Context, p chat.Principal, tenant, room string) error {
	f.calls++
	if p.SubjectID == "" || tenant != "t1" || room != "room" {
		return chat.ErrPermissionDenied
	}
	return f.err
}
func TestIntegrate2AdmissionDoesNotReadMemberOnlyConversation(t *testing.T) {
	f := &integrate2ConversationFixture{fakeService: &fakeService{}}
	s, _ := newAdapter(t, f)
	status := &integrate2StatusFixture{}
	s.status = status
	ctx := s.statusAdmission(t.Context(), chat.Principal{TenantID: "t1", SubjectID: "applicant"}, "t1", "room")
	if err := chat.RecheckChannelMutation(ctx); err != nil || status.calls != 1 {
		t.Fatal("applicant admission bypassed fenced join authority", status.calls, err)
	}
	status.err = chat.ErrChannelStatus
	if err := chat.RecheckChannelMutation(ctx); !errors.Is(err, chat.ErrChannelStatus) {
		t.Fatal("admission ignored current status", err)
	}
}

func (f *integrate2StatusFixture) CheckPersonaReplyChannelStatus(ctx context.Context, r chat.PersonaReplyCommitRequest) error {
	_, f.lease = chatrouting.WriteLeaseFromContext(ctx)
	f.calls++
	return f.err
}

type integrate2PersonaCommitFixture struct {
	*integrate2ConversationFixture
	commits int
}

func (f *integrate2PersonaCommitFixture) CommitPersonaReply(ctx context.Context, r chat.PersonaReplyCommitRequest) (chat.Post, error) {
	if err := chat.RecheckChannelMutation(ctx); err != nil {
		return chat.Post{}, err
	}
	f.commits++
	return chat.Post{ID: "reply", Body: r.Body}, nil
}

func TestIntegrate2PersonaCommitRechecksUnderLease(t *testing.T) {
	f := &integrate2PersonaCommitFixture{integrate2ConversationFixture: &integrate2ConversationFixture{fakeService: &fakeService{}}}
	s, directory := newAdapter(t, f)
	status := &integrate2StatusFixture{err: chat.ErrChannelStatus}
	s.status = status
	route, err := directory.Reserve(t.Context(), chatrouting.ReserveRequest{ConversationID: "room", HostTenantID: "t1", ShardID: "s1", IdempotencyKey: "persona-route"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := directory.Activate(t.Context(), "room", "t1", route.Epoch); err != nil {
		t.Fatal(err)
	}
	r := chat.PersonaReplyCommitRequest{TenantID: "t1", ConversationID: "room", AuthorID: "agent", Body: "answer"}
	if _, err := s.CommitPersonaReply(t.Context(), r); !errors.Is(err, chat.ErrChannelStatus) || f.commits != 0 || status.calls != 1 || !status.lease {
		t.Fatal("unfenced refused persona reply", f.commits, status.calls, status.lease, err)
	}
	status.err = nil
	if post, err := s.CommitPersonaReply(t.Context(), r); err != nil || post.Body != "answer" || f.commits != 1 || status.calls != 2 || !status.lease {
		t.Fatal(post, err, f.commits, status.calls)
	}
	s.status = nil
	if _, err := s.CommitPersonaReply(t.Context(), r); !errors.Is(err, chat.ErrUnavailable) || f.commits != 1 {
		t.Fatal("missing status checker failed open", err, f.commits)
	}
}
