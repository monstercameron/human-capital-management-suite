package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

// agentUX075StoreRecorder is a store whose agent-question port records what an
// agent that is not a member of the conversation puts and takes away.
type agentUX075StoreRecorder struct {
	*fakeStore
	memberPuts, agentPuts int
	removed               []string
	put                   []Reaction
}

func (s *agentUX075StoreRecorder) PutReaction(_ context.Context, r Reaction) (Reaction, error) {
	s.memberPuts++
	return r, nil
}

func (s *agentUX075StoreRecorder) PutAgentQuestionReaction(_ context.Context, r Reaction) (Reaction, error) {
	s.agentPuts++
	s.put = append(s.put, r)
	return r, nil
}

func (s *agentUX075StoreRecorder) RemoveAgentQuestionReaction(_ context.Context, _, _, _, _, subject, emoji string) error {
	s.removed = append(s.removed, subject+":"+emoji)
	return nil
}

// An agent reacts wherever it may answer, member of the conversation or not, and
// replaces its first reaction with the one that fits the outcome (AGENTUX-075,
// AGENTUX-052); the replaced emoji must itself be in the closed set.
func TestTodo_AGENTUX_075_Outcome(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(100, 0).UTC()
	base := &fakeStore{
		conversation: conversation(),
		membership:   Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", HistoryVisibility: FullHistory},
		post:         Post{ID: "p1", TenantID: "t1", ConversationID: "c1", AuthorID: "u1", CreatedAt: now.Add(-time.Minute)},
	}
	recorder := &agentUX075StoreRecorder{fakeStore: base}
	s := NewService(recorder, func() time.Time { return now })
	s.SetAuthority(verifiedAuthority{store: base})

	request := AgentQuestionReaction{TenantID: "t1", ConversationID: "c1", PostID: "p1", AgentSubjectID: "assistant", Asker: principal(), Emoji: AgentOutcomeAnswered(), Replaces: "🔎"}
	if _, err := s.CommitAgentQuestionReaction(ctx, request); err != nil {
		t.Fatalf("the outcome reaction was refused: %v", err)
	}
	if recorder.memberPuts != 0 || recorder.agentPuts != 1 || recorder.put[0].SubjectID != "assistant" || recorder.put[0].Emoji != "✅" {
		t.Fatalf("the reaction went through the member path or lost its author: %+v", recorder)
	}
	if len(recorder.removed) != 1 || recorder.removed[0] != "assistant:🔎" {
		t.Fatalf("the first reaction was not taken away: %v", recorder.removed)
	}
	// Replacing an emoji with itself takes nothing away.
	same := request
	same.Emoji, same.Replaces = "🔎", "🔎"
	if _, err := s.CommitAgentQuestionReaction(ctx, same); err != nil || len(recorder.removed) != 1 {
		t.Fatalf("an emoji replacing itself: %v removed=%v", err, recorder.removed)
	}
	// The closed set: nothing outside it is put or taken away, and the table's
	// three outcomes are in it.
	for _, outcome := range []string{AgentOutcomeAnswered(), AgentOutcomeCouldNotAnswer(), AgentOutcomeFailed()} {
		if !ValidAgentQuestionReactionEmoji(outcome) {
			t.Errorf("outcome %q is not in the closed set", outcome)
		}
	}
	for name, mutate := range map[string]func(*AgentQuestionReaction){
		"an emoji outside the set put":       func(r *AgentQuestionReaction) { r.Emoji = "💣" },
		"an emoji outside the set removed":   func(r *AgentQuestionReaction) { r.Replaces = "💣" },
		"another person's message":           func(r *AgentQuestionReaction) { r.Asker.SubjectID = "u2" },
		"the asker reacting as the agent":    func(r *AgentQuestionReaction) { r.AgentSubjectID = r.Asker.SubjectID },
		"a conversation the post is not in":  func(r *AgentQuestionReaction) { r.ConversationID = "" },
		"an outcome on a message not theirs": nil,
	} {
		if mutate == nil {
			continue
		}
		bad := request
		mutate(&bad)
		if _, err := s.CommitAgentQuestionReaction(ctx, bad); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	base.post.AuthorID = "u2"
	if _, err := s.CommitAgentQuestionReaction(ctx, request); !errors.Is(err, ErrPermissionDenied) {
		t.Errorf("a reaction to a message the asker did not write: %v", err)
	}
}
