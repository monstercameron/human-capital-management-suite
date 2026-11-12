package scheduler

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	wfruntime "github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepswait "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// stepClock is a clock a test moves by hand. Nothing in these tests sleeps
// waiting for an instant: every decision is made against a reading the test
// supplied.
type stepClock struct {
	mu  sync.Mutex
	now time.Time
}

func newStepClock(at time.Time) *stepClock { return &stepClock{now: at} }

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stepClock) set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = at
}

// parked is one Promotion instance waiting on a durable timer.
type parked struct {
	instanceID uuid.UUID
	start      wfruntime.StartRequest
	subject    string
}

// promotionDispatcher is the composition-root half SVC-004 deliberately leaves
// outside this package: it turns one claimed ready-work row into an
// internal/workflow/execute Driver call, under the instance fence the claim was
// taken with.
//
// It is where the workflow semantics live -- the pinned plan, the wake
// requirement, the typed WAIT resolution -- which is exactly why the scheduler
// itself carries none of them.
type promotionDispatcher struct {
	t      *testing.T
	cell   promotionCell
	fireAt time.Time
	clock  *stepClock
	ledger *dispatchLedger
}

// dispatchLedger is the shared record of what a fleet dispatched. The
// dispatchers themselves are per replica (each over its own connection and its
// own cell, because two replicas are two processes); what has to be shared to
// make the assertion meaningful is only the count.
type dispatchLedger struct {
	mu     sync.Mutex
	calls  int
	byRow  map[uuid.UUID]int
	starts map[uuid.UUID]wfruntime.StartRequest
}

func newDispatchLedger() *dispatchLedger {
	return &dispatchLedger{byRow: map[uuid.UUID]int{}, starts: map[uuid.UUID]wfruntime.StartRequest{}}
}

func (l *dispatchLedger) register(p parked) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.starts[p.instanceID] = p.start
}

// begin records one dispatch and returns the start request the instance was
// created from.
func (l *dispatchLedger) begin(work Work) (wfruntime.StartRequest, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	l.byRow[work.Row.ReadyWorkID]++
	start, known := l.starts[work.Row.InstanceID]
	return start, known
}

func (l *dispatchLedger) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

func (l *dispatchLedger) perRow() map[uuid.UUID]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[uuid.UUID]int{}
	for k, v := range l.byRow {
		out[k] = v
	}
	return out
}

func newPromotionDispatcher(t *testing.T, cell promotionCell, fireAt time.Time,
	clock *stepClock, ledger *dispatchLedger,
) *promotionDispatcher {
	return &promotionDispatcher{t: t, cell: cell, fireAt: fireAt, clock: clock, ledger: ledger}
}

func (d *promotionDispatcher) Dispatch(ctx context.Context, work Work) (Disposition, error) {
	start, known := d.ledger.begin(work)
	if !known {
		return DispositionAbandoned, fmt.Errorf("no start request registered for instance %s", work.Row.InstanceID)
	}

	requirement := d.cell.waitRequirement(d.t)
	timerID := timer.TimerID(work.Row.TenantID, work.Row.InstanceID, work.Row.NodeID, requirement.Digest)

	instanceVersion, err := instanceVersionOf(ctx, d.cell.db, work.Row.TenantID, work.Row.InstanceID)
	if err != nil {
		return DispositionRetry, err
	}

	resolution, err := stepswait.Resolve(requirement, values.NewInstant(d.fireAt),
		stepswait.WakeEvent{Kind: stepswait.EventWake})
	if err != nil {
		return DispositionRetry, err
	}
	outcome := resolution.ToNodeOutcome(work.Row.NodeID)
	outcome.OutputDigest = "sha256:" + requirement.Digest[:32] + requirement.Digest[:32]

	at := d.clock.Now()
	fence := work.Fence.RuntimeFence(at)
	drv := d.cell.driver(d.t, &fence, at)
	result, err := drv.ResumeTimer(ctx, execute.ResumeTimerRequest{
		Start: start, InstanceID: work.Row.InstanceID, ExpectedInstanceVersion: instanceVersion,
		TimerID: timerID, Outcome: outcome, RecordedAt: at,
	})
	if err != nil {
		return DispositionRetry, err
	}
	if result.Status != execute.StatusComplete {
		return DispositionRetry, fmt.Errorf("resumed instance %s is %s, not COMPLETE", work.Row.InstanceID, result.Status)
	}
	return DispositionCompleted, nil
}

