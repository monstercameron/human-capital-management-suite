package clockservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

type executorBindingFake struct {
	row TimeClockRunBinding
	err error
}

func (f executorBindingFake) Save(context.Context, TimeClockRunBinding) error { return nil }
func (f executorBindingFake) Load(context.Context, uuid.UUID, string) (TimeClockRunBinding, error) {
	return f.row, f.err
}

type executorReceiptFake struct {
	result PunchResult
	found  bool
}

func (f executorReceiptFake) LoadPunchResult(context.Context, string, string) (PunchResult, bool, error) {
	return f.result, f.found, nil
}

func TestWorkflowPunchDriver_ValidBindingRequiresCanonicalIdentity(t *testing.T) {
	tenant := uuid.New()
	row := TimeClockRunBinding{TenantID: tenant, TenantKey: "ironridge", SessionID: "s", InstanceID: uuid.New(), WorkflowID: "wf", PlanDigest: "sha256:p", StartKey: "timeclock:session:" + tenant.String() + ":s", CorrelationID: "timeclock:session:" + tenant.String() + ":s", CreatedAt: time.Unix(1, 0)}
	d := WorkflowPunchDriver{Resolver: func(string) (uuid.UUID, error) { return tenant, nil }, Bindings: executorBindingFake{row: row}}
	if _, err := d.validBinding(context.Background(), "ironridge", "s"); err != nil {
		t.Fatal(err)
	}
	row.TenantKey = "other"
	d.Bindings = executorBindingFake{row: row}
	if _, err := d.validBinding(context.Background(), "ironridge", "s"); err == nil {
		t.Fatal("foreign canonical tenant accepted")
	}
}

func TestWorkflowPunchDriver_RejectsIncompleteComposition(t *testing.T) {
	d := WorkflowPunchDriver{}
	if _, err := d.ExecutePunch(context.Background(), "ironridge", PunchWork{Observation: ObservationRecord{ID: "o"}}); err == nil {
		t.Fatal("incomplete composition accepted")
	}
}

type recoveringClockRuntime struct {
	bindings *bindingFake
	evidence *recoveringClockEvidence
	identity runtime.StartReceipt
	calls    int
}

func (r *recoveringClockRuntime) Execute(context.Context, execute.ExecuteRequest) (execute.Result, error) {
	r.calls++
	r.evidence.found = true
	return execute.Result{Start: r.identity}, nil
}

func (r *recoveringClockRuntime) ResumeSignal(context.Context, execute.ResumeSignalRequest) (execute.Result, error) {
	return execute.Result{}, errors.New("unexpected signal")
}

type recoveringClockEvidence struct {
	row   PunchNodeEvidence
	found bool
}

func (e *recoveringClockEvidence) LoadPunchNodeEvidence(context.Context, uuid.UUID, uuid.UUID, string) (PunchNodeEvidence, bool, error) {
	return e.row, e.found, nil
}

func TestTodo_WTIME004_CommittedEffectRecoversMissingWorkflowTracking(t *testing.T) {
	for _, existingBinding := range []bool{false, true} {
		t.Run(map[bool]string{false: "lost binding", true: "lost advancement"}[existingBinding], func(t *testing.T) {
			tenant, instance := uuid.New(), uuid.New()
			key := startKey(tenant, "session")
			binding := TimeClockRunBinding{TenantID: tenant, TenantKey: "tenant", InstanceID: instance, SessionID: "session", WorkflowID: "workflow", PlanDigest: "plan", StartKey: key, CorrelationID: key}
			bindings := &bindingFake{}
			if existingBinding {
				bindings.row = binding
			}
			evidence := &recoveringClockEvidence{row: PunchNodeEvidence{TenantID: tenant, InstanceID: instance, NodeID: "commit_punch", ObservationID: "observation", SessionID: "session", PlanDigest: "plan", TraceID: "trace", OutputDigest: PunchCommitEvidenceDigest("observation", "session"), CompletedState: "SUCCEEDED", Attempt: 1, InstanceVersion: 4}}
			work := PunchWork{SessionIsNew: true, Session: SessionRecord{TenantID: "tenant", ID: "session", WorkerRef: "worker", AssignmentRef: "assignment"}, Observation: ObservationRecord{TenantID: "tenant", ID: "observation", Digest: "original", OccurredAt: time.Unix(10, 0)}}
			telemetry, _, _ := newClockWorkflowTelemetryFixture(t)
			r := &recoveringClockRuntime{bindings: bindings, evidence: evidence, identity: runtime.StartReceipt{InstanceID: instance, WorkflowID: "workflow", CompiledPlanDigest: "plan", CreatedAt: time.Unix(20, 0)}}
			driver := WorkflowPunchDriver{Factory: func(context.Context, string, PunchWork) (WorkflowRuntime, error) { return r, nil }, Adapter: &TimeClockRuntimeAdapter{Resolver: resolverFake{}, Versions: version.NewRegistry(), Signals: signalFake{}, Source: "clock", SchemaRef: "time/v1", Clock: func() time.Time { return time.Unix(30, 0) }}, Resolver: func(string) (uuid.UUID, error) { return tenant, nil }, Bindings: bindings, Receipts: executorReceiptFake{found: true, result: PunchResult{Session: work.Session, Observation: work.Observation, Duplicate: true}}, Evidence: evidence, Telemetry: telemetry}
			got, err := driver.ExecutePunch(context.Background(), "tenant", work)
			if err != nil || !got.Committed || r.calls != 1 || bindings.row.CreatedAt != r.identity.CreatedAt {
				t.Fatalf("recovery got=%+v err=%v calls=%d binding=%+v", got, err, r.calls, bindings.row)
			}
			if _, err := driver.ExecutePunch(context.Background(), "tenant", work); err != nil || r.calls != 1 {
				t.Fatalf("proven replay restarted workflow: err=%v calls=%d", err, r.calls)
			}
			work.Observation.Digest = "changed"
			if _, err := driver.ExecutePunch(context.Background(), "tenant", work); err == nil || r.calls != 1 {
				t.Fatal("changed committed effect replay was accepted")
			}
		})
	}
}
