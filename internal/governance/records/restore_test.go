package records

import (
	"testing"
	"time"
)

// PRIV-004 RED: restore-time manifest reapplication before restore.go.
func TestTodo_PRIV_004(t *testing.T) {
	at := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	epoch := uint64(42)
	req := RestoreRequest{
		RestoreID: "restore-1", Tenant: "tenant-1", At: at,
		BackupEpoch: epoch,
		Manifests: RestoreManifests{
			Epoch: epoch,
			Tombstones: []Tombstone{
				{CopyID: "copy-restored", Digest: "sha256:tomb-1"},
			},
			Restrictions: []RestrictionManifest{
				{SubjectID: "worker-1", Purposes: []string{"payroll"}, Digest: "sha256:restr-1"},
			},
			Holds: []DeletionHold{
				{ID: "hold-1", CopyID: "copy-derived", Authority: "legal-1", Reason: "litigation"},
			},
		},
		Copies: []RestoredCopy{
			{ID: "copy-restored", Kind: CopyKindRestored, Tenant: "tenant-1", TombstoneDigest: "sha256:tomb-1"},
			{ID: "copy-derived", Kind: CopyKindDerived, Tenant: "tenant-1"},
			{ID: "copy-canonical", Kind: CopyKindCanonical, Tenant: "tenant-1"},
		},
	}

	rec, err := AcceptRestore(req)
	if err != nil {
		t.Fatalf("AcceptRestore: %v", err)
	}
	if rec.Status != RestoreOpen {
		t.Fatalf("status=%q findings=%+v, want OPEN", rec.Status, rec.Findings)
	}
	if rec.ReappliedTombstones != 1 || rec.ReappliedRestrictions != 1 {
		t.Fatalf("receipt=%+v, want tombstone and restriction reapplied", rec)
	}
	open := rec

	// RED: a restore that resurrects without the tombstone epoch stays
	// fenced — deleted data must not come back to service.
	stale := req
	stale.Manifests.Epoch = epoch - 1
	rec, err = AcceptRestore(stale)
	if err != nil {
		t.Fatalf("AcceptRestore(stale): %v", err)
	}
	if rec.Status != RestoreFenced {
		t.Fatal("epoch-mismatched restore opened, want FENCED")
	}

	// RED: reapplication is idempotent — the second acceptance agrees and
	// re-deletes nothing twice.
	again, err := AcceptRestore(req)
	if err != nil {
		t.Fatalf("AcceptRestore again: %v", err)
	}
	if again.Digest != open.Digest || again.Status != RestoreOpen {
		t.Fatal("identical restores disagree")
	}
}
