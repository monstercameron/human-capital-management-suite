package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_CHAT_006_IntegrationFenceRequiresHostAndEpoch(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	plan := chatrouting.MovePlan{ConversationID: "c-1", HostTenantID: "tenant-a", SourceEpoch: 1, MoveEpoch: 2}
	if err := s.RunTenantTx(ctx, plan.HostTenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_conversation SET route_shard='chat-local' WHERE tenant_id=$1 AND id=$2`, plan.HostTenantID, plan.ConversationID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.FenceWrites(ctx, chatrouting.MovePlan{ConversationID: plan.ConversationID, SourceEpoch: 1, MoveEpoch: 2}); !errors.Is(err, chatrouting.ErrInvalid) {
		t.Fatalf("missing host tenant: %v", err)
	}
	wrongHost := plan
	wrongHost.HostTenantID = "tenant-b"
	if err := s.FenceWrites(ctx, wrongHost); !errors.Is(err, chatrouting.ErrStaleEpoch) {
		t.Fatalf("foreign host fence: %v", err)
	}
	if err := s.FenceWrites(ctx, plan); err != nil {
		t.Fatalf("fence: %v", err)
	}
	if _, err := s.sendPostRaw(ctx, SendRequest{TenantID: plan.HostTenantID, HomeTenantID: plan.HostTenantID, ConversationID: plan.ConversationID, AuthorID: "u-1", RouteEpoch: plan.SourceEpoch, ShardID: "chat-local", ClientKey: "fenced-write", Body: "must not commit"}); !errors.Is(err, chatrouting.ErrStaleEpoch) {
		t.Fatalf("write with old route lease: %v", err)
	}
	if err := s.FenceWrites(ctx, plan); !errors.Is(err, chatrouting.ErrStaleEpoch) {
		t.Fatalf("stale replay fence: %v", err)
	}
	var epoch int64
	var state string
	if err := s.RunTenantTx(ctx, plan.HostTenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT route_epoch,route_state FROM chat_conversation WHERE tenant_id=$1 AND id=$2`, plan.HostTenantID, plan.ConversationID).Scan(&epoch, &state)
	}); err != nil {
		t.Fatal(err)
	}
	if epoch != 2 || state != "MOVING" {
		t.Fatalf("fence state: epoch=%d state=%q", epoch, state)
	}
	if err := s.AbortWrites(ctx, plan, 3); err != nil {
		t.Fatalf("abort local fence: %v", err)
	}
	if _, err := s.sendPostRaw(ctx, SendRequest{TenantID: plan.HostTenantID, HomeTenantID: plan.HostTenantID, ConversationID: plan.ConversationID, AuthorID: "u-1", RouteEpoch: 3, ShardID: "chat-local", ClientKey: "restored-write", Body: "can commit"}); err != nil {
		t.Fatalf("write after rollback: %v", err)
	}
	if err := s.AbortWrites(ctx, plan, 3); !errors.Is(err, chatrouting.ErrStaleEpoch) {
		t.Fatalf("stale rollback replay: %v", err)
	}
}
