package importdraft

import (
	"context"
	"errors"
	"testing"
)

func TestIdempotencyKey_BoundToSimulationAndRow(t *testing.T) {
	k := IdempotencyKey("sha256:sim-a", "row-1")
	if k != IdempotencyKey("sha256:sim-a", "row-1") {
		t.Fatal("the key for the same simulation and row must be stable")
	}
	if k == IdempotencyKey("sha256:sim-b", "row-1") {
		t.Fatal("a different simulation reused the key")
	}
	if k == IdempotencyKey("sha256:sim-a", "row-2") {
		t.Fatal("a different row reused the key")
	}
	if len(k) != len("import-item:")+32 {
		t.Fatalf("key %q is not the 16-byte hex form", k)
	}
}

func TestMemoryCheckpoints_CompareAndSet(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryCheckpoints()
	if _, found, err := store.Load(ctx, "imp-1"); err != nil || found {
		t.Fatalf("empty store Load = found %v, err %v", found, err)
	}
	first := Checkpoint{ImportID: "imp-1", Version: 1, Items: map[string]ItemState{"row-1": {Status: ItemCommitted}}}
	if err := store.Save(ctx, first, 0); err != nil {
		t.Fatalf("first save at version 0: %v", err)
	}
	if err := store.Save(ctx, Checkpoint{ImportID: "imp-1", Version: 2}, 0); !errors.Is(err, ErrCheckpointConflict) {
		t.Fatalf("stale save err = %v, want ErrCheckpointConflict", err)
	}
	if err := store.Save(ctx, Checkpoint{ImportID: "imp-1", Version: 2}, 1); err != nil {
		t.Fatalf("save at the stored version: %v", err)
	}
	got, found, err := store.Load(ctx, "imp-1")
	if err != nil || !found || got.Version != 2 {
		t.Fatalf("Load = version %d, found %v, err %v", got.Version, found, err)
	}
}

func TestMemoryCheckpoints_CopiesItemsOnBothSides(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryCheckpoints()
	items := map[string]ItemState{"row-1": {Status: ItemCommitted}}
	if err := store.Save(ctx, Checkpoint{ImportID: "imp-1", Items: items}, 0); err != nil {
		t.Fatalf("save: %v", err)
	}
	items["row-1"] = ItemState{Status: ItemFailed}
	stored, _, _ := store.Load(ctx, "imp-1")
	if stored.Items["row-1"].Status != ItemCommitted {
		t.Fatal("mutating the saved map changed the stored checkpoint")
	}
	stored.Items["row-1"] = ItemState{Status: ItemFailed}
	again, _, _ := store.Load(ctx, "imp-1")
	if again.Items["row-1"].Status != ItemCommitted {
		t.Fatal("mutating a loaded checkpoint changed the stored one")
	}
}

func TestItemStatusTokens_AreTheClosedWireVocabulary(t *testing.T) {
	want := map[string]string{
		ItemCommitted: "COMMITTED", ItemFailed: "FAILED", ItemNoOp: "NO_OP", ItemError: "ERROR", ItemConflict: "CONFLICT",
		ReconciliationMatched: "MATCHED", ReconciliationMismatch: "MISMATCH",
	}
	for got, token := range want {
		if got != token {
			t.Errorf("constant renders %q, want %q", got, token)
		}
	}
}
