package workflowcontrol

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var fixedNow = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

// fixture is one tenant, an app-role connection and the promotion execute
// plan every instance is pinned to.
type fixture struct {
	t      *testing.T
	db     *pgtest.DB
	tenant uuid.UUID
	key    values.TenantId
	conn   *pgxadapter.Conn
	plan   *workflow.CompiledWorkflow
	auth   *resolver
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := pgtest.New(t)
	return newFixtureOn(t, db, "ctl-"+uuid.NewString()[:8])
}

func newFixtureOn(t *testing.T, db *pgtest.DB, key string) *fixture {
	t.Helper()
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenant, key, "tenant "+key)
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatalf("compile promotion plan: %v", err)
	}
	f := &fixture{t: t, db: db, tenant: tenant, key: values.TenantId(key), conn: f0conn(t, db), plan: plan}
	f.auth = &resolver{f: f, dual: true, simulate: true, role: jit.RoleIntegrityRepair}
	return f
}

func f0conn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume app role: %v", err)
	}
	return conn
}

func (f *fixture) tx(fn func(tx dbport.Tx) error) {
	f.t.Helper()
	ctx := context.Background()
	tx, err := f.conn.Begin(ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, f.tenant); err != nil {
		f.t.Fatal(err)
	}
	if err := fn(tx); err != nil {
		f.t.Fatalf("fixture tx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		f.t.Fatal(err)
	}
}

// node is one node execution a fixture instance starts with.
type node struct {
	id     string
	status []runtime.NodeStatus // path of statuses from READY
}

// instance creates a RUNNING instance at frontier with the given node history.
func (f *fixture) instance(frontier []string, nodes ...node) (uuid.UUID, int64) {
	f.t.Helper()
	ctx := context.Background()
	inst, err := runtime.NewInstance(f.tenant, uuid.New(), "cell-local", f.plan, workflow.ModeExecute, "sha256:input", "corr", fixedNow)
	if err != nil {
		f.t.Fatalf("NewInstance: %v", err)
	}
	var version int64
	f.tx(func(tx dbport.Tx) error {
		store := runtime.Store{}
		stored, err := store.CreateInstance(ctx, tx, inst)
		if err != nil {
			return err
		}
		running, err := store.RecordInstanceState(ctx, tx, runtime.InstanceTransition{
			TenantID: f.tenant, InstanceID: inst.InstanceID, ExpectedVersion: stored.InstanceVersion,
			Status: runtime.InstanceRunning, CurrentNodeIDs: frontier,
		})
		if err != nil {
			return err
		}
		version = running.InstanceVersion
		for _, n := range nodes {
			cn, _ := f.plan.Node(n.id)
			exec := runtime.NewNodeExecution(f.tenant, inst.InstanceID, n.id, 1, cn.Type, runtime.NodeReady)
			exec.RecordedAt = fixedNow
			if _, version, err = store.RecordNodeExecution(ctx, tx, exec, version); err != nil {
				return err
			}
			for _, s := range n.status {
				if _, version, err = store.RecordNodeTransition(ctx, tx, runtime.NodeTransition{
					TenantID: f.tenant, InstanceID: inst.InstanceID, NodeID: n.id, Attempt: 1,
					ExpectedInstanceVersion: version, Status: s,
				}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return inst.InstanceID, version
}

func (f *fixture) load(id uuid.UUID) (runtime.Instance, []runtime.NodeExecution) {
	f.t.Helper()
	var inst runtime.Instance
	var nodes []runtime.NodeExecution
	f.tx(func(tx dbport.Tx) error {
		var err error
		if inst, err = (runtime.Store{}).LoadInstance(context.Background(), tx, f.tenant, id); err != nil {
			return err
		}
		nodes, err = (runtime.Store{}).LoadNodeExecutions(context.Background(), tx, f.tenant, id)
		return err
	})
	return inst, nodes
}

func (f *fixture) readyWork(id uuid.UUID, nodeID string) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRow(context.Background(), `SELECT count(*) FROM workflow_ready_work WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3`,
		f.tenant, id, nodeID).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// resolver hands out current authority for a control.
type resolver struct {
	f        *fixture
	mu       sync.Mutex
	dual     bool
	simulate bool
	role     jit.Role
	tenant   values.TenantId
	fail     error
}

func (r *resolver) ResolveAuthority(_ context.Context, tenant values.TenantId, op string, kind operator.Kind, instanceID string) (Authority, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return Authority{}, r.fail
	}
	grantTenant := tenant
	if r.tenant != "" {
		grantTenant = r.tenant
	}
	g, err := jit.New("jit-"+uuid.NewString(), jit.Request{Principal: op, Tenant: grantTenant, Role: r.role, TicketRef: "INC-1",
		Justification: "stuck promotion", Capabilities: []string{string(kind)}, Purpose: "workflow control", TTL: time.Hour},
		jit.Approval{Approver: "approver:lead", At: fixedNow}, fixedNow)
	if err != nil {
		return Authority{}, err
	}
	a := Authority{JIT: g}
	if r.dual {
		a.SecondApprover = "operator:second"
	}
	if r.simulate {
		cmd := lastCommand(instanceID)
		a.Simulation = &operator.Simulation{Digest: "sha256:sim", Scope: cmd, At: fixedNow.Add(-time.Minute)}
	}
	return a, nil
}

// lastCommand rebuilds the scope a simulation covers from the instance id the
// resolver is asked about; retry scopes are set by the test through scopeFor.
var scopeOverride sync.Map

func lastCommand(instanceID string) operator.Scope {
	if s, ok := scopeOverride.Load(instanceID); ok {
		return s.(operator.Scope)
	}
	return operator.Scope{Resource: "workflow_instance", IDs: []string{instanceID}}
}

type plans struct{ plan *workflow.CompiledWorkflow }

func (p plans) ResolvePlan(context.Context, runtime.Executor, runtime.Instance) (*workflow.CompiledWorkflow, error) {
	return p.plan, nil
}

func (f *fixture) controller(journal operator.Journal) *Controller {
	f.t.Helper()
	return f.controllerWith(journal, plans{f.plan}, f.conn)
}

func (f *fixture) controllerWith(journal operator.Journal, pr PlanResolver, db dbport.Beginner) *Controller {
	f.t.Helper()
	c, err := New(db, journal, pr, f.auth, func() time.Time { return fixedNow })
	if err != nil {
		f.t.Fatal(err)
	}
	return c
}

func (f *fixture) cmd(id uuid.UUID, version int64, key string) Command {
	return Command{TenantID: f.tenant, Tenant: f.key, InstanceID: id, ExpectedVersion: version,
		IdempotencyKey: key, ReasonRef: "INC-1", Operator: "operator:ana"}
}

func retryCmd(f *fixture, id uuid.UUID, nodeID string, attempt int, key string) Command {
	c := f.cmd(id, 0, key)
	c.NodeID, c.ExpectedAttempt = nodeID, attempt
	scopeOverride.Store(id.String(), c.Scope(operator.KindWorkflowRetryNode))
	return c
}

func expect(t *testing.T, what string, got Result, err error, outcome Outcome, code string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: unexpected error %v", what, err)
	}
	if got.Outcome != outcome || (code != "" && got.Code != code) {
		t.Fatalf("%s = %+v, want %s %s", what, got, outcome, code)
	}
}

// TestWorkflowControlEndpointsRespectSafePointAuthorityIdempotencyAndEffectBoundary
// is EP-WF-002's primary proof over PostgreSQL: pause waits for a safe point,
// resume revalidates, cancel refuses to claim a reversal after a committed
// effect and routes an in-flight one to repair, retry re-runs exactly one
// failed idempotent attempt, and every repeat returns the recorded outcome
// without a second transition.
func TestWorkflowControlEndpointsRespectSafePointAuthorityIdempotencyAndEffectBoundary(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	journal := operator.NewMemoryJournal()
	c := f.controller(journal)

	// Pause while a node is RUNNING: recorded, pending the safe point.
	busy, v := f.instance([]string{"evaluate_band"}, node{"evaluate_band", []runtime.NodeStatus{runtime.NodeRunning}})
	res, err := c.Pause(ctx, f.cmd(busy, v, "pause-busy"))
	expect(t, "pause at an unsafe boundary", res, err, OutcomePendingSafePoint, "")
	if res.InstanceStatus != runtime.InstancePauseRequested || res.NodeID != "evaluate_band" || res.IntentInstanceID == "" {
		t.Fatalf("pending pause = %+v", res)
	}
	again, err := c.Pause(ctx, f.cmd(busy, v, "pause-busy"))
	expect(t, "repeat pause", again, err, OutcomePendingSafePoint, "")
	if !again.Replayed || again.InstanceVersion != res.InstanceVersion {
		t.Fatalf("repeat pause = %+v, want the recorded outcome replayed", again)
	}
	if inst, _ := f.load(busy); inst.InstanceVersion != res.InstanceVersion {
		t.Fatalf("a repeated pause moved the instance version %d -> %d", res.InstanceVersion, inst.InstanceVersion)
	}

	// Pause at a safe point: applied; resume then revalidates.
	idle, v := f.instance([]string{"approve_manager"})
	paused, err := c.Pause(ctx, f.cmd(idle, v, "pause-idle"))
	expect(t, "pause at a safe point", paused, err, OutcomeApplied, "")
	stale, err := c.Resume(ctx, f.cmd(idle, v, "resume-stale"))
	expect(t, "resume with a stale version", stale, err, OutcomeDenied, runtime.CodeStaleInstance)
	resumed, err := c.Resume(ctx, f.cmd(idle, paused.InstanceVersion, "resume"))
	expect(t, "resume", resumed, err, OutcomeApplied, "")
	if resumed.InstanceStatus != runtime.InstanceRunning {
		t.Fatalf("resumed status = %s", resumed.InstanceStatus)
	}
	notPaused, err := c.Resume(ctx, f.cmd(idle, resumed.InstanceVersion, "resume-again"))
	expect(t, "resume a running instance", notPaused, err, OutcomeDenied, runtime.CodeNotPaused)

	// Cancel after the business effect committed: too late, nothing claimed.
	committed, v := f.instance([]string{"observe_payroll"}, node{"execute_promotion", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeSucceeded}})
	late, err := c.Cancel(ctx, f.cmd(committed, v, "cancel-committed"))
	expect(t, "cancel after a committed effect", late, err, OutcomeTooLate, CodeEffectCommitted)
	if inst, _ := f.load(committed); inst.RuntimeStatus != runtime.InstanceRunning {
		t.Fatalf("a too-late cancel changed the instance to %s", inst.RuntimeStatus)
	}
	// Cancel with the effect in flight: routed to repair.
	inflight, v := f.instance([]string{"execute_promotion"}, node{"execute_promotion", []runtime.NodeStatus{runtime.NodeRunning}})
	repair, err := c.Cancel(ctx, f.cmd(inflight, v, "cancel-inflight"))
	expect(t, "cancel with an in-flight effect", repair, err, OutcomeRepairRequired, CodeEffectInFlight)
	if repair.InstanceStatus != runtime.InstanceRepairRequired {
		t.Fatalf("in-flight cancel status = %s", repair.InstanceStatus)
	}
	// A clean cancel, then a second cancel on history.
	clean, v := f.instance([]string{"approve_manager"})
	done, err := c.Cancel(ctx, f.cmd(clean, v, "cancel-clean"))
	expect(t, "clean cancel", done, err, OutcomeApplied, "")
	if done.InstanceStatus != runtime.InstanceCancelled {
		t.Fatalf("clean cancel status = %s", done.InstanceStatus)
	}
	tooLate, err := c.Cancel(ctx, f.cmd(clean, done.InstanceVersion, "cancel-twice"))
	expect(t, "cancel a cancelled instance", tooLate, err, OutcomeTooLate, CodeInstanceTerminal)

	// RetryNode: a successful node is not retried.
	ok, _ := f.instance([]string{"observe_payroll"}, node{"execute_promotion", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeSucceeded}})
	r, err := c.RetryNode(ctx, retryCmd(f, ok, "execute_promotion", 1, "retry-success"))
	expect(t, "retry a successful node", r, err, OutcomeDenied, CodeNodeNotFailed)

	// A failed idempotent node is retried exactly once.
	failed, _ := f.instance([]string{"execute_promotion"}, node{"execute_promotion", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeFailed}})
	retried, err := c.RetryNode(ctx, retryCmd(f, failed, "execute_promotion", 1, "retry"))
	expect(t, "retry a failed idempotent node", retried, err, OutcomeApplied, "")
	if retried.Attempt != 2 {
		t.Fatalf("retry attempt = %d, want 2", retried.Attempt)
	}
	repeat, err := c.RetryNode(ctx, retryCmd(f, failed, "execute_promotion", 1, "retry"))
	expect(t, "repeat retry", repeat, err, OutcomeApplied, "")
	if !repeat.Replayed {
		t.Fatal("a repeated retry was not a replay")
	}
	_, nodes := f.load(failed)
	statuses := map[int]runtime.NodeStatus{}
	for _, n := range nodes {
		statuses[n.Attempt] = n.Status
	}
	if len(nodes) != 2 || statuses[1] != runtime.NodeRetrying || statuses[2] != runtime.NodeReady || f.readyWork(failed, "execute_promotion") != 1 {
		t.Fatalf("after retry: attempts %v, ready work %d; want attempt 1 RETRYING, attempt 2 READY, one ready work row", statuses, f.readyWork(failed, "execute_promotion"))
	}
	staleAttempt, err := c.RetryNode(ctx, retryCmd(f, failed, "execute_promotion", 1, "retry-stale"))
	expect(t, "retry the superseded attempt", staleAttempt, err, OutcomeDenied, CodeAttemptStale)

	// A failed node whose lease is still held is not retried.
	leased, _ := f.instance([]string{"execute_promotion"}, node{"execute_promotion", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeFailed}})
	f.tx(func(tx dbport.Tx) error {
		_, err := lease.Manager{}.Acquire(ctx, tx, lease.AcquireRequest{TenantID: f.tenant,
			Resource: lease.Resource{Kind: lease.ResourceNodeExecution, ID: leased.String() + "/execute_promotion"},
			Holder:   lease.Identity{WorkloadRef: "workload:hcmnext-workflow-runtime", InstanceRef: "replica:1"}, Now: fixedNow, TTL: time.Hour})
		return err
	})
	held, err := c.RetryNode(ctx, retryCmd(f, leased, "execute_promotion", 1, "retry-leased"))
	expect(t, "retry a leased node", held, err, OutcomeDenied, CodeNodeLeased)

	// A failed node whose write is not idempotent goes to repair, not re-run.
	nonIdem := *f.plan
	nonIdem.Nodes = slices.Clone(f.plan.Nodes)
	for i := range nonIdem.Nodes {
		if nonIdem.Nodes[i].ID == "execute_promotion" {
			capCopy := *nonIdem.Nodes[i].Capability
			capCopy.IdempotencyKeyMapping = ""
			nonIdem.Nodes[i].Capability = &capCopy
		}
	}
	unsafe := f.controllerWith(operator.NewMemoryJournal(), plans{&nonIdem}, f.conn)
	nonIdemFailed, _ := f.instance([]string{"execute_promotion"}, node{"execute_promotion", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeFailed}})
	routed, err := unsafe.RetryNode(ctx, retryCmd(f, nonIdemFailed, "execute_promotion", 1, "retry-nonidem"))
	expect(t, "retry a non-idempotent write", routed, err, OutcomeRepairRequired, CodeNonIdempotentRetry)
	if _, nodes := f.load(nonIdemFailed); len(nodes) != 1 || nodes[0].Status != runtime.NodeFailed {
		t.Fatalf("a non-idempotent retry wrote attempts %+v", nodes)
	}

	// Authority: without dual control a cancel is denied and changes nothing.
	f.auth.dual = false
	undual, v := f.instance([]string{"approve_manager"})
	d, err := c.Cancel(ctx, f.cmd(undual, v, "cancel-no-dual"))
	expect(t, "cancel without dual control", d, err, OutcomeDenied, operator.CodeDualControlRequired)
	if inst, _ := f.load(undual); inst.RuntimeStatus != runtime.InstanceRunning {
		t.Fatalf("a denied cancel changed the instance to %s", inst.RuntimeStatus)
	}
	f.auth.dual = true

	// Every applied control left exactly one evidence receipt.
	for _, rec := range journal.Receipts() {
		if rec.Verify() != nil || rec.AuthorityKind != operator.AuthorityJIT || rec.Outcome != operator.OutcomeApplied {
			t.Errorf("journal receipt %+v", rec)
		}
	}
}

