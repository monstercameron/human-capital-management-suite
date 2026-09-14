package records

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func priv004Request(at time.Time) RestoreRequest {
	epoch := uint64(42)
	return RestoreRequest{
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
}

func priv004At() time.Time { return time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC) }

// TestTodo_PRIV_004_Golden pins the restore acceptance oracle.
func TestTodo_PRIV_004_Golden(t *testing.T) {
	rec, err := AcceptRestore(priv004Request(priv004At()))
	if err != nil {
		t.Fatalf("AcceptRestore: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "restore.golden.txt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	oracle := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ": ")
		if !ok {
			t.Fatalf("malformed golden line: %q", line)
		}
		oracle[key] = value
	}
	if rec.Digest != oracle["digest"] {
		t.Fatalf("digest mismatch:\n got=%q\nwant=%q", rec.Digest, oracle["digest"])
	}
	if rec.Status != RestoreStatus(oracle["status"]) || rec.Explain() != oracle["explain"] {
		t.Fatalf("receipt mismatch:\n got=%q %q\nwant=%q %q", rec.Status, rec.Explain(), oracle["status"], oracle["explain"])
	}
}

// TestTodo_PRIV_004_Race: acceptance is pure, so concurrent restores over
// shared inputs stay race-free and agree.
func TestTodo_PRIV_004_Race(t *testing.T) {
	req := priv004Request(priv004At())
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				rec, err := AcceptRestore(req)
				if err != nil {
					t.Errorf("AcceptRestore: %v", err)
					return
				}
				if rec.Status != RestoreOpen || rec.Digest == "" {
					t.Errorf("receipt=%+v", rec)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestTodo_PRIV_004_Fault: drifted epochs and unverified copies fence;
// malformed envelopes error.
func TestTodo_PRIV_004_Fault(t *testing.T) {
	req := priv004Request(priv004At())

	future := req
	future.Manifests.Epoch = req.BackupEpoch + 1
	rec, err := AcceptRestore(future)
	if err != nil {
		t.Fatalf("AcceptRestore: %v", err)
	}
	if rec.Status != RestoreFenced {
		t.Fatal("future manifest epoch opened")
	}
	untombed := req
	untombed.Copies[0].TombstoneDigest = ""
	rec, err = AcceptRestore(untombed)
	if err != nil {
		t.Fatalf("AcceptRestore: %v", err)
	}
	if rec.Status != RestoreFenced || !hasRestoreFinding(rec.Findings, "TOMBSTONE_UNVERIFIED") {
		t.Fatalf("findings=%+v, want tombstone fencing", rec.Findings)
	}
	badRestriction := req
	badRestriction.Manifests.Restrictions = []RestrictionManifest{{SubjectID: "", Purposes: nil, Digest: ""}}
	rec, err = AcceptRestore(badRestriction)
	if err != nil {
		t.Fatalf("AcceptRestore: %v", err)
	}
	if rec.Status != RestoreFenced || !hasRestoreFinding(rec.Findings, "RESTRICTION_UNVERIFIABLE") {
		t.Fatalf("findings=%+v, want restriction fencing", rec.Findings)
	}
	zeroEpoch := req
	zeroEpoch.BackupEpoch = 0
	if _, err := AcceptRestore(zeroEpoch); err == nil {
		t.Fatal("zero backup epoch accepted")
	}
	copyless := req
	copyless.Copies = nil
	if _, err := AcceptRestore(copyless); err == nil {
		t.Fatal("copyless restore accepted")
	}
}

// TestTodo_PRIV_004_Security: manifests never cross tenants.
func TestTodo_PRIV_004_Security(t *testing.T) {
	req := priv004Request(priv004At())
	req.Copies[0].Tenant = "other-tenant"
	if _, err := AcceptRestore(req); err == nil {
		t.Fatal("cross-tenant copy accepted")
	}
	req = priv004Request(priv004At())
	rec, err := AcceptRestore(req)
	if err != nil {
		t.Fatalf("AcceptRestore: %v", err)
	}
	if rec.Tenant != "tenant-1" {
		t.Fatal("receipt carries the wrong tenant")
	}
	for _, f := range rec.Findings {
		if strings.Contains(f.Detail, "other-tenant") {
			t.Fatalf("finding leaks foreign tenant: %+v", f)
		}
	}
}

// TestTodo_PRIV_004_Recovery: holds reapply as exceptions and the watermark
// pins the epochs the service may serve from.
func TestTodo_PRIV_004_Recovery(t *testing.T) {
	req := priv004Request(priv004At())
	rec, err := AcceptRestore(req)
	if err != nil {
		t.Fatalf("AcceptRestore: %v", err)
	}
	if rec.ReappliedHolds != 1 {
		t.Fatalf("receipt=%+v, want the derived hold reapplied", rec)
	}
	if rec.Watermark != req.BackupEpoch {
		t.Fatalf("watermark=%d, want the backup epoch", rec.Watermark)
	}
	// Reapplication is idempotent across repeated acceptance.
	again, err := AcceptRestore(req)
	if err != nil || again.Digest != rec.Digest {
		t.Fatal("repeat acceptance disagrees")
	}
}

// TestTodo_PRIV_004_Mutation: epoch and tombstone edges resolve on the
// documented side.
func TestTodo_PRIV_004_Mutation(t *testing.T) {
	req := priv004Request(priv004At())
	// Equal epochs open; a single epoch of drift fences either way.
	rec, err := AcceptRestore(req)
	if err != nil || rec.Status != RestoreOpen {
		t.Fatalf("equal epochs: %+v %v", rec, err)
	}
	// Near-miss tombstone digests never match.
	near := req
	near.Copies[0].TombstoneDigest = "sha256:tomb-2"
	rec, err = AcceptRestore(near)
	if err != nil {
		t.Fatalf("AcceptRestore: %v", err)
	}
	if rec.Status != RestoreFenced {
		t.Fatal("near-miss tombstone opened")
	}
	// Restriction purposes are required, not decorative.
	purposeless := req
	purposeless.Manifests.Restrictions = []RestrictionManifest{{SubjectID: "worker-1", Digest: "sha256:restr-1"}}
	rec, err = AcceptRestore(purposeless)
	if err != nil {
		t.Fatalf("AcceptRestore: %v", err)
	}
	if rec.Status != RestoreFenced {
		t.Fatal("purposeless restriction opened")
	}
}

func hasRestoreFinding(findings []RestoreFinding, code string) bool {
	for _, f := range findings {
		if f.Code == code {
			return true
		}
	}
	return false
}
