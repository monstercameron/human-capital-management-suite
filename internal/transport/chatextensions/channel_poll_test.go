package chatextensions

import (
	"context"
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type pollService struct {
	Service
	principal chat.Principal
	mutation  chat.ChannelPollMutation
	expected  uint64
}

func (s *pollService) ChannelPoll(_ context.Context, p chat.Principal, _, conversation string) (chat.ChannelPoll, error) {
	s.principal = p
	return chat.ChannelPoll{ConversationID: conversation, Revision: 3, Question: "Lunch?", TotalVotes: 2, MyOptionID: "b", Options: []chat.ChannelPollOption{{ID: "a", Text: "Pizza", Count: 1}, {ID: "b", Text: "Sushi", Count: 1}}}, nil
}

func (s *pollService) MutateChannelPoll(_ context.Context, p chat.Principal, _, conversation string, expected uint64, mutation chat.ChannelPollMutation) (chat.ChannelPoll, error) {
	s.principal, s.expected, s.mutation = p, expected, mutation
	return chat.ChannelPoll{ConversationID: conversation, Revision: expected + 1, Question: mutation.Question, MyOptionID: mutation.OptionID}, nil
}

func TestChannelPollRPCMapsCallerProjectionAndMutation(t *testing.T) {
	svc := &pollService{}
	s := &server{deps: Dependencies{Service: svc}}
	ctx := grantContext(t, "home")
	response, err := s.GetChannelPoll(ctx, &chatv1.GetChannelPollRequest{HostTenantId: "host", ConversationId: "channel"})
	if err != nil || response.GetPoll().GetQuestion() != "Lunch?" || response.GetPoll().GetRevision() != 3 || response.GetPoll().GetTotalVotes() != 2 || response.GetPoll().GetMyOptionId() != "b" || response.GetPoll().GetOptions()[1].GetVoteCount() != 1 || svc.principal.SubjectID != "admin" {
		t.Fatalf("read = %+v principal=%+v err=%v", response, svc.principal, err)
	}
	changed, err := s.MutateChannelPoll(ctx, &chatv1.MutateChannelPollRequest{HostTenantId: "host", ConversationId: "channel", ExpectedRevision: 3, Operation: "CREATE", Question: "Team lunch?", Options: []string{"A", "B"}})
	if err != nil || changed.GetPoll().GetRevision() != 4 || svc.expected != 3 || svc.mutation.Operation != "CREATE" || svc.mutation.Question != "Team lunch?" || len(svc.mutation.Options) != 2 || svc.principal.TenantID != "home" {
		t.Fatalf("write = %+v service=%+v err=%v", changed, svc, err)
	}
}
