package chatstore

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestAgentUXAmbient_Invalidation_Security_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := t.Context()
	tenant, conversation := "tenant-a", "ambient-events"
	if _, err := s.CreateConversation(ctx, chat.Conversation{ID: conversation, TenantID: tenant, Kind: chat.PublicChannel, OwnerID: "alice", Revision: 1}, []chat.Membership{{TenantID: tenant, HomeTenantID: tenant, ConversationID: conversation, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO agentux_ambient_offer(tenant_id,id,conversation_id,source_id,source_revision,agent_id,kind,scope,person_id,reason,title,data) VALUES($1,'secret-card',$2,'source',1,'task-catcher','TASK','PRIVATE','alice','self_commitment','Secret title','{}')`, tenant, conversation); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentux_ambient_grant(tenant_id,conversation_id,agent_id,updated_by) VALUES($1,$2,'task-catcher','alice')`, tenant, conversation); err != nil {
			return err
		}
		if err := AppendAmbientOfferChanged(ctx, tx, tenant, conversation, "task-catcher", "secret-card", 1); err != nil {
			return err
		}
		return AppendAmbientGrantChanged(ctx, tx, tenant, conversation, "task-catcher", "alice")
	}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT payload FROM chat_outbox WHERE tenant_id=$1 AND event_type=$2`, tenant, AgentUXAmbientOffersChanged)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			if err = rows.Scan(&raw); err != nil {
				return err
			}
			var envelope OutboxEnvelope
			if err = json.Unmarshal(raw, &envelope); err != nil {
				return err
			}
			if string(envelope.Value) != "{}" || envelope.TargetID != conversation || envelope.ConversationID != conversation {
				t.Fatalf("private information in invalidation: %s", raw)
			}
			count++
		}
		return rows.Err()
	}); err != nil || count != 2 {
		t.Fatalf("durable invalidations %d %v", count, err)
	}
	for _, forged := range []string{"tenant-b", "tenant-a"} {
		err := s.RunTenantTx(ctx, forged, func(tx dbport.Tx) error {
			return AppendAmbientOfferChanged(ctx, tx, forged, conversation, "task-catcher", "secret-card", 2)
		})
		if !errors.Is(err, chat.ErrPermissionDenied) {
			t.Fatalf("forged invalidation accepted %s %v", forged, err)
		}
	}
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return AppendAmbientGrantChanged(ctx, tx, tenant, conversation, "task-catcher", "peer")
	}); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("unverified grant actor accepted", err)
	}
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO agentux_ambient_read(tenant_id,conversation_id,agent_id,post_id,revision,at,outcome) VALUES($1,$2,'task-catcher','source',1,now(),'RESERVED')`, tenant, conversation)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agentux_ambient_read SET outcome='FAKED' WHERE tenant_id=$1`, tenant)
		return err
	}); err == nil {
		t.Fatal("read journal mutation accepted")
	}
}
