package clockservice

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/clockpunch"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

type runtimeFake struct {
	executes, resumes int
	request           runtime.StartRequest
	resumeRequest     execute.ResumeSignalRequest
}

func (f *runtimeFake) Execute(_ context.Context, req execute.ExecuteRequest) (execute.Result, error) {
	f.executes++
	f.request = req.Start
	id := uuid.New()
	return execute.Result{Start: runtime.StartReceipt{InstanceID: id, WorkflowID: "wf.time", CompiledPlanDigest: "sha256:plan"}}, nil
}
func (f *runtimeFake) ResumeSignal(_ context.Context, req execute.ResumeSignalRequest) (execute.Result, error) {
	f.resumes++
	f.resumeRequest = req
	return execute.Result{}, nil
}

type runtimeVersionFake int64

func (v runtimeVersionFake) LoadWorkflowInstanceVersion(context.Context, uuid.UUID, uuid.UUID) (int64, error) {
	return int64(v), nil
}

type bindingFake struct{ row TimeClockRunBinding }

func (f *bindingFake) Save(_ context.Context, b TimeClockRunBinding) error { f.row = b; return nil }
func (f *bindingFake) Load(context.Context, uuid.UUID, string) (TimeClockRunBinding, error) {
	return f.row, nil
}

type signalFake struct{ receipt SignalReceipt }

func (f signalFake) Receive(context.Context, SignalDelivery) (SignalReceipt, error) {
	return f.receipt, nil
}

type capturedClockSignal struct {
	delivery SignalDelivery
	calls    int
}

func (f *capturedClockSignal) Receive(_ context.Context, delivery SignalDelivery) (SignalReceipt, error) {
	f.delivery = delivery
	f.calls++
	return SignalReceipt{SignalID: uuid.New(), SubscriptionID: uuid.New()}, nil
}

func TestTodo_TCLOCK_WORKFLOW_ManualClockOutMatchesPublishedSignal(t *testing.T) {
	tenant, instance := uuid.New(), uuid.New()
	bindings := &bindingFake{row: TimeClockRunBinding{TenantID: tenant, SessionID: "session", InstanceID: instance,
		WorkflowID: clockpunch.WorkflowID, PlanDigest: "sha256:plan", StartKey: startKey(tenant, "session")}}
	signals := &capturedClockSignal{}
	driver := &runtimeFake{}
	adapter := TimeClockRuntimeAdapter{Runtime: driver, Resolver: resolverFake{}, Versions: version.NewRegistry(),
		Bindings: bindings, Signals: signals, VersionsReader: runtimeVersionFake(7), SchemaRef: "legacy/time", Source: "hcmnext.time.clock",
		ResolveTenant: func(string) (uuid.UUID, error) { return tenant, nil }, Clock: func() time.Time { return time.Unix(100, 0) }}
	if err := adapter.SignalRun(context.Background(), "tenant", "session", "OUT_PUNCH", "observation", time.Unix(90, 0)); err != nil {
		t.Fatal(err)
	}
	if signals.delivery.EventType != "hcmnext.events.time.clock_out" || signals.delivery.SchemaRef != clockpunch.ClockOutSignalSchemaRef() || signals.delivery.CorrelationValue != "time_session:session" || signals.delivery.ExpectedInstanceID != instance || driver.resumes != 1 || driver.resumeRequest.ExpectedInstanceVersion != 7 {
		t.Fatalf("clock-out delivery did not match published subscription: %+v, resumes=%d", signals.delivery, driver.resumes)
	}
	if err := adapter.SignalRun(context.Background(), "tenant", "session", "BREAK_START", "other", time.Unix(91, 0)); err == nil || signals.calls != 1 || driver.resumes != 1 {
		t.Fatalf("unsupported event reached clock-out subscription: err=%v calls=%d resumes=%d", err, signals.calls, driver.resumes)
	}
}

type resolverFake struct{}

func (resolverFake) ResolveWorkflow(context.Context, runtime.StartRequest) (runtime.WorkflowSelection, error) {
	return runtime.WorkflowSelection{}, nil
}

