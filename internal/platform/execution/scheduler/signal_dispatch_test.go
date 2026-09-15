package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/signals"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	wfruntime "github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepSignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// WF-RUN-005 on the served path, end to end over one real PostgreSQL: the
// real compiler parks a SIGNAL node through the driver's SignalSubscriber
// port (internal/platform/execution.SignalSubscriptions over
// internal/data/signals), internal/data/signals receives inbound signals and
// commits their dispositions, and this package's SignalDispatcher claims the
// matched continuation and resumes the node through execute.Driver.ResumeSignal.

const (
	signalWorkflowID = "hcmnext.workflows.test.promotion_signal_scheduled"

	nodeSignalPrepare   = "prepare_promotion"
	nodeSignalAwait     = "await_hris_ack"
	nodeSignalApplied   = "end_signal_applied"
	nodeSignalExpired   = "end_signal_timed_out"
	nodeSignalCancelled = "end_signal_cancelled"
	nodeSignalDegraded  = "end_signal_review"

	signalEventType = "hcmnext.events.test.promotion_ack"
	signalSource    = "hcmnext.integrations.hris"
	signalKey       = "subject:employment"
)

var signalPayloadSchema = demoSchema("PromotionAckPayload")

func promotionSignalDefinition() workflow.Definition {
	def := promotionWaitDefinition(fixtureFireAt)
	def.WorkflowID, def.Name = signalWorkflowID, "Promotion signal (scheduler)"
	prepare := def.Nodes[0]
	prepare.ID = nodeSignalPrepare
	governance := workflow.NodeGovernance{
		Purpose: "PROMOTION_HRIS_ACK", Classification: "CONFIDENTIAL_HR",
		RevalidationBoundary:  workflow.RevalidatePreExecution,
		DataAccessManifestRef: "data-access.test.promotion-signal-scheduled/v1",
	}
	await := workflow.Node{
		ID: nodeSignalAwait, Type: workflow.StepSignal,
		InputSchema: demoSchema("SignalAwaitInput"), OutputSchema: demoSchema("SignalAwaitResult"),
		Inputs: []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}},
		InputMappings: []workflow.Mapping{
			{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
		},
		DeclaredEffect: capability.EffectPure,
		Signal: &workflow.SignalSpec{
			EventType: signalEventType, CorrelationKeyExpression: signalKey,
			ExpectedSchemaRef: signalPayloadSchema, AcceptedSources: []string{signalSource},
			Ordering: workflow.SignalOrderingNone, CloseAfterSeconds: 86400,
		},
		FailureRoute: nodeSignalDegraded,
		Governance:   governance,
	}
	def.Nodes = []workflow.Node{
		prepare, await,
		demoTerminalNode(nodeSignalApplied, "PROMOTION_APPLIED_AFTER_ACK", workflow.RuntimeCompleted,
			demoCompletion("CLOSED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED"),
			"receipt.test.promotion-signal-scheduled/v1"),
		demoTerminalNode(nodeSignalExpired, "ACK_TIMED_OUT", workflow.RuntimeCancelled,
			demoCompletion("CANCELLED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), ""),
		demoTerminalNode(nodeSignalCancelled, "CANCELLED", workflow.RuntimeCancelled,
			demoCompletion("CANCELLED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), ""),
		demoTerminalNode(nodeSignalDegraded, "SIGNAL_REVIEW_REQUIRED", workflow.RuntimeCompleted,
			demoCompletion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), ""),
	}
	def.Edges = []workflow.Edge{
		{From: nodeSignalPrepare, To: nodeSignalAwait, RouteKey: "SUCCEEDED"},
		{From: nodeSignalPrepare, To: nodeSignalDegraded, RouteKey: "FAILED"},
		{From: nodeSignalAwait, To: nodeSignalApplied, RouteKey: "SUCCEEDED"},
		{From: nodeSignalAwait, To: nodeSignalExpired, RouteKey: "TIMED_OUT"},
		{From: nodeSignalAwait, To: nodeSignalCancelled, RouteKey: "CANCELLED"},
	}
	return def
}

// signalCell is one composed signal-capable workflow cell over one connection.
type signalCell struct {
	db       dbport.Beginner
	plan     *workflow.CompiledWorkflow
	start    wfruntime.StartRequest
	driver   *execute.Driver
	tenantID uuid.UUID
	subject  string
}

func newSignalCell(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, subject string, at time.Time) signalCell {
	t.Helper()
	def := promotionSignalDefinition()
	plan, err := workflow.Compile(def, workflow.Options{Phase: workflow.PhaseP1B})
	if err != nil {
		t.Fatalf("compile the promotion signal definition: %v", err)
	}
	versions := version.NewRegistry()
	published, err := version.Publish(versions, def, plan, workflow.Options{Phase: workflow.PhaseP1B}, version.PublishMeta{
		SemanticVersion: "1.0.0", PublishedAt: at, PublishedBy: "test:scheduler",
	})
	if err != nil {
		t.Fatalf("version.Publish: %v", err)
	}
	if _, err := version.Activate(versions, published.CompiledPlanDigest, version.ActivationEvidence{
		Authorized: true, ApprovedBy: "test:release", Authority: "authority:release",
		ApprovedAt: at, ReviewedPlanDigest: published.CompiledPlanDigest, TestsPassed: true,
	}); err != nil {
		t.Fatalf("version.Activate: %v", err)
	}
	cell := promotionCell{
		db: appConn(t, db), plan: plan, versions: versions,
		resolver: effects.PolicyResolver{Entries: []effects.PolicyEntry{{
			WorkflowID: plan.WorkflowID, Pin: version.Pin{CompiledPlanDigest: published.CompiledPlanDigest}, Plan: plan,
		}}},
	}
	proposal := newDemoProposal(t, values.TenantId(tenantID.String()), "intent:signal:"+subject, subject, at)
	return newSignalCellOn(t, cell.db, signalCell{plan: plan, start: cell.startRequest(tenantID, proposal, subject, at),
		tenantID: tenantID, subject: subject})
}

// resumer is the test's SignalResumer: it reads the instance's current
// version and resumes through the real driver, counting advancements that
// actually committed.
func (c signalCell) resumer(resumed *atomic.Int64) SignalResumer {
	return SignalResumerFunc(func(ctx context.Context, work SignalWork) (Disposition, error) {
		instance, err := c.instance(ctx, work.Row.InstanceID)
		if err != nil {
			return DispositionRetry, err
		}
		result, err := c.driver.ResumeSignal(ctx, execute.ResumeSignalRequest{
			Start: c.start, InstanceID: work.Row.InstanceID, ExpectedInstanceVersion: instance.InstanceVersion,
			SignalID: work.Receipt.SignalID, SubscriptionID: work.Receipt.SubscriptionID,
		})
		if err != nil {
			return DispositionRetry, err
		}
		resumed.Add(1)
		if result.Status != execute.StatusComplete {
			return DispositionRetry, fmt.Errorf("resumed instance is %s, want COMPLETE", result.Status)
		}
		return DispositionCompleted, nil
	})
}

func (c signalCell) instance(ctx context.Context, instanceID uuid.UUID) (wfruntime.Instance, error) {
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return wfruntime.Instance{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, c.tenantID); err != nil {
		return wfruntime.Instance{}, err
	}
	return (wfruntime.Store{}).LoadInstance(ctx, tx, c.tenantID, instanceID)
}

type signalVerifier struct{ forged string }

func (v signalVerifier) Verify(sig stepSignal.Signal) error {
	if v.forged != "" && sig.IdempotencyKey == v.forged {
		return errors.New("signature does not verify")
	}
	return nil
}

func (c signalCell) signal(key, source, schema, value string, at time.Time) signals.ReceiveRequest {
	return signals.ReceiveRequest{ReceivedAt: at, Signal: stepSignal.Signal{
		Tenant: values.TenantId(c.tenantID.String()), Source: source, EventType: signalEventType,
		SchemaRef: schema, CorrelationKey: signalKey, CorrelationValue: value,
		IdempotencyKey: key, Payload: []byte(`{"acknowledged":true}`), ReceivedAt: values.NewInstant(at),
	}}
}

func receive(t *testing.T, db dbport.Beginner, tenantID uuid.UUID, req signals.ReceiveRequest, verify stepSignal.Verifier) signals.Receipt {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin receive: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope receive: %v", err)
	}
	got, err := (signals.Store{}).Receive(ctx, tx, req, verify)
	if err != nil {
		t.Fatalf("receive %s: %v", req.Signal.IdempotencyKey, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit receive: %v", err)
	}
	return got
}

func onlyStatus(t *testing.T, got signals.Receipt, want stepSignal.Status, what string) {
	t.Helper()
	if len(got.Dispositions) != 1 || got.Dispositions[0].Status != want {
		t.Fatalf("%s: dispositions = %+v, want exactly one %s", what, got.Dispositions, want)
	}
	if want != stepSignal.StatusAccepted && got.Dispositions[0].ContinuationRef != "" {
		t.Fatalf("%s: a %s disposition carries continuation %q", what, want, got.Dispositions[0].ContinuationRef)
	}
}

func acquireQueue(t *testing.T, db *pgtest.DB, claim lease.AcquireRequest, at time.Time) lease.Fence {
	t.Helper()
	var grant lease.Grant
	inTenantTx(t, db, claim.TenantID, func(tx dbport.Tx) error {
		var err error
		req := claim
		req.Now, req.TTL = at, time.Hour
		grant, err = (lease.Manager{}).Acquire(context.Background(), tx, req)
		return err
	})
	return grant.Fence
}

func signalRows(t *testing.T, db *pgtest.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func startParked(t *testing.T, cell signalCell) wfruntime.Instance {
	t.Helper()
	result, err := cell.driver.Execute(context.Background(), execute.ExecuteRequest{Start: cell.start})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != execute.StatusParked {
		t.Fatalf("Execute status = %s, want PARKED on the SIGNAL node", result.Status)
	}
	instance, err := cell.instance(context.Background(), result.Start.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(instance.CurrentNodeIDs) != 1 || instance.CurrentNodeIDs[0] != nodeSignalAwait {
		t.Fatalf("parked frontier = %v, want [%s]", instance.CurrentNodeIDs, nodeSignalAwait)
	}
	return instance
}

// TestTodo_WF_RUN_005_ServedPath parks a SIGNAL node, proves every refusal
// (unmatched, unauthorized source, forged signature, incompatible schema)
// leaves an inspectable disposition and wakes nothing, then resumes the node
// exactly once from a matching signal, and proves a duplicate, a late signal,
// a second sweep, a direct replayed resume and a routed redelivery never
// resume it again.
func TestTodo_WF_RUN_005_ServedPath(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	at := fixtureAt
	tenantID := insertTenant(t, db, "wf-run-005-served", at.Add(-time.Hour))
	cell := newSignalCell(t, db, tenantID, "jane", at)
	instance := startParked(t, cell)
	subscriptionID := signals.SubscriptionIDFor(tenantID, instance.InstanceID, nodeSignalAwait, 1)
	if n := signalRows(t, db, `SELECT count(*) FROM workflow_signal_subscription WHERE subscription_id = $1 AND subscription_state = 'OPEN' AND correlation_value = 'employment:jane'`, subscriptionID); n != 1 {
		t.Fatalf("open subscriptions for the parked node = %d, want 1", n)
	}

	var resumed atomic.Int64
	logger := &recordingLogger{}
	dispatcher, err := NewSignalDispatcher(SignalDispatcherConfig{DB: appConn(t, db), Resumer: cell.resumer(&resumed), Clock: func() time.Time { return at }, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	claim := claimFixture(tenantID, "replica:signal")
	fence := acquireQueue(t, db, claim, at)
	verify := signalVerifier{forged: "forged"}
	schema := signalPayloadSchema.String()

	onlyStatus(t, receive(t, cell.db, tenantID, cell.signal("unmatched", signalSource, schema, "employment:someone-else", at), verify),
		stepSignal.StatusRefusedUnmatched, "unmatched correlation")
	onlyStatus(t, receive(t, cell.db, tenantID, cell.signal("wrong-source", "hcmnext.integrations.rogue", schema, "employment:jane", at), verify),
		stepSignal.StatusRefusedWrongSource, "unauthorized source")
	onlyStatus(t, receive(t, cell.db, tenantID, cell.signal("forged", signalSource, schema, "employment:jane", at), verify),
		stepSignal.StatusRefusedInvalidSignature, "forged signature")
	onlyStatus(t, receive(t, cell.db, tenantID, cell.signal("wrong-schema", signalSource, "hcmnext.workflows.test.PromotionAckPayload/v2", "employment:jane", at), verify),
		stepSignal.StatusRefusedWrongSchema, "incompatible schema")
	if count, err := dispatcher.RunFencedSignalRole(ctx, claim, fence, at, "signal"); err != nil || count != 0 {
		t.Fatalf("sweep after refusals = %d, %v; want nothing to claim", count, err)
	}
	if n := signalRows(t, db, `SELECT count(*) FROM workflow_ready_work WHERE instance_id = $1`, instance.InstanceID); n != 0 {
		t.Fatalf("refused signals enqueued %d ready work rows, want 0", n)
	}

	accepted := receive(t, cell.db, tenantID, cell.signal("ack-1", signalSource, schema, "employment:jane", at), verify)
	onlyStatus(t, accepted, stepSignal.StatusAccepted, "matching signal")
	if accepted.Dispositions[0].ContinuationRef != signals.ContinuationRef(accepted.SignalID) {
		t.Fatalf("accepted continuation = %q, want the signal reference", accepted.Dispositions[0].ContinuationRef)
	}
	onlyStatus(t, receive(t, cell.db, tenantID, cell.signal("ack-1", signalSource, schema, "employment:jane", at), verify),
		stepSignal.StatusDuplicateSameBytes, "duplicate redelivery")

	count, err := dispatcher.RunFencedSignalRole(ctx, claim, fence, at, "signal")
	if err != nil || count != 1 {
		t.Fatalf("sweep after the match = %d, %v; want exactly one claimed continuation", count, err)
	}
	if resumed.Load() != 1 {
		t.Fatalf("resumes = %d, want 1", resumed.Load())
	}
	done, err := cell.instance(ctx, instance.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if done.RuntimeStatus != wfruntime.InstanceCompleted {
		t.Fatalf("instance after resume = %s, want COMPLETED", done.RuntimeStatus)
	}
	var output string
	if err := db.QueryRow(ctx, `SELECT output_artifact_ref FROM workflow_node_execution WHERE instance_id = $1 AND node_id = $2`,
		instance.InstanceID, nodeSignalAwait).Scan(&output); err != nil {
		t.Fatal(err)
	}
	if output != signals.ContinuationRef(accepted.SignalID) {
		t.Fatalf("SIGNAL node output = %q, want the continuation reference, never the payload", output)
	}

	onlyStatus(t, receive(t, cell.db, tenantID, cell.signal("ack-late", signalSource, schema, "employment:jane", at.Add(time.Minute)), verify),
		stepSignal.StatusRefusedLate, "late signal after resume")
	if count, err := dispatcher.RunFencedSignalRole(ctx, claim, fence, at.Add(time.Minute), "signal"); err != nil || count != 0 {
		t.Fatalf("second sweep = %d, %v; want nothing left to claim", count, err)
	}
	if _, err := cell.driver.ResumeSignal(ctx, execute.ResumeSignalRequest{
		Start: cell.start, InstanceID: instance.InstanceID, ExpectedInstanceVersion: done.InstanceVersion,
		SignalID: accepted.SignalID, SubscriptionID: subscriptionID,
	}); err == nil {
		t.Fatal("a replayed resume from the consumed receipt advanced again")
	}
	var readyRow runtimestate.ReadyWork
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		rows, err := (runtimestate.ReadyWorkStore{}).PendingForInstance(ctx, tx, tenantID, instance.InstanceID)
		if len(rows) != 0 {
			return fmt.Errorf("pending ready work after resume = %d, want 0", len(rows))
		}
		readyRow.TenantID, readyRow.InstanceID, readyRow.NodeID, readyRow.Attempt = tenantID, instance.InstanceID, nodeSignalAwait, 1
		return err
	})
	routedCalls := 0
	routed := dispatcher.Route(DispatcherFunc(func(context.Context, Work) (Disposition, error) {
		routedCalls++
		return DispositionCompleted, nil
	}))
	if disposition, err := routed.Dispatch(ctx, Work{Row: readyRow}); err == nil || disposition != DispositionRetry {
		t.Fatalf("routed redelivery of the consumed receipt = %s, %v; want a refused retry", disposition, err)
	}
	other := readyRow
	other.NodeID = nodeSignalPrepare
	if disposition, err := routed.Dispatch(ctx, Work{Row: other}); err != nil || disposition != DispositionCompleted || routedCalls != 1 {
		t.Fatalf("routed non-signal work = %s, %v, fallback calls %d; want the fallback dispatcher", disposition, err, routedCalls)
	}
	if resumed.Load() != 1 {
		t.Fatalf("resumes after every redelivery = %d, want still 1", resumed.Load())
	}

	statuses := map[string]int{}
	rows, err := db.Conn.Query(ctx, `SELECT status, count(*) FROM workflow_signal_disposition WHERE tenant_id = $1 GROUP BY status`, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			t.Fatal(err)
		}
		statuses[status] = n
	}
	rows.Close()
	want := map[string]int{"REFUSED_UNMATCHED": 1, "REFUSED_WRONG_SOURCE": 1, "REFUSED_INVALID_SIGNATURE": 1,
		"REFUSED_WRONG_SCHEMA": 1, "ACCEPTED": 1, "DUPLICATE_SAME_BYTES": 1, "REFUSED_LATE": 1}
	if fmt.Sprint(statuses) != fmt.Sprint(want) {
		t.Fatalf("inspectable dispositions = %v, want %v", statuses, want)
	}
	if n := signalRows(t, db, `SELECT count(*) FROM workflow_signal_receipt WHERE instance_id = $1`, instance.InstanceID); n != 1 {
		t.Fatalf("receipts = %d, want exactly the one accepted signal", n)
	}
	if state, _ := readyStateOf(t, db, tenantID, onlyReadyID(t, db, instance.InstanceID)); state != runtimestate.ReadyDone {
		t.Fatalf("matched continuation settled %s, want DONE", state)
	}
	if logger.count("scheduler.signal_continuation_settled") != 1 {
		t.Fatalf("settle log lines = %d, want 1", logger.count("scheduler.signal_continuation_settled"))
	}
}

func onlyReadyID(t *testing.T, db *pgtest.DB, instanceID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow(context.Background(), `SELECT ready_work_id FROM workflow_ready_work WHERE instance_id = $1`, instanceID).Scan(&id); err != nil {
		t.Fatalf("load the one ready work row: %v", err)
	}
	return id
}

// TestTodo_WF_RUN_005_Race receives the same signal concurrently from two
// independent connections and then sweeps concurrently from two independent
// dispatchers: one receipt, one claim, one resume.
func TestTodo_WF_RUN_005_Race(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	at := fixtureAt
	tenantID := insertTenant(t, db, "wf-run-005-served-race", at.Add(-time.Hour))
	cell := newSignalCell(t, db, tenantID, "rahul", at)
	instance := startParked(t, cell)
	schema := signalPayloadSchema.String()

	start := make(chan struct{})
	var wg sync.WaitGroup
	statuses := make(chan stepSignal.Status, 2)
	for i := range 2 {
		conn := appConn(t, db)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Errorf("replica %d begin: %v", i, err)
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
				t.Errorf("replica %d scope: %v", i, err)
				return
			}
			got, err := (signals.Store{}).Receive(ctx, tx, cell.signal("ack-race", signalSource, schema, "employment:rahul", at), signalVerifier{})
			if err != nil {
				t.Errorf("replica %d receive: %v", i, err)
				return
			}
			if err := tx.Commit(ctx); err != nil {
				t.Errorf("replica %d commit: %v", i, err)
				return
			}
			statuses <- got.Dispositions[0].Status
		}()
	}
	close(start)
	wg.Wait()
	close(statuses)
	seen := map[stepSignal.Status]int{}
	for s := range statuses {
		seen[s]++
	}
	if seen[stepSignal.StatusAccepted] != 1 || seen[stepSignal.StatusDuplicateSameBytes] != 1 {
		t.Fatalf("concurrent receipt statuses = %v, want one ACCEPTED and one DUPLICATE_SAME_BYTES", seen)
	}

	claim := claimFixture(tenantID, "replica:signal-race")
	fence := acquireQueue(t, db, claim, at)
	var resumed atomic.Int64
	counts := make(chan int, 2)
	sweepStart := make(chan struct{})
	for i := range 2 {
		dispatcher, err := NewSignalDispatcher(SignalDispatcherConfig{DB: appConn(t, db), Resumer: cell.forResumeConn(t, db).resumer(&resumed), Clock: func() time.Time { return at }})
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-sweepStart
			count, err := dispatcher.RunFencedSignalRole(ctx, claim, fence, at, fmt.Sprintf("signal-%d", i))
			if err != nil {
				t.Errorf("dispatcher %d: %v", i, err)
			}
			counts <- count
		}()
	}
	close(sweepStart)
	wg.Wait()
	close(counts)
	total := 0
	for c := range counts {
		total += c
	}
	if total != 1 || resumed.Load() != 1 {
		t.Fatalf("concurrent sweeps claimed %d and resumed %d; want exactly one of each", total, resumed.Load())
	}
	if n := signalRows(t, db, `SELECT count(*) FROM workflow_node_execution WHERE instance_id = $1 AND node_id = $2 AND status = 'SUCCEEDED'`,
		instance.InstanceID, nodeSignalAwait); n != 1 {
		t.Fatalf("succeeded SIGNAL node executions = %d, want 1", n)
	}
}

// forResumeConn rebuilds the cell's driver over an independent connection, so
// two concurrent resumers never share one session.
func (c signalCell) forResumeConn(t *testing.T, db *pgtest.DB) signalCell {
	t.Helper()
	return newSignalCellOn(t, appConn(t, db), c)
}

// newSignalCellOn composes the signal-capable driver over conn.
func newSignalCellOn(t *testing.T, conn dbport.Beginner, base signalCell) signalCell {
	t.Helper()
	ports := platformexecution.SignalSubscriptions{}
	drv, err := execute.New(execute.Options{
		DB: conn, Steps: endOnlySteps{},
		Terminal: &effects.LedgerTerminalWriter{
			Appender: newLedgerAppender(t), ProjectionName: "workflow_promotion_signal_scheduled",
			SourceRef: "hcmnext:test:scheduler-signal",
		},
		Signals: ports, SignalReader: ports,
		Guard:     idempotency.PostgresStore{},
		Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour},
		Clock:     func() time.Time { return fixtureAt },
		Leases: platformexecution.NewInstanceLeaser(lease.Identity{
			WorkloadRef: "workload:hcmnext-serve", InstanceRef: "replica:" + uuid.NewString(),
		}, 0),
		FenceVerifier: lease.Fenced{Manager: lease.Manager{}},
	})
	if err != nil {
		t.Fatalf("execute.New: %v", err)
	}
	out := base
	out.db, out.driver = conn, drv
	return out
}

