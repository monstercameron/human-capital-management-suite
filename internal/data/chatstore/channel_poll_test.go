package chatstore

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

func TestChannelPollMutationValidation(t *testing.T) {
	valid := ChannelPollMutation{Operation: "CREATE", Question: "Lunch?", Options: []string{"Pizza", "Sushi"}}
	if err := validateChannelPollMutation(valid); err != nil {
		t.Fatalf("valid create: %v", err)
	}
	for _, mutation := range []ChannelPollMutation{
		{Operation: "CREATE", Question: " ", Options: []string{"A", "B"}},
		{Operation: "CREATE", Question: "Q", Options: []string{"A"}},
		{Operation: "CREATE", Question: "Q", Options: []string{"A", "a"}},
		{Operation: "CREATE", Question: "Q", Options: []string{"A", "B\x00"}},
		{Operation: "VOTE", OptionID: "x", Question: "smuggled"},
		{Operation: "OTHER"},
	} {
		if err := validateChannelPollMutation(mutation); !errors.Is(err, chat.ErrInvalidArgument) {
			t.Errorf("mutation %+v: got %v", mutation, err)
		}
	}
}

func TestChannelPollIntegration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "polls", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", Role: "manager", State: "active"}, {MemberID: "worker", Role: "member", State: "active"}})
	ctx := context.Background()
	initial, err := s.ChannelPoll(ctx, "tenant-a", "tenant-a", "polls", "worker", todoAllow)
	if err != nil || initial.Revision != 1 || len(initial.Options) != 0 {
		t.Fatalf("initial poll = %+v, %v", initial, err)
	}
	poll, err := s.MutateChannelPoll(ctx, "tenant-a", "tenant-a", "polls", "worker", 1, ChannelPollMutation{Operation: "CREATE", Question: "  Lunch?  ", Options: []string{" Pizza ", "Sushi"}}, todoAllow)
	if err != nil || poll.Revision != 2 || poll.Question != "Lunch?" || len(poll.Options) != 2 || poll.Options[0].Text != "Pizza" || poll.TotalVotes != 0 {
		t.Fatalf("create poll = %+v, %v", poll, err)
	}
	if _, err := s.MutateChannelPoll(ctx, "tenant-a", "tenant-a", "polls", "owner", 1, ChannelPollMutation{Operation: "CREATE", Question: "Another?", Options: []string{"A", "B"}}, todoAllow); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("second create = %v", err)
	}
	if _, err := s.MutateChannelPoll(ctx, "tenant-a", "tenant-a", "polls", "worker", 2, ChannelPollMutation{Operation: "VOTE", OptionID: "forged"}, todoAllow); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("forged option = %v", err)
	}
	poll, err = s.MutateChannelPoll(ctx, "tenant-a", "tenant-a", "polls", "worker", 2, ChannelPollMutation{Operation: "VOTE", OptionID: poll.Options[0].ID}, todoAllow)
	if err != nil || poll.Revision != 3 || poll.TotalVotes != 1 || poll.MyOptionID != poll.Options[0].ID || poll.Options[0].Count != 1 {
		t.Fatalf("first vote = %+v, %v", poll, err)
	}
	poll, err = s.MutateChannelPoll(ctx, "tenant-a", "tenant-a", "polls", "worker", 3, ChannelPollMutation{Operation: "VOTE", OptionID: poll.Options[1].ID}, todoAllow)
	if err != nil || poll.Revision != 4 || poll.TotalVotes != 1 || poll.MyOptionID != poll.Options[1].ID || poll.Options[0].Count != 0 || poll.Options[1].Count != 1 {
		t.Fatalf("changed vote = %+v, %v", poll, err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		var voteCount int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_channel_poll_vote WHERE tenant_id='tenant-a' AND conversation_id='polls'`).Scan(&voteCount); err != nil {
			return err
		}
		var home, subject, priorOption, option string
		if err := tx.QueryRow(ctx, `SELECT vote_home_tenant_id,vote_subject_id,prior_option_id,option_id FROM chat_channel_poll_revision WHERE tenant_id='tenant-a' AND conversation_id='polls' AND revision=4`).Scan(&home, &subject, &priorOption, &option); err != nil {
			return err
		}
		var legacyBallotColumns int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='chat_channel_poll_revision' AND column_name IN ('prior_votes_json','votes_json')`).Scan(&legacyBallotColumns); err != nil {
			return err
		}
		if voteCount != 1 || home != "tenant-a" || subject != "worker" || priorOption != poll.Options[0].ID || option != poll.Options[1].ID || legacyBallotColumns != 0 {
			t.Errorf("normalized votes=%d delta=%s/%s %s=>%s legacy snapshot columns=%d", voteCount, home, subject, priorOption, option, legacyBallotColumns)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	managerView, err := s.ChannelPoll(ctx, "tenant-a", "tenant-a", "polls", "owner", todoAllow)
	if err != nil || managerView.MyOptionID != "" || managerView.TotalVotes != 1 || managerView.Options[1].Count != 1 {
		t.Fatalf("other member projection = %+v, %v", managerView, err)
	}
	if _, err := s.ChannelPoll(ctx, "tenant-b", "tenant-b", "polls", "owner", todoAllow); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("foreign tenant read = %v", err)
	}
}

