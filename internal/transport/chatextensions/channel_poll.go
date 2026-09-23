package chatextensions

import (
	"context"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

func channelPollOut(v chatstore.ChannelPoll) *chatv1.ChannelPoll {
	out := &chatv1.ChannelPoll{ConversationId: v.ConversationID, Revision: v.Revision, Question: v.Question, MyOptionId: v.MyOptionID, TotalVotes: uint32(v.TotalVotes)}
	for _, option := range v.Options {
		out.Options = append(out.Options, &chatv1.ChannelPollOption{Id: option.ID, Text: option.Text, VoteCount: uint32(option.Count)})
	}
	return out
}

func (s *server) GetChannelPoll(ctx context.Context, r *chatv1.GetChannelPollRequest) (*chatv1.GetChannelPollResponse, error) {
	p, err := s.principal(ctx)
	if err != nil {
		return nil, err
	}
	svc, ok := s.deps.Service.(ChannelPollService)
	if !ok {
		return nil, mapped(chat.ErrUnavailable)
	}
	v, err := svc.ChannelPoll(ctx, p, r.GetHostTenantId(), r.GetConversationId())
	if err != nil {
		return nil, mapped(err)
	}
	return &chatv1.GetChannelPollResponse{Poll: channelPollOut(v)}, nil
}

func (s *server) MutateChannelPoll(ctx context.Context, r *chatv1.MutateChannelPollRequest) (*chatv1.MutateChannelPollResponse, error) {
	p, err := s.principal(ctx)
	if err != nil {
		return nil, err
	}
	svc, ok := s.deps.Service.(ChannelPollService)
	if !ok {
		return nil, mapped(chat.ErrUnavailable)
	}
	mutation := chatstore.ChannelPollMutation{Operation: r.GetOperation(), Question: r.GetQuestion(), Options: r.GetOptions(), OptionID: r.GetOptionId()}
	v, err := svc.MutateChannelPoll(ctx, p, r.GetHostTenantId(), r.GetConversationId(), r.GetExpectedRevision(), mutation)
	if err != nil {
		return nil, mapped(err)
	}
	return &chatv1.MutateChannelPollResponse{Poll: channelPollOut(v)}, nil
}
