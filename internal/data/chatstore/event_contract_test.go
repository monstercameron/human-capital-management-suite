package chatstore

import (
	"context"
	"encoding/json"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestChatOutboxInitialMembersHaveDistinctOrderedEvents(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "c1", TenantID: "tenant-a", Kind: chat.PrivateChannel, OwnerID: "alice", Revision: 1}
	members := []chat.Membership{
		{TenantID: c.TenantID, ConversationID: c.ID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager},
		{TenantID: c.TenantID, ConversationID: c.ID, HomeTenantID: c.TenantID, SubjectID: "bob", Role: chat.Member},
		{TenantID: c.TenantID, ConversationID: c.ID, HomeTenantID: c.TenantID, SubjectID: "carol", Role: chat.Member},
	}
	if _, err := s.CreateConversation(ctx, c, members, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT event_type,payload FROM chat_outbox WHERE tenant_id=$1 AND aggregate_id=$2 ORDER BY id`, c.TenantID, c.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		var sequence int
		for rows.Next() {
			var kind string
			var payload []byte
			if err := rows.Scan(&kind, &payload); err != nil {
				return err
			}
			var event struct {
				EventSequence int
				SchemaVersion int
				ActorID       string
				CorrelationID string
			}
			if err := json.Unmarshal(payload, &event); err != nil {
				return err
			}
			sequence++
			if event.EventSequence != sequence || event.SchemaVersion != 1 || event.ActorID != "alice" || event.CorrelationID == "" {
				t.Fatalf("event %d %s has incomplete payload: %+v", sequence, kind, event)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if sequence != 4 {
			t.Fatalf("outbox events=%d, want conversation plus three memberships", sequence)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
