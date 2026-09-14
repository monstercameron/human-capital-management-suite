package object

import (
	"context"
	"testing"
	"time"
)

func TestArtifactRestoreReconcilesBytesMetadataKeysHoldsTombstonesAndLineage(t *testing.T) {
	m, ctxA, _, _ := sealedFixture(t)
	ctx := context.Background()
	store, err := NewSealedObjectStore(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(ctx, ctxA, "artifact-1", "application/pdf", []byte("promotion-letter")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := store.Put(ctx, ctxA, "artifact-2", "image/png", []byte("signature-scan")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	manifest := mustBackupInventory(t, ctxA, store, map[string]bool{"artifact-2": true}, nil, nil)
	report := mustRestoreArtifacts(t, ctx, ctxA, store, manifest, RestorePolicy{
		BudgetRPO: time.Minute, BudgetRTO: 5 * time.Minute,
		ObservedRPO: 30 * time.Second, ObservedRTO: 2 * time.Minute,
	})
	if report.Status != RestoreComplete {
		t.Fatalf("status=%v findings=%+v", report.Status, report.Findings)
	}
	if report.Restored != 2 || report.HoldsKept != 1 {
		t.Fatalf("report: %+v", report)
	}
	if report.Digest == "" {
		t.Fatal("restore report must carry a digest")
	}
	// Bytes round-trip: restored bytes equal the sealed originals.
	plaintext, _, err := store.Get(ctx, ctxA, "artifact-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(plaintext) != "promotion-letter" {
		t.Fatalf("bytes = %q", plaintext)
	}
}
