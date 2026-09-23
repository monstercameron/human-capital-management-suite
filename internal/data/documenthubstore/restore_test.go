package documenthubstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func maxOutboxID(t *testing.T, s *Store, ctx context.Context, tenant, aggregate string) int64 {
	t.Helper()
	var id int64
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT COALESCE(max(id),0) FROM document_outbox WHERE tenant_id=$1 AND aggregate_id=$2`, tenant, aggregate).Scan(&id)
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestTodo_HUB_042 is the PRIMARY test for HUB-042: an independent backup
// restores immutable versions, scoped deployments, grants and outbox into
// a fresh database, and reconcile rebuilds indexes to a consistent
// watermark.
func TestTodo_HUB_042(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1 := searchFixture(t, s, ctx, "tenant-a", docA, "# Cats\n\nA guide to cats.\n")
	grantRead(t, s, ctx, "tenant-a", docA, "u-anna")
	if _, err := s.EmbedSections(ctx, "tenant-a", docA, v1.ID, testEmbeddingPolicy(), "hub-internal-v1", stubEmbedder); err != nil {
		t.Fatal(err)
	}
	snap, err := s.SnapshotDocument(ctx, "tenant-a", docA, "u-author")
	if err != nil {
		t.Fatalf("snapshot refused: %v", err)
	}
	if len(snap.Versions) != 1 || snap.Versions[0].Hash != v1.Hash || len(snap.Pointers) != 1 || len(snap.Grants) == 0 || len(snap.Outbox) == 0 {
		t.Fatalf("snapshot incomplete: %+v", snap)
	}
	fresh, _ := documentFixture(t)
	rep, err := fresh.RestoreDocument(ctx, "tenant-a", snap, "u-author")
	if err != nil {
		t.Fatalf("restore refused: %v", err)
	}
	if rep.Versions != 1 || rep.Deployments != 1 || rep.Grants == 0 || rep.Events == 0 {
		t.Fatalf("restore report wrong: %+v", rep)
	}
	live, err := fresh.ResolveDeployment(ctx, "tenant-a", docA, "default", "")
	if err != nil || live.VersionID != v1.ID {
		t.Fatalf("pointer lost: %+v err=%v", live, err)
	}
	rec, err := fresh.ReconcileDocument(ctx, "tenant-a", docA, "u-author", ReconcileOptions{Embed: stubEmbedder, Policy: testEmbeddingPolicy(), ModelID: "hub-internal-v1"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Watermark != maxOutboxID(t, fresh, ctx, "tenant-a", docA) {
		t.Fatalf("watermark inconsistent: %+v", rec)
	}
	hits, err := fresh.SearchLexical(ctx, "tenant-a", "cats", "person", "u-anna")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].VersionID != v1.ID {
		t.Fatalf("restored search wrong: %+v", hits)
	}
}

// TestTodo_HUB_042_Recovery is the RECOVERY test for HUB-042: post-restore
// lifecycle works and rollback leaves no stale vectors behind.
func TestTodo_HUB_042_Recovery(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1 := searchFixture(t, s, ctx, "tenant-a", docA, "# Cats\n\nA guide to cats.\n")
	snap, err := s.SnapshotDocument(ctx, "tenant-a", docA, "u-author")
	if err != nil {
		t.Fatal(err)
	}
	fresh, _ := documentFixture(t)
	if _, err := fresh.RestoreDocument(ctx, "tenant-a", snap, "u-author"); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.ReconcileDocument(ctx, "tenant-a", docA, "u-author", ReconcileOptions{Embed: stubEmbedder, Policy: testEmbeddingPolicy(), ModelID: "hub-internal-v1"}); err != nil {
		t.Fatal(err)
	}
	vecs, err := fresh.SectionVectors(ctx, "tenant-a", docA, v1.ID, "hub-internal-v1")
	if err != nil || len(vecs) == 0 {
		t.Fatalf("live vectors not rebuilt: %+v err=%v", vecs, err)
	}
	if _, err := fresh.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docA, SubjectKind: "person", SubjectID: "u-author", Action: ActionRetire, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.Withdraw(ctx, "tenant-a", WithdrawInput{DocumentID: docA, ScopeKind: "default", ScopeID: "", ActorID: "u-author", ExpectedLive: v1.ID, Reason: "rollback"}); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.ReconcileDocument(ctx, "tenant-a", docA, "u-author", ReconcileOptions{}); err != nil {
		t.Fatal(err)
	}
	vecs, err = fresh.SectionVectors(ctx, "tenant-a", docA, v1.ID, "hub-internal-v1")
	if err != nil || len(vecs) != 0 {
		t.Fatalf("stale vectors after rollback: %+v err=%v", vecs, err)
	}
}

// TestTodo_HUB_042_Fault is the FAULT test for HUB-042: tampered bytes
// and double restore are refused.
func TestTodo_HUB_042_Fault(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	searchFixture(t, s, ctx, "tenant-a", docA, "# Cats\n\nA guide to cats.\n")
	snap, err := s.SnapshotDocument(ctx, "tenant-a", docA, "u-author")
	if err != nil {
		t.Fatal(err)
	}
	bad := snap
	bad.Versions = append([]SnapshotVersion(nil), snap.Versions...)
	bad.Versions[0].Markdown += "tampered"
	fresh, _ := documentFixture(t)
	if _, err := fresh.RestoreDocument(ctx, "tenant-a", bad, "u-author"); !errors.Is(err, ErrRestoreTampered) {
		t.Fatalf("tampered backup restored: %v", err)
	}
	if _, err := fresh.RestoreDocument(ctx, "tenant-a", snap, "u-author"); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.RestoreDocument(ctx, "tenant-a", snap, "u-author"); !errors.Is(err, ErrRestoreExists) {
		t.Fatalf("double restore accepted: %v", err)
	}
	if _, err := s.SnapshotDocument(ctx, "tenant-a", docA, "u-stranger"); !errors.Is(err, ErrDenied) {
		t.Fatalf("stranger snapshot: %v", err)
	}
}
