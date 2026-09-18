package execute_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/ledger"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	wfrecover "github.com/monstercameron/human-capital-management-suite/internal/workflow/recover"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// WF-RUN-003's served half: a driver that dies in the middle of a synchronous
// drain, at each persistence boundary the drain crosses, is redelivered by the
// recovery sweep exactly once -- no lost work, no second business effect, a
// committed advancement replayed rather than re-executed, and a superseded
// driver or a second sweeper refused by the lease fence.

// errProcessDied is the injected death. Once a crash is armed every further
// Begin fails, which is what a dead process looks like to the database: it
// commits nothing more and releases nothing, not even its instance lease.
var errProcessDied = errors.New("injected process death")

type crashDB struct {
	inner dbport.Beginner
	armed *atomic.Bool
}

func (c crashDB) Begin(ctx context.Context) (dbport.Tx, error) {
	if c.armed.Load() {
		return nil, errProcessDied
	}
	return c.inner.Begin(ctx)
}

// drainBoundary names where in the drain the driver dies.
type drainBoundary string

const (
	// boundaryBeforeEffect: execute_promotion's READY activation is committed
	// and the driver dies as its step starts, before any effect.
	boundaryBeforeEffect drainBoundary = "AFTER_READY_COMMIT_BEFORE_STEP_EFFECT"
	// boundaryAfterEffect: the step effect and its idempotency record are
	// committed and the driver dies before the advance transaction.
	boundaryAfterEffect drainBoundary = "AFTER_STEP_EFFECT_BEFORE_ADVANCE_COMMIT"
	// boundaryAfterAdvance: execute_promotion's advancement is committed and
	// the driver dies before draining the READY successor it derived.
	boundaryAfterAdvance drainBoundary = "AFTER_ADVANCE_COMMIT_BEFORE_CONTINUATION_DISPATCH"
)

// promotionEffect is execute_promotion's business effect: one outbox row per
// performance, written through the guarded transaction. Every performance
// uses a fresh effect identity, so the outbox row count is the number of
// performances that committed and a duplicate cannot hide behind the outbox's
// own dedupe.
type promotionEffect struct {
	crash    *atomic.Bool
	boundary drainBoundary
	performs *atomic.Int32
}

func (e promotionEffect) Guards(node workflow.CompiledNode) bool {
	return node.ID == promotionexec.NodeExecutePromotion
}

func (e promotionEffect) Perform(ctx context.Context, tx dbport.Tx, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	identity := "wfrun003:promotion:" + req.InstanceID.String() + ":" + uuid.NewString()
	if _, err := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{
		Tenant: req.TenantID, EffectIdentity: identity, OrderingKey: "wfrun003:" + req.InstanceID.String(),
		SchemaRef: wfrun003EffectSchema, Payload: []byte("promote"),
	}); err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	e.performs.Add(1)
	if e.boundary == boundaryAfterEffect {
		// The effect's own transaction still commits: the process dies after
		// it, before the driver opens the advance transaction.
		e.crash.Store(true)
	}
	out, _, err := promotionRunner{}.Run(ctx, req)
	return out, runtime.GovernanceRefs{EffectRefs: []string{identity}}, err
}

const wfrun003EffectSchema = "hcmnext.test.wfrun003.effect/v1"

// crashingSteps arms the crash as execute_promotion's step starts.
type crashingSteps struct {
	next     execute.StepRunner
	crash    *atomic.Bool
	boundary drainBoundary
}

func (s crashingSteps) Run(ctx context.Context, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	if s.boundary == boundaryBeforeEffect && req.Node.ID == promotionexec.NodeExecutePromotion {
		s.crash.Store(true)
	}
	return s.next.Run(ctx, req)
}

// crashingInstrumentation arms the crash when the first advancement span ends
// successfully, which the driver does only after that advancement committed.
type crashingInstrumentation struct {
	execute.NoopInstrumentation
	crash    *atomic.Bool
	boundary drainBoundary
}

type crashingSpan struct {
	crash    *atomic.Bool
	boundary drainBoundary
}

func (s crashingSpan) End(outcome string, err error) {
	if s.boundary == boundaryAfterAdvance && outcome == execute.OutcomeSuccess && err == nil {
		s.crash.Store(true)
	}
}