// instanceVersionOf reads one instance's optimistic version, which is what an
// advancement fences itself with.
func instanceVersionOf(ctx context.Context, db dbport.Beginner, tenantID, instanceID uuid.UUID) (int64, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return 0, err
	}
	var version int64
	if err := tx.QueryRow(ctx, `
		SELECT instance_version FROM workflow_instance
		WHERE tenant_id = $1 AND instance_id = $2`, tenantID, instanceID).Scan(&version); err != nil {
		return 0, err
	}
	return version, tx.Commit(ctx)
}

// newWorld brings up an isolated database with one ACTIVE tenant and the
// promotion wait plan published and ACTIVE, plus the composed cell a test
// starts instances through. Every replica in the test opens its own connection
// on the same database.
func newWorld(t *testing.T, key string) (*pgtest.DB, uuid.UUID, promotionCell) {
	t.Helper()
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, key, fixtureAt)
	versions, plan, activated := publishActiveWaitPlan(t, fixtureFireAt, fixtureAt)
	return db, tenantID, newPromotionCell(t, appConn(t, db), plan, versions, activated)
}

// park starts one Promotion instance and leaves it waiting on a durable timer.
// The start runs unfenced: the instance id is derived by Execute itself, so
// no WORKFLOW_INSTANCE lease can name it before the start. Every advancement
// after the start is fenced by the scheduler's claim on that instance.
func park(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, cell promotionCell, subject string, at time.Time) parked {
	t.Helper()
	ctx := context.Background()

	proposal := newDemoProposal(t, values.TenantId(subject), "intent:"+subject, subject, at)
	start := cell.startRequest(tenantID, proposal, subject, at)
	drv := cell.driver(t, nil, at)

	result, err := drv.Execute(ctx, execute.ExecuteRequest{Start: start})
	if err != nil {
		t.Fatalf("Execute %s: %v", subject, err)
	}
	if result.Status != execute.StatusParked {
		t.Fatalf("Execute %s status = %s, want PARKED on a durable timer", subject, result.Status)
	}
	if len(result.Timers) != 1 {
		t.Fatalf("Execute %s created %d timers, want exactly 1", subject, len(result.Timers))
	}
	return parked{instanceID: result.Start.InstanceID, start: start, subject: subject}
}

// newReplica builds one scheduler replica over its own connection.
func newReplica(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, name, queue string,
	dispatcher Dispatcher, clock *stepClock,
) (*Scheduler, *recordingLogger) {
	t.Helper()
	claim := claimFixture(tenantID, name)
	claim.Resource.ID = queue
	logger := &recordingLogger{}
	s, err := New(Config{
		DB:         appConn(t, db),
		Claims:     []lease.AcquireRequest{claim},
		Misfire:    catchUpOnce(),
		Dispatcher: dispatcher,
		Clock:      clock.Now,
		BatchSize:  8,
		QueueTTL:   30 * time.Second,
		Logger:     logger,
	})
	if err != nil {
		t.Fatalf("New replica %s: %v", name, err)
	}
	return s, logger
}

