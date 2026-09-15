package cancellation_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/cancellation"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var at = time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)

type fixture struct {
	t      *testing.T
	db     *pgtest.DB
	tenant uuid.UUID
	conn   *pgxadapter.Conn
	plan   *workflow.CompiledWorkflow
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'cancellation', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenant, "cancel-"+tenant.String()[:8])
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, db: db, tenant: tenant, conn: appConn(t, db), plan: plan}
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	return conn
}

// compensablePlan is the promotion plan with a published compensation bound
// to execute_promotion. The digest is the compiled one, so an instance pinned
// to it resolves to exactly these declarations.
func compensablePlan(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatal(err)
	}
	for i := range plan.Nodes {
		if plan.Nodes[i].ID == promotionexec.NodeExecutePromotion {
			plan.Nodes[i].CompensationRef = &workflow.ResolvedReference{Kind: workflow.RefCompensation, ID: "compensation.promotion.reverse", Version: "1"}
		}
	}
	return plan
}

func (f *fixture) tx(conn *pgxadapter.Conn, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, f.tenant); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type node struct {
	id       string
	statuses []runtime.NodeStatus
	effects  []string
}

func (f *fixture) instance(plan *workflow.CompiledWorkflow, frontier []string, nodes ...node) uuid.UUID {
	f.t.Helper()
	ctx := context.Background()
	inst, err := runtime.NewInstance(f.tenant, uuid.New(), "cell-local", plan, workflow.ModeExecute, "sha256:input", "corr-"+uuid.NewString()[:8], at)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.tx(f.conn, func(tx dbport.Tx) error {
		store := runtime.Store{}
		stored, err := store.CreateInstance(ctx, tx, inst)
		if err != nil {
			return err
		}
		running, err := store.RecordInstanceState(ctx, tx, runtime.InstanceTransition{TenantID: f.tenant, InstanceID: inst.InstanceID,
			ExpectedVersion: stored.InstanceVersion, Status: runtime.InstanceRunning, CurrentNodeIDs: frontier})
		if err != nil {
			return err
		}
		version := running.InstanceVersion
		for _, n := range nodes {
			cn, _ := plan.Node(n.id)
			exec := runtime.NewNodeExecution(f.tenant, inst.InstanceID, n.id, 1, cn.Type, runtime.NodeReady)
			exec.RecordedAt = at
			if _, version, err = store.RecordNodeExecution(ctx, tx, exec, version); err != nil {
				return err
			}
			for i, s := range n.statuses {
				t := runtime.NodeTransition{TenantID: f.tenant, InstanceID: inst.InstanceID, NodeID: n.id, Attempt: 1, ExpectedInstanceVersion: version, Status: s}
				if i == len(n.statuses)-1 {
					t.Refs.EffectRefs = n.effects
				}
				if _, version, err = store.RecordNodeTransition(ctx, tx, t); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		f.t.Fatalf("seed instance: %v", err)
	}
	return inst.InstanceID
}

func (f *fixture) link(parent, child uuid.UUID) {
	f.t.Helper()
	if err := f.tx(f.conn, func(tx dbport.Tx) error {
		return (runtimestate.ChildLinkStore{}).Link(context.Background(), tx, runtimestate.ChildLink{
			TenantID: f.tenant, Parent: parent, Child: child, ParentNodeID: promotionexec.NodeExecutePromotion, Ordinal: 1,
			Mode: runtimestate.ChildAwait, InputDigest: strings.Repeat("c", 64), CreatedAt: at})
	}); err != nil {
		f.t.Fatalf("link child: %v", err)
	}
}

func (f *fixture) load(id uuid.UUID) (runtime.Instance, []runtime.NodeExecution) {
	f.t.Helper()
	var inst runtime.Instance
	var nodes []runtime.NodeExecution
	if err := f.tx(f.conn, func(tx dbport.Tx) error {
		var err error
		if inst, err = (runtime.Store{}).LoadInstance(context.Background(), tx, f.tenant, id); err != nil {
			return err
		}
		nodes, err = (runtime.Store{}).LoadNodeExecutions(context.Background(), tx, f.tenant, id)
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
	return inst, nodes
}

func (f *fixture) decisions(id uuid.UUID) []cancellation.Record {
	f.t.Helper()
	var out []cancellation.Record
	if err := f.tx(f.conn, func(tx dbport.Tx) error {
		var err error
		out, err = cancellation.Decisions(context.Background(), tx, f.tenant, id)
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *fixture) decide(conn *pgxadapter.Conn, plan *workflow.CompiledWorkflow, id uuid.UUID, expected int64) (cancellation.Outcome, error) {
	var out cancellation.Outcome
	err := f.tx(conn, func(tx dbport.Tx) error {
		var err error
		out, err = cancellation.Decide(context.Background(), tx, cancellation.Request{TenantID: f.tenant, InstanceID: id,
			ExpectedInstanceVersion: expected, Plan: plan, Reason: "PROMOTION_WITHDRAWN", RequestedBy: "principal:operator", RecordedAt: at})
		return err
	})
	return out, err
}

// TestTodo_WF_RUN_010_Integration proves the served decision against
// PostgreSQL: a cancel before any effect is CANCELLED with evidence and
// history intact; a produced compensable effect is COMPENSATION_REQUIRED with
// the obligation recorded and the instance CANCELLING, never CANCELLED; an
// uncompensatable one is CANNOT_CANCEL with the instance untouched; an effect
// in flight is REPAIR_REQUIRED; a child that cannot cancel refuses the parent
// unchanged; a cancellable child is cancelled before its parent.
func TestTodo_WF_RUN_010_Integration(t *testing.T) {
	f := newFixture(t)

	t.Run("clean cancel before any effect", func(t *testing.T) {
		id := f.instance(f.plan, []string{promotionexec.NodeApproveManager},
			node{id: promotionexec.NodeSnapshotWorker, statuses: []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeSucceeded}})
		timerID := uuid.New()
		if err := f.tx(f.conn, func(tx dbport.Tx) error {
			return (runtimestate.TimerStore{}).Set(context.Background(), tx, runtimestate.Timer{TenantID: f.tenant, TimerID: timerID,
				InstanceID: id, NodeID: promotionexec.NodeWaitEffectiveDate, Key: "effective-date", Kind: runtimestate.TimerDelay,
				FiresAt: at.Add(24 * time.Hour), CreatedAt: at})
		}); err != nil {
			t.Fatal(err)
		}
		before, nodesBefore := f.load(id)
		out, err := f.decide(f.conn, f.plan, id, before.InstanceVersion)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.tx(f.conn, func(tx dbport.Tx) error {
			timer, err := (runtimestate.TimerStore{}).Load(context.Background(), tx, f.tenant, timerID)
			if err == nil && timer.State != runtimestate.TimerCancelled {
				t.Errorf("pending timer after cancel = %s, want CANCELLED", timer.State)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if out.Decision != workflow.Cancelled || out.Instance.RuntimeStatus != runtime.InstanceCancelled || out.Replayed {
			t.Fatalf("outcome = %+v", out)
		}
		after, nodesAfter := f.load(id)
		if after.RuntimeStatus != runtime.InstanceCancelled || after.InstanceVersion != before.InstanceVersion+2 || after.CompletedAt == nil {
			t.Fatalf("instance after = %+v", after)
		}
		if len(nodesAfter) != len(nodesBefore) || nodesAfter[0].Status != runtime.NodeSucceeded {
			t.Fatalf("history changed: %+v -> %+v", nodesBefore, nodesAfter)
		}
		recs := f.decisions(id)
		if len(recs) != 1 || recs[0].Decision != workflow.Cancelled || recs[0].StatusBefore != runtime.InstanceRunning ||
			recs[0].StatusAfter != runtime.InstanceCancelled || recs[0].Phase != "RUNNING@approve_manager" ||
			recs[0].Evidence.Digest == "" || recs[0].DecisionID != out.DecisionID || recs[0].VersionAfter != after.InstanceVersion {
			t.Fatalf("evidence = %+v", recs)
		}
	})

	t.Run("compensable effect requires compensation", func(t *testing.T) {
		plan := compensablePlan(t)
		id := f.instance(plan, []string{promotionexec.NodeObservePayroll},
			node{id: promotionexec.NodeExecutePromotion, statuses: []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeSucceeded}})
		out, err := f.decide(f.conn, plan, id, 0)
		if err != nil {
			t.Fatal(err)
		}
		if out.Decision != workflow.CompensationRequired || out.Instance.RuntimeStatus != runtime.InstanceCancelling {
			t.Fatalf("outcome = %+v", out)
		}
		if len(out.CompensationRefs) != 1 || out.CompensationRefs[0] != "execute_promotion#1=compensation.promotion.reverse@1" {
			t.Fatalf("obligation = %v", out.CompensationRefs)
		}
		if r, ok := out.Blocking(); !ok || r.Code != cancellation.ReasonEffectCompensation || r.NodeID != promotionexec.NodeExecutePromotion {
			t.Fatalf("blocking = %+v, %v", r, ok)
		}
		after, _ := f.load(id)
		if after.RuntimeStatus == runtime.InstanceCancelled || after.CompletedAt != nil {
			t.Fatalf("a compensation-required instance was declared cancelled: %+v", after)
		}
		recs := f.decisions(id)
		if len(recs) != 1 || recs[0].Decision != workflow.CompensationRequired || len(recs[0].CompensationRefs) != 1 ||
			recs[0].Evidence.Effects[0].Disposition != "COMPENSATE:compensation.promotion.reverse@1" {
			t.Fatalf("evidence = %+v", recs)
		}
	})

	t.Run("uncompensatable effect cannot cancel", func(t *testing.T) {
		id := f.instance(f.plan, []string{promotionexec.NodeObservePayroll},
			node{id: promotionexec.NodeExecutePromotion, statuses: []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeSucceeded}})
		before, _ := f.load(id)
		out, err := f.decide(f.conn, f.plan, id, before.InstanceVersion)
		if err != nil {
			t.Fatal(err)
		}
		if out.Decision != workflow.CannotCancel {
			t.Fatalf("decision = %s", out.Decision)
		}
		if r, ok := out.Blocking(); !ok || r.Code != cancellation.ReasonEffectIrreversible || r.Ref != "execute_promotion#1" {
			t.Fatalf("blocking = %+v", r)
		}
		after, _ := f.load(id)
		if after.RuntimeStatus != before.RuntimeStatus || after.InstanceVersion != before.InstanceVersion {
			t.Fatalf("a refusal changed the instance %+v -> %+v", before, after)
		}
		if recs := f.decisions(id); len(recs) != 1 || recs[0].Decision != workflow.CannotCancel || recs[0].StatusAfter != runtime.InstanceRunning {
			t.Fatalf("refusal evidence = %+v", recs)
		}
	})

	t.Run("in-flight effect requires repair", func(t *testing.T) {
		id := f.instance(f.plan, []string{promotionexec.NodeExecutePromotion},
			node{id: promotionexec.NodeExecutePromotion, statuses: []runtime.NodeStatus{runtime.NodeRunning}})
		out, err := f.decide(f.conn, f.plan, id, 0)
		if err != nil {
			t.Fatal(err)
		}
		if out.Decision != workflow.RepairRequired || out.Instance.RuntimeStatus != runtime.InstanceRepairRequired {
			t.Fatalf("outcome = %+v", out)
		}
		if r, _ := out.Blocking(); r.Code != cancellation.ReasonEffectAmbiguous {
			t.Fatalf("blocking = %+v", r)
		}
	})

	t.Run("failed attempt with a recorded effect is ambiguous", func(t *testing.T) {
		id := f.instance(f.plan, []string{promotionexec.NodeExecutePromotion},
			node{id: promotionexec.NodeExecutePromotion, statuses: []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeFailed}, effects: []string{"outbox:promotion-commit"}})
		out, err := f.decide(f.conn, f.plan, id, 0)
		if err != nil || out.Decision != workflow.RepairRequired {
			t.Fatalf("outcome = %+v, %v", out, err)
		}
	})

	t.Run("child that cannot cancel refuses the parent unchanged", func(t *testing.T) {
		parent := f.instance(f.plan, []string{promotionexec.NodeApproveManager})
		child := f.instance(f.plan, []string{promotionexec.NodeObservePayroll},
			node{id: promotionexec.NodeExecutePromotion, statuses: []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeSucceeded}})
		f.link(parent, child)
		pBefore, _ := f.load(parent)
		cBefore, _ := f.load(child)
		out, err := f.decide(f.conn, f.plan, parent, pBefore.InstanceVersion)
		if err != nil {
			t.Fatal(err)
		}
		if out.Decision != workflow.CannotCancel || len(out.Evidence.ChildReports) != 1 || out.Evidence.ChildReports[0].Report != "CANNOT_CANCEL" {
			t.Fatalf("outcome = %+v", out)
		}
		pAfter, _ := f.load(parent)
		cAfter, _ := f.load(child)
		if pAfter.InstanceVersion != pBefore.InstanceVersion || cAfter.InstanceVersion != cBefore.InstanceVersion {
			t.Fatal("a refused parent cancellation changed the parent or the child")
		}
		if len(f.decisions(child)) != 0 {
			t.Fatal("a refused parent recorded a child decision")
		}
	})

	t.Run("cancellable child is cancelled before its parent", func(t *testing.T) {
		parent := f.instance(f.plan, []string{promotionexec.NodeApproveManager})
		child := f.instance(f.plan, []string{promotionexec.NodeApproveFinance})
		f.link(parent, child)
		out, err := f.decide(f.conn, f.plan, parent, 0)
		if err != nil {
			t.Fatal(err)
		}
		if out.Decision != workflow.Cancelled || len(out.Children) != 1 || out.Children[0].InstanceID != child {
			t.Fatalf("outcome = %+v", out)
		}
		cAfter, _ := f.load(child)
		if cAfter.RuntimeStatus != runtime.InstanceCancelled {
			t.Fatalf("child = %s", cAfter.RuntimeStatus)
		}
		recs := f.decisions(child)
		if len(recs) != 1 || recs[0].ParentDecisionID == nil || *recs[0].ParentDecisionID != out.DecisionID {
			t.Fatalf("child evidence = %+v", recs)
		}
	})

	t.Run("compensable child lifts its obligation to the parent", func(t *testing.T) {
		plan := compensablePlan(t)
		parent := f.instance(plan, []string{promotionexec.NodeApproveManager})
		child := f.instance(plan, []string{promotionexec.NodeObservePayroll},
			node{id: promotionexec.NodeExecutePromotion, statuses: []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeSucceeded}})
		f.link(parent, child)
		out, err := f.decide(f.conn, plan, parent, 0)
		if err != nil {
			t.Fatal(err)
		}
		if out.Decision != workflow.CompensationRequired || out.Instance.RuntimeStatus != runtime.InstanceCancelling || len(out.Children) != 1 ||
			out.Children[0].Decision != workflow.CompensationRequired {
			t.Fatalf("outcome = %+v", out)
		}
		if cAfter, _ := f.load(child); cAfter.RuntimeStatus != runtime.InstanceCancelling {
			t.Fatalf("child = %s", cAfter.RuntimeStatus)
		}
		// A second cancellation of the same state replays the decision.
		again, err := f.decide(f.conn, plan, parent, 0)
		if err != nil || !again.Replayed || again.DecisionID != out.DecisionID {
			t.Fatalf("replay = %+v, %v", again, err)
		}
	})

	t.Run("child states settle without recursion", func(t *testing.T) {
		parent := f.instance(f.plan, []string{promotionexec.NodeApproveManager})
		done := f.instance(f.plan, []string{promotionexec.NodeEndComplete})
		if _, err := f.decide(f.conn, f.plan, done, 0); err != nil {
			t.Fatal(err)
		}
		f.link(parent, done)
		out, err := f.decide(f.conn, f.plan, parent, 0)
		if err != nil || out.Decision != workflow.Cancelled || len(out.Children) != 0 {
			t.Fatalf("parent over an already-cancelled child = %+v, %v", out, err)
		}
	})
}

// TestTodo_WF_RUN_010_IntegrationRace proves concurrent cancels of the same
// instance, each on its own connection, converge on one decision and one
// transition.
func TestTodo_WF_RUN_010_IntegrationRace(t *testing.T) {
	f := newFixture(t)
	id := f.instance(f.plan, []string{promotionexec.NodeApproveFinance})
	before, _ := f.load(id)
	const workers = 6
	conns := make([]*pgxadapter.Conn, workers)
	for i := range conns {
		conns[i] = appConn(t, f.db)
	}
	outs := make([]cancellation.Outcome, workers)
	errs := make([]error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			outs[i], errs[i] = f.decide(conns[i], f.plan, id, before.InstanceVersion)
		}(i)
	}
	close(start)
	wg.Wait()
	replays := 0
	for i := range outs {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if outs[i].DecisionID != outs[0].DecisionID || outs[i].Decision != workflow.Cancelled {
			t.Fatalf("worker %d decided %+v, worker 0 decided %+v", i, outs[i], outs[0])
		}
		if outs[i].Replayed {
			replays++
		}
	}
	if replays != workers-1 {
		t.Fatalf("replays = %d, want %d", replays, workers-1)
	}
	if recs := f.decisions(id); len(recs) != 1 {
		t.Fatalf("decision rows = %d, want 1", len(recs))
	}
	if after, _ := f.load(id); after.InstanceVersion != before.InstanceVersion+2 {
		t.Fatalf("instance version %d -> %d, want exactly one CANCELLING/CANCELLED transition", before.InstanceVersion, after.InstanceVersion)
	}
}

// TestTodo_WF_RUN_010_IntegrationFault proves refusals write nothing and the
// decision row is append-only.
func TestTodo_WF_RUN_010_IntegrationFault(t *testing.T) {
	f := newFixture(t)
	id := f.instance(f.plan, []string{promotionexec.NodeApproveFinance})
	before, _ := f.load(id)

	if _, err := f.decide(f.conn, nil, id, 0); !errors.Is(err, cancellation.ErrInvalid) {
		t.Fatalf("nil plan = %v", err)
	}
	if err := f.tx(f.conn, func(tx dbport.Tx) error {
		_, err := cancellation.Decide(context.Background(), tx, cancellation.Request{TenantID: f.tenant, InstanceID: id, Plan: f.plan, RecordedAt: at})
		return err
	}); !errors.Is(err, cancellation.ErrInvalid) {
		t.Fatalf("missing actor = %v", err)
	}
	if _, err := f.decide(f.conn, f.plan, id, before.InstanceVersion+5); !errors.Is(err, cancellation.ErrStale) {
		t.Fatalf("stale version = %v", err)
	}
	if _, err := f.decide(f.conn, compensablePlan(t), id, 0); err != nil {
		t.Fatalf("a plan with the same digest must resolve: %v", err)
	}
	after, _ := f.load(id)
	if after.RuntimeStatus != runtime.InstanceCancelled {
		t.Fatalf("status = %s", after.RuntimeStatus)
	}

	other := f.instance(f.plan, []string{promotionexec.NodeApproveFinance})
	simulation, err := promotionexec.CompileSimulation()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.decide(f.conn, simulation, other, 0); !errors.Is(err, cancellation.ErrPlanMismatch) {
		t.Fatalf("plan mismatch = %v", err)
	}
	if len(f.decisions(other)) != 0 {
		t.Fatal("a refused request recorded a decision")
	}

	// A terminal instance with no decision for its state is refused.
	terminal := f.instance(f.plan, []string{promotionexec.NodeApproveFinance})
	tb, _ := f.load(terminal)
	if err := f.tx(f.conn, func(tx dbport.Tx) error {
		_, err := (runtime.Store{}).RecordInstanceState(context.Background(), tx, runtime.InstanceTransition{TenantID: f.tenant, InstanceID: terminal,
			ExpectedVersion: tb.InstanceVersion, Status: runtime.InstanceQuarantined, CompletedAt: &at})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.decide(f.conn, f.plan, terminal, 0); !errors.Is(err, cancellation.ErrTerminal) {
		t.Fatalf("terminal = %v", err)
	}
	if _, err := f.decide(f.conn, f.plan, uuid.New(), 0); err == nil {
		t.Fatal("an unknown instance was decided")
	}

	// History is never rewritten: the decision row refuses UPDATE and DELETE.
	for _, stmt := range []string{
		`UPDATE workflow_cancellation_decision SET decision = 'CANNOT_CANCEL' WHERE instance_id = $1`,
		`DELETE FROM workflow_cancellation_decision WHERE instance_id = $1`,
	} {
		if err := f.tx(f.conn, func(tx dbport.Tx) error {
			_, err := tx.Exec(context.Background(), stmt, id)
			return err
		}); err == nil {
			t.Fatalf("%s succeeded on an append-only decision", strings.Fields(stmt)[0])
		}
	}
	if recs := f.decisions(id); len(recs) != 1 {
		t.Fatalf("decisions after mutation attempts = %d", len(recs))
	}
}