func (i crashingInstrumentation) StartAdvanceSpan(ctx context.Context, _ execute.SpanAttributes) (context.Context, execute.Span) {
	return ctx, crashingSpan{crash: i.crash, boundary: i.boundary}
}

// driverLeaser takes the WORKFLOW_INSTANCE lease as one named workload.
type driverLeaser struct {
	holder lease.Identity
	ttl    time.Duration
}

func (l driverLeaser) AcquireInstance(ctx context.Context, ex runtime.Executor, tenantID, instanceID uuid.UUID, at time.Time) (runtime.Fence, error) {
	grant, err := lease.Manager{}.Acquire(ctx, ex, lease.AcquireRequest{
		TenantID: tenantID, Resource: lease.Resource{Kind: lease.ResourceWorkflowInstance, ID: instanceID.String()},
		Holder: l.holder, Now: at, TTL: l.ttl,
	})
	return grant.Fence.RuntimeFence(at), err
}

func (l driverLeaser) ReleaseInstance(ctx context.Context, ex runtime.Executor, tenantID uuid.UUID, fence runtime.Fence, at time.Time) error {
	_, err := lease.Manager{}.Release(ctx, ex, lease.Fence{
		TenantID: tenantID, Resource: lease.Resource{Kind: fence.ResourceKind, ID: fence.ResourceID},
		LeaseID: fence.LeaseID, Holder: l.holder, Token: fence.Token,
	}, at)
	return err
}

var (
	deadDriver   = lease.Identity{WorkloadRef: "workload:hcmnext-execution", InstanceRef: "replica:dies-mid-drain"}
	driverTTL    = 30 * time.Second
	sweepHolderA = lease.Identity{WorkloadRef: "workload:hcmnext-serve-recovery", InstanceRef: "replica:sweeper-a"}
	sweepHolderB = lease.Identity{WorkloadRef: "workload:hcmnext-serve-recovery", InstanceRef: "replica:sweeper-b"}
)

// redeliveryHarness is one promotion instance parked at execute_promotion
// READY, plus every counter the exactly-once assertions read.
type redeliveryHarness struct {
	f        promotionFixture
	crash    *atomic.Bool
	performs *atomic.Int32
	clock    *atomic.Pointer[time.Time]
}