type versionsFake struct{}

func (versionsFake) Get(context.Context, string, interface{}) (interface{}, error) { return nil, nil }

func TestTimeClockRuntimeAdapter_StartBuildsTriggerRequest(t *testing.T) {
	tenant := uuid.New()
	r := &runtimeFake{}
	b := &bindingFake{}
	a := TimeClockRuntimeAdapter{Runtime: r, Resolver: resolverFake{}, Versions: nil, Bindings: b, Signals: signalFake{}}
	if err := a.StartRun(context.Background(), tenant.String(), "session-1", "worker-1", "assignment-1", "obs-1", time.Unix(10, 0)); err == nil {
		t.Fatal("nil version store unexpectedly accepted")
	}
}

func TestStartKeyStable(t *testing.T) {
	tenant := uuid.New()
	want := "timeclock:session:" + tenant.String() + ":s"
	if got := startKey(tenant, "s"); got != want {
		t.Fatalf("start key=%q want %q", got, want)
	}
	if startKey(tenant, "s") == startKey(uuid.New(), "s") || startKey(tenant, "s") == startKey(tenant, "other") {
		t.Fatal("start key aliases distinct session identities")
	}
}

func TestTimeClockRuntimeAdapter_StartAndSignalUseDurableIdentity(t *testing.T) {
	tenant := uuid.New()
	instance := uuid.New()
	r := &runtimeFake{}
	b := &bindingFake{}
	s := signalFake{receipt: SignalReceipt{SignalID: uuid.New(), SubscriptionID: uuid.New()}}
	a := TimeClockRuntimeAdapter{Runtime: r, Resolver: resolverFake{}, Versions: version.NewRegistry(), Bindings: b, Signals: s, VersionsReader: runtimeVersionFake(4),
		ResolveTenant: func(string) (uuid.UUID, error) { return tenant, nil }, Clock: func() time.Time { return time.Unix(100, 0) }, SchemaRef: "time/v1", Source: "clock"}
	if err := a.StartRun(context.Background(), "ironridge", "session-1", "worker-1", "assignment-1", "obs-1", time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	if r.executes != 1 || b.row.InstanceID == uuid.Nil || b.row.PlanDigest == "" || r.request.CreatedAt != time.Unix(100, 0).UTC() {
		t.Fatalf("start runtime=%d binding=%+v request=%+v", r.executes, b.row, r.request)
	}
	b.row.InstanceID = instance
	if err := a.SignalRun(context.Background(), "ironridge", "session-1", "BREAK_START", "obs-2", time.Unix(20, 0)); err != nil {
		t.Fatal(err)
	}
	if r.resumes != 1 {
		t.Fatalf("resume calls=%d", r.resumes)
	}
}

func TestTimeClockRuntimeAdapter_RejectsIncompleteStart(t *testing.T) {
	tenant := uuid.New()
	r := &runtimeFake{}
	r.executes = 0
	r.request = runtime.StartRequest{}
	a := TimeClockRuntimeAdapter{Runtime: incompleteRuntimeFake{}, Resolver: resolverFake{}, Versions: version.NewRegistry(), Bindings: &bindingFake{}, Signals: signalFake{},
		ResolveTenant: func(string) (uuid.UUID, error) { return tenant, nil }, Clock: func() time.Time { return time.Unix(100, 0) }, SchemaRef: "time/v1", Source: "clock"}
	if err := a.StartRun(context.Background(), "tenant", "s", "w", "a", "o", time.Unix(1, 0)); err == nil {
		t.Fatal("incomplete runtime identity accepted")
	}
}

type incompleteRuntimeFake struct{}

func (incompleteRuntimeFake) Execute(context.Context, execute.ExecuteRequest) (execute.Result, error) {
	return execute.Result{Start: runtime.StartReceipt{}}, nil
}
func (incompleteRuntimeFake) ResumeSignal(context.Context, execute.ResumeSignalRequest) (execute.Result, error) {
	return execute.Result{}, nil
}
