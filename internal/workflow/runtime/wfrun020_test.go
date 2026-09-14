package runtime

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// WF-RUN-020 RED: names the stuck-detector contract before stuck.go exists.
func TestTodo_WF_RUN_020(t *testing.T) {
	tenant := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	inst := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	detector, err := NewStuckDetector([]StuckExpectation{
		{State: NodeWaiting, MaxIdle: 24 * time.Hour, Kinds: StuckKindsAll()},
		{State: NodeRunning, MaxIdle: time.Hour, Kinds: StuckKindsAll()},
		{State: NodeRetrying, MaxIdle: 30 * time.Minute, Kinds: StuckKindsAll()},
	}, 2*time.Hour)
	if err != nil {
		t.Fatalf("NewStuckDetector: %v", err)
	}

	// RED: a long legal wait is not flagged by age alone. This WAITING node
	// has been idle for 20 hours against a 24-hour budget with its timer
	// still in the future: no missed expectation, no finding.
	legal := []NodeObservation{{
		Tenant: tenant, InstanceID: inst, NodeID: "wait-approval",
		State:          NodeWaiting,
		LastProgressAt: at.Add(-20 * time.Hour),
		NextTimerAt:    ptrTime(at.Add(4 * time.Hour)),
	}}
	findings, err := detector.Detect(at, tenant, legal)
	if err != nil {
		t.Fatalf("Detect(legal wait): %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("legal wait flagged by age alone: %+v", findings[0])
	}

	// RED: a poison node with a missed retry must not stay invisible.
	poison := []NodeObservation{{
		Tenant: tenant, InstanceID: inst, NodeID: "sync-vendor",
		State:          NodeRetrying,
		LastProgressAt: at.Add(-2 * time.Hour),
		NextRetryAt:    ptrTime(at.Add(-time.Hour)),
		Attempts:       7,
	}}
	findings, err = detector.Detect(at, tenant, poison)
	if err != nil {
		t.Fatalf("Detect(poison): %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("poison node invisible: got %d findings", len(findings))
	}
	got := findings[0]
	if got.IncidentKey == "" {
		t.Fatal("finding carries no incident key for open/link dedupe")
	}
	if !hasStuckKind(got.Missed, StuckKindRetry) {
		t.Fatalf("finding misses the retry kind: %+v", got)
	}
	// GREEN: detector is read-only; inputs come back unmodified.
	if poison[0].State != NodeRetrying || poison[0].Attempts != 7 {
		t.Fatal("detector mutated its business input")
	}
}

func ptrTime(v time.Time) *time.Time { return &v }

func hasStuckKind(kinds []StuckKind, want StuckKind) bool {
	for _, k := range kinds {
		if k == want {
			return true
		}
	}
	return false
}