// TestTodo_EP_WF_002_Race proves concurrent controls on one instance produce
// exactly one runtime transition: sixteen retries of one failed attempt under
// distinct keys create one new attempt, and the rest are refused or failed
// without writing.
func TestTodo_EP_WF_002_Race(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	c := f.controller(operator.NewMemoryJournal())
	failed, _ := f.instance([]string{"execute_promotion"}, node{"execute_promotion", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeFailed}})
	retryCmd(f, failed, "execute_promotion", 1, "seed")
	var wg sync.WaitGroup
	results := make([]Result, 16)
	errs := make([]error, 16)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn := f0conn(t, f.db)
			ctl := f.controllerWith(c.journal(), plans{f.plan}, conn)
			cmd := f.cmd(failed, 0, fmt.Sprintf("race-%d", i))
			cmd.NodeID, cmd.ExpectedAttempt = "execute_promotion", 1
			results[i], errs[i] = ctl.RetryNode(ctx, cmd)
		}()
	}
	wg.Wait()
	applied := 0
	for i := range results {
		if errs[i] == nil && results[i].Outcome == OutcomeApplied {
			applied++
		}
	}
	_, nodes := f.load(failed)
	if applied != 1 || len(nodes) != 2 || f.readyWork(failed, "execute_promotion") != 1 {
		t.Fatalf("concurrent retries applied %d, attempts %d, ready work %d; want exactly one of each", applied, len(nodes), f.readyWork(failed, "execute_promotion"))
	}
}

