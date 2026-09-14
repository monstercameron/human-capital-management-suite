package iacrecovery

import (
	"strings"
	"testing"
)

func mustStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore("recovery-trust", "recovery-admin")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func mustAppend(t *testing.T, store *Store, copy Copy, actor string) {
	t.Helper()
	if err := store.Append(copy, actor); err != nil {
		t.Fatalf("Append(%s): %v", copy.ID, err)
	}
}

func seedStore(t *testing.T) *Store {
	t.Helper()
	store := mustStore(t)
	mustAppend(t, store, Copy{ID: "copy-data-1", SourceID: "prod-pg", Digest: "sha256:data", Locked: true}, "recovery-admin")
	mustAppend(t, store, Copy{ID: "copy-config-1", SourceID: "prod-config", Digest: "sha256:config", Locked: true}, "recovery-admin")
	return store
}

func mustProvision(t *testing.T, store *Store, cellID string, copies, deps []string) Cell {
	t.Helper()
	cell, err := ProvisionCell(store, cellID, copies, deps, []string{"prod-pg.internal", "prod-object.internal"})
	if err != nil {
		t.Fatalf("ProvisionCell(%s): %v", cellID, err)
	}
	return cell
}

func seedInput() ReadinessInput {
	return ReadinessInput{
		ConfigDigest: "sha256:config",
		DataDigest:   "sha256:data",
		KeyRefs:      []string{"keys/backup-kek@v3"},
		KnownKeyRefs: []string{"keys/backup-kek@v3"},
	}
}

func mustReady(t *testing.T, cell Cell, store *Store, in ReadinessInput) ReadinessReceipt {
	t.Helper()
	receipt, err := CheckReadiness(cell, store, in)
	if err != nil {
		t.Fatalf("CheckReadiness(%s): %v", cell.ID, err)
	}
	return receipt
}

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error code %s, got nil", code)
	}
	if !strings.Contains(err.Error(), code) {
		t.Fatalf("expected error code %s, got %v", code, err)
	}
}

// TestTodo_IAC_010_Integration: two fenced cells share one immutable
// store and both admit identical readiness proofs.
func TestTodo_IAC_010_Integration(t *testing.T) {
	store := seedStore(t)
	input := seedInput()
	first := mustReady(t, mustProvision(t, store, "recovery-cell-a",
		[]string{"copy-data-1", "copy-config-1"},
		[]string{"recovery-pg.local"}), store, input)
	second := mustReady(t, mustProvision(t, store, "recovery-cell-b",
		[]string{"copy-data-1", "copy-config-1"},
		[]string{"recovery-pg.local"}), store, input)
	if first.Digest == second.Digest {
		t.Fatal("distinct cells must not share a readiness digest")
	}
	for _, receipt := range []ReadinessReceipt{first, second} {
		cellID := receipt.CellID
		cell, err := ProvisionCell(store, cellID,
			[]string{"copy-data-1", "copy-config-1"},
			[]string{"recovery-pg.local"},
			[]string{"prod-pg.internal", "prod-object.internal"})
		if err != nil {
			t.Fatalf("re-provision %s: %v", cellID, err)
		}
		if got := VerifyReadiness(receipt, cell, input); got != "" {
			t.Fatalf("re-verify %s: %q", cellID, got)
		}
	}
}

// TestTodo_IAC_010_Fault: production reach, mutation, production
// contact and unknown references fail closed with exact codes.
func TestTodo_IAC_010_Fault(t *testing.T) {
	if _, err := NewStore(ProductionBoundary, "recovery-admin"); err == nil {
		t.Fatal("store under the production trust boundary admitted")
	} else {
		requireCode(t, err, CodeProductionReach)
	}
	store := seedStore(t)
	// A compromised production identity cannot append recovery copies.
	requireCode(t, store.Append(Copy{ID: "copy-evil", SourceID: "prod-pg", Digest: "sha256:evil", Locked: true}, ProductionBoundary), CodeProductionReach)
	// Immutability: an existing copy id can never be rebound.
	requireCode(t, store.Append(Copy{ID: "copy-data-1", SourceID: "prod-pg", Digest: "sha256:changed", Locked: true}, "recovery-admin"), CodeImmutableCopy)
	// Unlocked copies are refused at the gate.
	requireCode(t, store.Append(Copy{ID: "copy-unlocked", SourceID: "prod-pg", Digest: "sha256:x"}, "recovery-admin"), CodeMissingField)
	// A restored cell cannot contact production dependencies.
	_, err := ProvisionCell(store, "recovery-cell-evil",
		[]string{"copy-data-1"}, []string{"prod-pg.internal"},
		[]string{"prod-pg.internal"})
	requireCode(t, err, CodeProductionDependency)
	// Unknown copy references refuse.
	_, err = ProvisionCell(store, "recovery-cell-ghost",
		[]string{"copy-missing"}, []string{"recovery-pg.local"},
		[]string{"prod-pg.internal"})
	requireCode(t, err, CodeUnknownCopyRef)
	// Readiness blocks on wrong digests and unknown keys.
	cell := mustProvision(t, store, "recovery-cell-a",
		[]string{"copy-data-1", "copy-config-1"},
		[]string{"recovery-pg.local"})
	bad := seedInput()
	bad.DataDigest = "sha256:tampered"
	requireCode(t, func() error { _, err := CheckReadiness(cell, store, bad); return err }(), CodeReadinessBlocked)
	bad = seedInput()
	bad.KeyRefs = []string{"keys/unknown@v9"}
	requireCode(t, func() error { _, err := CheckReadiness(cell, store, bad); return err }(), CodeReadinessBlocked)
}

// TestTodo_IAC_010_Recovery: an isolated store rebuilt from the copy
// manifest reproduces the readiness proof exactly.
func TestTodo_IAC_010_Recovery(t *testing.T) {
	store := seedStore(t)
	input := seedInput()
	cell := mustProvision(t, store, "recovery-cell-a",
		[]string{"copy-data-1", "copy-config-1"},
		[]string{"recovery-pg.local"})
	before := mustReady(t, cell, store, input)

	rebuilt, err := Rebuild("recovery-trust", "recovery-admin", store.Manifest(), "recovery-admin")
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	// A production identity cannot drive the rebuild either.
	if _, err := Rebuild("recovery-trust", "recovery-admin", store.Manifest(), ProductionBoundary); err == nil {
		t.Fatal("production-driven rebuild admitted")
	} else {
		requireCode(t, err, CodeProductionReach)
	}
	after, err := CheckReadiness(cell, rebuilt, input)
	if err != nil {
		t.Fatalf("CheckReadiness after rebuild: %v", err)
	}
	if before.Digest != after.Digest {
		t.Fatalf("readiness drift across rebuild:\n got=%q\nwant=%q", after.Digest, before.Digest)
	}
	if got := VerifyReadiness(after, cell, input); got != "" {
		t.Fatalf("re-verify after rebuild: %q", got)
	}
}
