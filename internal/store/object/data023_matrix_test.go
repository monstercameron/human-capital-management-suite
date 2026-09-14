package object

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/envelope"
)

func mustBackupInventory(t *testing.T, cctx custody.Context, store *SealedObjectStore, holds, tombstones map[string]bool, erasures map[string]string) BackupManifest {
	t.Helper()
	manifest, err := BackupInventory(context.Background(), cctx, store, holds, tombstones, erasures, func(id string) string {
		return "ledger:lineage:" + id
	})
	if err != nil {
		t.Fatalf("BackupInventory: %v", err)
	}
	return manifest
}

func seedRestorePolicy() RestorePolicy {
	return RestorePolicy{
		BudgetRPO: time.Minute, BudgetRTO: 5 * time.Minute,
		ObservedRPO: 30 * time.Second, ObservedRTO: 2 * time.Minute,
	}
}

func mustRestoreArtifacts(t *testing.T, ctx context.Context, cctx custody.Context, store *SealedObjectStore, manifest BackupManifest, policy RestorePolicy) RestoreReport {
	t.Helper()
	report, err := RestoreArtifacts(ctx, cctx, store, manifest, policy)
	if err != nil {
		t.Fatalf("RestoreArtifacts: %v", err)
	}
	return report
}

func lifecycleFixture(t *testing.T) (*SealedObjectStore, custody.Context, custody.Context, *fakeCustodyProvider) {
	t.Helper()
	m, ctxA, ctxB, p := sealedFixture(t)
	store, err := NewSealedObjectStore(m)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	put := func(id, media string, bytes []byte) {
		t.Helper()
		if _, err := store.Put(ctx, ctxA, id, media, bytes); err != nil {
			t.Fatalf("Put(%s): %v", id, err)
		}
	}
	put("artifact-active", "application/pdf", []byte("active-bytes"))
	put("artifact-held", "application/pdf", []byte("held-bytes"))
	put("artifact-tomb", "image/png", []byte("tomb-bytes"))
	put("artifact-erased", "image/png", []byte("erased-bytes"))
	_ = ctxB
	return store, ctxA, ctxB, p
}

// TestTodo_DATA_023_Property: lifecycle states restore to their
// documented outcomes across tombstone/hold/erasure combinations.
func TestTodo_DATA_023_Property(t *testing.T) {
	store, ctxA, _, _ := lifecycleFixture(t)
	for _, combo := range []struct {
		name       string
		holds      map[string]bool
		tombstones map[string]bool
		restored   int
		holdsKept  int
		tombs      int
	}{
		{"plain", nil, nil, 4, 0, 0},
		{"held", map[string]bool{"artifact-held": true}, nil, 4, 1, 0},
		{"tombstoned", nil, map[string]bool{"artifact-tomb": true}, 3, 0, 1},
		{"held+tombstoned", map[string]bool{"artifact-held": true}, map[string]bool{"artifact-tomb": true}, 3, 1, 1},
	} {
		manifest := mustBackupInventory(t, ctxA, store, combo.holds, combo.tombstones, nil)
		report := mustRestoreArtifacts(t, context.Background(), ctxA, store, manifest, seedRestorePolicy())
		if report.Status != RestoreComplete {
			t.Fatalf("%s: %+v", combo.name, report)
		}
		if report.Restored != combo.restored || report.HoldsKept != combo.holdsKept || report.Tombstones != combo.tombs {
			t.Fatalf("%s: %+v", combo.name, report)
		}
	}
}

// TestTodo_DATA_023_Golden pins the artifact restore digest oracle.
func TestTodo_DATA_023_Golden(t *testing.T) {
	store, ctxA, _, _ := lifecycleFixture(t)
	manifest := mustBackupInventory(t, ctxA, store,
		map[string]bool{"artifact-held": true},
		map[string]bool{"artifact-tomb": true}, nil)
	// The golden vector keeps the erased object readable-proof out of
	// scope: erasure needs its own destroyed-key fixture (see Security).
	report := mustRestoreArtifacts(t, context.Background(), ctxA, store, manifest, seedRestorePolicy())
	if report.Status != RestoreComplete {
		t.Fatalf("status=%v findings=%+v", report.Status, report.Findings)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "data023.golden.txt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if want := strings.TrimSpace(string(raw)); report.Digest != want {
		t.Fatalf("report digest mismatch:\n got=%q\nwant=%q", report.Digest, want)
	}
}

