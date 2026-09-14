package records

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func model028Request(at time.Time) DeletionRequest {
	return DeletionRequest{
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
}

func model028At() time.Time { return time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC) }

// TestTodo_MODEL_028_Property: outcome algebra — every covered copy lands
// in exactly one outcome, holds always except, ordering never moves the
// digest.
func TestTodo_MODEL_028_Property(t *testing.T) {
	req := model028Request(model028At())
	cert, err := ExecuteDeletion(req)
	if err != nil {
		t.Fatalf("ExecuteDeletion: %v", err)
	}
	if len(cert.Outcomes) != len(req.Copies) {
		t.Fatalf("outcomes=%d for %d copies", len(cert.Outcomes), len(req.Copies))
	}
	// Copy reorder is the same deletion.
	rev := req
	rev.Copies = append([]DeletableCopy(nil), req.Copies...)
	for i, j := 0, len(rev.Copies)-1; i < j; i, j = i+1, j-1 {
		rev.Copies[i], rev.Copies[j] = rev.Copies[j], rev.Copies[i]
	}
	again, err := ExecuteDeletion(rev)
	if err != nil || again.Digest != cert.Digest {
		t.Fatal("copy reorder moved the digest")
	}
	// Retention-future and anonymize flags route honestly. Retention is a
	// lawful terminal outcome: the certificate completes recording it.
	retained := model028Request(model028At())
	later := model028At().Add(24 * time.Hour)
	retained.Copies[0].RetainUntil = &later
	cert, err = ExecuteDeletion(retained)
	if err != nil {
		t.Fatalf("ExecuteDeletion: %v", err)
	}
	if outcomeOf(cert.Outcomes, "copy-canonical") != OutcomeRetained || !cert.Complete {
		t.Fatalf("retained copy: %+v, want a complete certificate recording retention", cert.Outcomes)
	}
	anon := model028Request(model028At())
	anon.Copies[0].Anonymize = true
	cert, err = ExecuteDeletion(anon)
	if err != nil {
		t.Fatalf("ExecuteDeletion: %v", err)
	}
	if outcomeOf(cert.Outcomes, "copy-canonical") != OutcomeAnonymized {
		t.Fatalf("outcomes=%+v, want anonymization", cert.Outcomes)
	}
}

