package application

import (
	"context"
	"errors"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type agentux052Visibility struct {
	private bool
	err     error
}

func (v agentux052Visibility) AnswerWasPrivate(context.Context, chatcore.Principal, string, string, string, string) (bool, error) {
	return v.private, v.err
}

// A private answer in a channel keeps ✅: the chooser stands down, so the
// reaction cannot reveal the answer's topic. An answer whose visibility cannot
// be read is treated as private; only a known-public answer may carry the
// chooser's contextual pick (AGENTUX-052).
func TestAgentUX052_PrivateAnswerKeepsCheck(t *testing.T) {
	ctx := context.Background()
	post := chatcore.Post{ID: "question", TenantID: "t1", ConversationID: "c1", AuthorID: "u1", Body: "When is payday?"}
	asker := chatcore.Principal{TenantID: "t1", SubjectID: "u1"}
	for _, test := range []struct {
		name       string
		visibility personaAnswerVisibility
		want       string
	}{
		{"public", agentux052Visibility{private: false}, "💰"},
		{"private", agentux052Visibility{private: true}, "✅"},
		{"unreadable", agentux052Visibility{err: errors.New("the thread read is down")}, "✅"},
		{"no reader", nil, "💰"},
	} {
		committer := &agentUX075Committer{}
		reactor := servedPersonaQuestionReactor{chat: committer, chooser: agentUX075Chooser{pick: "💰"}, visibility: test.visibility}
		reactor.ReactToOutcome(ctx, post, asker, "assistant", chatcore.AgentOutcomeAnswered())
		if len(committer.reactions) != 1 || committer.reactions[0].Emoji != test.want {
			t.Errorf("%s: %v, want one %s", test.name, committer.reactions, test.want)
		}
	}
}

// servedAnswerVisibility over durable chat: an answer is private when the
// agent posted no thread reply to the question outside the asker's direct
// conversation with it; a direct answer is public, and a conversation that
// cannot be read answers private with its error.
func TestAgentUX052_ServedVisibilityReadsDurableChat(t *testing.T) {
	room := newAgentUX075Chat(t)
	visibility := newServedAnswerVisibility(room.service)
	asker := room.asker
	channel := room.ask(t, room.channel, "agentux052-visibility-channel", "@Assistant list all the holidays")
	if private, err := visibility.AnswerWasPrivate(room.ctx, asker, "tenant-a", room.channel, channel.ID, "assistant"); err != nil || !private {
		t.Fatalf("a channel question with no thread reply: private=%v err=%v", private, err)
	}
	reply, err := room.service.SendPost(room.ctx, chatcore.SendPostRequest{Principal: asker, TenantID: "tenant-a", ConversationID: room.channel, ParentID: channel.ID, Body: "public thread reply", IdempotencyKey: "agentux052-visibility-reply"})
	if err != nil {
		t.Fatal(err)
	}
	_ = reply
	// A reply by anyone else is not the agent's answer.
	if private, err := visibility.AnswerWasPrivate(room.ctx, asker, "tenant-a", room.channel, channel.ID, "assistant"); err != nil || !private {
		t.Fatalf("a channel question with only another person's reply: private=%v err=%v", private, err)
	}
	direct := room.ask(t, room.direct, "agentux052-visibility-direct", "a direct question")
	if private, err := visibility.AnswerWasPrivate(room.ctx, asker, "tenant-a", room.direct, direct.ID, "assistant"); err != nil || private {
		t.Fatalf("a direct question: private=%v err=%v", private, err)
	}
	if private, err := visibility.AnswerWasPrivate(room.ctx, asker, "tenant-a", "no-such-room", channel.ID, "assistant"); err == nil || !private {
		t.Fatalf("an unreadable conversation: private=%v err=%v", private, err)
	}
}
