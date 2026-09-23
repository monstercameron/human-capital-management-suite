package chatextensions

import (
	"context"
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

type todoService struct {
	Service
	principal          chat.Principal
	host, conversation string
	mutation           chatstore.ChannelTodoMutation
	expected           uint64
	err                error
}

func (s *todoService) ChannelTodo(_ context.Context, p chat.Principal, host, conversation string) (chatstore.ChannelTodoList, error) {
	s.principal, s.host, s.conversation = p, host, conversation
	return chatstore.ChannelTodoList{ConversationID: conversation, Revision: 1, Items: []chatstore.ChannelTodoItem{}}, s.err
}

func (s *todoService) MutateChannelTodo(_ context.Context, p chat.Principal, host, conversation string, expected uint64, mutation chatstore.ChannelTodoMutation) (chatstore.ChannelTodoList, error) {
	s.principal, s.host, s.conversation, s.expected, s.mutation = p, host, conversation, expected, mutation
	return chatstore.ChannelTodoList{ConversationID: conversation, Revision: expected + 1, Items: []chatstore.ChannelTodoItem{{ID: "item", Text: mutation.Text, SourcePostID: mutation.SourcePostID, CreatedBy: p.SubjectID, CreatedByHomeTenantID: p.TenantID, Completed: true, CompletedBySubjectID: p.SubjectID, CompletedByHomeTenantID: p.TenantID, CompletedAtUnix: 123, CompletionMode: mutation.CompletionMode, SelectedCompleters: mutation.SelectedCompleters, CanToggle: true, CanManageCompletionPolicy: true}}}, s.err
}

func TestChannelTodoRPCBindsPrincipalAndFields(t *testing.T) {
	svc := &todoService{}
	s := &server{deps: Dependencies{Service: svc}}
	ctx := grantContext(t, "home")
	got, err := s.GetChannelTodoList(ctx, &chatv1.GetChannelTodoListRequest{HostTenantId: "host", ConversationId: "channel"})
	if err != nil || got.GetList().GetRevision() != 1 || svc.principal.TenantID != "home" || svc.principal.SubjectID != "admin" || svc.host != "host" {
		t.Fatalf("get = %+v, principal=%+v host=%q err=%v", got, svc.principal, svc.host, err)
	}
	changed, err := s.MutateChannelTodoList(ctx, &chatv1.MutateChannelTodoListRequest{HostTenantId: "host", ConversationId: "channel", ExpectedRevision: 1, Operation: "ADD", Text: "task", SourcePostId: "post"})
	if err != nil || changed.GetList().GetRevision() != 2 || changed.GetList().GetItems()[0].GetSourcePostId() != "post" || changed.GetList().GetItems()[0].GetCreatedByHomeTenantId() != "home" || changed.GetList().GetItems()[0].GetCompletedBySubjectId() != "admin" || changed.GetList().GetItems()[0].GetCompletedByHomeTenantId() != "home" || changed.GetList().GetItems()[0].GetCompletedAtUnix() != 123 || svc.mutation.Operation != "ADD" || svc.expected != 1 || svc.host != "host" {
		t.Fatalf("mutate = %+v, service=%+v err=%v", changed, svc, err)
	}
	policy, err := s.MutateChannelTodoList(ctx, &chatv1.MutateChannelTodoListRequest{HostTenantId: "host", ConversationId: "channel", ExpectedRevision: 2, Operation: "SET_COMPLETION_POLICY", ItemId: "item", CompletionMode: "ME_AND_SELECTED", SelectedCompleters: []*chatv1.ChannelTodoSelectedMember{{HomeTenantId: "guest", SubjectId: "worker"}}})
	if err != nil || svc.mutation.CompletionMode != "ME_AND_SELECTED" || len(svc.mutation.SelectedCompleters) != 1 || svc.mutation.SelectedCompleters[0].HomeTenantID != "guest" || policy.GetList().GetItems()[0].GetCompletionMode() != "ME_AND_SELECTED" || len(policy.GetList().GetItems()[0].GetSelectedCompleters()) != 1 || !policy.GetList().GetItems()[0].GetCanToggle() || !policy.GetList().GetItems()[0].GetCanManageCompletionPolicy() {
		t.Fatalf("policy mapping = %+v, service=%+v err=%v", policy, svc, err)
	}
	svc.err = chat.ErrPermissionDenied
	if _, err := s.GetChannelTodoList(ctx, &chatv1.GetChannelTodoListRequest{ConversationId: "channel"}); err == nil {
		t.Fatal("denied read succeeded")
	} else if e, ok := envelope.As(err); !ok || e.Code() != envelope.CodePermissionDenied {
		t.Fatalf("denial mapping: %v", err)
	}
	if _, err := s.MutateChannelTodoList(context.Background(), &chatv1.MutateChannelTodoListRequest{ConversationId: "channel"}); err == nil {
		t.Fatal("unauthenticated mutation succeeded")
	}
}