// TestTodo_MODEL_028_Golden pins the deletion certificate oracle.
func TestTodo_MODEL_028_Golden(t *testing.T) {
	cert, err := ExecuteDeletion(model028Request(model028At()))
	if err != nil {
		t.Fatalf("ExecuteDeletion: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "deletion.golden.txt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var want string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		want = line
	}
	if cert.Digest != want {
		t.Fatalf("digest mismatch:\n got=%q\nwant=%q", cert.Digest, want)
	}
}

// TestTodo_MODEL_028_Fault: misscoped and contradictory envelopes refuse;
// unverified copies block rather than complete.
func TestTodo_MODEL_028_Fault(t *testing.T) {
	foreign := model028Request(model028At())
	foreign.Copies[0].Tenant = "other-tenant"
	if _, err := ExecuteDeletion(foreign); err == nil {
		t.Fatal("cross-tenant copy accepted")
	}
	dupe := model028Request(model028At())
	dupe.Copies = append(dupe.Copies, DeletableCopy{ID: "copy-canonical", Kind: CopyKindCanonical, Tenant: "tenant-1"})
	if _, err := ExecuteDeletion(dupe); err == nil {
		t.Fatal("duplicate copy accepted")
	}
	unknown := model028Request(model028At())
	unknown.Copies[0].Kind = "PHANTOM"
	if _, err := ExecuteDeletion(unknown); err == nil {
		t.Fatal("unknown copy class accepted")
	}
	ghost := model028Request(model028At())
	ghost.Holds = []DeletionHold{{ID: "hold-ghost", CopyID: "copy-GHOST", Authority: "legal-1", Reason: "x"}}
	if _, err := ExecuteDeletion(ghost); err == nil {
		t.Fatal("hold on an unaccounted copy accepted")
	}
	noredel := model028Request(model028At())
	noredel.Copies[3].ReDeleted = false
	cert, err := ExecuteDeletion(noredel)
	if err != nil {
		t.Fatalf("ExecuteDeletion: %v", err)
	}
	if cert.Complete {
		t.Fatal("backup without re-delete evidence completed")
	}
	mismatch := model028Request(model028At())
	mismatch.Tombstones = []Tombstone{{CopyID: "copy-restored", Digest: "sha256:wrong"}}
	cert, err = ExecuteDeletion(mismatch)
	if err != nil {
		t.Fatalf("ExecuteDeletion: %v", err)
	}
	if cert.Complete {
		t.Fatal("tombstone mismatch completed")
	}
	empty := model028Request(model028At())
	empty.Copies = nil
	if _, err := ExecuteDeletion(empty); err == nil {
		t.Fatal("copyless deletion accepted")
	}
}

// TestTodo_MODEL_028_Recovery: restored backups reapply tombstones before
// service, and re-delete evidence closes the backup loop.
func TestTodo_MODEL_028_Recovery(t *testing.T) {
	req := model028Request(model028At())
	cert, err := ExecuteDeletion(req)
	if err != nil {
		t.Fatalf("ExecuteDeletion: %v", err)
	}
	if !cert.Complete {
		t.Fatalf("outcomes=%+v, want a complete certificate", cert.Outcomes)
	}
	if outcomeOf(cert.Outcomes, "copy-restored") != OutcomeDestroyed {
		t.Fatalf("outcomes=%+v, tombstoned restore must destroy", cert.Outcomes)
	}
	if outcomeOf(cert.Outcomes, "copy-backup") != OutcomeDestroyed {
		t.Fatalf("outcomes=%+v, re-deleted backup must destroy", cert.Outcomes)
	}
	// A second identical execution agrees: the certificate is deterministic.
	again, err := ExecuteDeletion(req)
	if err != nil || again.Digest != cert.Digest {
		t.Fatal("repeat execution disagrees")
	}
}

// FuzzTodo_MODEL_028: hostile deletion envelopes never panic, never error
// with a digest attached, and never destroy a held copy.
func FuzzTodo_MODEL_028(f *testing.F) {
	f.Add("del-1", "tenant-1", "copy-canonical", "legal-1")
	f.Add("", "", "\x00", "   ")
	f.Add("d", "t", "copy-\xff", "h")
	f.Fuzz(func(t *testing.T, deletionID, tenant, copyID, authority string) {
		at := model028At()
		req := DeletionRequest{
			DeletionID: deletionID, Tenant: tenant, RecordID: "worker-1",
			RequestedBy: "privacy-officer", At: at,
			Copies: []DeletableCopy{
				{ID: copyID, Kind: CopyKindCanonical, Tenant: tenant},
				{ID: "copy-backup", Kind: CopyKindBackup, Tenant: tenant, ReDeleted: true, ReDeleteRef: "redel-1"},
				{ID: "copy-restored", Kind: CopyKindRestored, Tenant: tenant, TombstoneDigest: "sha256:tomb-1"},
				{ID: "copy-derived", Kind: CopyKindDerived, Tenant: tenant},
				{ID: "copy-external", Kind: CopyKindExternal, Tenant: tenant},
			},
			Tombstones: []Tombstone{{CopyID: "copy-restored", Digest: "sha256:tomb-1"}},
			Holds:      []DeletionHold{{ID: "hold-1", CopyID: "copy-derived", Authority: authority, Reason: "litigation"}},
		}
		cert, err := ExecuteDeletion(req)
		if err != nil {
			if cert.Digest != "" {
				t.Fatal("errored deletion carries a digest")
			}
			return
		}
		for _, o := range cert.Outcomes {
			if o.CopyID == "copy-derived" && (o.Outcome == OutcomeDestroyed || o.Outcome == OutcomeAnonymized) {
				t.Fatalf("held copy destroyed: %+v", o)
			}
		}
	})
}