func countRows(t *testing.T, db *pgtest.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// ---------------------------------------------------------------------------
// SVC-004 PRIMARY
// ---------------------------------------------------------------------------

// TestTodo_SVC_004 is SVC-004's primary test: one scheduler replica advances a
// durable workflow frontier end to end without any caller driving it.
//
// The instance is started and parked on a real workflow_timer. Then nothing
// touches it except ticks: a tick before the wake instant settles nothing and
// claims nothing (a promise is not due because a scheduler happened to look at
// it); the tick at the wake instant fires the promise into a
// workflow_ready_work row, claims that row under the instance's own lease and a
// compare-and-swap on its version, dispatches it, and settles it DONE -- and
// the instance reaches its governed terminal write.
func TestTodo_SVC_004(t *testing.T) {
	ctx := context.Background()
	db, tenantID, cell := newWorld(t, "svc004-primary")
	clock := newStepClock(fixtureAt)
	ledger := newDispatchLedger()
	dispatcher := newPromotionDispatcher(t, cell.forConn(t, db), fixtureFireAt, clock, ledger)
	replica, logger := newReplica(t, db, tenantID, "replica:a", testQueue, dispatcher, clock)

	instance := park(t, db, tenantID, cell, "svc004-primary-1", fixtureAt)
	ledger.register(instance)

	var status string
	if err := db.QueryRow(ctx, `SELECT runtime_status FROM workflow_instance WHERE tenant_id = $1 AND instance_id = $2`,
		tenantID, instance.instanceID).Scan(&status); err != nil {
		t.Fatalf("read instance status: %v", err)
	}
	if !Admissible(status) {
		t.Fatalf("an instance parked on a timer is %s, which this scheduler would never publish work for", status)
	}

	// --- A tick an hour in: the promise is not due, so nothing happens. ---
	clock.set(fixtureAt.Add(time.Hour))
	early, err := replica.Tick(ctx)
	if err != nil {
		t.Fatalf("early tick: %v", err)
	}
	if early.Leased != 1 {
		t.Fatalf("the first tick leased %d queues, want 1", early.Leased)
	}
	if early.Fired != 0 || early.Claimed != 0 || ledger.count() != 0 {
		t.Fatalf("a tick before the wake instant fired %d, claimed %d and dispatched %d",
			early.Fired, early.Claimed, ledger.count())
	}
	if !early.Idle() {
		t.Fatal("a tick that found no due work does not report itself idle, so Run would spin")
	}
	if n := countRows(t, db, `SELECT count(*) FROM workflow_ready_work WHERE tenant_id = $1`, tenantID); n != 0 {
		t.Fatalf("%d ready-work rows before the wake instant, want none", n)
	}

	// --- The tick at the wake instant does the whole cycle. ---
	clock.set(fixtureFireAt)
	due, err := replica.Tick(ctx)
	if err != nil {
		t.Fatalf("due tick: %v", err)
	}
	if due.Fired != 1 {
		t.Fatalf("the due tick fired %d timers, want exactly 1", due.Fired)
	}
	if due.Selected != 1 || due.Claimed != 1 || due.Contended != 0 {
		t.Fatalf("the due tick selected %d, claimed %d, contended %d; want 1/1/0",
			due.Selected, due.Claimed, due.Contended)
	}
	if due.Completed != 1 || due.Retried != 0 || due.Abandoned != 0 {
		t.Fatalf("the due tick settled %d completed, %d retried, %d abandoned; want 1/0/0",
			due.Completed, due.Retried, due.Abandoned)
	}
	if ledger.count() != 1 {
		t.Fatalf("the dispatcher ran %d times, want exactly 1", ledger.count())
	}

	// --- Everything the tick decided is a durable row, not process memory. ---
	var timerState string
	if err := db.QueryRow(ctx, `SELECT timer_state FROM workflow_timer WHERE tenant_id = $1 AND instance_id = $2`,
		tenantID, instance.instanceID).Scan(&timerState); err != nil {
		t.Fatalf("read timer: %v", err)
	}
	if timerState != "FIRED" {
		t.Fatalf("timer state = %s, want FIRED", timerState)
	}

	var readyID uuid.UUID
	var readyState string
	if err := db.QueryRow(ctx, `
		SELECT ready_work_id, ready_state FROM workflow_ready_work
		WHERE tenant_id = $1 AND instance_id = $2`, tenantID, instance.instanceID).Scan(&readyID, &readyState); err != nil {
		t.Fatalf("read ready work: %v", err)
	}
	if readyState != runtimestate.ReadyDone {
		t.Fatalf("ready work settled as %s, want DONE", readyState)
	}

	if err := db.QueryRow(ctx, `SELECT runtime_status FROM workflow_instance WHERE tenant_id = $1 AND instance_id = $2`,
		tenantID, instance.instanceID).Scan(&status); err != nil {
		t.Fatalf("read instance status: %v", err)
	}
	if status != string(wfruntime.InstanceCompleted) {
		t.Fatalf("instance is %s, want COMPLETED", status)
	}
	if n := countRows(t, db, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, tenantID); n != 1 {
		t.Fatalf("%d ledger events, want exactly one governed business fact", n)
	}

	// The instance lease the claim took is given back, not left held: an
	// instance still leased by a replica that finished with it would be
	// unrecoverable until its window lapsed.
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		observed, err := cell.leases.Observe(ctx, tx, tenantID,
			lease.Resource{Kind: lease.ResourceWorkflowInstance, ID: instance.instanceID.String()}, fixtureFireAt)
		if err != nil {
			return err
		}
		if observed.Held {
			t.Fatalf("the completed instance is still leased by %s", observed.HolderID)
		}
		return nil
	})

	// --- A further tick finds nothing: DONE work is not republished. ---
	after, err := replica.Tick(ctx)
	if err != nil {
		t.Fatalf("trailing tick: %v", err)
	}
	if !after.Idle() || ledger.count() != 1 {
		t.Fatalf("a trailing tick republished settled work: %+v, dispatches=%d", after, ledger.count())
	}
	if logger.count("scheduler.ready_work_claimed") != 1 {
		t.Fatalf("%d claims logged, want exactly 1", logger.count("scheduler.ready_work_claimed"))
	}
}

