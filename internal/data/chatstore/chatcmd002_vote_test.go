package chatstore

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

// chatcmd002PollFixture posts a poll message in a channel with three members
// and returns a vote request for "worker" on the first option.
func chatcmd002PollFixture(t *testing.T, change func(*chat.Chatcmd002Poll)) (*Store, chat.Chatcmd002Request, chat.Post) {
	t.Helper()
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "cards", TenantID: "tenant-a", Kind: "PUBLIC_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", Role: "manager", State: "active"}, {MemberID: "worker", Role: "member", State: "active"}, {MemberID: "third", Role: "member", State: "active"}})
	poll := &chat.Chatcmd002Poll{Results: "always", AddOptions: "author", Options: []chat.ChannelPollOption{{ID: "tacos", Text: "Tacos"}, {ID: "pho", Text: "Pho"}}}
	if change != nil {
		change(poll)
	}
	body, err := (chat.Chatcmd002Card{Kind: "poll", Title: "Lunch spot?", Poll: poll}).Body()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s.sendPostRaw(context.Background(), SendRequest{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "cards", AuthorID: "owner", ClientKey: "poll-1", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	post, err := chatPost(raw)
	if err != nil {
		t.Fatal(err)
	}
	return s, chat.Chatcmd002Request{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "worker"}, TenantID: "tenant-a", ConversationID: "cards", PostID: post.ID, ExpectedRevision: post.Revision, Mutation: chat.Chatcmd002Mutation{Operation: "VOTE", Options: []string{"tacos"}}}, post
}

func chatcmd002As(r chat.Chatcmd002Request, subject string, options ...string) chat.Chatcmd002Request {
	r.Principal.SubjectID = subject
	r.Mutation = chat.Chatcmd002Mutation{Operation: "VOTE", Options: options}
	return r
}

func chatcmd002Counts(t *testing.T, s *Store, r chat.Chatcmd002Request) (map[string]int, chat.Chatcmd002View) {
	t.Helper()
	view, err := s.Chatcmd002Read(context.Background(), r, todoAllow)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, option := range view.Card.Poll.Options {
		counts[option.ID] = option.Count
	}
	return counts, view
}

func chatcmd002VoteRows(t *testing.T, s *Store, post string) []string {
	t.Helper()
	var out []string
	ctx := context.Background()
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT subject_id||'='||option_id FROM chat_post_card_vote WHERE tenant_id='tenant-a' AND post_id=$1 ORDER BY 1`, post)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row string
			if err := rows.Scan(&row); err != nil {
				return err
			}
			out = append(out, row)
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestTodo_CHATBUG_057 is the defect itself: pressing Vote on a poll posted in
// the conversation stores the ballot, the count every member reads changes, and
// one event tells the other readers.
func TestTodo_CHATBUG_057(t *testing.T) {
	s, r, post := chatcmd002PollFixture(t, nil)
	ctx := context.Background()
	voted, err := s.Chatcmd002Mutate(ctx, r, todoAllow)
	if err != nil || voted.Revision != post.Revision+1 {
		t.Fatalf("vote %+v %v", voted, err)
	}
	if rows := chatcmd002VoteRows(t, s, post.ID); strings.Join(rows, ",") != "worker=tacos" {
		t.Fatalf("stored ballots %v", rows)
	}
	counts, mine := chatcmd002Counts(t, s, r)
	if counts["tacos"] != 1 || counts["pho"] != 0 || strings.Join(mine.MyOptions, ",") != "tacos" || !mine.Voted || !mine.ResultsVisible || mine.Revision != voted.Revision {
		t.Fatalf("voter reads %+v", mine)
	}
	other := chatcmd002As(r, "owner")
	counts, theirs := chatcmd002Counts(t, s, other)
	if counts["tacos"] != 1 || len(theirs.MyOptions) != 0 || theirs.Voted || len(theirs.Voters["tacos"]) != 1 || theirs.Voters["tacos"][0].SubjectID != "worker" {
		t.Fatalf("another member reads %+v", theirs)
	}
	card, ok := chat.Chatcmd002Decode(voted.Body)
	if !ok || card.Poll.Options[0].Count != 1 || !card.Interacted {
		t.Fatalf("message body %+v", card)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		var events int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_outbox WHERE tenant_id='tenant-a' AND aggregate_id=$1 AND event_type='post.edited'`, post.ID).Scan(&events); err != nil {
			return err
		}
		if events != 1 {
			t.Errorf("events %d, want 1", events)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The same ballot again changes nothing and tells nobody.
	again, err := s.Chatcmd002Mutate(ctx, r, todoAllow)
	if err != nil || again.Revision != voted.Revision {
		t.Fatalf("repeated vote %+v %v", again, err)
	}
	// A changed mind moves the vote; it is never counted twice. The request
	// still carries the revision the voter first saw.
	moved, err := s.Chatcmd002Mutate(ctx, chatcmd002As(r, "worker", "pho"), todoAllow)
	if err != nil || moved.Revision != voted.Revision+1 {
		t.Fatalf("changed vote %+v %v", moved, err)
	}
	if counts, _ := chatcmd002Counts(t, s, r); counts["tacos"] != 0 || counts["pho"] != 1 {
		t.Fatalf("after change %v", counts)
	}
	// Withdrawing leaves no ballot.
	if _, err := s.Chatcmd002Mutate(ctx, chatcmd002As(r, "worker"), todoAllow); err != nil {
		t.Fatal(err)
	}
	if rows := chatcmd002VoteRows(t, s, post.ID); len(rows) != 0 {
		t.Fatalf("withdrawn ballot kept %v", rows)
	}
}

