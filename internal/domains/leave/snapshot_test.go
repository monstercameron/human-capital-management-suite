package leave

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/documents/intake"
)

// snapshotEntries binds every required input to a governed reference from
// a fresh pipeline service, so every call mints the same deterministic
// identities.
func snapshotEntries(t *testing.T) []InputEntry {
	t.Helper()
	svc, _ := rev024MintService(t)
	return rev024Entries(t, svc)
}

func TestTodo_LEAVE_003(t *testing.T) {
	snapshot, err := BuildSnapshot(snapshotEntries(t))
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if len(snapshot.Entries) != len(requiredLeaveInputs) {
		t.Fatalf("entries = %d, want %d", len(snapshot.Entries), len(requiredLeaveInputs))
	}
	if !snapshot.Satisfied() || len(snapshot.Pending()) != 0 {
		t.Fatal("all-ready snapshot is not satisfied")
	}
	if err := snapshot.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// RED: every omission or hollow binding refuses.
	for _, input := range requiredLeaveInputs {
		var kept []InputEntry
		for _, entry := range snapshotEntries(t) {
			if entry.Input != input {
				kept = append(kept, entry)
			}
		}
		if _, err := BuildSnapshot(kept); err == nil {
			t.Fatalf("omitted input %q built", input)
		}
	}
	hollow := snapshotEntries(t)
	hollow[0].Revision = ""
	if _, err := BuildSnapshot(hollow); err == nil {
		t.Fatal("hollow revision built")
	}
	unissued := snapshotEntries(t)
	unissued[0].EvidenceRef = intake.EvidenceRef{}
	if _, err := BuildSnapshot(unissued); err == nil {
		t.Fatal("unissued evidence reference built")
	}
	unstamped := snapshotEntries(t)
	unstamped[0].Effective = ""
	if _, err := BuildSnapshot(unstamped); err == nil {
		t.Fatal("unstamped input built")
	}
	dup := append(snapshotEntries(t), snapshotEntries(t)[0])
	if _, err := BuildSnapshot(dup); err == nil {
		t.Fatal("duplicate input built")
	}
	// Unsatisfied inputs keep their exact statuses.
	partial := snapshotEntries(t)
	partial[0].Status = InputPartial
	partial[1].Status = InputStale
	pending, err := BuildSnapshot(partial)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if pending.Satisfied() || len(pending.Pending()) != 2 {
		t.Fatalf("pending = %v", pending.Pending())
	}
}

func TestTodo_LEAVE_003_Property(t *testing.T) {
	first, err := BuildSnapshot(snapshotEntries(t))
	if err != nil {
		t.Fatal(err)
	}
	// Deterministic across input orderings.
	reversed := snapshotEntries(t)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	second, err := BuildSnapshot(reversed)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatal("input ordering changed the snapshot digest")
	}
	// Satisfied holds exactly when every input is READY.
	for _, status := range []string{InputPartial, InputStale, InputUnknown, InputDenied} {
		variant := snapshotEntries(t)
		variant[3].Status = status
		built, err := BuildSnapshot(variant)
		if err != nil {
			t.Fatal(err)
		}
		if built.Satisfied() {
			t.Fatalf("status %s snapshot satisfied", status)
		}
		if len(built.Pending()) != 1 || !strings.HasSuffix(built.Pending()[0], ":"+status) {
			t.Fatalf("pending = %v", built.Pending())
		}
	}
	if _, err := BuildSnapshot(snapshotEntries(t)); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_LEAVE_003_Golden(t *testing.T) {
	snapshot, err := BuildSnapshot(snapshotEntries(t))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	lines = append(lines, "inputs="+strings.Join(requiredLeaveInputs, ","))
	lines = append(lines, "satisfied=true")
	for _, entry := range snapshot.Entries {
		lines = append(lines, "input="+entry.Input+" rev="+entry.Revision+" status="+entry.Status+" ref="+entry.EvidenceRef.ID)
	}
	lines = append(lines, "digest="+snapshot.Digest)
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "leave003_snapshot.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set HCMNEXT_UPDATE_GOLDEN=1)", err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestTodo_LEAVE_003_Security(t *testing.T) {
	// Restricted inputs enter as DENIED references, never as bytes: the
	// entry carries a governed reference with no content field, so bytes
	// cannot even be expressed, and denial never voids the reference.
	restricted := snapshotEntries(t)
	for i := range restricted {
		if restricted[i].Input == "legal-context" {
			restricted[i].Status = InputDenied
		}
	}
	snapshot, err := BuildSnapshot(restricted)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if snapshot.Satisfied() {
		t.Fatal("denied snapshot satisfied")
	}
	for _, entry := range snapshot.Entries {
		if entry.Input == "legal-context" {
			if entry.EvidenceRef.ID == "" {
				t.Fatalf("denied entry lost its reference: %+v", entry)
			}
			if err := entry.EvidenceRef.Check(); err != nil {
				t.Fatalf("denied entry reference unusable: %v", err)
			}
		}
	}
	// Off-vocabulary statuses never become input truth.
	rogue := snapshotEntries(t)
	rogue[0].Status = "READY-ISH"
	if _, err := BuildSnapshot(rogue); err == nil {
		t.Fatal("off-vocabulary status built")
	}
}

func TestTodo_LEAVE_003_Conformance(t *testing.T) {
	snapshot, err := BuildSnapshot(snapshotEntries(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 16 {
		t.Fatalf("entries = %d, want the 16 required inputs", len(snapshot.Entries))
	}
	seen := make(map[string]string, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		seen[entry.Input] = entry.Status
	}
	for _, required := range []string{"worker", "employment", "assignment", "location", "manager", "schedule", "calendar", "balances", "benefits", "payroll-context", "legal-context", "evidence-usability", "source-authority", "freshness", "effective-time", "known-time"} {
		if seen[required] != InputReady {
			t.Fatalf("input %q status = %q", required, seen[required])
		}
	}
}

func TestTodo_LEAVE_003_Mutation(t *testing.T) {
	base, err := BuildSnapshot(snapshotEntries(t))
	if err != nil {
		t.Fatal(err)
	}
	// Material input change changes the canonical digest.
	changed := snapshotEntries(t)
	changed[0].Revision = "rev-8"
	rebuilt, err := BuildSnapshot(changed)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.Digest == base.Digest {
		t.Fatal("revision mutation kept the snapshot digest")
	}
	// Status change changes the digest and flips satisfaction.
	restaled := snapshotEntries(t)
	restaled[5].Status = InputStale
	stale, err := BuildSnapshot(restaled)
	if err != nil {
		t.Fatal(err)
	}
	if stale.Digest == base.Digest || stale.Satisfied() {
		t.Fatal("status mutation kept digest or satisfaction")
	}
	// Tampered entries break the seal.
	forged := base
	forged.Entries[0].Revision = "rev-forged"
	if err := forged.Verify(); err == nil {
		t.Fatal("forged snapshot verified")
	}
}
