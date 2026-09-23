package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

func (s *ChatExtensions) ChannelPoll(ctx context.Context, p chat.Principal, host, conversation string) (chatstore.ChannelPoll, error) {
	if err := s.channelTodoActor(ctx, p, host, conversation); err != nil {
		return chatstore.ChannelPoll{}, err
	}
	if host == "" {
		host = p.TenantID
	}
	return s.TodoStore.ChannelPoll(ctx, host, p.TenantID, conversation, p.SubjectID, func(ctx context.Context) error {
		return s.channelTodoActor(ctx, p, host, conversation)
	})
}

func (s *ChatExtensions) MutateChannelPoll(ctx context.Context, p chat.Principal, host, conversation string, expected uint64, mutation chatstore.ChannelPollMutation) (chatstore.ChannelPoll, error) {
	if err := s.channelTodoActor(ctx, p, host, conversation); err != nil {
		return chatstore.ChannelPoll{}, err
	}
	if host == "" {
		host = p.TenantID
	}
	return s.TodoStore.MutateChannelPoll(ctx, host, p.TenantID, conversation, p.SubjectID, expected, mutation, func(ctx context.Context) error {
		return s.channelTodoActor(ctx, p, host, conversation)
	})
}
