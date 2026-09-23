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

func (s *ChatExtensions) ChannelTodo(ctx context.Context, p chat.Principal, host, conversation string) (chatstore.ChannelTodoList, error) {
	if err := s.channelTodoActor(ctx, p, host, conversation); err != nil {
		return chatstore.ChannelTodoList{}, err
	}
	if host == "" {
		host = p.TenantID
	}
	return s.TodoStore.ChannelTodo(ctx, host, p.TenantID, conversation, p.SubjectID, func(ctx context.Context) error {
		return s.channelTodoActor(ctx, p, host, conversation)
	})
}

func (s *ChatExtensions) MutateChannelTodo(ctx context.Context, p chat.Principal, host, conversation string, expected uint64, mutation chatstore.ChannelTodoMutation) (chatstore.ChannelTodoList, error) {
	if err := s.channelTodoActor(ctx, p, host, conversation); err != nil {
		return chatstore.ChannelTodoList{}, err
	}
	if host == "" {
		host = p.TenantID
	}
	return s.TodoStore.MutateChannelTodo(ctx, host, p.TenantID, conversation, p.SubjectID, expected, mutation, func(ctx context.Context) error {
		return s.channelTodoActor(ctx, p, host, conversation)
	})
}

func (s *ChatExtensions) ChannelWidgets(ctx context.Context, p chat.Principal, host, conversation string) (chatstore.ChannelWidgets, error) {
	if err := s.channelTodoActor(ctx, p, host, conversation); err != nil {
		return chatstore.ChannelWidgets{}, err
	}
	if host == "" {
		host = p.TenantID
	}
	return s.TodoStore.ChannelWidgets(ctx, host, p.TenantID, conversation, p.SubjectID, func(ctx context.Context) error {
		return s.channelTodoActor(ctx, p, host, conversation)
	})
}

func (s *ChatExtensions) MutateChannelWidget(ctx context.Context, p chat.Principal, host, conversation string, expected uint64, mutation chatstore.ChannelWidgetMutation) (chatstore.ChannelWidgets, error) {
	if err := s.channelTodoActor(ctx, p, host, conversation); err != nil {
		return chatstore.ChannelWidgets{}, err
	}
	if host == "" {
		host = p.TenantID
	}
	return s.TodoStore.MutateChannelWidget(ctx, host, p.TenantID, conversation, p.SubjectID, expected, mutation, func(ctx context.Context) error {
		return s.channelTodoActor(ctx, p, host, conversation)
	})
}