// TestTodo_DATA_023_Integration: backup and restore compose over the
// sealed store with metadata and lineage intact.
func TestTodo_DATA_023_Integration(t *testing.T) {
	store, ctxA, _, _ := lifecycleFixture(t)
	manifest := mustBackupInventory(t, ctxA, store, map[string]bool{"artifact-held": true}, nil, nil)
	for _, backup := range manifest.Artifacts {
		if backup.Digest == "" || backup.KeyID == "" || backup.KeyVersion == "" || backup.LineageRef == "" {
			t.Fatalf("backup without identity: %+v", backup)
		}
		if backup.State != ArtifactActive {
			t.Fatalf("fresh backup state = %s", backup.State)
		}
	}
	report := mustRestoreArtifacts(t, context.Background(), ctxA, store, manifest, seedRestorePolicy())
	if report.Status != RestoreComplete || report.Restored != 4 || report.HoldsKept != 1 {
		t.Fatalf("report: %+v", report)
	}
	if report.EffectsEmitted != 0 {
		t.Fatalf("restore emitted %d effects", report.EffectsEmitted)
	}
}

// TestTodo_DATA_023_Fault: missing bytes, drifted digests, rotated keys
// and blown budgets fence the restore.
func TestTodo_DATA_023_Fault(t *testing.T) {
	store, ctxA, _, _ := lifecycleFixture(t)
	manifest := mustBackupInventory(t, ctxA, store, nil, nil, nil)
	if _, err := RestoreArtifacts(context.Background(), ctxA, store, BackupManifest{}, seedRestorePolicy()); err == nil {
		t.Fatal("manifest-less restore admitted")
	}
	drifted := manifest
	drifted.Artifacts = append([]ArtifactBackup(nil), manifest.Artifacts...)
	drifted.Artifacts[0].Digest = "sha256:forged"
	if report := mustRestoreArtifacts(t, context.Background(), ctxA, store, drifted, seedRestorePolicy()); report.Status != RestoreFenced {
		t.Fatalf("drifted digest passed: %+v", report)
	}
	rotated := manifest
	rotated.Artifacts = append([]ArtifactBackup(nil), manifest.Artifacts...)
	rotated.Artifacts[1].KeyVersion = "v99"
	if report := mustRestoreArtifacts(t, context.Background(), ctxA, store, rotated, seedRestorePolicy()); report.Status != RestoreFenced {
		t.Fatalf("rotated key passed: %+v", report)
	}
	late := seedRestorePolicy()
	late.ObservedRTO = 30 * time.Minute
	if report := mustRestoreArtifacts(t, context.Background(), ctxA, store, manifest, late); report.Status != RestoreFenced {
		t.Fatalf("blown RTO passed: %+v", report)
	}
	if _, err := RestoreArtifacts(context.Background(), ctxA, store, manifest, RestorePolicy{}); err == nil {
		t.Fatal("budget-less policy admitted")
	}
}

