package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

// An agent's reaction lands only on the question of the person who asked it,
// only with an emoji from the fixed set, and is stored as the agent's own.
func TestTodo_AGENTUX_075_Security(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(100, 0).UTC()
	base := &fakeStore{
		conversation: conversation(),
		membership:   Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", HistoryVisibility: FullHistory},
		post:         Post{ID: "p1", TenantID: "t1", ConversationID: "c1", AuthorID: "u1", CreatedAt: now.Add(-time.Minute)},
	}
	recorder := &chat024MutationRecorder{fakeStore: base}
	s := NewService(recorder, func() time.Time { return now })
	s.SetAuthority(verifiedAuthority{store: base})

	ask := func(mutate func(*AgentQuestionReaction)) error {
		request := AgentQuestionReaction{TenantID: "t1", ConversationID: "c1", PostID: "p1", AgentSubjectID: "assistant", Asker: principal(), Emoji: "👀"}
		if mutate != nil {
			mutate(&request)
		}
		_, err := s.CommitAgentQuestionReaction(ctx, request)
		return err
	}
	if err := ask(nil); err != nil {
		t.Fatalf("the agent's reaction to the question was refused: %v", err)
	}
	if recorder.reaction.SubjectID != "assistant" || recorder.reaction.Emoji != "👀" || recorder.reaction.PostID != "p1" || !recorder.reaction.CreatedAt.Equal(now) {
		t.Fatalf("stored reaction %+v, want the agent's own 👀 at the server's time", recorder.reaction)
	}
	puts := recorder.puts
	for name, mutate := range map[string]func(*AgentQuestionReaction){
		"an emoji outside the set":        func(r *AgentQuestionReaction) { r.Emoji = "💣" },
		"an empty emoji":                  func(r *AgentQuestionReaction) { r.Emoji = "" },
		"the asker reacting as the agent": func(r *AgentQuestionReaction) { r.AgentSubjectID = r.Asker.SubjectID },
		"no agent":                        func(r *AgentQuestionReaction) { r.AgentSubjectID = " " },
		"another person's message":        func(r *AgentQuestionReaction) { r.Asker.SubjectID = "u2" },
		"a foreign tenant asker":          func(r *AgentQuestionReaction) { r.Asker.TenantID = "t2" },
	} {
		if err := ask(mutate); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// A message another person wrote is not a question to this agent.
	base.post.AuthorID = "u2"
	if err := ask(nil); !errors.Is(err, ErrPermissionDenied) {
		t.Errorf("a reaction to a message the asker did not write: %v", err)
	}
	if recorder.puts != puts {
		t.Fatalf("a refused reaction was stored: %d writes, want %d", recorder.puts, puts)
	}
}