func TestNewSignalDispatcher_RefusesIncompleteWiring(t *testing.T) {
	resumer := SignalResumerFunc(func(context.Context, SignalWork) (Disposition, error) { return DispositionCompleted, nil })
	for name, cfg := range map[string]SignalDispatcherConfig{
		"no database":       {Resumer: resumer},
		"no resumer":        {DB: failingBeginner{}},
		"negative batch":    {DB: failingBeginner{}, Resumer: resumer, BatchSize: -1},
		"negative instance": {DB: failingBeginner{}, Resumer: resumer, InstanceTTL: -time.Second},
	} {
		if _, err := NewSignalDispatcher(cfg); !errors.Is(err, ErrConfig) {
			t.Fatalf("%s: err = %v, want ErrConfig", name, err)
		}
	}
	d, err := NewSignalDispatcher(SignalDispatcherConfig{DB: failingBeginner{err: errors.New("down")}, Resumer: resumer})
	if err != nil {
		t.Fatal(err)
	}
	if d.cfg.BatchSize != DefaultBatchSize || d.cfg.InstanceTTL != DefaultInstanceTTL || d.cfg.Clock == nil || d.cfg.Logger == nil {
		t.Fatalf("defaults not filled: %+v", d.cfg)
	}
	claim := claimFixture(uuid.New(), "replica:unit")
	if _, err := d.RunSignalRole(context.Background(), claim, fixtureAt, "signal"); !errors.Is(err, ErrSignalRoleUnfenced) {
		t.Fatalf("unfenced role err = %v, want ErrSignalRoleUnfenced", err)
	}
	if count, err := d.RunFencedSignalRole(context.Background(), claim, lease.Fence{Token: 1}, fixtureAt, "signal"); err == nil || count != 0 {
		t.Fatalf("fenced role over an unreachable database = %d, %v; want an error and nothing claimed", count, err)
	}
	if disposition, err := d.Route(nil).Dispatch(context.Background(), Work{Row: readyWorkFixture()}); err == nil || disposition != DispositionRetry {
		t.Fatalf("route over an unreachable database = %s, %v; want retry with error", disposition, err)
	}
	if got, err := resumer.ResumeSignal(context.Background(), SignalWork{}); err != nil || got != DispositionCompleted {
		t.Fatalf("SignalResumerFunc = %s, %v", got, err)
	}
}