func TestTodo_CHATBUG_057_Security(t *testing.T) {
	s, r, post := chatcmd002PollFixture(t, nil)
	ctx := context.Background()
	if _, err := s.Chatcmd002Mutate(ctx, chatcmd002As(r, "worker", "tacos", "pho"), todoAllow); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("two choices in a one-choice poll: %v", err)
	}
	if _, err := s.Chatcmd002Mutate(ctx, chatcmd002As(r, "worker", "forged"), todoAllow); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("unknown option: %v", err)
	}
	if _, err := s.Chatcmd002Mutate(ctx, chatcmd002As(r, "outsider", "tacos"), todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("non-member voted: %v", err)
	}
	if rows := chatcmd002VoteRows(t, s, post.ID); len(rows) != 0 {
		t.Fatalf("refused votes stored %v", rows)
	}
	// Counts written into a body by hand are not votes.
	forged, err := (chat.Chatcmd002Card{Kind: "poll", Title: "Forged?", Poll: &chat.Chatcmd002Poll{Results: "always", AddOptions: "author", Options: []chat.ChannelPollOption{{ID: "a", Text: "A", Count: 99}, {ID: "b", Text: "B"}}}}).Body()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "cards", AuthorID: "worker", ClientKey: "forged", Body: forged})
	if err != nil {
		t.Fatal(err)
	}
	fake := r
	fake.PostID = raw.ID
	if counts, _ := chatcmd002Counts(t, s, fake); counts["a"] != 0 {
		t.Fatalf("forged count shown %v", counts)
	}
	// A closed poll takes no vote, and reopening is the author's.
	closing := r
	closing.Principal.SubjectID = "owner"
	closing.Mutation = chat.Chatcmd002Mutation{Operation: "CLOSE"}
	closed, err := s.Chatcmd002Mutate(ctx, closing, todoAllow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Chatcmd002Mutate(ctx, r, todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("closed poll voted: %v", err)
	}
	stale := closing
	stale.Mutation.Operation = "REOPEN"
	if _, err := s.Chatcmd002Mutate(ctx, stale, todoAllow); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("reopen against a stale revision: %v", err)
	}
	stale.ExpectedRevision = closed.Revision
	if _, err := s.Chatcmd002Mutate(ctx, stale, todoAllow); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Chatcmd002Mutate(ctx, r, todoAllow); err != nil {
		t.Fatalf("reopened poll refused a vote: %v", err)
	}
}

// An anonymous poll stores that a person voted and, separately, how many chose
// each option. Nothing stored joins the two, and the body does not move with a
// vote, so the revision and event that name the voter say nothing of the choice.
func TestTodo_CHATBUG_057_Anonymous(t *testing.T) {
	s, r, post := chatcmd002PollFixture(t, func(p *chat.Chatcmd002Poll) { p.Anonymous, p.Multiple = true, true })
	ctx := context.Background()
	voted, err := s.Chatcmd002Mutate(ctx, chatcmd002As(r, "worker", "tacos", "pho"), todoAllow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Chatcmd002Mutate(ctx, chatcmd002As(r, "third", "pho"), todoAllow); err != nil {
		t.Fatal(err)
	}
	if rows := chatcmd002VoteRows(t, s, post.ID); strings.Join(rows, ",") != "third=,worker=" {
		t.Fatalf("anonymous ballots name a choice: %v", rows)
	}
	card, _ := chat.Chatcmd002Decode(voted.Body)
	if card.Poll.Options[0].Count != 0 || card.Poll.Options[1].Count != 0 {
		t.Fatalf("anonymous body carries counts %+v", card.Poll.Options)
	}
	counts, view := chatcmd002Counts(t, s, r)
	if counts["tacos"] != 1 || counts["pho"] != 2 || !view.Voted || len(view.MyOptions) != 0 || len(view.Voters) != 0 {
		t.Fatalf("anonymous view %+v", view)
	}
	if _, err := s.Chatcmd002Mutate(ctx, chatcmd002As(r, "worker", "tacos"), todoAllow); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("anonymous ballot changed: %v", err)
	}
	if counts, _ := chatcmd002Counts(t, s, r); counts["tacos"] != 1 || counts["pho"] != 2 {
		t.Fatalf("refused change moved the tally %v", counts)
	}
}

