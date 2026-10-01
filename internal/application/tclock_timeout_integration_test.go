package application

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/application/timeclockstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/signals"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/clockpunch"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
	"github.com/pressly/goose/v3"
)

func TestTodo_TCLOCK_TimeoutOnPostgresReachesBlockedTerminalOnce(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	tenantID := uuid.New()
	tenant := "clock-timeout-" + tenantID.String()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-clock','Clock timeout','ACTIVE',$3)`, tenantID, tenant, now.Add(-time.Hour))
	plan, err := clockpunch.Compile()
	if err != nil {
		t.Fatal(err)
	}
	versions := version.NewRegistry()
	if err := versions.Put(version.CompiledVersion{WorkflowID: plan.WorkflowID, DefinitionVersion: plan.Version, SemanticVersion: "1.0.0", CompiledPlanDigest: plan.Digest(), Status: version.StatusActive}); err != nil {
		t.Fatal(err)
	}
	timeStore, timeStoreDB := timeoutTimeStore(t, ctx)
	timeStoreDB.Exec(t, `INSERT INTO time_self_clock_projection(tenant_id,worker_ref,assignment_ref) VALUES($1,'worker','assignment')`, tenant)
	work := clockservice.PunchWork{Session: clockservice.SessionRecord{ID: "session-timeout", TenantID: tenant, WorkerRef: "worker", AssignmentRef: "assignment", Status: "OPEN", Source: "WORKER_SELF", OpenedAt: now, Payload: []byte(`{"state":"OPEN"}`)}, SessionIsNew: true,
		SessionEvents: []clockservice.SessionEvent{{Kind: "OPENED", ActorRef: "worker", IdempotencyKey: "event-in", Digest: "event-in-digest", Payload: []byte(`{"observation_id":"observation-in"}`)}},
		Observation:   clockservice.ObservationRecord{ID: "observation-in", TenantID: tenant, WorkerRef: "worker", AssignmentRef: "assignment", Source: "WORKER_SELF", EventType: "IN", Timezone: "UTC", IdempotencyKey: "punch-in", Digest: "punch-in-digest", OccurredAt: now, ReceivedAt: now, Payload: []byte(`{"kind":"IN"}`)}}
	factory := timeoutFactory(t, db, tenantID, now, timeStore)
	resolver := clockDriverResolver{plan: plan}
	start := timeoutStart(tenantID, work, now, resolver, versions)
	driver, err := factory(ctx, tenant, work)
	if err != nil {
		t.Fatal(err)
	}
	started, err := driver.Execute(ctx, execute.ExecuteRequest{Start: start})
	if err != nil {
		t.Fatal(err)
	}
	instanceID := started.Start.InstanceID
	if instanceID == uuid.Nil {
		t.Fatal("clock-in did not create a workflow instance")
	}
	var beforeVersion int64
	if err := db.Conn.QueryRow(ctx, `SELECT instance_version FROM workflow_instance WHERE tenant_id=$1 AND instance_id=$2`, tenantID, instanceID).Scan(&beforeVersion); err != nil {
		t.Fatal(err)
	}
	var subscriptionID uuid.UUID
	var expiresAt time.Time
	if err := db.Conn.QueryRow(ctx, `SELECT subscription_id, expires_at FROM workflow_signal_subscription WHERE tenant_id=$1 AND instance_id=$2 AND node_id=$3`, tenantID, instanceID, clockpunch.NodeAwaitClockOut).Scan(&subscriptionID, &expiresAt); err != nil {
		t.Fatal(err)
	}
	if want := now.Add(16 * time.Hour); !expiresAt.Equal(want) {
		t.Fatalf("clock-out expiry=%s, want %s", expiresAt, want)
	}
	if expired := expireClockSubscription(t, db, tenantID, now.Add(16*time.Hour)); len(expired) != 1 || expired[0].SubscriptionID != subscriptionID {
		t.Fatalf("expired subscriptions=%v, want one %s", expired, subscriptionID)
	}
	if expired := expireClockSubscription(t, db, tenantID, now.Add(16*time.Hour)); len(expired) != 0 {
		t.Fatalf("retry expiry returned %v, want no duplicate wakeup", expired)
	}
	timeoutDriver, ok := driver.(interface {
		ResumeSignalTimeout(context.Context, execute.ResumeSignalTimeoutRequest) (execute.Result, error)
	})
	if !ok {
		t.Fatal("clock workflow runtime does not expose timeout resume")
	}
	timedOut, err := timeoutDriver.ResumeSignalTimeout(ctx, execute.ResumeSignalTimeoutRequest{Start: timeoutStart(tenantID, work, now, resolver, versions), InstanceID: instanceID, ExpectedInstanceVersion: beforeVersion, SubscriptionID: subscriptionID, RecordedAt: now.Add(16 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if timedOut.Status != execute.StatusComplete {
		t.Fatalf("timeout status=%s, want COMPLETE after reaching terminal END", timedOut.Status)
	}
	var runtimeStatus string
	if err := db.Conn.QueryRow(ctx, `SELECT runtime_status FROM workflow_instance WHERE tenant_id=$1 AND instance_id=$2`, tenantID, instanceID).Scan(&runtimeStatus); err != nil {
		t.Fatal(err)
	}
	if runtimeStatus != string(workflow.RuntimeBlocked) {
		t.Fatalf("runtime status=%q, want %q", runtimeStatus, workflow.RuntimeBlocked)
	}
	var clockOutCount, terminalCount, head int
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM workflow_node_execution WHERE tenant_id=$1 AND instance_id=$2 AND node_id=$3 AND status='SUCCEEDED'`, tenantID, instanceID, clockpunch.NodeCommitClockOut).Scan(&clockOutCount); err != nil {
		t.Fatal(err)
	}
	if clockOutCount != 0 {
		t.Fatalf("clock-out executions=%d, want no automatic OUT", clockOutCount)
	}
	stream := "workflow:" + clockpunch.WorkflowID + ":" + instanceID.String()
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM ledger_event WHERE tenant_id=$1 AND stream_key=$2`, tenantID, stream).Scan(&terminalCount); err != nil {
		t.Fatal(err)
	}
	if terminalCount != 1 {
		t.Fatalf("terminal ledger events=%d, want one immutable terminal fact", terminalCount)
	}
	if err := db.Conn.QueryRow(ctx, `SELECT head_sequence FROM stream_head WHERE tenant_id=$1 AND stream_key=$2`, tenantID, stream).Scan(&head); err != nil {
		t.Fatal(err)
	}
	if head != 1 {
		t.Fatalf("terminal ledger head=%d, want 1", head)
	}
	_, err = timeoutDriver.ResumeSignalTimeout(ctx, execute.ResumeSignalTimeoutRequest{Start: timeoutStart(tenantID, work, now, resolver, versions), InstanceID: instanceID, ExpectedInstanceVersion: beforeVersion, SubscriptionID: subscriptionID, RecordedAt: now.Add(16 * time.Hour)})
	if err == nil || (runtime.CodeOf(err) != runtime.CodeStaleInstance && !strings.Contains(err.Error(), "terminal")) {
		t.Fatalf("timeout retry error=%v, want fenced/terminal refusal", err)
	}
}

func expireClockSubscription(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, at time.Time) []signals.ExpiredSubscription {
	t.Helper()
	tx, err := db.Conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	due, err := (signals.Store{}).ExpireDue(context.Background(), tx, tenantID, at, 8)
	if err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	return due
}

func timeoutFactory(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, now time.Time, timeStore *timestore.Store) clockservice.WorkflowDriverFactory {
	t.Helper()
	telemetry := clockDriverTelemetry(t)
	signalAdapter := platformexecution.SignalSubscriptions{Store: signals.Store{}}
	terminal, err := NewClockWorkflowTerminalWriter("hcmnext:test:clock-timeout", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	factory, err := NewClockWorkflowDriverFactory(ClockWorkflowDriverFactoryOptions{DB: db.Conn, BaseSteps: clockDriverSteps{}, Commit: timeclockstore.Adapter{Store: timeStore}, Telemetry: telemetry, Terminal: terminal, Guard: idempotency.PostgresStore{}, Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour}, Signals: signalAdapter, SignalReader: signalAdapter, SignalTimeoutReader: signalAdapter, ResolveTenant: func(string) (uuid.UUID, error) { return tenantID, nil }, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return factory
}

func timeoutTimeStore(t *testing.T, ctx context.Context) (*timestore.Store, *pgtest.DB) {
	t.Helper()
	timeDB := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(timestore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, timeDB.SQL, migrationFS, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	store, err := timestore.New(ctx, timestore.Config{DSN: timeDB.URL, Schema: timeDB.Schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store, timeDB
}

func timeoutStart(tenantID uuid.UUID, work clockservice.PunchWork, now time.Time, resolver runtime.WorkflowResolver, versions *version.Registry) runtime.StartRequest {
	return runtime.StartRequest{TenantID: tenantID, CellID: "cell-clock", StartIdempotencyKey: "start-timeout", Resolver: resolver, Versions: versions, Source: &runtime.StartSource{Kind: runtime.StartSourceTrigger, IntentType: clockpunch.IntentType, Trigger: &runtime.TriggerStartSource{TriggerID: "start-timeout", TriggerType: "clock", Key: work.Session.ID}}, BusinessSubjectRefs: []string{"time_session:" + work.Session.ID}, ExecutionMode: workflow.ModeExecute, CorrelationID: "corr-timeout", CreatedAt: now}
}
