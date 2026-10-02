package chatroutingadapter

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
)

type agentReactionFake struct {
	*fakeService
	calls int
	lease chatrouting.WriteLease
}

func (f *agentReactionFake) CommitAgentQuestionReaction(ctx context.Context, r chat.AgentQuestionReaction) (chat.Reaction, error) {
	f.calls++
	lease, ok := chatrouting.WriteLeaseFromContext(ctx)
	if !ok {
		return chat.Reaction{}, errors.New("missing route lease")
	}
	f.lease = lease
	return chat.Reaction{TenantID: r.TenantID, ConversationID: r.ConversationID, PostID: r.PostID, SubjectID: r.AgentSubjectID, Emoji: r.Emoji}, nil
}

// An agent's reaction to the question it was asked is a write to a routed
// conversation: it carries the conversation's current route lease, and a stale
// route stops it before the store (AGENTUX-075). Without the lease the server
// refused every such reaction.
func TestTodo_AGENTUX_075_ReactionPassesRouteLease(t *testing.T) {
	f := &agentReactionFake{fakeService: &fakeService{}}
	s, d := newAdapter(t, f)
	route, err := d.Reserve(context.Background(), chatrouting.ReserveRequest{ConversationID: "c-dm", HostTenantID: "t1", ShardID: "s1", IdempotencyKey: "agent-reaction-route"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Activate(context.Background(), "c-dm", "t1", route.Epoch); err != nil {
		t.Fatal(err)
	}
	request := chat.AgentQuestionReaction{TenantID: "t1", ConversationID: "c-dm", PostID: "p1", AgentSubjectID: "assistant", Asker: chat.Principal{TenantID: "t1", SubjectID: "u1"}, Emoji: "👀"}
	reaction, err := s.CommitAgentQuestionReaction(context.Background(), request)
	if err != nil || reaction.Emoji != "👀" || reaction.SubjectID != "assistant" {
		t.Fatalf("reaction = %+v err=%v", reaction, err)
	}
	if f.calls != 1 || f.lease.Route.HostTenantID != "t1" || f.lease.Route.Epoch != route.Epoch {
		t.Fatalf("calls=%d lease=%+v: the reaction did not carry the route lease", f.calls, f.lease)
	}
	// A conversation moving to another shard is not written to.
	if _, err = d.BeginMove(context.Background(), "c-dm", "t1", route.Epoch, "s2"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommitAgentQuestionReaction(context.Background(), request); !errors.Is(err, chatrouting.ErrStaleEpoch) && !errors.Is(err, chatrouting.ErrNotWritable) {
		t.Fatalf("stale reaction = %v", err)
	}
	if f.calls != 1 {
		t.Fatalf("a stale reaction reached chat: %d calls", f.calls)
	}
	// Beneath a service that cannot store it, the reaction fails closed.
	plain, _ := newAdapter(t, &fakeService{})
	if _, err = plain.CommitAgentQuestionReaction(context.Background(), request); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("without the extension = %v", err)
	}
}
