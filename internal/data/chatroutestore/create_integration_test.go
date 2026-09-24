package chatroutestore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

type recoveryCreator struct {
	created map[string]bool
	fail    bool
	calls   int
}

func (c *recoveryCreator) EnsureConversation(_ context.Context, req chatrouting.CreateRequest) error {
	c.calls++
	if c.fail {
		return errors.New("chat database unavailable")
	}
	c.created[req.IdempotencyKey] = true
	return nil
}

func TestTodo_CHAT_006_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	conn := db.NewConn(t)
	directory, err := New(conn)
	if err != nil {
		t.Fatal(err)
	}
	if err = directory.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	creator := &recoveryCreator{created: make(map[string]bool), fail: true}
	coordinator := chatrouting.CreateCoordinator{Directory: directory, Chat: creator}
	request := chatrouting.CreateRequest{ConversationID: "conversation-1", HostTenantID: "tenant-1", ShardID: "chat-shard-1", IdempotencyKey: "create-key-1", PlacementPolicy: "tenant-default", PlacementPolicyVersion: 1}

	if route, err := coordinator.Create(context.Background(), request); err == nil || route.State != chatrouting.StatePending {
		t.Fatalf("failed create = %+v, %v; want inspectable pending route", route, err)
	}
	pending, err := directory.Lookup(context.Background(), request.ConversationID, request.HostTenantID)
	if err != nil || pending.State != chatrouting.StatePending {
		t.Fatalf("lookup pending route = %+v, %v", pending, err)
	}

	creator.fail = false
	active, err := coordinator.Reconcile(context.Background(), request)
	if err != nil || active.State != chatrouting.StateActive {
		t.Fatalf("reconcile = %+v, %v", active, err)
	}
	stored, err := directory.Lookup(context.Background(), request.ConversationID, request.HostTenantID)
	if err != nil || stored.State != chatrouting.StateActive || stored.CreateIdempotencyKey != request.IdempotencyKey {
		t.Fatalf("lookup reconciled route = %+v, %v", stored, err)
	}
	if !creator.created[request.IdempotencyKey] || creator.calls != 2 {
		t.Fatalf("chat creation state=%v calls=%d; want one durable create after one failed attempt", creator.created, creator.calls)
	}

	retried, err := coordinator.Create(context.Background(), request)
	if err != nil || retried.State != chatrouting.StateActive || creator.calls != 2 {
		t.Fatalf("idempotent retry = %+v, %v calls=%d", retried, err, creator.calls)
	}
}
