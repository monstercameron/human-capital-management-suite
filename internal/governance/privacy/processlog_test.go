package privacy

import (
	"strings"
	"testing"
	"time"
)

func processAt(seq int) time.Time {
	return time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC).Add(time.Duration(seq) * time.Minute)
}

func processProjector(t *testing.T) *ProcessProjector {
	t.Helper()
	projector, err := NewProcessProjector(ProcessScope{
		TenantID:         "tenant-a",
		Purpose:          "WORKFORCE_ANALYTICS",
		AllowedWorkflows: []string{"leave-and-return"},
		RedactResources:  true,
		RedactionSalt:    "salt:process-001",
	})
	if err != nil {
		t.Fatal(err)
	}
	return projector
}

func appendCaseEvent(t *testing.T, projector *ProcessProjector, event ProcessEvent) {
	t.Helper()
	if err := projector.Append(event); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_PROCESS_001 is PROCESS-001: the projection emits canonical
// case/activity/lifecycle/timestamp/resource tuples with scope, redaction,
// watermark and completeness state — and never mixes tenants or workflows,
// exposes payload, loses causation or effective time, or mistakes missing
// telemetry for completed activity.
func TestTodo_PROCESS_001(t *testing.T) {
	projector := processProjector(t)
	appendCaseEvent(t, projector, ProcessEvent{
		EventID: "evt-1", TenantID: "tenant-a", WorkflowID: "leave-and-return",
		CaseID: "case-1", CaseSequence: 1, Activity: "request submitted",
		Lifecycle: LifecycleStarted, At: processAt(1), Resource: "worker-1",
		Payload: map[string]string{"diagnosis": "secret-condition"},
	})
	appendCaseEvent(t, projector, ProcessEvent{
		EventID: "evt-2", TenantID: "tenant-a", WorkflowID: "leave-and-return",
		CaseID: "case-1", CaseSequence: 2, Activity: "evidence reviewed",
		Lifecycle: LifecycleCompleted, At: processAt(2), Resource: "admin-9",
		CausedBy: "evt-1",
	})
	projection, err := projector.Project("case-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Events) != 2 {
		t.Fatalf("events = %d", len(projection.Events))
	}
	first := projection.Events[0]
	if first.CaseID != "case-1" || first.Activity != "request submitted" ||
		first.Lifecycle != LifecycleStarted || first.Timestamp == "" {
		t.Fatalf("first event is not canonical: %+v", first)
	}
	if first.ScopeTenant != "tenant-a" || first.ScopePurpose != "WORKFORCE_ANALYTICS" {
		t.Fatalf("event scope = %q/%q", first.ScopeTenant, first.ScopePurpose)
	}
	if !first.Redacted || first.Resource == "worker-1" {
		t.Fatalf("resource escaped redaction: %+v", first)
	}
	if projection.Completeness != CompletenessComplete {
		t.Fatalf("completeness = %q", projection.Completeness)
	}
	if projection.Watermark == "" {
		t.Fatal("projection carries no watermark")
	}
	second := projection.Events[1]
	if second.CausedBy != "evt-1" {
		t.Fatalf("causation lost: %+v", second)
	}
	// Seeded defect: payload values never reach the projection, not even
	// through digests of convenience.
	flat := ""
	for _, event := range projection.Events {
		flat += event.CaseID + "\x00" + event.Activity + "\x00" + event.Resource + "\x00"
	}
	if strings.Contains(flat, "secret-condition") {
		t.Fatal("payload value exposed in projection")
	}
	if !first.PayloadPresent || first.PayloadDigest == "" {
		t.Fatal("payload presence is unaccounted")
	}

	// Missing telemetry is PARTIAL, never COMPLETE.
	appendCaseEvent(t, projector, ProcessEvent{
		EventID: "evt-9", TenantID: "tenant-a", WorkflowID: "leave-and-return",
		CaseID: "case-2", CaseSequence: 1, Activity: "request submitted",
		Lifecycle: LifecycleStarted, At: processAt(1), Resource: "worker-2",
	})
	appendCaseEvent(t, projector, ProcessEvent{
		EventID: "evt-10", TenantID: "tenant-a", WorkflowID: "leave-and-return",
		CaseID: "case-2", CaseSequence: 3, Activity: "returned to work",
		Lifecycle: LifecycleCompleted, At: processAt(3), Resource: "worker-2",
	})
	gapped, err := projector.Project("case-2")
	if err != nil {
		t.Fatal(err)
	}
	if gapped.Completeness != CompletenessPartial {
		t.Fatalf("gapped case completeness = %q", gapped.Completeness)
	}
	if len(gapped.MissingSequences) != 1 || gapped.MissingSequences[0] != 2 {
		t.Fatalf("missing sequences = %v", gapped.MissingSequences)
	}

	// RED negatives: tenant mixing, workflow mixing, unknown causation,
	// missing effective time and bad lifecycle never append.
	bad := ProcessEvent{
		EventID: "bad", TenantID: "tenant-a", WorkflowID: "leave-and-return",
		CaseID: "case-1", CaseSequence: 3, Activity: "x",
		Lifecycle: LifecycleObserved, At: processAt(3), Resource: "worker-1",
	}
	bad.TenantID = "tenant-b"
	if err := projector.Append(bad); err == nil {
		t.Fatal("cross-tenant event was appended")
	}
	bad.TenantID = "tenant-a"
	bad.WorkflowID = "payroll-run"
	if err := projector.Append(bad); err == nil {
		t.Fatal("out-of-scope workflow event was appended")
	}
	bad.WorkflowID = "leave-and-return"
	bad.CausedBy = "evt-missing"
	if err := projector.Append(bad); err == nil {
		t.Fatal("event with unknown causation was appended")
	}
	bad.CausedBy = ""
	bad.At = time.Time{}
	if err := projector.Append(bad); err == nil {
		t.Fatal("event without effective time was appended")
	}
}
