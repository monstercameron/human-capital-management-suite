package chatextensions

import (
	"context"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func channelTodoOut(v chat.ChannelTodoList) *chatv1.ChannelTodoList {
	out := &chatv1.ChannelTodoList{ConversationId: v.ConversationID, Revision: v.Revision, Pinned: v.Pinned}
	for _, item := range v.Items {
		projected := &chatv1.ChannelTodoItem{Id: item.ID, Text: item.Text, Completed: item.Completed, CreatedBy: item.CreatedBy, CreatedByHomeTenantId: item.CreatedByHomeTenantID, CreatedAtUnix: item.CreatedAtUnix, SourcePostId: item.SourcePostID, CompletedBySubjectId: item.CompletedBySubjectID, CompletedByHomeTenantId: item.CompletedByHomeTenantID, CompletedAtUnix: item.CompletedAtUnix, CompletionMode: item.CompletionMode, CanToggle: item.CanToggle, CanManageCompletionPolicy: item.CanManageCompletionPolicy}
		for _, selected := range item.SelectedCompleters {
			projected.SelectedCompleters = append(projected.SelectedCompleters, &chatv1.ChannelTodoSelectedMember{HomeTenantId: selected.HomeTenantID, SubjectId: selected.SubjectID})
		}
		out.Items = append(out.Items, projected)
	}
	return out
}

func (s *server) GetChannelTodoList(ctx context.Context, r *chatv1.GetChannelTodoListRequest) (*chatv1.GetChannelTodoListResponse, error) {
	p, err := s.principal(ctx)
	if err != nil {
		return nil, err
	}
	svc, ok := s.deps.Service.(ChannelTodoService)
	if !ok {
		return nil, mapped(chat.ErrUnavailable)
	}
	v, err := svc.ChannelTodo(ctx, p, r.GetHostTenantId(), r.GetConversationId())
	if err != nil {
		return nil, mapped(err)
	}
	return &chatv1.GetChannelTodoListResponse{List: channelTodoOut(v)}, nil
}

func (s *server) MutateChannelTodoList(ctx context.Context, r *chatv1.MutateChannelTodoListRequest) (*chatv1.MutateChannelTodoListResponse, error) {
	p, err := s.principal(ctx)
	if err != nil {
		return nil, err
	}
	svc, ok := s.deps.Service.(ChannelTodoService)
	if !ok {
		return nil, mapped(chat.ErrUnavailable)
	}
	m := chat.ChannelTodoMutation{Operation: r.GetOperation(), ItemID: r.GetItemId(), Text: r.GetText(), Completed: r.GetCompleted(), Pinned: r.GetPinned(), SourcePostID: r.GetSourcePostId(), CompletionMode: r.GetCompletionMode()}
	for _, selected := range r.GetSelectedCompleters() {
		m.SelectedCompleters = append(m.SelectedCompleters, chat.ChannelTodoSelectedMember{HomeTenantID: selected.GetHomeTenantId(), SubjectID: selected.GetSubjectId()})
	}
	v, err := svc.MutateChannelTodo(ctx, p, r.GetHostTenantId(), r.GetConversationId(), r.GetExpectedRevision(), m)
	if err != nil {
		return nil, mapped(err)
	}
	return &chatv1.MutateChannelTodoListResponse{List: channelTodoOut(v)}, nil
}
