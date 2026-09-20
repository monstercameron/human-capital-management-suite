package app

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestPinnedStartBindsTheInstancesOwnPlan proves a continuation's start
// carries the compiled plan its instance pinned, so a resolver serving more
// than one version continues the instance on its own version, and that the
// caller's start is otherwise unchanged.
func TestPinnedStartBindsTheInstancesOwnPlan(t *testing.T) {
	start := runtime.StartRequest{CellID: "cell-local", CorrelationID: "corr-1"}
	pinned := pinnedStart(start, runtime.Instance{CompiledPlanHash: "sha256:frozen"})
	if pinned.PinnedCompiledPlanDigest != "sha256:frozen" {
		t.Fatalf("pinned digest = %q, want the instance's plan", pinned.PinnedCompiledPlanDigest)
	}
	if pinned.CellID != start.CellID || pinned.CorrelationID != start.CorrelationID {
		t.Fatalf("pinnedStart changed the start identity: %+v", pinned)
	}
	if start.PinnedCompiledPlanDigest != "" {
		t.Fatal("pinnedStart mutated the caller's start")
	}
}
