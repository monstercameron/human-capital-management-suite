package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// TestTodo_CHATBUG_074 covers the store half of "a poll can be closed by its
// author and a new one started": the channel held one poll for good, with no
// way to end it.
func TestTodo_CHATBUG_074(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "polls", TenantID: "tenant-a", Kind: "PUBLIC_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", Role: "manager", State: "active"}, {MemberID: "author", Role: "member", State: "active"}, {MemberID: "worker", Role: "member", State: "active"}})
	ctx := context.Background()
	mutate := func(subject string, expected uint64, m ChannelPollMutation) (ChannelPoll, error) {
		return s.MutateChannelPoll(ctx, "tenant-a", "tenant-a", "polls", subject, expected, m, todoAllow)
	}
	if _, err := mutate("author", 1, ChannelPollMutation{Operation: "CLOSE"}); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("closing a channel with no poll: %v", err)
	}
	poll, err := mutate("author", 1, ChannelPollMutation{Operation: "CREATE", Question: "Lunch?", Options: []string{"Tacos", "Pho"}})
	if err != nil {
		t.Fatal(err)
	}
	poll, err = mutate("worker", poll.Revision, ChannelPollMutation{Operation: "VOTE", OptionID: poll.Options[0].ID})
	if err != nil || poll.TotalVotes != 1 {
		t.Fatalf("vote %+v %v", poll, err)
	}
	if _, err := mutate("worker", poll.Revision, ChannelPollMutation{Operation: "CLOSE"}); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("a member who did not start the poll closed it: %v", err)
	}
	if _, err := mutate("author", poll.Revision, ChannelPollMutation{Operation: "CLOSE", Question: "smuggled"}); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("close with a question: %v", err)
	}
	if _, err := mutate("author", poll.Revision-1, ChannelPollMutation{Operation: "CLOSE"}); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("close against a stale revision: %v", err)
	}
	closed, err := mutate("author", poll.Revision, ChannelPollMutation{Operation: "CLOSE"})
	if err != nil || closed.Question != "" || len(closed.Options) != 0 || closed.TotalVotes != 0 || closed.Revision != poll.Revision+1 {
		t.Fatalf("closed poll = %+v, %v", closed, err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		var ballots int
		var operation, question string
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_channel_poll_vote WHERE tenant_id='tenant-a' AND conversation_id='polls'`).Scan(&ballots); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT operation,prior_question FROM chat_channel_poll_revision WHERE tenant_id='tenant-a' AND conversation_id='polls' AND revision=$1`, closed.Revision).Scan(&operation, &question); err != nil {
			return err
		}
		if ballots != 0 || operation != "CLOSE" || question != "Lunch?" {
			t.Errorf("after close: %d ballots, audit row %s %q", ballots, operation, question)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A new poll starts, with nobody's old ballot in it.
	next, err := mutate("worker", closed.Revision, ChannelPollMutation{Operation: "CREATE", Question: "Dinner?", Options: []string{"Sushi", "Pizza"}})
	if err != nil || next.Question != "Dinner?" || next.TotalVotes != 0 || next.MyOptionID != "" {
		t.Fatalf("new poll = %+v, %v", next, err)
	}
	// A channel manager may close a poll someone else started.
	if ended, err := mutate("owner", next.Revision, ChannelPollMutation{Operation: "CLOSE"}); err != nil || ended.Question != "" {
		t.Fatalf("manager close = %+v, %v", ended, err)
	}
}
