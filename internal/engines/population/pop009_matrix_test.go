package population

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func pop009Snapshot(def string, ids []string) Snapshot {
	return Snapshot{DefinitionID: def, SubjectIDs: ids, Digest: "sha256:fixture"}
}

// TestTodo_POP_009_Property: partition algebra — every expected member is
// matched or missing, every observed occurrence is matched, unexpected or
// duplicate, and ordering never moves the digest.
func TestTodo_POP_009_Property(t *testing.T) {
	pool := []string{"a", "b", "c", "d", "e"}
	for trial := 0; trial < 64; trial++ {
		var expected, observed []string
		for i, id := range pool {
			if (trial>>(i))&1 == 1 {
				expected = append(expected, id)
			}
			if (trial>>(i+2))&1 == 1 {
				observed = append(observed, id)
			}
		}
		if len(expected) == 0 {
			expected = []string{"a"}
		}
		rep, err := ReconcileCompleteness(ExpectedRoster{DefinitionID: "d", Expected: expected}, pop009Snapshot("d", observed), values.Instant{})
		if err != nil {
			t.Fatalf("trial %d: %v", trial, err)
		}
		if len(rep.Matched)+len(rep.Missing) != len(expected) {
			t.Fatalf("trial %d: expected %d split into %d matched + %d missing", trial, len(expected), len(rep.Matched), len(rep.Missing))
		}
		if len(rep.Matched)+len(rep.Unexpected)+len(rep.Duplicates) != len(observed) {
			t.Fatalf("trial %d: observed %d split into %d+%d+%d", trial, len(observed), len(rep.Matched), len(rep.Unexpected), len(rep.Duplicates))
		}
		// Roster reorder is the same expectation.
		rev := append([]string(nil), expected...)
		for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
			rev[i], rev[j] = rev[j], rev[i]
		}
		again, err := ReconcileCompleteness(ExpectedRoster{DefinitionID: "d", Expected: rev}, pop009Snapshot("d", observed), values.Instant{})
		if err != nil || again.Digest != rep.Digest {
			t.Fatalf("trial %d: reorder moved the digest", trial)
		}
	}
	// Exact agreement completes with no obligations.
	full, err := ReconcileCompleteness(
		ExpectedRoster{DefinitionID: "d", Expected: []string{"a", "b"}},
		pop009Snapshot("d", []string{"a", "b"}), values.Instant{})
	if err != nil || !full.Complete || len(full.Obligations) != 0 || full.Digest == "" {
		t.Fatalf("exact agreement: %+v %v", full, err)
	}
}

// TestTodo_POP_009_Golden pins the completeness digest oracle.
func TestTodo_POP_009_Golden(t *testing.T) {
	rep, err := ReconcileCompleteness(
		ExpectedRoster{
			DefinitionID: "pop-promotion-eligible-v1",
			Expected:     []string{"worker:A", "worker:B", "worker:C"},
			Authorized:   []string{"worker:A", "worker:B", "worker:C", "worker:D"},
		},
		pop009Snapshot("pop-promotion-eligible-v1", []string{"worker:A", "worker:B", "worker:B", "worker:Z"}),
		values.Instant{})
	if err != nil {
		t.Fatalf("ReconcileCompleteness: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "completeness.golden.txt"))
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
	if rep.Digest != want {
		t.Fatalf("digest mismatch:\n got=%q\nwant=%q", rep.Digest, want)
	}
}

// TestTodo_POP_009_Security: protected membership never leaks through
// reconciliation, and scope confusion fails closed.
func TestTodo_POP_009_Security(t *testing.T) {
	protected := Snapshot{DefinitionID: "d", MembershipProtected: true, Digest: "sha256:sealed"}
	rep, err := ReconcileCompleteness(
		ExpectedRoster{DefinitionID: "d", Expected: []string{"a"}},
		protected, values.Instant{})
	if err != nil {
		t.Fatalf("ReconcileCompleteness: %v", err)
	}
	if rep.Complete || len(rep.Matched)+len(rep.Missing)+len(rep.Unexpected)+len(rep.Duplicates) != 0 {
		t.Fatalf("protected reconciliation disclosed membership: %+v", rep)
	}
	found := false
	for _, o := range rep.Obligations {
		if o.Reason == ObligationFactRedacted {
			found = true
		}
	}
	if !found {
		t.Fatalf("obligations=%+v, want redaction evidence", rep.Obligations)
	}
	// Definition confusion fails closed.
	if _, err := ReconcileCompleteness(
		ExpectedRoster{DefinitionID: "d1", Expected: []string{"a"}},
		pop009Snapshot("d2", []string{"a"}), values.Instant{}); err == nil {
		t.Fatal("cross-definition reconciliation accepted")
	}
	// Without an authorized set the unauthorized distinction stays off.
	rep, err = ReconcileCompleteness(
		ExpectedRoster{DefinitionID: "d", Expected: []string{"a"}},
		pop009Snapshot("d", []string{"a", "z"}), values.Instant{})
	if err != nil {
		t.Fatalf("ReconcileCompleteness: %v", err)
	}
	if len(rep.Unauthorized) != 0 {
		t.Fatalf("unauthorized=%q without an authorized set", rep.Unauthorized)
	}
	if len(rep.Unexpected) != 1 {
		t.Fatalf("unexpected=%q, want the out-of-roster member", rep.Unexpected)
	}
	// Stale snapshots distrust their own absences.
	wm, err := values.NewInstantFromUnix(1000, 0)
	if err != nil {
		t.Fatal(err)
	}
	require, err := values.NewInstantFromUnix(2000, 0)
	if err != nil {
		t.Fatal(err)
	}
	stale := pop009Snapshot("d", []string{"a"})
	stale.Watermarks = map[SubjectKind]values.Instant{"worker": wm}
	rep, err = ReconcileCompleteness(
		ExpectedRoster{DefinitionID: "d", Expected: []string{"a"}}, stale, require)
	if err != nil || !rep.Stale || rep.Complete {
		t.Fatalf("stale=%+v err=%v, want stale and incomplete", rep, err)
	}
}