// journal exposes the controller's journal for recomposition in tests.
func (c *Controller) journal() operator.Journal { return c.journalRef }

// TestTodo_EP_WF_002_Fault covers dependency faults: a plan or authority
// failure changes nothing, a journal failure after the transition commits is
// never re-run, and malformed commands and wiring are refused.
func TestTodo_EP_WF_002_Fault(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	boom := errors.New("dependency down")

	id, v := f.instance([]string{"approve_manager"})
	badPlans := f.controllerWith(operator.NewMemoryJournal(), failingPlans{boom}, f.conn)
	if _, err := badPlans.Pause(ctx, f.cmd(id, v, "pause-plan-fault")); !errors.Is(err, boom) {
		t.Fatalf("plan fault = %v", err)
	}
	if inst, _ := f.load(id); inst.RuntimeStatus != runtime.InstanceRunning {
		t.Fatalf("a plan fault changed the instance to %s", inst.RuntimeStatus)
	}
	other, err := promotionexec.CompileSimulation()
	if err != nil {
		t.Fatal(err)
	}
	wrongPlan := f.controllerWith(operator.NewMemoryJournal(), plans{other}, f.conn)
	if r, err := wrongPlan.Pause(ctx, f.cmd(id, v, "pause-wrong-plan")); err != nil || r.Code != runtime.CodeAdvancePlanMismatch {
		t.Fatalf("wrong plan = %+v, %v", r, err)
	}
	f.auth.fail = boom
	if _, err := f.controller(operator.NewMemoryJournal()).Pause(ctx, f.cmd(id, v, "pause-auth-fault")); !errors.Is(err, boom) {
		t.Fatalf("authority fault = %v", err)
	}
	f.auth.fail = nil

	// The transition commits, then recording its outcome fails: the retry of
	// the same key is REPAIR_REQUIRED and the instance was paused exactly once.
	journal := &flakyJournal{MemoryJournal: operator.NewMemoryJournal(), failComplete: true}
	flaky := f.controller(journal)
	if _, err := flaky.Pause(ctx, f.cmd(id, v, "pause-flaky")); err == nil {
		t.Fatal("a journal failure after commit was reported as success")
	}
	paused, _ := f.load(id)
	journal.failComplete = false
	again, err := flaky.Pause(ctx, f.cmd(id, v, "pause-flaky"))
	expect(t, "retry after a lost outcome", again, err, OutcomeRepairRequired, operator.CodeRepairRequired)
	if now, _ := f.load(id); now.InstanceVersion != paused.InstanceVersion || now.RuntimeStatus != runtime.InstancePaused {
		t.Fatalf("the retry re-ran the pause: %s v%d -> %s v%d", paused.RuntimeStatus, paused.InstanceVersion, now.RuntimeStatus, now.InstanceVersion)
	}

	for name, cmd := range map[string]Command{
		"no tenant":   {InstanceID: id, ExpectedVersion: 1, IdempotencyKey: "k", ReasonRef: "r", Operator: "o"},
		"no instance": {TenantID: f.tenant, Tenant: f.key, ExpectedVersion: 1, IdempotencyKey: "k", ReasonRef: "r", Operator: "o"},
		"no key":      {TenantID: f.tenant, Tenant: f.key, InstanceID: id, ExpectedVersion: 1, ReasonRef: "r", Operator: "o"},
		"no reason":   {TenantID: f.tenant, Tenant: f.key, InstanceID: id, ExpectedVersion: 1, IdempotencyKey: "k", Operator: "o"},
		"no operator": {TenantID: f.tenant, Tenant: f.key, InstanceID: id, ExpectedVersion: 1, IdempotencyKey: "k", ReasonRef: "r"},
		"no version":  {TenantID: f.tenant, Tenant: f.key, InstanceID: id, IdempotencyKey: "k", ReasonRef: "r", Operator: "o"},
	} {
		if _, err := f.controller(operator.NewMemoryJournal()).Cancel(ctx, cmd); !errors.Is(err, ErrInvalidCommand) {
			t.Errorf("%s: %v", name, err)
		}
	}
	retry := f.cmd(id, 0, "k")
	if _, err := f.controller(operator.NewMemoryJournal()).RetryNode(ctx, retry); !errors.Is(err, ErrInvalidCommand) {
		t.Errorf("retry without node: %v", err)
	}
	if _, err := New(nil, operator.NewMemoryJournal(), plans{}, f.auth, nil); !errors.Is(err, ErrInvalidCommand) {
		t.Errorf("nil db: %v", err)
	}
	if _, err := New(f.conn, nil, plans{}, f.auth, nil); err == nil {
		t.Error("nil journal accepted")
	}
	if _, err := decodeEffect("garbage"); err == nil {
		t.Error("garbage effect decoded")
	}
	if _, err := decodeEffect(effectPrefix + "|APPLIED||not-a-uuid||1||1"); err == nil {
		t.Error("bad instance id decoded")
	}
}