// TestTodo_DATA_023_Security: erased bytes verify unopenable with
// proof, and a foreign tenant context opens nothing.
func TestTodo_DATA_023_Security(t *testing.T) {
	m, ctxA, ctxB, p := sealedFixture(t)
	store, err := NewSealedObjectStore(m)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := store.Put(ctx, ctxA, "artifact-gone", "application/pdf", []byte("gone-bytes")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Crypto-erasure: destroy the tenant key, then prove it.
	p.destroy(custody.Handle{ID: "tenant-a-kek", Kind: custody.Key, Version: "v1", Tenant: "tenant-a", Region: "us-east"})
	manifest := mustBackupInventory(t, ctxA, store, nil, nil, map[string]string{"artifact-gone": "KEY-DESTROYED-9"})
	report := mustRestoreArtifacts(t, ctx, ctxA, store, manifest, seedRestorePolicy())
	if report.Status != RestoreComplete || report.Erased != 1 {
		t.Fatalf("erasure proof: %+v", report)
	}
	// Without erasure state the unreadable object refuses backup itself:
	// unopenable bytes are inventoriable only as erased with proof.
	if _, err := BackupInventory(ctx, ctxA, store, nil, nil, nil, func(id string) string { return "ledger:" + id }); err == nil {
		t.Fatal("unreadable bytes inventoried without erasure proof")
	}
	// A foreign tenant context opens nothing.
	if _, _, err := store.Get(ctx, ctxB, "artifact-gone"); err == nil {
		t.Fatal("foreign tenant opened sealed bytes")
	}
}

// TestTodo_DATA_023_Conformance: the conformance vector pins outcome
// codes and the no-effects invariant.
func TestTodo_DATA_023_Conformance(t *testing.T) {
	store, ctxA, _, _ := lifecycleFixture(t)
	manifest := mustBackupInventory(t, ctxA, store,
		map[string]bool{"artifact-held": true},
		map[string]bool{"artifact-tomb": true}, nil)
	report := mustRestoreArtifacts(t, context.Background(), ctxA, store, manifest, seedRestorePolicy())
	outcomes := map[string]ArtifactOutcome{}
	for _, result := range report.Results {
		outcomes[result.ObjectID] = result.Outcome
	}
	if outcomes["artifact-active"] != OutcomeRestored || outcomes["artifact-held"] != OutcomeRestored {
		t.Fatalf("outcomes: %+v", outcomes)
	}
	if outcomes["artifact-tomb"] != OutcomeTombstoneKept {
		t.Fatalf("outcomes: %+v", outcomes)
	}
	if outcomes["artifact-erased"] != OutcomeRestored {
		t.Fatalf("non-erased backup restores readable: %+v", outcomes)
	}
	if report.EffectsEmitted != 0 {
		t.Fatalf("effects emitted: %d", report.EffectsEmitted)
	}
}

// TestTodo_DATA_023_Recovery: a fenced restore heals by re-seal and
// replays receipt-stable.
func TestTodo_DATA_023_Recovery(t *testing.T) {
	store, ctxA, _, _ := lifecycleFixture(t)
	manifest := mustBackupInventory(t, ctxA, store, nil, nil, nil)
	drifted := manifest
	drifted.Artifacts = append([]ArtifactBackup(nil), manifest.Artifacts...)
	drifted.Artifacts[0].Digest = "sha256:forged"
	fenced := mustRestoreArtifacts(t, context.Background(), ctxA, store, drifted, seedRestorePolicy())
	if fenced.Status != RestoreFenced {
		t.Fatalf("drifted passed: %+v", fenced)
	}
	healed := mustRestoreArtifacts(t, context.Background(), ctxA, store, manifest, seedRestorePolicy())
	if healed.Status != RestoreComplete {
		t.Fatalf("healed: %+v", healed)
	}
	replay := mustRestoreArtifacts(t, context.Background(), ctxA, store, manifest, seedRestorePolicy())
	if replay.Digest != healed.Digest {
		t.Fatalf("replay drift:\n got=%q\nwant=%q", replay.Digest, healed.Digest)
	}
}

// BenchmarkTodo_DATA_023 measures artifact restore verification.
func BenchmarkTodo_DATA_023(b *testing.B) {
	p := newFakeCustodyProvider()
	kek := custody.Handle{ID: "bench-kek", Kind: custody.Key, Version: "v1", Tenant: "bench", Region: "us-east"}
	p.keys[kek] = []byte("bench-master-key")
	m, err := envelope.New(custody.Handle{ID: "root", Kind: custody.Key, Version: "v1", Tenant: "root", Region: "us-east"}, p)
	if err != nil {
		b.Fatal(err)
	}
	ctxA := custody.Context{RequestContext: custody.RequestContext{Workload: "w-bench", Tenant: "bench", Region: "us-east", Purpose: "object-store", Destination: "local"}}
	if err := m.RegisterTenant(ctxA, "bench", kek); err != nil {
		b.Fatal(err)
	}
	store, err := NewSealedObjectStore(m)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	if _, err := store.Put(ctx, ctxA, "artifact-bench", "application/pdf", []byte("bench-bytes")); err != nil {
		b.Fatal(err)
	}
	manifest, err := BackupInventory(ctx, ctxA, store, nil, nil, nil, func(id string) string { return "ledger:lineage:" + id })
	if err != nil {
		b.Fatal(err)
	}
	policy := seedRestorePolicy()
	b.ResetTimer()
	for range b.N {
		if _, err := RestoreArtifacts(ctx, ctxA, store, manifest, policy); err != nil {
			b.Fatal(err)
		}
	}
}

// TestTodo_DATA_023_Mutation: manifest and identity edges resolve on
// the documented side.
func TestTodo_DATA_023_Mutation(t *testing.T) {
	store, ctxA, _, _ := lifecycleFixture(t)
	manifest := mustBackupInventory(t, ctxA, store, nil, nil, nil)
	// RTO exactly at budget passes.
	edge := seedRestorePolicy()
	edge.ObservedRTO = edge.BudgetRTO
	if report := mustRestoreArtifacts(t, context.Background(), ctxA, store, manifest, edge); report.Status != RestoreComplete {
		t.Fatalf("at-budget: %+v", report)
	}
	// Duplicate manifest entries fence.
	duped := manifest
	duped.Artifacts = append(append([]ArtifactBackup(nil), manifest.Artifacts...), manifest.Artifacts[0])
	if report := mustRestoreArtifacts(t, context.Background(), ctxA, store, duped, seedRestorePolicy()); report.Status != RestoreFenced {
		t.Fatalf("duplicated manifest passed: %+v", report)
	}
	// Erasure without proof refuses at backup time.
	if _, err := BackupInventory(context.Background(), ctxA, store, nil, nil, map[string]string{"artifact-active": ""}, func(id string) string { return "ledger:" + id }); err == nil {
		t.Fatal("proofless erasure inventoried")
	}
	// Lineage-less artifacts refuse at backup time.
	if _, err := BackupInventory(context.Background(), ctxA, store, nil, nil, nil, func(string) string { return "" }); err == nil {
		t.Fatal("lineage-less artifact inventoried")
	}
}
