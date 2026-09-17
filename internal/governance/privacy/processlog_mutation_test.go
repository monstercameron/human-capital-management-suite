package privacy

import (
	"testing"
)

// TestTodo_PROCESS_001_Mutation proves mutated log writes never land:
// duplicate events and sequences, unknown causation, cross-tenant and
// out-of-scope writes are all refused, and the projection digest is
// unchanged by every refused append.
func TestTodo_PROCESS_001_Mutation(t *testing.T) {
	projector := processProjector(t)
	base := ProcessEvent{
		EventID: "evt-1", TenantID: "tenant-a", WorkflowID: "leave-and-return",
		CaseID: "case-1", CaseSequence: 1, Activity: "request submitted",
		Lifecycle: LifecycleStarted, At: processAt(1), Resource: "worker-1",
	}
	appendCaseEvent(t, projector, base)
	before, err := projector.Project("case-1")
	if err != nil {
		t.Fatal(err)
	}

	mutations := []struct {
		name  string
		event ProcessEvent
	}{
		{"duplicate event", ProcessEvent{
			EventID: "evt-1", TenantID: "tenant-a", WorkflowID: "leave-and-return",
			CaseID: "case-1", CaseSequence: 3, Activity: "replay",
			Lifecycle: LifecycleStarted, At: processAt(3), Resource: "worker-1",
		}},
		{"duplicate sequence", ProcessEvent{
			EventID: "evt-9", TenantID: "tenant-a", WorkflowID: "leave-and-return",
			CaseID: "case-1", CaseSequence: 1, Activity: "fork",
			Lifecycle: LifecycleStarted, At: processAt(3), Resource: "worker-1",
		}},
		{"unknown causation", ProcessEvent{
			EventID: "evt-10", TenantID: "tenant-a", WorkflowID: "leave-and-return",
			CaseID: "case-1", CaseSequence: 3, Activity: "orphan",
			Lifecycle: LifecycleStarted, At: processAt(3), Resource: "worker-1", CausedBy: "evt-ghost",
		}},
		{"cross tenant", ProcessEvent{
			EventID: "evt-11", TenantID: "tenant-b", WorkflowID: "leave-and-return",
			CaseID: "case-1", CaseSequence: 3, Activity: "leak",
			Lifecycle: LifecycleStarted, At: processAt(3), Resource: "worker-1",
		}},
		{"out of scope workflow", ProcessEvent{
			EventID: "evt-12", TenantID: "tenant-a", WorkflowID: "termination",
			CaseID: "case-1", CaseSequence: 3, Activity: "drift",
			Lifecycle: LifecycleStarted, At: processAt(3), Resource: "worker-1",
		}},
	}
	for _, mutation := range mutations {
		if err := projector.Append(mutation.event); err == nil {
			t.Fatalf("%s appended", mutation.name)
		}
	}
	after, err := projector.Project("case-1")
	if err != nil {
		t.Fatal(err)
	}
	if after.CanonicalDigest != before.CanonicalDigest || len(after.Events) != len(before.Events) {
		t.Fatal("refused appends changed the projection")
	}
}
