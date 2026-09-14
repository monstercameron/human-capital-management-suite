package population

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// POP-009 RED: expected-population completeness before completeness.go.
func TestTodo_POP_009(t *testing.T) {
	roster := ExpectedRoster{
		DefinitionID: "pop-promotion-eligible-v1",
		Expected:     []string{"worker:A", "worker:B", "worker:C"},
		Authorized:   []string{"worker:A", "worker:B", "worker:C", "worker:D"},
	}
	observed := Snapshot{
		DefinitionID: "pop-promotion-eligible-v1",
		SubjectIDs:   []string{"worker:A", "worker:B", "worker:B", "worker:Z"},
		Digest:       "sha256:observed",
	}

	rep, err := ReconcileCompleteness(roster, observed, values.Instant{})
	if err != nil {
		t.Fatalf("ReconcileCompleteness: %v", err)
	}
	// RED: counts alone (3 expected vs 3 observed rows) must not hide the
	// missing member, the duplicate or the unauthorized one.
	if rep.Complete {
		t.Fatal("mismatched population reported complete")
	}
	if !hasSubject(rep.Missing, "worker:C") {
		t.Fatalf("missing=%q, want worker:C", rep.Missing)
	}
	if !hasSubject(rep.Duplicates, "worker:B") {
		t.Fatalf("duplicates=%q, want worker:B", rep.Duplicates)
	}
	if !hasSubject(rep.Unauthorized, "worker:Z") {
		t.Fatalf("unauthorized=%q, want worker:Z", rep.Unauthorized)
	}
	if len(rep.Obligations) == 0 {
		t.Fatal("no repair obligations created")
	}
	// GREEN: partitions recorded, snapshot untouched.
	if rep.ObservedDigest != "sha256:observed" || rep.ExpectedCount != 3 {
		t.Fatalf("report=%+v", rep)
	}
	if len(observed.SubjectIDs) != 4 || observed.Digest != "sha256:observed" {
		t.Fatal("reconciliation mutated the snapshot")
	}
}

func hasSubject(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