type failingPlans struct{ err error }

func (p failingPlans) ResolvePlan(context.Context, runtime.Executor, runtime.Instance) (*workflow.CompiledWorkflow, error) {
	return nil, p.err
}

type flakyJournal struct {
	*operator.MemoryJournal
	failComplete bool
}

func (j *flakyJournal) Complete(ctx context.Context, r operator.Receipt) error {
	if j.failComplete {
		return errors.New("journal write lost")
	}
	return j.MemoryJournal.Complete(ctx, r)
}

// TestTodo_EP_WF_002_Security refuses borrowed or foreign authority,
// cross-tenant targets and direct executor invocation.
func TestTodo_EP_WF_002_Security(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	c := f.controller(operator.NewMemoryJournal())
	id, v := f.instance([]string{"approve_manager"})

	f.auth.tenant = "tenant-foreign"
	r, err := c.Pause(ctx, f.cmd(id, v, "pause-foreign-grant"))
	expect(t, "foreign-tenant grant", r, err, OutcomeDenied, operator.CodeAuthorityMismatch)
	f.auth.tenant = ""

	f.auth.role = jit.RoleSupportReadOnly
	r, err = c.Pause(ctx, f.cmd(id, v, "pause-read-role"))
	expect(t, "read-only role", r, err, OutcomeDenied, operator.CodeAuthorityMismatch)
	f.auth.role = jit.RoleIntegrityRepair

	f.auth.simulate = false
	failed, _ := f.instance([]string{"execute_promotion"}, node{"execute_promotion", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeFailed}})
	r, err = c.RetryNode(ctx, retryCmd(f, failed, "execute_promotion", 1, "retry-no-sim"))
	expect(t, "retry without simulation", r, err, OutcomeDenied, operator.CodeSimulationRequired)
	f.auth.simulate = true

	// Another tenant's instance does not exist for this tenant.
	other := newFixtureOn(t, f.db, "ctl-other-"+uuid.NewString()[:6])
	otherID, otherV := other.instance([]string{"approve_manager"})
	cmd := f.cmd(otherID, otherV, "pause-cross-tenant")
	r, err = c.Pause(ctx, cmd)
	expect(t, "cross-tenant instance", r, err, OutcomeDenied, runtime.CodeInstanceNotFound)
	if inst, _ := other.load(otherID); inst.RuntimeStatus != runtime.InstanceRunning {
		t.Fatalf("a cross-tenant pause changed the other tenant's instance to %s", inst.RuntimeStatus)
	}

	// Executors refuse without the gateway's authorization.
	if _, err := c.applyCancel(withCommand(ctx, f.cmd(id, v, "direct")), operator.Authorization{}, operator.Request{}); operator.CodeOf(err) != operator.CodeUnauthorizedEffect {
		t.Fatalf("direct executor call = %v", err)
	}
	if _, err := c.applyCancel(ctx, operator.Authorization{}, operator.Request{}); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("executor without a command = %v", err)
	}
	if inst, _ := f.load(id); inst.RuntimeStatus != runtime.InstanceRunning {
		t.Fatalf("a refused control changed the instance to %s", inst.RuntimeStatus)
	}
}

