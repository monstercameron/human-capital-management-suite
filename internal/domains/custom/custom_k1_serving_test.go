package custom

import (
	"context"
	"testing"
)

// TestTodo_CUSTOM_001_ServedPath proves the custom schema, policy,
// relationship, capability and event seams are reachable from the serving
// contract owned by the application composition root.
func TestTodo_CUSTOM_001_ServedPath(t *testing.T) {
	if ServingContractID == "" {
		t.Fatal("serving contract id is empty")
	}
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("custom serving contract: %v", err)
	}
}

// TestTodo_CUSTOM_004_Golden pins the canonical event and outbox payload
// digest emitted for the reference create mutation.
func TestTodo_CUSTOM_004_Golden(t *testing.T) {
	store := NewEventStore()
	receipt, err := store.Commit(context.Background(), testMutation(t, "tenant-golden", "vehicle-golden", "GOLDEN-1", OperationCreate, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	const wantDigest = "sha256:0963009571bda38193f9ab823ab1a03edbe50519d9b165e4552100a2a8a652e1"
	if receipt.Event.Digest != wantDigest {
		t.Fatalf("golden event digest = %q, want %q", receipt.Event.Digest, wantDigest)
	}
	if receipt.Outbox.PayloadDigest != wantDigest {
		t.Fatalf("golden outbox digest = %q, want %q", receipt.Outbox.PayloadDigest, wantDigest)
	}
}
