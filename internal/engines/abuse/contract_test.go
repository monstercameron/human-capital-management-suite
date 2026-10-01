package abuse_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/abuse"
)

// TestTodo_ABUSE_001_Golden pins the package's ARCH-GO-009 engine contract
// shape: a stable Version() and a closed, non-empty signal-kind vocabulary
// whose contents are the ones this todo committed to (adding a kind is
// fine; silently renaming/removing one without updating this pin is not).
func TestTodo_ABUSE_001_Golden(t *testing.T) {
	if got := abuse.Version(); got != 1 {
		t.Fatalf("abuse.Version() = %d, want 1", got)
	}

	want := []abuse.SignalKind{
		abuse.SignalKindAccessGrant,
		abuse.SignalKindAuthAnomaly,
		abuse.SignalKindBulkExport,
		abuse.SignalKindPrivilegedChange,
		abuse.SignalKindSensitiveRead,
	}
	got := abuse.SignalKinds()
	if len(got) != len(want) {
		t.Fatalf("SignalKinds() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SignalKinds()[%d] = %q, want %q (full: got=%v want=%v)", i, got[i], want[i], got, want)
		}
	}
}

func TestTodo_ABUSE_005_Served(t *testing.T) {
	if err := abuse.ValidateServingContract(); err != nil {
		t.Fatalf("abuse serving contract: %v", err)
	}
}