// ---------------------------------------------------------------------------
// SVC-004 INTEGRATION
// ---------------------------------------------------------------------------

// TestTodo_SVC_004_Integration is SVC-004's integration test: two scheduler
// replicas over one database, one Promotion instance with a WAIT timer,
// exactly one dispatch of the one logical execution, and the instance
// completes.
//
// The replicas are real: two independent connections, two Schedulers, two
// holder identities, both configured for the same tenant and the same queue.
// They are ticked alternately so the sequence is deterministic and the
// assertions are about the durable outcome rather than about who won a race.
// The race itself is TestTodo_SVC_004_Race.
func TestTodo_SVC_004_Integration(t *testing.T) {
	ctx := context.Background()
	db, tenantID, cell := newWorld(t, "svc004-integration")
	clock := newStepClock(fixtureAt)
	ledger := newDispatchLedger()

	replicaA, _ := newReplica(t, db, tenantID, "replica:a", testQueue,
		newPromotionDispatcher(t, cell.forConn(t, db), fixtureFireAt, clock, ledger), clock)
	replicaB, _ := newReplica(t, db, tenantID, "replica:b", testQueue,
		newPromotionDispatcher(t, cell.forConn(t, db), fixtureFireAt, clock, ledger), clock)

	instance := park(t, db, tenantID, cell, "svc004-integration-1", fixtureAt)
	ledger.register(instance)

	// --- Both replicas tick before the wake instant. Exactly one of them
	//     holds the queue; the other is refused, which is the fleet working. ---
	clock.set(fixtureAt.Add(time.Hour))
	firstA, err := replicaA.Tick(ctx)
	if err != nil {
		t.Fatalf("replica A early tick: %v", err)
	}
	firstB, err := replicaB.Tick(ctx)
	if err != nil {
		t.Fatalf("replica B early tick: %v", err)
	}
	if firstA.Leased+firstB.Leased != 1 || firstA.Refused+firstB.Refused != 1 {
		t.Fatalf("queue leases: A=%+v B=%+v; want exactly one holder and one refusal", firstA, firstB)
	}

	// --- Both tick at the wake instant. Between them the promise fires once
	//     and the woken work is dispatched once. ---
	clock.set(fixtureFireAt)
	dueA, err := replicaA.Tick(ctx)
	if err != nil {
		t.Fatalf("replica A due tick: %v", err)
	}
	dueB, err := replicaB.Tick(ctx)
	if err != nil {
		t.Fatalf("replica B due tick: %v", err)
	}
	if fired := dueA.Fired + dueB.Fired; fired != 1 {
		t.Fatalf("the fleet fired the one promise %d times", fired)
	}
	if claimed := dueA.Claimed + dueB.Claimed; claimed != 1 {
		t.Fatalf("the fleet claimed the one unit of work %d times", claimed)
	}
	if completed := dueA.Completed + dueB.Completed; completed != 1 {
		t.Fatalf("the fleet settled %d completions, want 1", completed)
	}
	if ledger.count() != 1 {
		t.Fatalf("the fleet dispatched the one logical execution %d times", ledger.count())
	}

	// --- Both tick again. Nothing is republished. ---
	afterA, err := replicaA.Tick(ctx)
	if err != nil {
		t.Fatalf("replica A trailing tick: %v", err)
	}
	afterB, err := replicaB.Tick(ctx)
	if err != nil {
		t.Fatalf("replica B trailing tick: %v", err)
	}
	if afterA.Claimed+afterB.Claimed != 0 || ledger.count() != 1 {
		t.Fatalf("a trailing round republished work: A=%+v B=%+v dispatches=%d", afterA, afterB, ledger.count())
	}

	// --- The instance completed, once, with one governed business fact. ---
	var status string
	if err := db.QueryRow(ctx, `SELECT runtime_status FROM workflow_instance WHERE tenant_id = $1 AND instance_id = $2`,
		tenantID, instance.instanceID).Scan(&status); err != nil {
		t.Fatalf("read instance status: %v", err)
	}
	if status != string(wfruntime.InstanceCompleted) {
		t.Fatalf("instance is %s after two replicas ran it, want COMPLETED", status)
	}
	if n := countRows(t, db, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, tenantID); n != 1 {
		t.Fatalf("%d ledger events, want exactly one", n)
	}
	if n := countRows(t, db, `
		SELECT count(*) FROM workflow_ready_work WHERE tenant_id = $1 AND ready_state <> $2`,
		tenantID, runtimestate.ReadyDone); n != 0 {
		t.Fatalf("%d ready-work rows are not DONE", n)
	}
	if n := countRows(t, db, `SELECT count(*) FROM workflow_timer WHERE tenant_id = $1 AND timer_state <> 'FIRED'`,
		tenantID); n != 0 {
		t.Fatalf("%d timers are not FIRED", n)
	}
}

