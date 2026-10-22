package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

type agentux052Store struct {
	*fakeStore
	policies  map[string]AgentReactionPolicy
	policyErr error
	writes    int
}

func (s *agentux052Store) ReadAgentReactionPolicy(_ context.Context, _, conversation string) (AgentReactionPolicy, *AgentReactionPolicy, error) {
	if s.policyErr != nil {
		return DefaultAgentReactionPolicy(), nil, s.policyErr
	}
	workspace := DefaultAgentReactionPolicy()
	if p, ok := s.policies[""]; ok {
		workspace = p
	}
	if conversation == "" {
		return workspace, nil, nil
	}
	if p, ok := s.policies[conversation]; ok {
		channel := p
		return workspace, &channel, nil
	}
	return workspace, nil, nil
}

func (s *agentux052Store) WriteAgentReactionPolicy(_ context.Context, _, conversation string, p AgentReactionPolicy, _ string) error {
	s.writes++
	if s.policies == nil {
		s.policies = map[string]AgentReactionPolicy{}
	}
	s.policies[conversation] = p
	return nil
}

func agentux052Service(policies map[string]AgentReactionPolicy, err error) (*Service, *agentux052Store) {
	now := time.Now().UTC()
	f := &fakeStore{
		conversation: Conversation{ID: "room", TenantID: "t1", Kind: PublicChannel, Revision: 1},
		membership:   Membership{ConversationID: "room", TenantID: "t1", SubjectID: "u1", Revision: 1},
		post:         Post{ID: "q", TenantID: "t1", ConversationID: "room", AuthorID: "u1", Body: "a question"},
	}
	s := &agentux052Store{fakeStore: f, policies: policies, policyErr: err}
	service := NewService(s, func() time.Time { return now })
	service.SetAuthority(verifiedAuthority{store: f})
	return service, s
}

// The workspace and conversation switches gate an agent's reaction at commit
// time: either switch off refuses it, the conversation's row wins over the
// workspace's, and a switch nobody set (or that cannot be read) stays on, like
// the persona owner's switch (AGENTUX-052).
func TestAgentUX052_ReactionPolicyGatesCommit(t *testing.T) {
	asker := Principal{TenantID: "t1", SubjectID: "u1"}
	react := func(service *Service) error {
		_, err := service.CommitAgentQuestionReaction(context.Background(), AgentQuestionReaction{TenantID: "t1", ConversationID: "room", PostID: "q", AgentSubjectID: "assistant", Asker: asker, Emoji: "👀"})
		return err
	}
	off := AgentReactionPolicy{ReactionsEnabled: false}
	on := AgentReactionPolicy{ReactionsEnabled: true}
	for _, test := range []struct {
		name     string
		policies map[string]AgentReactionPolicy
		wantErr  error
	}{
		{"no rows anywhere", nil, nil},
		{"workspace on, conversation silent", map[string]AgentReactionPolicy{"": on}, nil},
		{"workspace off", map[string]AgentReactionPolicy{"": off}, ErrAgentReactionsDisabled},
		{"conversation off", map[string]AgentReactionPolicy{"room": off}, ErrAgentReactionsDisabled},
		{"conversation on wins over workspace off", map[string]AgentReactionPolicy{"": off, "room": on}, nil},
		{"conversation off wins over workspace on", map[string]AgentReactionPolicy{"": on, "room": off}, ErrAgentReactionsDisabled},
	} {
		service, _ := agentux052Service(test.policies, nil)
		if err := react(service); !errors.Is(err, test.wantErr) {
			t.Errorf("%s: commit err = %v, want %v", test.name, err, test.wantErr)
		}
	}
	// An unreadable switch leaves the reaction on: it is cosmetic, and a lost
	// setting must not become a silent agent.
	service, _ := agentux052Service(nil, errors.New("the settings row is down"))
	if err := react(service); err != nil {
		t.Errorf("an unreadable switch refused the reaction: %v", err)
	}
	// A store from before the settings existed (no policy port) still commits.
	legacy := &fakeStore{
		conversation: Conversation{ID: "room", TenantID: "t1", Kind: PublicChannel, Revision: 1},
		membership:   Membership{ConversationID: "room", TenantID: "t1", SubjectID: "u1", Revision: 1},
		post:         Post{ID: "q", TenantID: "t1", ConversationID: "room", AuthorID: "u1", Body: "a question"},
	}
	if err := react(newTestService(legacy, time.Now)); err != nil {
		t.Errorf("a store without the policy port refused the reaction: %v", err)
	}
}
