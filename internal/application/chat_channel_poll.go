package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

func (s *ChatExtensions) ChannelPoll(ctx context.Context, p chat.Principal, host, conversation string) (chat.ChannelPoll, error) {
	if err := s.channelTodoActor(ctx, p, host, conversation); err != nil {
		return chat.ChannelPoll{}, err
	}
	if host == "" {
		host = p.TenantID
	}
	v, err := s.TodoStore.ChannelPoll(ctx, host, p.TenantID, conversation, p.SubjectID, func(ctx context.Context) error {
		return s.channelTodoActor(ctx, p, host, conversation)
	})
	return channelPollContract(v), err
}

func (s *ChatExtensions) MutateChannelPoll(ctx context.Context, p chat.Principal, host, conversation string, expected uint64, mutation chat.ChannelPollMutation) (chat.ChannelPoll, error) {
	if err := s.channelTodoActor(ctx, p, host, conversation); err != nil {
		return chat.ChannelPoll{}, err
	}
	if host == "" {
		host = p.TenantID
	}
	v, err := s.TodoStore.MutateChannelPoll(ctx, host, p.TenantID, conversation, p.SubjectID, expected, chatstore.ChannelPollMutation{Operation: mutation.Operation, Question: mutation.Question, Options: mutation.Options, OptionID: mutation.OptionID}, func(ctx context.Context) error {
		return s.channelTodoActor(ctx, p, host, conversation)
	})
	return channelPollContract(v), err
}

func channelPollContract(v chatstore.ChannelPoll) chat.ChannelPoll {
	out := chat.ChannelPoll{ConversationID: v.ConversationID, Revision: v.Revision, Question: v.Question, MyOptionID: v.MyOptionID, TotalVotes: v.TotalVotes}
	for _, option := range v.Options {
		out.Options = append(out.Options, chat.ChannelPollOption{ID: option.ID, Text: option.Text, Count: option.Count})
	}
	return out
}
