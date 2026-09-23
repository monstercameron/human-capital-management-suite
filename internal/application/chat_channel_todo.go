package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func (s *ChatExtensions) channelTodoActor(ctx context.Context, p chat.Principal, host, conversation string) error {
	if s == nil || s.TodoStore == nil || s.Conversations == nil {
		return chat.ErrUnavailable
	}
	if _, ok := transport.InvocationFromContext(ctx); !ok {
		return chat.ErrPermissionDenied
	}
	identity, ok := trust.FromContext(ctx)
	if !ok || identity == nil || identity.SubjectKind() != trust.SubjectKindHuman || identity.Tenant().String() != p.TenantID || identity.Subject() != p.SubjectID {
		return chat.ErrPermissionDenied
	}
	if host == "" {
		host = p.TenantID
	}
	c, err := s.Conversations.GetConversation(ctx, chat.GetConversationRequest{Principal: p, TenantID: host, ConversationID: conversation})
	if err != nil {
		return err
	}
	if c.Kind != chat.PublicChannel && c.Kind != chat.PrivateChannel {
		return chat.ErrInvalidArgument
	}
	return nil
}

func (s *ChatExtensions) ChannelTodo(ctx context.Context, p chat.Principal, host, conversation string) (chat.ChannelTodoList, error) {
	if err := s.channelTodoActor(ctx, p, host, conversation); err != nil {
		return chat.ChannelTodoList{}, err
	}
	if host == "" {
		host = p.TenantID
	}
	v, err := s.TodoStore.ChannelTodo(ctx, host, p.TenantID, conversation, p.SubjectID, func(ctx context.Context) error {
		return s.channelTodoActor(ctx, p, host, conversation)
	})
	return channelTodoContract(v), err
}

func (s *ChatExtensions) MutateChannelTodo(ctx context.Context, p chat.Principal, host, conversation string, expected uint64, mutation chat.ChannelTodoMutation) (chat.ChannelTodoList, error) {
	if err := s.channelTodoActor(ctx, p, host, conversation); err != nil {
		return chat.ChannelTodoList{}, err
	}
	if host == "" {
		host = p.TenantID
	}
	v, err := s.TodoStore.MutateChannelTodo(ctx, host, p.TenantID, conversation, p.SubjectID, expected, channelTodoMutation(mutation), func(ctx context.Context) error {
		return s.channelTodoActor(ctx, p, host, conversation)
	})
	return channelTodoContract(v), err
}

func (s *ChatExtensions) ChannelWidgets(ctx context.Context, p chat.Principal, host, conversation string) (chat.ChannelWidgets, error) {
	if err := s.channelTodoActor(ctx, p, host, conversation); err != nil {
		return chat.ChannelWidgets{}, err
	}
	if host == "" {
		host = p.TenantID
	}
	v, err := s.TodoStore.ChannelWidgets(ctx, host, p.TenantID, conversation, p.SubjectID, func(ctx context.Context) error {
		return s.channelTodoActor(ctx, p, host, conversation)
	})
	return channelWidgetsContract(v), err
}

func (s *ChatExtensions) MutateChannelWidget(ctx context.Context, p chat.Principal, host, conversation string, expected uint64, mutation chat.ChannelWidgetMutation) (chat.ChannelWidgets, error) {
	if err := s.channelTodoActor(ctx, p, host, conversation); err != nil {
		return chat.ChannelWidgets{}, err
	}
	if host == "" {
		host = p.TenantID
	}
	v, err := s.TodoStore.MutateChannelWidget(ctx, host, p.TenantID, conversation, p.SubjectID, expected, channelWidgetMutation(mutation), func(ctx context.Context) error {
		return s.channelTodoActor(ctx, p, host, conversation)
	})
	return channelWidgetsContract(v), err
}

