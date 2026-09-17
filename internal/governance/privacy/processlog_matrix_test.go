package privacy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func goldenCase(t *testing.T, projector *ProcessProjector) CaseProjection {
	t.Helper()
	for i, step := range []struct {
		id        string
		sequence  uint64
		activity  string
		lifecycle EventLifecycle
		resource  string
		causedBy  string
	}{
		{"evt-g-1", 1, "request submitted", LifecycleStarted, "worker-1", ""},
		{"evt-g-2", 2, "evidence reviewed", LifecycleCompleted, "admin-9", "evt-g-1"},
	} {
		if err := projector.Append(ProcessEvent{
			EventID: step.id, TenantID: "tenant-a", WorkflowID: "leave-and-return",
			CaseID: "case-golden", CaseSequence: step.sequence, Activity: step.activity,
			Lifecycle: step.lifecycle, At: processAt(i + 1), Resource: step.resource,
			CausedBy: step.causedBy,
		}); err != nil {
			t.Fatal(err)
		}
	}
	projection, err := projector.Project("case-golden")
	if err != nil {
		t.Fatal(err)
	}
	return projection
}

// TestTodo_PROCESS_001_Golden: the canonical digest of the fixed
// projection is pinned. Drift fails here until re-vetted.
func TestTodo_PROCESS_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "processlog.golden"))
	if err != nil {
		t.Fatal(err)
	}
	projection := goldenCase(t, processProjector(t))
	if projection.Completeness != CompletenessComplete || len(projection.Events) != 2 {
		t.Fatalf("golden projection = %+v", projection)
	}
	if !strings.Contains(string(raw), "digest: "+projection.CanonicalDigest) {
		t.Fatalf("projection digest %q is not the vetted golden", projection.CanonicalDigest)
	}
}

// TestTodo_PROCESS_001_Race: concurrent appends and projections share no
// torn state, and replay in any order rebuilds the identical projection.
func TestTodo_PROCESS_001_Race(t *testing.T) {
	projector := processProjector(t)
	var wg sync.WaitGroup
	for c := 0; c < 4; c++ {
		wg.Add(1)
		go func(c int) {
			defer wg.Done()
			caseID := fmt.Sprintf("case-race-%d", c)
			for seq := uint64(1); seq <= 3; seq++ {
				event := ProcessEvent{
					EventID:  fmt.Sprintf("evt-%s-%d", caseID, seq),
					TenantID: "tenant-a", WorkflowID: "leave-and-return",
					CaseID: caseID, CaseSequence: seq, Activity: "step",
					Lifecycle: LifecycleObserved, At: processAt(int(seq)), Resource: "worker-1",
				}
				if seq == 3 {
					event.Lifecycle = LifecycleCompleted
				}
				if err := projector.Append(event); err != nil {
					t.Error(err)
					return
				}
			}
			projection, err := projector.Project(caseID)
			if err != nil {
				t.Error(err)
				return
			}
			if projection.Completeness != CompletenessComplete || len(projection.Events) != 3 {
				t.Errorf("case %s = %+v", caseID, projection)
			}
		}(c)
	}
	wg.Wait()
	// Rebuildable: a second projector fed in reverse order reaches the
	// same canonical digest for every case.
	replay := processProjector(t)
	for c := 0; c < 4; c++ {
		caseID := fmt.Sprintf("case-race-%d", c)
		for seq := uint64(3); seq >= 1; seq-- {
			event := ProcessEvent{
				EventID:  fmt.Sprintf("evt-%s-%d", caseID, seq),
				TenantID: "tenant-a", WorkflowID: "leave-and-return",
				CaseID: caseID, CaseSequence: seq, Activity: "step",
				Lifecycle: LifecycleObserved, At: processAt(int(seq)), Resource: "worker-1",
			}
			if seq == 3 {
				event.Lifecycle = LifecycleCompleted
			}
			if err := replay.Append(event); err != nil {
				t.Fatal(err)
			}
			if seq == 1 {
				break
			}
		}
		want, err := projector.Project(caseID)
		if err != nil {
			t.Fatal(err)
		}
		got, err := replay.Project(caseID)
		if err != nil {
			t.Fatal(err)
		}
		if got.CanonicalDigest != want.CanonicalDigest {
			t.Fatalf("replay of %s diverged", caseID)
		}
	}
}

// TestTodo_PROCESS_001_ScopeRejects: unscoped projectors never open and
// malformed events never append, so there is no unbound log to leak
// through.
func TestTodo_PROCESS_001_ScopeRejects(t *testing.T) {
	for _, scope := range []ProcessScope{
		{},
		{TenantID: "tenant-a"},
		{TenantID: "tenant-a", Purpose: "WORKFORCE_ANALYTICS"},
		{
			TenantID: "tenant-a", Purpose: "WORKFORCE_ANALYTICS",
			AllowedWorkflows: []string{"leave-and-return"}, RedactResources: true,
		},
	} {
		if _, err := NewProcessProjector(scope); err == nil {
			t.Fatalf("scope %+v opened a projector", scope)
		}
	}
	projector := processProjector(t)
	bad := ProcessEvent{
		EventID: "bad", TenantID: "tenant-a", WorkflowID: "leave-and-return",
		CaseID: "case-1", CaseSequence: 1, Activity: "x",
		Lifecycle: "GUESS", At: processAt(1), Resource: "worker-1",
	}
	if err := projector.Append(bad); err == nil {
		t.Fatal("undeclared lifecycle was appended")
	}
	bad.Lifecycle = LifecycleObserved
	bad.CaseSequence = 0
	if err := projector.Append(bad); err == nil {
		t.Fatal("zero sequence was appended")
	}
	bad.CaseSequence = 1
	bad.Resource = ""
	if err := projector.Append(bad); err == nil {
		t.Fatal("resourceless event was appended")
	}
}

// TestTodo_PROCESS_001_Security: no rendering of a projection carries raw
// payload values or unredacted resources, and scope binds every event.
func TestTodo_PROCESS_001_Security(t *testing.T) {
	projector := processProjector(t)
	secret := "secret-condition-9"
	if err := projector.Append(ProcessEvent{
		EventID: "evt-s-1", TenantID: "tenant-a", WorkflowID: "leave-and-return",
		CaseID: "case-s", CaseSequence: 1, Activity: "request submitted",
		Lifecycle: LifecycleStarted, At: processAt(1), Resource: "worker-1",
		Payload: map[string]string{"diagnosis": secret, "note": "follow up " + secret},
	}); err != nil {
		t.Fatal(err)
	}
	projection, err := projector.Project("case-s")
	if err != nil {
		t.Fatal(err)
	}
	flat := fmt.Sprintf("%+v", projection)
	if strings.Contains(flat, secret) || strings.Contains(flat, "worker-1") {
		t.Fatal("projection rendering exposes protected values")
	}
	for _, event := range projection.Events {
		if !event.Redacted || event.ScopeTenant != "tenant-a" ||
			event.ScopePurpose != "WORKFORCE_ANALYTICS" || event.WorkflowID != "leave-and-return" {
			t.Fatalf("event lost its scope bindings: %+v", event)
		}
	}
	// Unknown cases project UNKNOWN, never an empty COMPLETE.
	empty, err := projector.Project("case-missing")
	if err != nil {
		t.Fatal(err)
	}
	if empty.Completeness != CompletenessUnknown || len(empty.Events) != 0 {
		t.Fatalf("missing case = %+v", empty)
	}
}
