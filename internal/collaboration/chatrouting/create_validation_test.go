package chatrouting

import (
	"context"
	"errors"
	"testing"
)

func TestCreateCoordinatorRejectsMismatchedRetries(t *testing.T) {
	tests := []struct {
		name   string
		change func(*CreateRequest)
	}{
		{name: "idempotency key", change: func(req *CreateRequest) { req.IdempotencyKey = "other-key" }},
		{name: "shard", change: func(req *CreateRequest) { req.ShardID = "other-shard" }},
		{name: "placement policy", change: func(req *CreateRequest) { req.PlacementPolicy = "other-policy" }},
		{name: "placement policy version", change: func(req *CreateRequest) { req.PlacementPolicyVersion++ }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewMemoryDirectory()
			chat := &fakeCreator{fail: true}
			coordinator := CreateCoordinator{Directory: d, Chat: chat}
			request := CreateRequest{ConversationID: "c1", HostTenantID: "t1", ShardID: "s1", IdempotencyKey: "k1", PlacementPolicy: "tenant-default", PlacementPolicyVersion: 1}
			if route, err := coordinator.Create(context.Background(), request); err == nil || route.State != StatePending {
				t.Fatalf("first create = %+v, %v; want inspectable pending route", route, err)
			}
			callsAfterFailure := chat.calls

			retry := request
			tt.change(&retry)
			if route, err := coordinator.Reconcile(context.Background(), retry); !errors.Is(err, ErrAlreadyExists) || route.State != StatePending {
				t.Fatalf("mismatched reconcile = %+v, %v; want pending route and conflict", route, err)
			}
			if chat.calls != callsAfterFailure {
				t.Fatalf("mismatched reconcile called chat creator: calls %d -> %d", callsAfterFailure, chat.calls)
			}
		})
	}
}

func TestCreateCoordinatorRejectsMismatchedCreateRetry(t *testing.T) {
	d := NewMemoryDirectory()
	chat := &fakeCreator{fail: true}
	coordinator := CreateCoordinator{Directory: d, Chat: chat}
	request := CreateRequest{ConversationID: "c1", HostTenantID: "t1", ShardID: "s1", IdempotencyKey: "k1"}
	if _, err := coordinator.Create(context.Background(), request); err == nil {
		t.Fatal("expected chat creation failure")
	}
	callsAfterFailure := chat.calls

	retry := request
	retry.ShardID = "other-shard"
	if route, err := coordinator.Create(context.Background(), retry); !errors.Is(err, ErrAlreadyExists) || route.State != "" {
		t.Fatalf("mismatched create = %+v, %v; want empty result and conflict", route, err)
	}
	if chat.calls != callsAfterFailure {
		t.Fatalf("mismatched create called chat creator: calls %d -> %d", callsAfterFailure, chat.calls)
	}
}