func TestSignalDispatcher_ResumeTurnsFailuresIntoRetries(t *testing.T) {
	logger := &recordingLogger{}
	for name, tc := range map[string]struct {
		resumer SignalResumerFunc
		want    Disposition
		log     string
	}{
		"error": {resumer: func(context.Context, SignalWork) (Disposition, error) {
			return DispositionCompleted, errors.New("boom")
		},
			want: DispositionRetry, log: "scheduler.signal_resume_failed"},
		"undeclared": {resumer: func(context.Context, SignalWork) (Disposition, error) { return Disposition("MAYBE"), nil },
			want: DispositionRetry, log: "scheduler.signal_resume_undeclared_disposition"},
		"abandoned": {resumer: func(context.Context, SignalWork) (Disposition, error) { return DispositionAbandoned, nil },
			want: DispositionAbandoned},
	} {
		d, err := NewSignalDispatcher(SignalDispatcherConfig{DB: failingBeginner{}, Resumer: tc.resumer, Logger: logger})
		if err != nil {
			t.Fatal(err)
		}
		if got := d.resume(context.Background(), SignalWork{Row: readyWorkFixture()}); got != tc.want {
			t.Fatalf("%s: disposition = %s, want %s", name, got, tc.want)
		}
		if tc.log != "" && logger.count(tc.log) == 0 {
			t.Fatalf("%s: no %s log line", name, tc.log)
		}
	}
	d, _ := NewSignalDispatcher(SignalDispatcherConfig{DB: failingBeginner{}, Resumer: SignalResumerFunc(func(context.Context, SignalWork) (Disposition, error) { return DispositionCompleted, nil })})
	if err := d.settle(context.Background(), SignalWork{Row: readyWorkFixture()}, Disposition("MAYBE")); !errors.Is(err, ErrConfig) {
		t.Fatalf("settle with an undeclared disposition = %v, want ErrConfig", err)
	}
}
