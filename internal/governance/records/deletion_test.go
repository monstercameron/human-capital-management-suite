package records

import (
	"testing"
	"time"
)

// MODEL-028 RED: verified deletion and backup re-delete before deletion.go.
func TestTodo_MODEL_028(t *testing.T) {
	at := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	req := DeletionRequest{
		DeletionID: "del-1", Tenant: "tenant-1", RecordID: "worker-1",
		RequestedBy: "privacy-officer", At: at,
		Copies: []DeletableCopy{
			{ID: "copy-canonical", Kind: CopyKindCanonical, Tenant: "tenant-1"},
			{ID: "copy-derived", Kind: CopyKindDerived, Tenant: "tenant-1"},
			{ID: "copy-external", Kind: CopyKindExternal, Tenant: "tenant-1"},
			{ID: "copy-backup", Kind: CopyKindBackup, Tenant: "tenant-1", ReDeleted: true, ReDeleteRef: "redel-1"},
			{ID: "copy-restored", Kind: CopyKindRestored, Tenant: "tenant-1", TombstoneDigest: "sha256:tomb-1"},
		},
		Tombstones: []Tombstone{{CopyID: "copy-restored", Digest: "sha256:tomb-1"}},
		Holds:      []DeletionHold{{ID: "hold-1", CopyID: "copy-derived", Authority: "legal-1", Reason: "litigation"}},
	}

	cert, err := ExecuteDeletion(req)
	if err != nil {
		t.Fatalf("ExecuteDeletion: %v", err)
	}
	if !cert.Complete {
		t.Fatalf("certificate incomplete: %+v", cert)
	}
	if cert.Digest == "" {
		t.Fatal("certificate carries no digest")
	}
	// The held derived copy is an exception, never a destruction.
	if outcomeOf(cert.Outcomes, "copy-derived") != OutcomeException {
		t.Fatalf("outcomes=%+v, held copy must be an exception", cert.Outcomes)
	}

	// RED: deletion is incomplete while an eligible copy is unhandled —
	// drop the external copy and the certificate must not complete.
	partial := req
	partial.Copies = req.Copies[:3]
	if _, err := ExecuteDeletion(partial); err == nil {
		t.Fatal("deletion without the backup/restored copies completed")
	}

	// RED: a restored backup serves only behind its tombstone.
	untombed := req
	untombed.Tombstones = nil
	cert, err = ExecuteDeletion(untombed)
	if err != nil {
		t.Fatalf("ExecuteDeletion(untombed): %v", err)
	}
	if cert.Complete {
		t.Fatal("restored copy without a tombstone certified")
	}
}

func outcomeOf(outcomes []CopyOutcome, id string) OutcomeKind {
	for _, o := range outcomes {
		if o.CopyID == id {
			return o.Outcome
		}
	}
	return OutcomeUnspecified
}
