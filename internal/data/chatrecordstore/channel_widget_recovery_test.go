package chatrecordstore_test

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestChannelWidgetSnapshotRestorePreservesTenantAndRevisions(t *testing.T) {
	ctx := context.Background()
	source := newStore(t)
	target := newStore(t)
	seed := func(store interface {
		RunTenantTx(context.Context, string, func(dbport.Tx) error) error
	}, tenant, current string) {
		t.Helper()
		room := "room"
		if tenant != "tenant-a" {
			room = "other-room"
		}
		err := store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
			if _, err := tx.Exec(ctx, `INSERT INTO chat_conversation(id,tenant_id,kind,name,owner_id) VALUES($1, $2, 'PUBLIC_CHANNEL', 'General', 'alice')`, room, tenant); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO chat_channel_widget(tenant_id,conversation_id,kind,revision,pinned,payload_json) VALUES($1,$3,'TEAM',3,true,jsonb_build_object('title',$2::text)),($1,$3,'PROJECT',2,false,'{"name":"Project current"}'::jsonb)`, tenant, current, room); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO chat_channel_widget_revision(tenant_id,conversation_id,kind,revision,actor_home_tenant_id,actor_id,operation,prior_pinned,pinned,prior_payload_json,payload_json) VALUES($1,$3,'TEAM',2,$1,'alice','ADD',false,true,'{"title":"Team original"}'::jsonb,'{"title":"Team prior"}'::jsonb),($1,$3,'TEAM',3,$1,'alice','EDIT',true,true,'{"title":"Team prior"}'::jsonb,jsonb_build_object('title',$2::text)),($1,$3,'PROJECT',2,$1,'alice','ADD',false,false,'{}'::jsonb,'{"name":"Project current"}'::jsonb)`, tenant, current, room); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	seed(source.Chat, "tenant-a", "Team current")
	seed(source.Chat, "tenant-b", "Other tenant secret")
	seed(target.Chat, "tenant-b", "Target tenant secret")

	snapshot, err := source.Snapshot(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"chat_channel_widget", "chat_channel_widget_revision"} {
		if len(snapshot.RawTables[table]) == 0 || string(snapshot.RawTables[table]) == "[]" {
			t.Fatalf("snapshot missing %s", table)
		}
		if strings.Contains(string(snapshot.RawTables[table]), "Other tenant secret") {
			t.Fatalf("snapshot leaked another tenant through %s", table)
		}
	}
	if !strings.Contains(string(snapshot.RawTables["chat_channel_widget_revision"]), "Team original") || !strings.Contains(string(snapshot.RawTables["chat_channel_widget_revision"]), "Team prior") {
		t.Fatal("snapshot lost widget revision payloads")
	}
	if err := target.Restore(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := target.Chat.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		var current, prior, project string
		var count int
		if err := tx.QueryRow(ctx, `SELECT payload_json->>'title' FROM chat_channel_widget WHERE tenant_id='tenant-a' AND conversation_id='room' AND kind='TEAM'`).Scan(&current); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT prior_payload_json->>'title' FROM chat_channel_widget_revision WHERE tenant_id='tenant-a' AND conversation_id='room' AND kind='TEAM' AND revision=3`).Scan(&prior); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT payload_json->>'name' FROM chat_channel_widget WHERE tenant_id='tenant-a' AND conversation_id='room' AND kind='PROJECT'`).Scan(&project); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_channel_widget_revision WHERE tenant_id='tenant-a' AND conversation_id='room'`).Scan(&count); err != nil {
			return err
		}
		if current != "Team current" || prior != "Team prior" || project != "Project current" || count != 3 {
			t.Fatalf("restored team=%q prior=%q project=%q revision count=%d", current, prior, project, count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := target.Chat.RunTenantTx(ctx, "tenant-b", func(tx dbport.Tx) error {
		var title string
		if err := tx.QueryRow(ctx, `SELECT payload_json->>'title' FROM chat_channel_widget WHERE tenant_id='tenant-b' AND conversation_id='other-room' AND kind='TEAM'`).Scan(&title); err != nil {
			return err
		}
		if title != "Target tenant secret" {
			t.Fatalf("restore changed another tenant: %q", title)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
