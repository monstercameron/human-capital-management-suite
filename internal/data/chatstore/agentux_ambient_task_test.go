package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestAgentUXAmbient_ChannelTask_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	tenant, conversation := "tenant-a", "ambient-tasks"
	c := chat.Conversation{ID: conversation, TenantID: tenant, Kind: chat.PublicChannel, OwnerID: "alice", Revision: 1}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{{TenantID: tenant, HomeTenantID: tenant, ConversationID: conversation, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, ""); err != nil {
		t.Fatal(err)
	}
	p, err := s.SendPost(ctx, chat.SendPostRequest{Principal: chat.Principal{TenantID: tenant, SubjectID: "alice"}, TenantID: tenant, ConversationID: conversation, IdempotencyKey: "source"}, chat.Post{AuthorID: "alice", AuthorHomeTenantID: tenant, Body: "Can someone book the room?"})
	if err != nil {
		t.Fatal(err)
	}
	allow := func(context.Context) error { return nil }
	for range 2 {
		if err = s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
			return s.AddAmbientChannelTask(dbport.ContextWithTx(ctx, tx), tenant, conversation, "alice", "ambient-one", "Book the room", p.ID, allow)
		}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.ChannelTodo(ctx, tenant, tenant, conversation, "alice", allow)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].ID != "ambient-one" || list.Revision != 2 {
		t.Fatalf("not one item: %+v", list)
	}
	var source string
	if err = s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT items_json->0->>'source_post_id' FROM chat_channel_todo WHERE tenant_id=$1 AND conversation_id=$2`, tenant, conversation).Scan(&source)
	}); err != nil || source != p.ID {
		t.Fatalf("source not persisted: %q %v", source, err)
	}
	if err = s.AddAmbientChannelTask(ctx, tenant, conversation, "alice", "ambient-two", "Other", p.ID, allow); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("missing transaction accepted: %v", err)
	}
	if err = s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return s.AddAmbientChannelTask(dbport.ContextWithTx(ctx, tx), tenant, conversation, "outsider", "ambient-two", "Other", p.ID, allow)
	}); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("outsider accepted: %v", err)
	}
	if err = s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO agentux_ambient_offer(tenant_id,id,conversation_id,source_id,source_revision,agent_id,kind,scope,state,reason,title,data) VALUES($1,'ambient-one',$2,$3,1,'task-catcher','TASK','PUBLIC','ADDED','channel_task','Book the room','{}')`, tenant, conversation, p.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE chat_post SET tombstoned=true,revision=revision+1 WHERE tenant_id=$1 AND id=$2`, tenant, p.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
			return s.CancelAmbientChannelTask(dbport.ContextWithTx(ctx, tx), tenant, conversation, "ambient-one", p.ID)
		}); err != nil {
			t.Fatal(err)
		}
	}
	list, err = s.ChannelTodo(ctx, tenant, tenant, conversation, "alice", allow)
	if err != nil || len(list.Items) != 0 || list.Revision != 3 {
		t.Fatalf("source cancellation %+v %v", list, err)
	}
}