func channelTodoContract(v chatstore.ChannelTodoList) chat.ChannelTodoList {
	out := chat.ChannelTodoList{ConversationID: v.ConversationID, Revision: v.Revision, Pinned: v.Pinned}
	for _, item := range v.Items {
		x := chat.ChannelTodoItem{ID: item.ID, Text: item.Text, Completed: item.Completed, CreatedBy: item.CreatedBy, CreatedByHomeTenantID: item.CreatedByHomeTenantID, CreatedAtUnix: item.CreatedAtUnix, SourcePostID: item.SourcePostID, CompletedBySubjectID: item.CompletedBySubjectID, CompletedByHomeTenantID: item.CompletedByHomeTenantID, CompletedAtUnix: item.CompletedAtUnix, CompletionMode: item.CompletionMode, CanToggle: item.CanToggle, CanManageCompletionPolicy: item.CanManageCompletionPolicy}
		for _, member := range item.SelectedCompleters {
			x.SelectedCompleters = append(x.SelectedCompleters, chat.ChannelTodoSelectedMember{HomeTenantID: member.HomeTenantID, SubjectID: member.SubjectID})
		}
		out.Items = append(out.Items, x)
	}
	return out
}

func channelTodoMutation(v chat.ChannelTodoMutation) chatstore.ChannelTodoMutation {
	out := chatstore.ChannelTodoMutation{Operation: v.Operation, ItemID: v.ItemID, Text: v.Text, Completed: v.Completed, Pinned: v.Pinned, SourcePostID: v.SourcePostID, CompletionMode: v.CompletionMode}
	for _, member := range v.SelectedCompleters {
		out.SelectedCompleters = append(out.SelectedCompleters, chatstore.ChannelTodoSelectedMember{HomeTenantID: member.HomeTenantID, SubjectID: member.SubjectID})
	}
	return out
}

func channelWidgetsContract(v chatstore.ChannelWidgets) chat.ChannelWidgets {
	out := chat.ChannelWidgets{Team: chat.ChannelTeamWidget{ConversationID: v.Team.ConversationID, Revision: v.Team.Revision, Pinned: v.Team.Pinned, Purpose: v.Team.Purpose, CanPin: v.Team.CanPin}, Project: chat.ChannelProjectWidget{ConversationID: v.Project.ConversationID, Revision: v.Project.Revision, Pinned: v.Project.Pinned, Title: v.Project.Title, Summary: v.Project.Summary, CanPin: v.Project.CanPin}}
	for _, member := range v.Team.Members {
		out.Team.Members = append(out.Team.Members, chat.ChannelTeamMember{HomeTenantID: member.HomeTenantID, SubjectID: member.SubjectID, Role: member.Role, RoleLabel: member.RoleLabel})
	}
	for _, milestone := range v.Project.Milestones {
		out.Project.Milestones = append(out.Project.Milestones, chat.ChannelProjectMilestone{ID: milestone.ID, Text: milestone.Text, Status: milestone.Status, OwnerHomeTenantID: milestone.OwnerHomeTenantID, OwnerSubjectID: milestone.OwnerSubjectID, DueDate: milestone.DueDate})
	}
	return out
}

func channelWidgetMutation(v chat.ChannelWidgetMutation) chatstore.ChannelWidgetMutation {
	return chatstore.ChannelWidgetMutation{Kind: v.Kind, Operation: v.Operation, Pinned: v.Pinned, Purpose: v.Purpose, MemberHomeTenantID: v.MemberHomeTenantID, MemberSubjectID: v.MemberSubjectID, RoleLabel: v.RoleLabel, Title: v.Title, Summary: v.Summary, MilestoneID: v.MilestoneID, Milestone: chatstore.ChannelProjectMilestone{ID: v.Milestone.ID, Text: v.Milestone.Text, Status: v.Milestone.Status, OwnerHomeTenantID: v.Milestone.OwnerHomeTenantID, OwnerSubjectID: v.Milestone.OwnerSubjectID, DueDate: v.Milestone.DueDate}}
}
