package chatrecordstore_test

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestChannelPollSnapshotRestorePreservesBallotAndAudit(t *testing.T) {
	ctx := context.Background()
	source := newStore(t)
	target := newStore(t)
	seed := func(store interface {
		RunTenantTx(context.Context, string, func(dbport.Tx) error) error
	}, tenant, room, option string) {
		t.Helper()
		err := store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
			if _, err := tx.Exec(ctx, `INSERT INTO chat_conversation(id,tenant_id,kind,name,owner_id) VALUES($1,$2,'PUBLIC_CHANNEL','Poll test','owner')`, room, tenant); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO chat_channel_poll(tenant_id,conversation_id,revision,question,options_json) VALUES($1,$2,3,'Lunch?', '[{"id":"pizza","text":"Pizza"},{"id":"sushi","text":"Sushi"}]'::jsonb)`, tenant, room); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO chat_channel_poll_vote(tenant_id,conversation_id,home_tenant_id,subject_id,option_id) VALUES($1,$2,$1,'worker',$3)`, tenant, room, option); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO chat_channel_poll_revision(tenant_id,conversation_id,revision,actor_home_tenant_id,actor_id,operation,prior_question,question,prior_options_json,options_json,vote_home_tenant_id,vote_subject_id,prior_option_id,option_id) VALUES
				($1,$2,2,$1,'owner','CREATE','','Lunch?','[]'::jsonb,'[{"id":"pizza","text":"Pizza"},{"id":"sushi","text":"Sushi"}]'::jsonb,NULL,NULL,'',''),
				($1,$2,3,$1,'worker','VOTE','Lunch?','Lunch?','[{"id":"pizza","text":"Pizza"},{"id":"sushi","text":"Sushi"}]'::jsonb,'[{"id":"pizza","text":"Pizza"},{"id":"sushi","text":"Sushi"}]'::jsonb,$1,'worker','',$3)`, tenant, room, option)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	seed(source.Chat, "tenant-a", "room-a", "sushi")
	seed(source.Chat, "tenant-b", "room-b", "pizza")
	seed(target.Chat, "tenant-b", "room-b", "pizza")

	snapshot, err := source.Snapshot(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"chat_channel_poll", "chat_channel_poll_vote", "chat_channel_poll_revision"} {
		if len(snapshot.RawTables[table]) == 0 || string(snapshot.RawTables[table]) == "[]" {
			t.Fatalf("snapshot missing %s", table)
		}
		if strings.Contains(string(snapshot.RawTables[table]), "room-b") {
			t.Fatalf("snapshot leaked another tenant through %s", table)
		}
	}
	if !strings.Contains(string(snapshot.RawTables["chat_channel_poll_revision"]), `"prior_option_id"`) || !strings.Contains(string(snapshot.RawTables["chat_channel_poll_revision"]), `"option_id": "sushi"`) {
		t.Fatal("snapshot lost compact poll audit deltas")
	}
	if err := target.Restore(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := target.Chat.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		var question, option, priorOption, newOption string
		var count int
		if err := tx.QueryRow(ctx, `SELECT question FROM chat_channel_poll WHERE tenant_id='tenant-a' AND conversation_id='room-a'`).Scan(&question); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT option_id FROM chat_channel_poll_vote WHERE tenant_id='tenant-a' AND conversation_id='room-a' AND subject_id='worker'`).Scan(&option); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT prior_option_id,option_id FROM chat_channel_poll_revision WHERE tenant_id='tenant-a' AND conversation_id='room-a' AND revision=3`).Scan(&priorOption, &newOption); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_channel_poll_revision WHERE tenant_id='tenant-a' AND conversation_id='room-a'`).Scan(&count); err != nil {
			return err
		}
		if question != "Lunch?" || option != "sushi" || priorOption != "" || newOption != "sushi" || count != 2 {
			t.Fatalf("restored question=%q ballot=%q audit=%q=>%q revisions=%d", question, option, priorOption, newOption, count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := target.Chat.RunTenantTx(ctx, "tenant-b", func(tx dbport.Tx) error {
		var option string
		if err := tx.QueryRow(ctx, `SELECT option_id FROM chat_channel_poll_vote WHERE tenant_id='tenant-b' AND conversation_id='room-b' AND subject_id='worker'`).Scan(&option); err != nil {
			return err
		}
		if option != "pizza" {
			t.Fatalf("restore changed another tenant's vote: %q", option)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
