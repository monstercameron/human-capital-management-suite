package jobarch

import (
	"testing"
	"time"
)

// TestTodo_REV_101_07_Property proves the publication proposal's RequestedAt
// comes from the injected clock: a pinned clock stamps the exact instant,
// and the absent-clock default still stamps a wall time instead of zero.
func TestTodo_REV_101_07_Property(t *testing.T) {
	published, err := NewArchitectureRevision(architectureFixture())
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	arch, err := NewArchitectureRevision(workspaceArchFixture())
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}

	pinned := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	first, err := ProposeWorkspacePublication(workspaceAdmin(), published, arch, "profile-1",
		func() time.Time { return pinned })
	if err != nil {
		t.Fatalf("draft proposal: %v", err)
	}
	if !first.RequestedAt.Equal(pinned) {
		t.Fatalf("RequestedAt = %v, want pinned %v", first.RequestedAt, pinned)
	}

	// Replay: the same pinned clock stamps the same instant again.
	second, err := ProposeWorkspacePublication(workspaceAdmin(), published, arch, "profile-1",
		func() time.Time { return pinned })
	if err != nil {
		t.Fatalf("draft proposal: %v", err)
	}
	if !second.RequestedAt.Equal(first.RequestedAt) {
		t.Fatal("identical clock produced different RequestedAt; the proposal still reads the wall clock")
	}

	// The default path (no clock) stamps a live wall time, never zero.
	def, err := ProposeWorkspacePublication(workspaceAdmin(), published, arch, "profile-1")
	if err != nil {
		t.Fatalf("draft proposal: %v", err)
	}
	if def.RequestedAt.IsZero() || time.Since(def.RequestedAt) > time.Minute {
		t.Fatalf("default RequestedAt = %v, want a current wall time", def.RequestedAt)
	}
}