// TestTodo_EP_WF_002_Recovery proves outcomes survive a restart: a controller
// recomposed over the same journal replays every recorded control without a
// second transition.
func TestTodo_EP_WF_002_Recovery(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	journal := operator.NewMemoryJournal()
	id, v := f.instance([]string{"approve_manager"})
	first, err := f.controller(journal).Cancel(ctx, f.cmd(id, v, "cancel-once"))
	expect(t, "cancel", first, err, OutcomeApplied, "")
	restarted := f.controllerWith(journal, plans{f.plan}, f0conn(t, f.db))
	again, err := restarted.Cancel(ctx, f.cmd(id, v, "cancel-once"))
	expect(t, "cancel after restart", again, err, OutcomeApplied, "")
	if !again.Replayed || again.InstanceVersion != first.InstanceVersion || again.IntentInstanceID != first.IntentInstanceID {
		t.Fatalf("replay after restart = %+v, first %+v", again, first)
	}
	if inst, _ := f.load(id); inst.InstanceVersion != first.InstanceVersion {
		t.Fatalf("restart re-ran the cancel: version %d -> %d", first.InstanceVersion, inst.InstanceVersion)
	}
}

// TestTodo_EP_WF_002_Integration drives a full pause, resume and cancel
// lifecycle on one instance through one controller, with each step fenced by
// the version the previous one returned.
func TestTodo_EP_WF_002_Integration(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	c := f.controller(operator.NewMemoryJournal())
	id, v := f.instance([]string{"approve_manager"})
	p, err := c.Pause(ctx, f.cmd(id, v, "i-pause"))
	expect(t, "pause", p, err, OutcomeApplied, "")
	cancelPaused, err := c.Cancel(ctx, f.cmd(id, p.InstanceVersion, "i-cancel-paused"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.Resume(ctx, f.cmd(id, p.InstanceVersion, "i-resume"))
	if cancelPaused.Outcome == OutcomeApplied {
		expect(t, "resume a cancelled instance", r, err, OutcomeTooLate, CodeInstanceTerminal)
		return
	}
	expect(t, "resume", r, err, OutcomeApplied, "")
	x, err := c.Cancel(ctx, f.cmd(id, r.InstanceVersion, "i-cancel"))
	expect(t, "cancel", x, err, OutcomeApplied, "")
	if inst, _ := f.load(id); inst.RuntimeStatus != runtime.InstanceCancelled || inst.CompletedAt == nil {
		t.Fatalf("final instance = %+v", inst)
	}
}

// TestTodo_EP_WF_002_Property proves the recorded effect reference is a
// lossless encoding of every result a control can return.
func TestTodo_EP_WF_002_Property(t *testing.T) {
	for i, o := range []Outcome{OutcomeApplied, OutcomePendingSafePoint, OutcomeDenied, OutcomeTooLate, OutcomeRepairRequired} {
		for _, st := range []runtime.InstanceStatus{runtime.InstanceRunning, runtime.InstancePaused, ""} {
			r := Result{Outcome: o, Code: fmt.Sprintf("CODE_%d", i), InstanceID: uuid.New(), InstanceStatus: st,
				InstanceVersion: int64(i * 7), NodeID: []string{"", "execute_promotion"}[i%2], Attempt: i}
			got, err := decodeEffect(encodeEffect(r))
			if err != nil || got != r {
				t.Fatalf("round trip %+v -> %+v, %v", r, got, err)
			}
		}
	}
	a := Command{ResolvedContext: map[string]string{"b": "2", "a": "1"}}
	b := Command{ResolvedContext: map[string]string{"a": "1", "b": "2"}}
	if payloadDigest(a) != payloadDigest(b) || payloadDigest(a) == payloadDigest(Command{}) {
		t.Fatal("payload digest is not a stable function of the resolved context")
	}
}

// TestTodo_EP_WF_002_Conformance pins the operator vocabulary each control
// resolves to and the governance each one demands.
func TestTodo_EP_WF_002_Conformance(t *testing.T) {
	for kind, want := range map[operator.Kind]struct{ dual, sim bool }{
		operator.KindWorkflowPause: {}, operator.KindWorkflowResume: {},
		operator.KindWorkflowCancel: {dual: true}, operator.KindWorkflowRetryNode: {sim: true},
	} {
		p, ok := operator.PolicyFor(kind)
		if !ok || !p.Material || p.DualControl != want.dual || p.SimulationRequired != want.sim || len(p.Roles) == 0 {
			t.Errorf("%s policy = %+v", kind, p)
		}
	}
	id := uuid.New()
	c := Command{InstanceID: id, NodeID: "n", ExpectedAttempt: 3}
	if s := c.Scope(operator.KindWorkflowRetryNode); s.IDs[0] != id.String()+"/n#3" {
		t.Errorf("retry scope = %+v", s)
	}
	if s := c.Scope(operator.KindWorkflowCancel); s.IDs[0] != id.String() {
		t.Errorf("cancel scope = %+v", s)
	}
}

// TestTodo_EP_WF_002_Mutation proves the retry guards are load-bearing: the
// classification that keeps a non-idempotent or irreversible write from
// being re-run differs from the one that admits a safe retry.
func TestTodo_EP_WF_002_Mutation(t *testing.T) {
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatal(err)
	}
	write, _ := plan.Node("execute_promotion")
	read, _ := plan.Node("evaluate_band")
	if !retrySafe(write) || !retrySafe(read) {
		t.Fatal("the compiled idempotent write or the read was classified unsafe")
	}
	noKey := write
	capCopy := *write.Capability
	capCopy.IdempotencyKeyMapping = ""
	noKey.Capability = &capCopy
	irreversible := write
	irreversible.EffectClass = "IRREVERSIBLE_EXTERNAL_MUTATION"
	noCap := write
	noCap.Capability = nil
	for name, n := range map[string]workflow.CompiledNode{"no idempotency key": noKey, "irreversible": irreversible, "no capability": noCap} {
		if retrySafe(n) {
			t.Errorf("%s: a mutation that removes this guard would re-run a duplicate effect undetected", name)
		}
	}
	if r, _ := runtimeRefusal(runtime.Instance{RuntimeStatus: runtime.InstanceCancelled}, &runtime.Error{Code: runtime.CodeIllegalTransition}); r.Outcome != OutcomeTooLate {
		t.Errorf("illegal transition on history = %+v, want TOO_LATE", r)
	}
	if r, _ := runtimeRefusal(runtime.Instance{RuntimeStatus: runtime.InstanceRunning}, &runtime.Error{Code: runtime.CodeIllegalTransition}); r.Outcome != OutcomeDenied {
		t.Errorf("illegal transition on a live instance = %+v, want DENIED", r)
	}
	if _, err := runtimeRefusal(runtime.Instance{}, errors.New("storage")); err == nil {
		t.Error("an uncoded runtime failure was mapped to a governed outcome")
	}
}

func TestHandleMapsTransportRequests(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	c := f.controller(operator.NewMemoryJournal())
	id, v := f.instance([]string{"approve_manager"})
	ids := func(values.TenantId) (uuid.UUID, error) { return f.tenant, nil }
	res, err := c.Handle(ctx, ids, Request{Kind: operator.KindWorkflowPause, Tenant: f.key, InstanceID: id.String(),
		ExpectedVersion: uint64(v), IdempotencyKey: "h-pause", ReasonRef: "INC-1", Operator: "operator:ana"})
	if err != nil || res.Outcome != OutcomeApplied || res.InstanceID != id.String() || res.InstanceStatus != string(runtime.InstancePaused) || res.InstanceVersion == 0 {
		t.Fatalf("Handle pause = %+v, %v", res, err)
	}
	for _, k := range []operator.Kind{operator.KindWorkflowResume, operator.KindWorkflowCancel, operator.KindWorkflowRetryNode} {
		if _, err := c.Handle(ctx, ids, Request{Kind: k, Tenant: f.key, InstanceID: id.String(), IdempotencyKey: "h-" + string(k), ReasonRef: "r", Operator: "o"}); !errors.Is(err, ErrInvalidCommand) {
			t.Errorf("%s without its fence = %v", k, err)
		}
	}
	for name, tc := range map[string]struct {
		ids TenantIDs
		req Request
	}{
		"no mapping":  {nil, Request{Kind: operator.KindWorkflowPause}},
		"bad tenant":  {func(values.TenantId) (uuid.UUID, error) { return uuid.Nil, errors.New("unknown") }, Request{Kind: operator.KindWorkflowPause}},
		"bad id":      {ids, Request{Kind: operator.KindWorkflowPause, InstanceID: "nope"}},
		"not control": {ids, Request{Kind: operator.KindKeyRotation, InstanceID: id.String()}},
	} {
		if _, err := c.Handle(ctx, tc.ids, tc.req); !errors.Is(err, ErrInvalidCommand) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