// ---------------------------------------------------------------------------
// SVC-004 RACE
// ---------------------------------------------------------------------------

// TestTodo_SVC_004_Race is SVC-004's race test: two scheduler replicas on
// genuinely separate connections, ticking concurrently against one database
// over several parked instances, must between them dispatch each logical
// execution exactly once.
//
// This environment builds without -race (Go 1.26.3 windows/arm64 has no cgo
// toolchain here), so the contention this test creates is real database
// contention rather than a Go data race: two sessions selecting the same rows
// at the same instant, taking the same instance leases and racing the same
// compare-and-swap versions. That is the concurrency SVC-004's RED clause is
// about -- "two scheduler processes publish the same logical execution" is a
// property of the rows, not of the heap.
func TestTodo_SVC_004_Race(t *testing.T) {
	ctx := context.Background()
	db, tenantID, cell := newWorld(t, "svc004-race")
	clock := newStepClock(fixtureAt)
	ledger := newDispatchLedger()

	const instances = 4
	parkedInstances := make([]parked, 0, instances)
	for i := 0; i < instances; i++ {
		p := park(t, db, tenantID, cell, fmt.Sprintf("svc004-race-%d", i), fixtureAt)
		ledger.register(p)
		parkedInstances = append(parkedInstances, p)
	}

	replicaA, _ := newReplica(t, db, tenantID, "replica:a", testQueue,
		newPromotionDispatcher(t, cell.forConn(t, db), fixtureFireAt, clock, ledger), clock)
	replicaB, _ := newReplica(t, db, tenantID, "replica:b", testQueue,
		newPromotionDispatcher(t, cell.forConn(t, db), fixtureFireAt, clock, ledger), clock)

	clock.set(fixtureFireAt)

	// Both replicas tick repeatedly and concurrently until every instance has
	// been drained. Ticks that fail (a lease taken over, a version raced away)
	// are the expected outcome of contention and are retried, exactly as
	// Scheduler.Run would.
	var wg sync.WaitGroup
	for _, replica := range []*Scheduler{replicaA, replicaB} {
		wg.Add(1)
		go func(s *Scheduler) {
			defer wg.Done()
			for round := 0; round < 12; round++ {
				if _, err := s.Tick(ctx); err != nil {
					continue
				}
			}
		}(replica)
	}
	wg.Wait()

	// --- Every logical execution ran exactly once. ---
	for readyID, calls := range ledger.perRow() {
		if calls != 1 {
			t.Errorf("ready work %s was dispatched %d times", readyID, calls)
		}
	}
	if got := ledger.count(); got != instances {
		t.Fatalf("%d dispatches for %d instances", got, instances)
	}
	if n := countRows(t, db, `SELECT count(*) FROM workflow_ready_work WHERE tenant_id = $1`, tenantID); n != instances {
		t.Fatalf("%d ready-work rows for %d instances; a duplicate publication would show up here", n, instances)
	}
	if n := countRows(t, db, `
		SELECT count(*) FROM workflow_ready_work WHERE tenant_id = $1 AND ready_state = $2`,
		tenantID, runtimestate.ReadyDone); n != instances {
		t.Fatalf("%d of %d ready-work rows are DONE", n, instances)
	}
	if n := countRows(t, db, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, tenantID); n != instances {
		t.Fatalf("%d ledger events for %d instances, want exactly one each", n, instances)
	}
	for _, p := range parkedInstances {
		var status string
		if err := db.QueryRow(ctx, `SELECT runtime_status FROM workflow_instance WHERE tenant_id = $1 AND instance_id = $2`,
			tenantID, p.instanceID).Scan(&status); err != nil {
			t.Fatalf("read instance status: %v", err)
		}
		if status != string(wfruntime.InstanceCompleted) {
			t.Errorf("instance %s is %s, want COMPLETED", p.subject, status)
		}
	}
	// No instance is left leased by either replica.
	if n := countRows(t, db, `
		SELECT count(*) FROM workflow_lease
		WHERE tenant_id = $1 AND resource_kind = 'WORKFLOW_INSTANCE' AND lease_state = 'HELD'`, tenantID); n != 0 {
		t.Fatalf("%d instance leases are still held after every instance completed", n)
	}
}