// Results held back until a reader has voted, or until the poll closes, are
// held back by the server, in the view and in the body alike.
func TestTodo_CHATBUG_057_Results(t *testing.T) {
	for _, setting := range []string{"after-voting", "after-closing"} {
		t.Run(setting, func(t *testing.T) {
			s, r, _ := chatcmd002PollFixture(t, func(p *chat.Chatcmd002Poll) { p.Results = setting })
			ctx := context.Background()
			voted, err := s.Chatcmd002Mutate(ctx, r, todoAllow)
			if err != nil {
				t.Fatal(err)
			}
			if card, _ := chat.Chatcmd002Decode(voted.Body); card.Poll.Options[0].Count != 0 {
				t.Fatal("held-back result written into the body")
			}
			counts, watcher := chatcmd002Counts(t, s, chatcmd002As(r, "third"))
			if watcher.ResultsVisible || counts["tacos"] != 0 || len(watcher.Voters) != 0 {
				t.Fatalf("result shown early %+v", watcher)
			}
			counts, voter := chatcmd002Counts(t, s, r)
			if voter.ResultsVisible != (setting == "after-voting") || (counts["tacos"] == 1) != (setting == "after-voting") {
				t.Fatalf("voter's result %+v", voter)
			}
			closing := r
			closing.Principal.SubjectID = "owner"
			closing.ExpectedRevision = voted.Revision
			closing.Mutation = chat.Chatcmd002Mutation{Operation: "CLOSE"}
			if _, err := s.Chatcmd002Mutate(ctx, closing, todoAllow); err != nil {
				t.Fatal(err)
			}
			if counts, after := chatcmd002Counts(t, s, chatcmd002As(r, "third")); !after.ResultsVisible || counts["tacos"] != 1 {
				t.Fatalf("closed poll hides its result %+v", after)
			}
		})
	}
}

// Votes cast at the same moment each count once, whatever revision each voter
// had seen.
func TestTodo_CHATBUG_057_Race(t *testing.T) {
	s, r, post := chatcmd002PollFixture(t, nil)
	ctx := context.Background()
	voters := map[string]string{"owner": "tacos", "worker": "pho", "third": "pho"}
	var wg sync.WaitGroup
	errs := make(chan error, len(voters))
	for subject, option := range voters {
		wg.Add(1)
		go func(subject, option string) {
			defer wg.Done()
			_, err := s.Chatcmd002Mutate(ctx, chatcmd002As(r, subject, option), todoAllow)
			errs <- err
		}(subject, option)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent vote: %v", err)
		}
	}
	counts, view := chatcmd002Counts(t, s, r)
	if counts["tacos"] != 1 || counts["pho"] != 2 || view.Revision != post.Revision+3 {
		t.Fatalf("counts %v revision %d", counts, view.Revision)
	}
}

// TestTodo_CHATBUG_057_Integration reproduces the served cell's roles: the
// serving role holds only the schema's default table privileges. Changing a
// vote deletes a row, and deleting the message removes its ballots through a
// trigger that runs as the caller; migration 40 must grant both.
func TestTodo_CHATBUG_057_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	role := "chatbug057_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	db.Exec(t, `CREATE ROLE `+role+` NOLOGIN`)
	t.Cleanup(func() {
		_ = db.ExecErr(`DROP OWNED BY ` + role)
		_ = db.ExecErr(`DROP ROLE ` + role)
	})
	db.Exec(t, `GRANT USAGE ON SCHEMA `+db.Schema+` TO `+role)
	db.Exec(t, `ALTER DEFAULT PRIVILEGES IN SCHEMA `+db.Schema+` GRANT SELECT, INSERT, UPDATE ON TABLES TO `+role)
	migrations, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `INSERT INTO chat_conversation(id,tenant_id,kind,name,owner_id) VALUES('c','t','PUBLIC_CHANNEL','General','writer')`)
	db.Exec(t, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body) VALUES('post','t','c','writer','t',1,'poll')`)
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, `SET ROLE `+role); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for _, statement := range []string{
		`SELECT set_config('hcmnext.tenant_id','t',true)`,
		`INSERT INTO chat_post_card_vote(tenant_id,conversation_id,post_id,home_tenant_id,subject_id,option_id) VALUES('t','c','post','t','voter','a'),('t','c','post','t','other','b')`,
		`INSERT INTO chat_post_card_tally(tenant_id,conversation_id,post_id,option_id,votes) VALUES('t','c','post','a',1) ON CONFLICT(tenant_id,post_id,option_id) DO UPDATE SET votes=chat_post_card_tally.votes+1`,
		`DELETE FROM chat_post_card_vote WHERE tenant_id='t' AND post_id='post' AND subject_id='voter'`,
		`UPDATE chat_post SET body='',tombstoned=true,revision=revision+1 WHERE tenant_id='t' AND id='post'`,
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			t.Fatalf("the serving role cannot run %q: %v", statement, err)
		}
	}
	var votes, tallies int
	if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM chat_post_card_vote WHERE post_id='post'),(SELECT count(*) FROM chat_post_card_tally WHERE post_id='post')`).Scan(&votes, &tallies); err != nil {
		t.Fatal(err)
	}
	if votes != 0 || tallies != 0 {
		t.Fatalf("a deleted message kept %d ballots and %d tallies", votes, tallies)
	}
}