func TestChannelPollMigrationUpgradesLegacyJSONBallots(t *testing.T) {
	ctx := context.Background()
	database := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, database.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 13); err != nil {
		t.Fatalf("apply legacy chat schema: %v", err)
	}
	options := `[{"id":"pizza","text":"Pizza"},{"id":"sushi","text":"Sushi"}]`
	first := `[{"home_tenant_id":"tenant-a","subject_id":"worker","option_id":"pizza"}]`
	second := `[{"home_tenant_id":"tenant-a","subject_id":"worker","option_id":"sushi"}]`
	if _, err := database.SQL.ExecContext(ctx, `INSERT INTO chat_conversation(id,tenant_id,kind,name,owner_id) VALUES('poll-upgrade','tenant-a','PUBLIC_CHANNEL','Upgrade test','owner')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `INSERT INTO chat_channel_poll(tenant_id,conversation_id,revision,question,options_json,votes_json) VALUES('tenant-a','poll-upgrade',4,'Lunch?', $1::jsonb, $2::jsonb)`, options, second); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `INSERT INTO chat_channel_poll_revision(tenant_id,conversation_id,revision,actor_home_tenant_id,actor_id,operation,prior_question,question,prior_options_json,options_json,prior_votes_json,votes_json) VALUES
		('tenant-a','poll-upgrade',2,'tenant-a','owner','CREATE','','Lunch?','[]'::jsonb,$1::jsonb,'[]'::jsonb,'[]'::jsonb),
		('tenant-a','poll-upgrade',3,'tenant-a','worker','VOTE','Lunch?','Lunch?',$1::jsonb,$1::jsonb,'[]'::jsonb,$2::jsonb),
		('tenant-a','poll-upgrade',4,'tenant-a','worker','VOTE','Lunch?','Lunch?',$1::jsonb,$1::jsonb,$2::jsonb,$3::jsonb)`, options, first, second); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 14); err != nil {
		t.Fatalf("upgrade chat schema: %v", err)
	}
	var voteCount int
	var currentOption, firstPrior, firstOption, changePrior, changeOption string
	if err := database.SQL.QueryRowContext(ctx, `SELECT count(*),max(option_id) FROM chat_channel_poll_vote WHERE tenant_id='tenant-a' AND conversation_id='poll-upgrade'`).Scan(&voteCount, &currentOption); err != nil {
		t.Fatal(err)
	}
	if err := database.SQL.QueryRowContext(ctx, `SELECT prior_option_id,option_id FROM chat_channel_poll_revision WHERE tenant_id='tenant-a' AND conversation_id='poll-upgrade' AND revision=3`).Scan(&firstPrior, &firstOption); err != nil {
		t.Fatal(err)
	}
	if err := database.SQL.QueryRowContext(ctx, `SELECT prior_option_id,option_id FROM chat_channel_poll_revision WHERE tenant_id='tenant-a' AND conversation_id='poll-upgrade' AND revision=4`).Scan(&changePrior, &changeOption); err != nil {
		t.Fatal(err)
	}
	var currentLegacyColumns, revisionLegacyColumns int
	if err := database.SQL.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='chat_channel_poll' AND column_name='votes_json'`).Scan(&currentLegacyColumns); err != nil {
		t.Fatal(err)
	}
	if err := database.SQL.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='chat_channel_poll_revision' AND column_name IN ('prior_votes_json','votes_json')`).Scan(&revisionLegacyColumns); err != nil {
		t.Fatal(err)
	}
	if voteCount != 1 || currentOption != "sushi" || firstPrior != "" || firstOption != "pizza" || changePrior != "pizza" || changeOption != "sushi" || currentLegacyColumns != 0 || revisionLegacyColumns != 0 {
		t.Fatalf("upgrade vote=%d/%s first=%s=>%s change=%s=>%s legacy cols=%d/%d", voteCount, currentOption, firstPrior, firstOption, changePrior, changeOption, currentLegacyColumns, revisionLegacyColumns)
	}
}
