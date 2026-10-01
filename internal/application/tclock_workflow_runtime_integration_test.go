package application

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/application/timeclockstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/signals"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/buildinfo"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/clockpunch"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
	"github.com/pressly/goose/v3"
)

func TestTodo_WTIME003004_RealDriverStartsClockPunchOnPostgres(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	tenantID := uuid.New()
	tenant := "clock-driver-" + tenantID.String()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-clock','Clock driver','ACTIVE',$3)`, tenantID, tenant, now.Add(-time.Hour))
	plan, err := clockpunch.Compile()
	if err != nil {
		t.Fatal(err)
	}
	versions := version.NewRegistry()
	if err := versions.Put(version.CompiledVersion{WorkflowID: plan.WorkflowID, DefinitionVersion: plan.Version, SemanticVersion: "1.0.0", CompiledPlanDigest: plan.Digest(), Status: version.StatusActive}); err != nil {
		t.Fatal(err)
	}
	resolver := clockDriverResolver{plan: plan}
	telemetryAdapter := clockDriverTelemetry(t)
	timeDB := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(timestore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, timeDB.SQL, migrationFS, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	timeStore, err := timestore.New(ctx, timestore.Config{DSN: timeDB.URL, Schema: timeDB.Schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(timeStore.Close)
	timeDB.Exec(t, `INSERT INTO time_self_clock_projection(tenant_id,worker_ref,assignment_ref) VALUES($1,'worker','assignment')`, tenant)
	work := clockservice.PunchWork{
		Session: clockservice.SessionRecord{ID: "session-driver", TenantID: tenant, WorkerRef: "worker", AssignmentRef: "assignment", Status: "OPEN", Source: "WORKER_SELF", OpenedAt: now, Payload: []byte(`{"state":"OPEN"}`)}, SessionIsNew: true, ExpectedProjectionRevision: 1,
		SessionEvents: []clockservice.SessionEvent{{Kind: "OPENED", ActorRef: "worker", IdempotencyKey: "event-in", Digest: "event-in-digest", Payload: []byte(`{"observation_id":"observation-driver"}`)}},
		Observation:   clockservice.ObservationRecord{ID: "observation-driver", TenantID: tenant, WorkerRef: "worker", AssignmentRef: "assignment", Source: "WORKER_SELF", EventType: "IN", Timezone: "UTC", IdempotencyKey: "punch-in", Digest: "punch-in-digest", OccurredAt: now, ReceivedAt: now, Payload: []byte(`{"kind":"IN"}`)},
	}
	signalAdapter := platformexecution.SignalSubscriptions{Store: signals.Store{}}
	terminalWriter, err := NewClockWorkflowTerminalWriter("hcmnext:test:clock-workflow")
	if err != nil {
		t.Fatal(err)
	}
	factory, err := NewClockWorkflowDriverFactory(ClockWorkflowDriverFactoryOptions{DB: db.Conn, BaseSteps: clockDriverSteps{}, Commit: timeclockstore.Adapter{Store: timeStore}, Telemetry: telemetryAdapter, Terminal: terminalWriter, Guard: idempotency.PostgresStore{}, Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour}, Signals: signalAdapter, SignalReader: signalAdapter, SignalTimeoutReader: signalAdapter, ResolveTenant: func(string) (uuid.UUID, error) { return tenantID, nil }, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	r, err := factory(ctx, tenant, work)
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Execute(ctx, execute.ExecuteRequest{Start: runtime.StartRequest{TenantID: tenantID, CellID: "cell-clock", StartIdempotencyKey: "start-driver", Resolver: resolver, Versions: versions, Source: &runtime.StartSource{Kind: runtime.StartSourceTrigger, IntentType: clockpunch.IntentType, Trigger: &runtime.TriggerStartSource{TriggerID: "start-driver", TriggerType: "clock", Key: work.Session.ID}}, BusinessSubjectRefs: []string{"time_session:" + work.Session.ID}, ExecutionMode: workflow.ModeExecute, CorrelationID: "corr-driver", CreatedAt: now}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Start.InstanceID == uuid.Nil {
		t.Fatal("real driver did not persist an instance")
	}
	var instanceVersion int64
	if err := db.Conn.QueryRow(ctx, `SELECT instance_version FROM workflow_instance WHERE tenant_id=$1 AND instance_id=$2`, tenantID, result.Start.InstanceID).Scan(&instanceVersion); err != nil {
		t.Fatal(err)
	}
	if instanceVersion <= 0 {
		t.Fatalf("instance version=%d, want positive", instanceVersion)
	}
	var count int
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM workflow_node_execution WHERE tenant_id=$1 AND instance_id=$2 AND node_id='commit_punch' AND status='SUCCEEDED'`, tenantID, result.Start.InstanceID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("commit_punch successes=%d, want 1", count)
	}
	var projectionRevision int64
	var projectionStatus string
	if err := timeDB.QueryRow(ctx, `SELECT revision,status_code FROM time_self_clock_projection WHERE tenant_id=$1 AND worker_ref='worker' AND assignment_ref='assignment'`, tenant).Scan(&projectionRevision, &projectionStatus); err != nil {
		t.Fatal(err)
	}
	if projectionRevision != 2 || projectionStatus != "CLOCKED_IN" {
		t.Fatalf("IN projection revision=%d status=%s", projectionRevision, projectionStatus)
	}
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM workflow_signal_subscription WHERE tenant_id=$1 AND instance_id=$2 AND node_id='await_clock_out'`, tenantID, result.Start.InstanceID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("clock-out subscriptions=%d, want 1", count)
	}
	receiver := ClockSignalReceiver{DB: db.Conn, Verify: acceptingSignalVerifier{}, Now: func() time.Time { return now.Add(time.Minute) }}
	signal, err := receiver.Receive(ctx, clockservice.SignalDelivery{TenantID: tenantID, SessionID: work.Session.ID, Signal: "OUT_PUNCH", EventType: "hcmnext.events.time.clock_out", Source: "hcmnext.time.clock", CorrelationKey: "subject:time_session", CorrelationValue: "time_session:" + work.Session.ID, SchemaRef: clockpunch.ClockOutSignalSchemaRef(), IdempotencyKey: "observation-out", Payload: []byte(`{"session_id":"session-driver"}`), ExpectedInstanceID: result.Start.InstanceID, OccurredAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	outWork := work
	outWork.SessionIsNew = false
	committedIn, found, err := (timeclockstore.Adapter{Store: timeStore}).LoadPunchResult(ctx, tenant, work.Observation.ID)
	if err != nil || !found || committedIn.Session.Status != "OPEN" {
		t.Fatalf("persisted IN=%+v found=%v err=%v", committedIn, found, err)
	}
	outWork.ExpectedRevision = committedIn.Session.Revision
	outWork.ExpectedProjectionRevision = 2
	outWork.Session = committedIn.Session
	outWork.Session.Status = "CLOSED"
	outWork.Session.ClosedAt = now.Add(time.Hour)
	outWork.Session.Payload = []byte(`{"state":"CLOSED"}`)
	outWork.SessionEvents = []clockservice.SessionEvent{{Kind: "CLOSED", ActorRef: "worker", IdempotencyKey: "event-out", Digest: "event-out-digest", Payload: []byte(`{"observation_id":"observation-out"}`)}}
	outWork.Observation = clockservice.ObservationRecord{ID: "observation-out", TenantID: tenant, WorkerRef: "worker", AssignmentRef: "assignment", Source: "WORKER_SELF", EventType: "OUT", Timezone: "UTC", IdempotencyKey: "punch-out", Digest: "punch-out-digest", OccurredAt: now.Add(time.Hour), ReceivedAt: now.Add(time.Hour), Payload: []byte(`{"kind":"OUT"}`)}
	outRuntime, err := factory(ctx, tenant, outWork)
	if err != nil {
		t.Fatal(err)
	}
	out, err := outRuntime.ResumeSignal(ctx, execute.ResumeSignalRequest{Start: runtime.StartRequest{TenantID: tenantID, CellID: "cell-clock", StartIdempotencyKey: "start-driver", Resolver: resolver, Versions: versions, Source: &runtime.StartSource{Kind: runtime.StartSourceTrigger, IntentType: clockpunch.IntentType, Trigger: &runtime.TriggerStartSource{TriggerID: "start-driver", TriggerType: "clock", Key: work.Session.ID}}, BusinessSubjectRefs: []string{"time_session:" + work.Session.ID}, ExecutionMode: workflow.ModeExecute, CorrelationID: "corr-driver", CreatedAt: now, PinnedCompiledPlanDigest: plan.Digest()}, InstanceID: result.Start.InstanceID, ExpectedInstanceVersion: instanceVersion, SignalID: signal.SignalID, SubscriptionID: signal.SubscriptionID, RecordedAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	recoveredOut, found, err := (timeclockstore.Adapter{Store: timeStore}).LoadPunchResult(ctx, tenant, outWork.Observation.ID)
	if err != nil || !found || recoveredOut.Session.Status != "CLOSED" || recoveredOut.Observation.ID != "observation-out" {
		t.Fatalf("persisted OUT=%+v found=%v err=%v", recoveredOut, found, err)
	}
	replay, err := (timeclockstore.Adapter{Store: timeStore}).CommitPunch(ctx, tenant, outWork)
	if err != nil || !replay.Duplicate {
		t.Fatalf("OUT replay=%+v err=%v", replay, err)
	}
	if err := timeDB.QueryRow(ctx, `SELECT revision,status_code FROM time_self_clock_projection WHERE tenant_id=$1 AND worker_ref='worker' AND assignment_ref='assignment'`, tenant).Scan(&projectionRevision, &projectionStatus); err != nil {
		t.Fatal(err)
	}
	if projectionRevision != 3 || projectionStatus != "CLOCKED_OUT" {
		t.Fatalf("OUT replay projection revision=%d status=%s", projectionRevision, projectionStatus)
	}
	if out.Status != execute.StatusComplete {
		t.Fatalf("out status=%s, want COMPLETE", out.Status)
	}
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM workflow_node_execution WHERE tenant_id=$1 AND instance_id=$2 AND node_id='commit_clock_out' AND status='SUCCEEDED'`, tenantID, result.Start.InstanceID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("commit_clock_out successes=%d, want 1", count)
	}
}

type clockDriverResolver struct{ plan *workflow.CompiledWorkflow }

func (r clockDriverResolver) ResolveWorkflow(context.Context, runtime.StartRequest) (runtime.WorkflowSelection, error) {
	return runtime.WorkflowSelection{WorkflowID: r.plan.WorkflowID, Pin: version.Pin{CompiledPlanDigest: r.plan.Digest()}, Plan: r.plan}, nil
}

type clockDriverSteps struct{}

func (clockDriverSteps) Run(_ context.Context, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	if req.Node.ID == clockpunch.NodeCommitClockOut {
		return frontier.NodeOutcome{NodeID: req.Node.ID, Outcome: workflow.OutcomeSucceeded}, runtime.GovernanceRefs{}, nil
	}
	if req.Node.Type == workflow.StepEnd {
		return frontier.NodeOutcome{NodeID: req.Node.ID, OutputDigest: clockTerminalDigest(req)}, runtime.GovernanceRefs{}, nil
	}
	return frontier.NodeOutcome{NodeID: req.Node.ID, Await: frontier.AwaitSignal, AwaitRef: "clock_out"}, runtime.GovernanceRefs{}, nil
}

func clockDriverTelemetry(t *testing.T) *clockservice.ClockWorkflowTelemetry {
	t.Helper()
	allow, _ := telemetry.DefaultAllowlist()
	provider, err := hcmotel.NewProvider(context.Background(), hcmotel.Config{Resource: telemetry.NewResourceFromBuild(buildinfo.Info{Revision: "test"}, "clock", "instance", "test", "cell-clock", "us-east-1", telemetry.ProcessRoleAPI, telemetry.TenantClassStandard), Evaluator: telemetry.NewEvaluator(allow, telemetry.DefaultExportPolicy(telemetry.DefaultPolicyVersion), telemetry.DefaultSamplingPolicy()), ShutdownTimeout: time.Second, Trace: hcmotel.TraceConfig{StdoutWriter: io.Discard}, Metric: hcmotel.MetricConfig{StdoutWriter: io.Discard}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	adapter, err := clockservice.NewClockWorkflowTelemetry(provider, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return time.Now().UTC() })
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}
