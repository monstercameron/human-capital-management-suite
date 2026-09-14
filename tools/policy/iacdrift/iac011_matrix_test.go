package iacdrift

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedDesired() []Resource {
	return []Resource{
		{ID: "cell-pg", Kind: "postgres", Owner: "dataops", Capability: "ledger-store", ConfigDigest: "sha256:pg-v1", Protected: true},
		{ID: "cell-bucket", Kind: "object", Owner: "dataops", Capability: "artifact-store", ConfigDigest: "sha256:bk-v1", Protected: true},
		{ID: "cell-queue", Kind: "queue", Owner: "platform", Capability: "dispatch", ConfigDigest: "sha256:q-v1"},
	}
}

func mustDetect(t *testing.T, desired, observed []Resource) Report {
	t.Helper()
	report, err := Detect(desired, observed)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	return report
}

func mustRefuseDestructive(t *testing.T, entry DriftEntry, change DestructiveChange) {
	t.Helper()
	if err := AdmitDestructive(entry, change, BreakGlass{}); err == nil {
		t.Fatalf("destructive change to %s admitted without break-glass", entry.ResourceID)
	} else if !strings.Contains(err.Error(), CodeBreakGlassRequired) {
		t.Fatalf("expected %s, got %v", CodeBreakGlassRequired, err)
	}
}

func mustAdmitDestructive(t *testing.T, entry DriftEntry, change DestructiveChange, glass BreakGlass) {
	t.Helper()
	if err := AdmitDestructive(entry, change, glass); err != nil {
		t.Fatalf("AdmitDestructive(%s): %v", entry.ResourceID, err)
	}
}

func seedGlass() BreakGlass {
	return BreakGlass{ResourceID: "cell-pg", ApproverOne: "alice", ApproverTwo: "bob", Reason: "rotate bucket keys", ExpiresAt: "2026-10-01T00:00:00Z", RecoveryEvidence: "iac010:recovery-cell-a:sha256:ok"}
}

// TestTodo_IAC_011_Golden pins the drift report digest oracle.
func TestTodo_IAC_011_Golden(t *testing.T) {
	observed := seedDesired()
	observed[1].ConfigDigest = "sha256:bk-v2"
	report := mustDetect(t, seedDesired(), observed)
	raw, err := os.ReadFile(filepath.Join("testdata", "iac011.golden.txt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	want := strings.TrimSpace(string(raw))
	if report.Digest != want {
		t.Fatalf("drift digest mismatch:\n got=%q\nwant=%q", report.Digest, want)
	}
}

// TestTodo_IAC_011_Integration: clean inventories are silent; every
// drift class (mutation, addition, removal) maps to owner/capability.
func TestTodo_IAC_011_Integration(t *testing.T) {
	desired := seedDesired()
	if report := mustDetect(t, desired, seedDesired()); len(report.Entries) != 0 {
		t.Fatalf("clean inventory drifts: %+v", report.Entries)
	}
	observed := []Resource{
		{ID: "cell-pg", Kind: "postgres", Owner: "dataops", Capability: "ledger-store", ConfigDigest: "sha256:pg-v2", Protected: true},
		{ID: "cell-queue", Kind: "queue", Owner: "platform", Capability: "dispatch", ConfigDigest: "sha256:q-v1"},
		{ID: "cell-cache", Kind: "cache", Owner: "platform", Capability: "read-model", ConfigDigest: "sha256:c-v1"},
	}
	report := mustDetect(t, desired, observed)
	byID := map[string]DriftEntry{}
	for _, entry := range report.Entries {
		byID[entry.ResourceID] = entry
	}
	if byID["cell-pg"].From != "sha256:pg-v1" || byID["cell-pg"].Owner != "dataops" {
		t.Fatalf("mutation entry wrong: %+v", byID["cell-pg"])
	}
	if !byID["cell-cache"].Added || byID["cell-cache"].Capability != "read-model" {
		t.Fatalf("addition entry wrong: %+v", byID["cell-cache"])
	}
	if !byID["cell-bucket"].Removed || byID["cell-bucket"].Capability != "artifact-store" {
		t.Fatalf("removal entry wrong: %+v", byID["cell-bucket"])
	}
	// Unprotected queue replacement passes without break-glass.
	mustAdmitDestructive(t, DriftEntry{ResourceID: "cell-queue", Kind: "queue"}, DestructiveChange{Replacement: true}, BreakGlass{})
}

// TestTodo_IAC_011_Fault: broad destroy and self-approved break-glass
// stay refused; malformed inventories error.
func TestTodo_IAC_011_Fault(t *testing.T) {
	protected := DriftEntry{ResourceID: "cell-pg", Kind: "postgres", Protected: true}
	mustRefuseDestructive(t, protected, DestructiveChange{Destroy: true})
	// One approver twice is not two-person control.
	glass := seedGlass()
	glass.ApproverTwo = glass.ApproverOne
	if err := AdmitDestructive(protected, DestructiveChange{Destroy: true}, glass); err == nil {
		t.Fatal("self-approved break-glass admitted")
	} else if !strings.Contains(err.Error(), CodeBreakGlassRequired) {
		t.Fatalf("expected %s, got %v", CodeBreakGlassRequired, err)
	}
	// Missing reason, expiry, or recovery evidence each refuse.
	for i := range 3 {
		glass := seedGlass()
		switch i {
		case 0:
			glass.Reason = ""
		case 1:
			glass.ExpiresAt = ""
		case 2:
			glass.RecoveryEvidence = ""
		}
		if err := AdmitDestructive(protected, DestructiveChange{Replacement: true}, glass); err == nil {
			t.Fatalf("incomplete break-glass case %d admitted", i)
		}
	}
	// Malformed inventories error instead of silently passing.
	if _, err := Detect([]Resource{{ID: "", Owner: "x"}}, nil); err == nil {
		t.Fatal("desired resource without id/capability admitted")
	}
	if _, err := Detect(seedDesired(), []Resource{{ID: ""}}); err == nil {
		t.Fatal("observed resource without id admitted")
	}
}

// TestTodo_IAC_011_Security: break-glass approval is scoped to its
// named resource and unmarked irreplaceable kinds still gate.
func TestTodo_IAC_011_Security(t *testing.T) {
	bucket := DriftEntry{ResourceID: "cell-bucket", Kind: "object", Protected: true}
	pg := DriftEntry{ResourceID: "cell-pg", Kind: "postgres", Protected: true}
	bucketGlass := seedGlass()
	bucketGlass.ResourceID = "cell-bucket"
	mustAdmitDestructive(t, bucket, DestructiveChange{Replacement: true}, bucketGlass)
	// The same approval reused for another protected resource refuses.
	if err := AdmitDestructive(pg, DestructiveChange{Replacement: true}, bucketGlass); err == nil {
		t.Fatal("cross-resource break-glass replay admitted")
	} else if !strings.Contains(err.Error(), CodeBreakGlassRequired) {
		t.Fatalf("expected %s, got %v", CodeBreakGlassRequired, err)
	}
	// An unmarked database still gates by kind default.
	unmarked := DriftEntry{ResourceID: "cell-pg-shadow", Kind: "postgres"}
	shadowGlass := seedGlass()
	shadowGlass.ResourceID = "cell-pg-shadow"
	mustRefuseDestructive(t, unmarked, DestructiveChange{Destroy: true})
	mustAdmitDestructive(t, unmarked, DestructiveChange{Destroy: true}, shadowGlass)
}