func newRedeliveryHarness(t *testing.T, key string) redeliveryHarness {
	t.Helper()
	f := newPromotionFixture(t, key)
	f.db.Exec(t, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.test.wfrun003.effect', 1, 'hcmnext.test.wfrun003.effect', 'PROTOBUF', 'LEDGER_EVENT')`, f.tenantID, wfrun003EffectSchema)
	h := redeliveryHarness{f: f, crash: &atomic.Bool{}, performs: &atomic.Int32{}, clock: &atomic.Pointer[time.Time]{}}
	at := f.at
	h.clock.Store(&at)
	return h
}

func (h redeliveryHarness) now() time.Time { return *h.clock.Load() }

func (h redeliveryHarness) setNow(at time.Time) { h.clock.Store(&at) }

// driver composes the served driver shape over conn: fenced by an instance
// lease held as holder, execute_promotion's effect guarded, crashing at
// boundary ("" never crashes).
func (h redeliveryHarness) driver(t *testing.T, conn dbport.Beginner, holder lease.Identity, boundary drainBoundary) *execute.Driver {
	t.Helper()
	registry, err := datalogger.NewLedgerEventDigestRegistry()
	if err != nil {
		t.Fatal(err)
	}
	db := crashDB{inner: conn, armed: h.crash}
	retention := idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour}
	steps := crashingSteps{crash: h.crash, boundary: boundary, next: wfrecover.GuardedSteps{
		Inner:   promotionRunner{payroll: "PASS", recon: "PASS"},
		Effects: promotionEffect{crash: h.crash, boundary: boundary, performs: h.performs},
		DB:      db, Store: idempotency.PostgresStore{}, Retention: retention,
		Verifier: lease.Fenced{},
	}}
	d, err := execute.New(execute.Options{
		DB: db, Steps: steps, Guard: idempotency.PostgresStore{}, Retention: retention,
		Terminal:        &effects.LedgerTerminalWriter{Appender: datalogger.NewAppender(registry), ProjectionName: "wfrun003_outcome", SourceRef: "hcmnext:test:wfrun003"},
		Repair:          &repairRequester{},
		Clock:           h.now,
		Instrumentation: crashingInstrumentation{crash: h.crash, boundary: boundary},
		Leases:          driverLeaser{holder: holder, ttl: driverTTL},
		FenceVerifier:   lease.Fenced{},
		// The shipped graph ends at the acknowledge_release SIGNAL node, so
		// a drain that survives the crash parks there. The served
		// subscriber opens that wait durably inside the advancement
		// transaction, exactly as the served composition wires it.
		Signals: platformexecution.SignalSubscriptions{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// sweeper composes the recovery sweep redelivering through driver.
func (h redeliveryHarness) sweeper(t *testing.T, conn dbport.Beginner, holder lease.Identity, driver *execute.Driver, fp wfrecover.Failpoint) wfrecover.Sweeper {
	t.Helper()
	s, err := wfrecover.NewSweeper(wfrecover.SweeperOptions{
		DB: conn, Holder: holder, LeaseTTL: time.Minute, Leases: lease.Manager{}, Failpoints: fp,
		Redeliver: wfrecover.RedelivererFunc(func(ctx context.Context, req wfrecover.Redelivery) error {
			_, err := driver.RedeliverReady(ctx, execute.RedeliverRequest{
				Start: h.f.start, InstanceID: req.Orphan.InstanceID, ExpectedInstanceVersion: req.Orphan.InstanceVersion,
			})
			return err
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// die runs Execute until the armed boundary kills it.
func (h redeliveryHarness) die(t *testing.T, boundary drainBoundary) runtime.Instance {
	t.Helper()
	_, err := h.driver(t, h.f.conn, deadDriver, boundary).Execute(context.Background(), execute.ExecuteRequest{Start: h.f.start})
	if !errors.Is(err, errProcessDied) {
		t.Fatalf("Execute at %s = %v, want the injected process death", boundary, err)
	}
	h.crash.Store(false)
	return instanceByTenant(t, h.f)
}

type leaseRow struct {
	state, holder string
	token         int64
	leaseID       uuid.UUID
}

func (h redeliveryHarness) latestLease(t *testing.T, instanceID uuid.UUID) leaseRow {
	t.Helper()
	var row leaseRow
	if err := h.f.db.Conn.QueryRow(context.Background(), `SELECT lease_state, holder_id, fence_token, lease_id FROM workflow_lease
		WHERE tenant_id = $1 AND resource_kind = 'WORKFLOW_INSTANCE' AND resource_id = $2 ORDER BY fence_token DESC LIMIT 1`,
		h.f.tenantID, instanceID.String()).Scan(&row.state, &row.holder, &row.token, &row.leaseID); err != nil {
		t.Fatalf("read instance lease: %v", err)
	}
	return row
}

func (h redeliveryHarness) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := h.f.db.Conn.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count (%s): %v", query, err)
	}
	return n
}

// durable is the durable evidence every assertion compares: effect rows,
// step idempotency records, execute_promotion advancement receipts and
// terminal ledger facts.
type durable struct{ effects, stepRecords, promotionReceipts, ledgerFacts int }

func (h redeliveryHarness) durable(t *testing.T, instanceID uuid.UUID) durable {
	t.Helper()
	return durable{
		effects:     h.count(t, `SELECT count(*) FROM outbox WHERE tenant_id = $1 AND schema_ref = $2`, h.f.tenantID, wfrun003EffectSchema),
		stepRecords: h.count(t, `SELECT count(*) FROM idempotency_record WHERE tenant_id = $1 AND idempotency_key LIKE 'wf-step-effect:%'`, h.f.tenantID),
		promotionReceipts: h.count(t, `SELECT count(*) FROM workflow_advancement_receipt WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3`,
			h.f.tenantID, instanceID, promotionexec.NodeExecutePromotion),
		ledgerFacts: h.count(t, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, h.f.tenantID),
	}
}

// openSubscriptions counts the OPEN signal subscriptions for nodeID: the
// durable proof a drain that survived the crash parked on the signal wait
// instead of losing the tail of the graph.
func (h redeliveryHarness) openSubscriptions(t *testing.T, instanceID uuid.UUID, nodeID string) int {
	t.Helper()
	return h.count(t, `SELECT count(*) FROM workflow_signal_subscription WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND subscription_state = 'OPEN'`,
		h.f.tenantID, instanceID, nodeID)
}

// signalContinuations counts the persisted continuation records for nodeID:
// the scheduler's wake-up path back to the parked wait.
func (h redeliveryHarness) signalContinuations(t *testing.T, instanceID uuid.UUID, nodeID string) int {
	t.Helper()
	return h.count(t, `SELECT count(*) FROM workflow_continuation WHERE tenant_id = $1 AND instance_id = $2 AND target_node_id = $3`,
		h.f.tenantID, instanceID, nodeID)
}

func (h redeliveryHarness) latestStatus(t *testing.T, instanceID uuid.UUID, nodeID string) string {
	t.Helper()
	var status string
	if err := h.f.db.Conn.QueryRow(context.Background(), `SELECT status FROM workflow_node_execution
		WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 ORDER BY attempt DESC LIMIT 1`,
		h.f.tenantID, instanceID, nodeID).Scan(&status); err != nil {
		t.Fatalf("read %s status: %v", nodeID, err)
	}
	return status
}

func (h redeliveryHarness) findOrphans(t *testing.T, at time.Time) []wfrecover.Orphan {
	t.Helper()
	ctx := context.Background()
	tx, err := h.f.conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, h.f.tenantID); err != nil {
		t.Fatal(err)
	}
	orphans, err := wfrecover.FindOrphans(ctx, tx, h.f.tenantID, at, 10)
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}
	return orphans
}

// TestTodo_WF_RUN_003_Redelivery kills the served drain at every persistence
// boundary it crosses and proves the production recovery sweep redelivers the
// instance exactly once: nothing is lost, a missing effect runs once, a
// committed effect or advancement is replayed rather than re-executed, and
// the lease the dead driver left is taken over and settled.
func TestTodo_WF_RUN_003_Redelivery(t *testing.T) {
	for _, tc := range []struct {
		boundary drainBoundary
		// crashed is the durable evidence the death leaves; openNode is the
		// node the orphan signature names; promotionStatus is
		// execute_promotion's latest status at death.
		crashed         durable
		openNode        string
		promotionStatus runtime.NodeStatus
	}{
		{boundaryBeforeEffect, durable{}, promotionexec.NodeExecutePromotion, runtime.NodeReady},
		{boundaryAfterEffect, durable{effects: 1, stepRecords: 1}, promotionexec.NodeExecutePromotion, runtime.NodeReady},
		{boundaryAfterAdvance, durable{effects: 1, stepRecords: 1, promotionReceipts: 1}, promotionexec.NodeObservePayroll, runtime.NodeSucceeded},
	} {
		t.Run(string(tc.boundary), func(t *testing.T) {
			ctx := context.Background()
			h := newRedeliveryHarness(t, "wfrun003-"+string(tc.boundary))
			inst := h.die(t, tc.boundary)

			if got := h.durable(t, inst.InstanceID); got != tc.crashed {
				t.Fatalf("durable evidence at death = %+v, want %+v", got, tc.crashed)
			}
			if got := h.latestStatus(t, inst.InstanceID, promotionexec.NodeExecutePromotion); got != string(tc.promotionStatus) {
				t.Fatalf("execute_promotion at death = %s, want %s", got, tc.promotionStatus)
			}
			dead := h.latestLease(t, inst.InstanceID)
			if dead.state != "HELD" || dead.holder != deadDriver.HolderID() {
				t.Fatalf("instance lease at death = %+v, want HELD by the dead driver", dead)
			}

			// A lease that has not lapsed is a live driver, never an orphan.
			if orphans := h.findOrphans(t, h.f.at.Add(driverTTL/2)); len(orphans) != 0 {
				t.Fatalf("orphans before the lease lapsed = %+v, want none", orphans)
			}
			sweepAt := h.f.at.Add(time.Minute)
			orphans := h.findOrphans(t, sweepAt)
			if len(orphans) != 1 || orphans[0].InstanceID != inst.InstanceID || len(orphans[0].Nodes) != 1 ||
				orphans[0].Nodes[0].NodeID != tc.openNode || orphans[0].InstanceVersion != inst.InstanceVersion {
				t.Fatalf("orphans after the lapse = %+v, want instance %s open at %s", orphans, inst.InstanceID, tc.openNode)
			}

			// A fresh process: a healthy driver behind the recovery sweep.
			h.setNow(sweepAt)
			recovering := h.driver(t, h.f.conn, sweepHolderA, "")
			receipt, err := h.sweeper(t, h.f.conn, sweepHolderA, recovering, nil).Sweep(ctx, h.f.tenantID, sweepAt)
			if err != nil || receipt.Count(wfrecover.SweepRedelivered) != 1 || len(receipt.Items) != 1 {
				t.Fatalf("Sweep = %+v, %v; want exactly one redelivery", receipt, err)
			}

			// The redelivered drain replays the committed effect instead of
			// re-performing it and parks on the shipped graph's SIGNAL tail:
			// the instance is still RUNNING with the acknowledgement node
			// WAITING, its wait is open, its continuation is durable, and no
			// terminal fact exists because no terminal ran.
			after := instanceByTenant(t, h.f)
			if after.RuntimeStatus != runtime.InstanceRunning {
				t.Fatalf("instance after redelivery = %s, want RUNNING: work was lost", after.RuntimeStatus)
			}
			if got := h.latestStatus(t, inst.InstanceID, promotionexec.NodeAcknowledgeRelease); got != string(runtime.NodeWaiting) {
				t.Fatalf("acknowledge_release after redelivery = %s, want WAITING", got)
			}
			if n := h.openSubscriptions(t, inst.InstanceID, promotionexec.NodeAcknowledgeRelease); n != 1 {
				t.Fatalf("open acknowledge_release subscriptions = %d, want 1: the park was not durable", n)
			}
			if n := h.signalContinuations(t, inst.InstanceID, promotionexec.NodeAcknowledgeRelease); n != 1 {
				t.Fatalf("acknowledge_release continuations = %d, want 1", n)
			}
			want := durable{effects: 1, stepRecords: 1, promotionReceipts: 1}
			if got := h.durable(t, inst.InstanceID); got != want {
				t.Fatalf("durable evidence after redelivery = %+v, want %+v (exactly one effect, record and advancement; no terminal fact before the wait clears)", got, want)
			}
			if n := h.performs.Load(); n != 1 {
				t.Fatalf("execute_promotion effect performed %d times across the death and the redelivery, want 1", n)
			}
			settled := h.latestLease(t, inst.InstanceID)
			if settled.state != "RELEASED" || settled.holder != sweepHolderA.HolderID() || settled.token <= dead.token {
				t.Fatalf("instance lease after redelivery = %+v, want RELEASED by the sweeper above token %d", settled, dead.token)
			}

			// Redelivered once means once: nothing is left to sweep.
			again, err := h.sweeper(t, h.f.conn, sweepHolderB, recovering, nil).Sweep(ctx, h.f.tenantID, sweepAt.Add(time.Hour))
			if err != nil || len(again.Items) != 0 {
				t.Fatalf("second sweep = %+v, %v; want nothing to redeliver", again, err)
			}
			if got := h.durable(t, inst.InstanceID); got != want || h.performs.Load() != 1 {
				t.Fatalf("second sweep moved durable evidence to %+v (performs %d)", got, h.performs.Load())
			}
		})
	}
}

// TestTodo_WF_RUN_003_RedeliveryFence proves the fence, not luck, keeps a
// superseded holder out: a sweeper that claims the orphan and dies before
// redelivering leaves a live lease that the dead driver's returning fence
// cannot write under -- it performs no effect -- and once the sweeper's own
// claim lapses a later sweep takes over again and finishes the work once.
func TestTodo_WF_RUN_003_RedeliveryFence(t *testing.T) {
	ctx := context.Background()
	h := newRedeliveryHarness(t, "wfrun003-fence")
	inst := h.die(t, boundaryBeforeEffect)
	dead := h.latestLease(t, inst.InstanceID)

	sweepAt := h.f.at.Add(time.Minute)
	h.setNow(sweepAt)
	recovering := h.driver(t, h.f.conn, sweepHolderA, "")
	crashAt := &wfrecover.CrashAt{Phase: wfrecover.SweepPhaseAfterClaimCommit}
	receipt, err := h.sweeper(t, h.f.conn, sweepHolderA, recovering, crashAt).Sweep(ctx, h.f.tenantID, sweepAt)
	if !errors.Is(err, wfrecover.ErrCrashed) || receipt.CrashedAt != wfrecover.SweepPhaseAfterClaimCommit {
		t.Fatalf("Sweep with a claim-commit crash = %+v, %v; want ErrCrashed at %s", receipt, err, wfrecover.SweepPhaseAfterClaimCommit)
	}
	claimed := h.latestLease(t, inst.InstanceID)
	if claimed.state != "HELD" || claimed.holder != sweepHolderA.HolderID() || claimed.token != dead.token+1 {
		t.Fatalf("lease after the sweeper died = %+v, want HELD by sweeper A at token %d", claimed, dead.token+1)
	}

	// The dead driver comes back with the fence it held.
	stale := runtime.Fence{
		ResourceKind: lease.ResourceWorkflowInstance, ResourceID: inst.InstanceID.String(),
		LeaseID: dead.leaseID, HolderID: dead.holder, Token: uint64(dead.token),
	}
	returning := h.driver(t, h.f.conn, deadDriver, "")
	_, err = returning.RedeliverReady(execute.WithFence(ctx, stale), execute.RedeliverRequest{
		Start: h.f.start, InstanceID: inst.InstanceID, ExpectedInstanceVersion: inst.InstanceVersion,
	})
	if !errors.Is(err, wfrecover.ErrFenceRefused) || lease.CodeOf(err) != lease.CodeFenceStale {
		t.Fatalf("returning driver = %v, want the step effect refused FENCE_STALE", err)
	}
	if got := h.durable(t, inst.InstanceID); got != (durable{}) || h.performs.Load() != 0 {
		t.Fatalf("a superseded driver wrote %+v (performs %d)", got, h.performs.Load())
	}

	// Sweeper A never comes back; its own claim lapses and B finishes.
	laterAt := sweepAt.Add(2 * time.Minute)
	h.setNow(laterAt)
	recoveringB := h.driver(t, h.f.conn, sweepHolderB, "")
	receipt, err = h.sweeper(t, h.f.conn, sweepHolderB, recoveringB, nil).Sweep(ctx, h.f.tenantID, laterAt)
	if err != nil || receipt.Count(wfrecover.SweepRedelivered) != 1 {
		t.Fatalf("Sweep after sweeper A's claim lapsed = %+v, %v", receipt, err)
	}
	if got, want := h.durable(t, inst.InstanceID), (durable{effects: 1, stepRecords: 1, promotionReceipts: 1}); got != want || h.performs.Load() != 1 {
		t.Fatalf("after the second takeover durable = %+v (performs %d), want %+v once", got, h.performs.Load(), want)
	}
	if final := h.latestLease(t, inst.InstanceID); final.state != "RELEASED" || final.holder != sweepHolderB.HolderID() || final.token != dead.token+2 {
		t.Fatalf("final lease = %+v, want RELEASED by sweeper B at token %d", final, dead.token+2)
	}
}

// TestTodo_WF_RUN_003_RedeliveryRace runs two sweepers on their own
// connections against the same orphan at the same instant and proves exactly
// one of them redelivers it: the other is refused by the lease or finds the
// work already settled, and the business effect happens once.
func TestTodo_WF_RUN_003_RedeliveryRace(t *testing.T) {
	ctx := context.Background()
	h := newRedeliveryHarness(t, "wfrun003-race")
	inst := h.die(t, boundaryAfterEffect)
	sweepAt := h.f.at.Add(time.Minute)
	h.setNow(sweepAt)

	holders := []lease.Identity{sweepHolderA, sweepHolderB}
	receipts := make([]wfrecover.SweepReceipt, len(holders))
	errs := make([]error, len(holders))
	sweepers := make([]wfrecover.Sweeper, len(holders))
	for i, holder := range holders {
		conn := appConn(t, h.f.db)
		sweepers[i] = h.sweeper(t, conn, holder, h.driver(t, conn, holder, ""), nil)
	}
	var start, done sync.WaitGroup
	start.Add(1)
	for i := range sweepers {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Wait()
			receipts[i], errs[i] = sweepers[i].Sweep(ctx, h.f.tenantID, sweepAt)
		}(i)
	}
	start.Done()
	done.Wait()

	redelivered := 0
	for i := range sweepers {
		if errs[i] != nil {
			t.Fatalf("sweeper %d = %+v, %v", i, receipts[i], errs[i])
		}
		redelivered += receipts[i].Count(wfrecover.SweepRedelivered)
		if n := receipts[i].Count(wfrecover.SweepFailed); n != 0 {
			t.Fatalf("sweeper %d failed %d orphans: %+v", i, n, receipts[i])
		}
	}
	if redelivered != 1 {
		t.Fatalf("concurrent sweepers redelivered %d times (%+v), want exactly 1", redelivered, receipts)
	}
	if got, want := h.durable(t, inst.InstanceID), (durable{effects: 1, stepRecords: 1, promotionReceipts: 1}); got != want || h.performs.Load() != 1 {
		t.Fatalf("durable after the race = %+v (performs %d), want %+v", got, h.performs.Load(), want)
	}
	if status := instanceByTenant(t, h.f).RuntimeStatus; status != runtime.InstanceRunning {
		t.Fatalf("instance after the race = %s, want RUNNING", status)
	}
	if got := h.latestStatus(t, inst.InstanceID, promotionexec.NodeAcknowledgeRelease); got != string(runtime.NodeWaiting) {
		t.Fatalf("acknowledge_release after the race = %s, want WAITING", got)
	}
}

// TestRedeliverReadyRefusesWhatIsNotLostWork proves a redelivery runs nothing
// for an instance that moved: a wrong expected version, an instance with no
// READY node on its frontier, and a malformed request are all refused before
// any step runs.
func TestRedeliverReadyRefusesWhatIsNotLostWork(t *testing.T) {
	ctx := context.Background()
	h := newRedeliveryHarness(t, "wfrun003-refusals")
	inst := instanceByTenant(t, h.f)
	d := h.driver(t, h.f.conn, sweepHolderA, "")

	if _, err := d.RedeliverReady(ctx, execute.RedeliverRequest{Start: h.f.start}); !errors.Is(err, execute.ErrInvalidConfiguration) {
		t.Fatalf("malformed redelivery = %v, want ErrInvalidConfiguration", err)
	}
	if _, err := d.RedeliverReady(ctx, execute.RedeliverRequest{Start: h.f.start, InstanceID: inst.InstanceID, ExpectedInstanceVersion: inst.InstanceVersion + 1}); !errors.Is(err, execute.ErrRedeliveryStale) {
		t.Fatalf("stale version redelivery = %v, want ErrRedeliveryStale", err)
	}
	if h.performs.Load() != 0 {
		t.Fatalf("a refused redelivery performed the effect %d times", h.performs.Load())
	}

	// A healthy redelivery of the READY frontier drains the graph and parks
	// on the shipped SIGNAL tail; a second one has nothing READY left and is
	// refused stale.
	result, err := d.RedeliverReady(ctx, execute.RedeliverRequest{Start: h.f.start, InstanceID: inst.InstanceID, ExpectedInstanceVersion: inst.InstanceVersion})
	if err != nil || result.Status != execute.StatusParked {
		t.Fatalf("RedeliverReady = %+v, %v; want PARKED on acknowledge_release", result, err)
	}
	done := instanceByTenant(t, h.f)
	if done.RuntimeStatus != runtime.InstanceRunning {
		t.Fatalf("instance after redelivery = %s, want RUNNING", done.RuntimeStatus)
	}
	if _, err := d.RedeliverReady(ctx, execute.RedeliverRequest{Start: h.f.start, InstanceID: inst.InstanceID, ExpectedInstanceVersion: done.InstanceVersion}); !errors.Is(err, execute.ErrRedeliveryStale) {
		t.Fatalf("redelivering a parked instance = %v, want ErrRedeliveryStale", err)
	}
	if h.performs.Load() != 1 {
		t.Fatalf("effect performed %d times, want 1", h.performs.Load())
	}
	if fence, ok := execute.FenceFromContext(ctx); ok {
		t.Fatalf("a bare context carries fence %+v", fence)
	}
}
