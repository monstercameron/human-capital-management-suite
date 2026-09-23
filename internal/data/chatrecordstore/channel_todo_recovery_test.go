package chatrecordstore_test

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestChannelTodoSnapshotPreservesCurrentAndPriorContent(t *testing.T) {
	ctx := context.Background()
	source := newStore(t)
	target := newStore(t)
	err := source.Chat.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO chat_conversation(id,tenant_id,kind,name,owner_id) VALUES('room-a','tenant-a','PUBLIC_CHANNEL','General','alice')`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO chat_channel_todo(tenant_id,conversation_id,revision,pinned,items_json) VALUES('tenant-a','room-a',2,true,'[{"id":"item-a","text":"Current task"}]'::jsonb)`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO chat_channel_todo_revision(tenant_id,conversation_id,revision,actor_home_tenant_id,actor_id,operation,prior_pinned,pinned,prior_items_json,items_json) VALUES('tenant-a','room-a',2,'tenant-a','alice','ADD',false,true,'[{"id":"item-a","text":"Prior task"}]'::jsonb,'[{"id":"item-a","text":"Current task"}]'::jsonb)`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.Snapshot(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"chat_channel_todo", "chat_channel_todo_revision"} {
		if len(snapshot.RawTables[table]) == 0 || string(snapshot.RawTables[table]) == "[]" {
			t.Fatalf("snapshot missing %s", table)
		}
	}
	if !strings.Contains(string(snapshot.RawTables["chat_channel_todo_revision"]), "Prior task") {
		t.Fatal("snapshot lost prior task content")
	}
	other, err := source.Snapshot(ctx, "tenant-b")
	if err != nil {
		t.Fatal(err)
	}
	if string(other.RawTables["chat_channel_todo"]) != "[]" || string(other.RawTables["chat_channel_todo_revision"]) != "[]" {
		t.Fatal("cross-tenant todo content leaked into snapshot")
	}
	if err := target.Restore(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	err = target.Chat.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		var current, prior string
		if err := tx.QueryRow(ctx, `SELECT items_json::text FROM chat_channel_todo WHERE tenant_id='tenant-a' AND conversation_id='room-a'`).Scan(&current); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT prior_items_json::text FROM chat_channel_todo_revision WHERE tenant_id='tenant-a' AND conversation_id='room-a' AND revision=2`).Scan(&prior); err != nil {
			return err
		}
		if !strings.Contains(current, "Current task") || !strings.Contains(prior, "Prior task") {
			t.Fatalf("restore current=%s prior=%s", current, prior)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
