package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionguard"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// fakeWorkflowCancel answers CancelBound per intent and records every call.
type fakeWorkflowCancel struct {
	mu       sync.Mutex
	verdicts map[string]BoundCancellationVerdict
	fail     map[string]error
	calls    []BoundCancellation
}

func (f *fakeWorkflowCancel) CancelBound(_ context.Context, req BoundCancellation) (BoundCancellationVerdict, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	if err := f.fail[req.IntentID]; err != nil {
		return BoundCancellationVerdict{}, err
	}
	return f.verdicts[req.IntentID], nil
}

func (f *fakeWorkflowCancel) callsFor(intentID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c.IntentID == intentID {
			n++
		}
	}
	return n
}

// TestTodo_WF_RUN_010_CancelIntentGoverned proves CancelIntent consults the
// governed workflow decision whenever the kernel would cancel the intent, and
// that the intent's disposition follows the recorded verdict: a clean
// workflow cancel cancels the intent and releases its admission window; a
// refusal, a compensation obligation or a repair outside execution leaves the
// intent untouched; a repair during execution routes the intent to repair
// bound to the decision; a failing decision fails the call; and a refused or
// already-terminal intent never reaches the workflow.
func TestTodo_WF_RUN_010_CancelIntentGoverned(t *testing.T) {
	h := newLifecycleHarness(t)
	ctx := lifecycleCtx(t)
	fake := &fakeWorkflowCancel{verdicts: map[string]BoundCancellationVerdict{}, fail: map[string]error{}}
	var releasedMu sync.Mutex
	var released []string
	h.Service.workflowCancel = fake
	h.Service.releaseAdmission = func(_ context.Context, tenant values.TenantId, intentID string, at time.Time) error {
		releasedMu.Lock()
		defer releasedMu.Unlock()
		if tenant != lifecycleTestTenant || at.IsZero() {
			t.Errorf("release for %s/%s at %v", tenant, intentID, at)
		}
		released = append(released, intentID)
		return nil
	}
	decisionID := uuid.New()

	const (
		cleanID       = "20000000-0000-0000-0000-000000000001"
		cannotID      = "20000000-0000-0000-0000-000000000002"
		compensateID  = "20000000-0000-0000-0000-000000000003"
		repairExecID  = "20000000-0000-0000-0000-000000000004"
		repairIdleID  = "20000000-0000-0000-0000-000000000005"
		unboundID     = "20000000-0000-0000-0000-000000000006"
		failID        = "20000000-0000-0000-0000-000000000007"
		committedID   = "20000000-0000-0000-0000-000000000008"
		executingIdle = "20000000-0000-0000-0000-000000000009"
	)
	notStarted := lifecycleDims(lifecycle.RequestSubmitted, lifecycle.ExecutionNotPlanned)
	executing := lifecycleDims(lifecycle.RequestApproved, lifecycle.ExecutionExecuting)
	for id, dims := range map[string]lifecycle.Dimensions{
		cleanID: notStarted, cannotID: notStarted, compensateID: notStarted, repairExecID: executing,
		repairIdleID: notStarted, unboundID: notStarted, failID: notStarted, executingIdle: executing,
	} {
		h.seed(t, id, dims)
	}
	h.seed(t, committedID, lifecycleDims(lifecycle.RequestApproved, lifecycle.ExecutionCommitted),
		func(i *intent.Instance) { i.CommitReceiptRef = "receipt:fixture" })
	bound := func(d workflow.CancellationDecision) BoundCancellationVerdict {
		return BoundCancellationVerdict{Bound: true, InstanceID: uuid.New(), DecisionID: decisionID, Decision: d}
	}
	fake.verdicts[cleanID] = bound(workflow.Cancelled)
	fake.verdicts[cannotID] = bound(workflow.CannotCancel)
	fake.verdicts[compensateID] = bound(workflow.CompensationRequired)
	fake.verdicts[repairExecID] = bound(workflow.RepairRequired)
	fake.verdicts[repairIdleID] = bound(workflow.RepairRequired)
	fake.verdicts[executingIdle] = bound(workflow.Cancelled)
	fake.fail[failID] = errors.New("workflow store unavailable")

	cancel := func(id, key string, version uint64) (*intentsv1.IntentInstance, error) {
		resp, err := h.Service.CancelIntent(ctx, &intentsv1.CancelIntentRequest{
			IdempotencyKey: key, IntentId: id, ExpectedInstanceVersion: version, ReasonRef: "reason:withdrawn",
		})
		return resp.GetIntent(), err
	}
	disposition := func(msg *intentsv1.IntentInstance) string {
		d := msg.GetCancellationDecisions()
		if len(d) == 0 {
			return ""
		}
		return d[len(d)-1].GetEffectDispositionRef()
	}

	cases := []struct {
		id, want   string
		request    intentsv1.RequestState
		execution  intentsv1.ExecutionState
		release    bool
		consulted  bool
		repairsRef bool
	}{
		{cleanID, "CANCELLED", intentsv1.RequestState_REQUEST_STATE_CANCELLED, intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED, true, true, false},
		{cannotID, "TOO_LATE", intentsv1.RequestState_REQUEST_STATE_SUBMITTED, intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED, false, true, false},
		{compensateID, "CANCELLATION_PENDING", intentsv1.RequestState_REQUEST_STATE_SUBMITTED, intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED, false, true, false},
		{repairExecID, "REPAIR_REQUIRED", intentsv1.RequestState_REQUEST_STATE_APPROVED, intentsv1.ExecutionState_EXECUTION_STATE_REPAIR_REQUIRED, false, true, true},
		{repairIdleID, "REPAIR_REQUIRED", intentsv1.RequestState_REQUEST_STATE_SUBMITTED, intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED, false, true, false},
		{unboundID, "CANCELLED", intentsv1.RequestState_REQUEST_STATE_CANCELLED, intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED, true, true, false},
		{executingIdle, "CANCELLED", intentsv1.RequestState_REQUEST_STATE_CANCELLED, intentsv1.ExecutionState_EXECUTION_STATE_BLOCKED, true, true, false},
		{committedID, "TOO_LATE", intentsv1.RequestState_REQUEST_STATE_APPROVED, intentsv1.ExecutionState_EXECUTION_STATE_COMMITTED, false, false, false},
	}
	for _, tc := range cases {
		msg, err := cancel(tc.id, "idem-"+tc.id, 1)
		if err != nil {
			fatalWithDiagnostic(t, "CancelIntent("+tc.id+")", err)
		}
		if got := disposition(msg); got != tc.want {
			t.Fatalf("%s disposition = %s, want %s", tc.id, got, tc.want)
		}
		if msg.GetLifecycle().GetRequest() != tc.request || msg.GetLifecycle().GetExecution() != tc.execution {
			t.Fatalf("%s lifecycle = %s/%s, want %s/%s", tc.id, msg.GetLifecycle().GetRequest(), msg.GetLifecycle().GetExecution(), tc.request, tc.execution)
		}
		if consulted := fake.callsFor(tc.id) == 1; consulted != tc.consulted {
			t.Fatalf("%s consulted the workflow = %v, want %v", tc.id, consulted, tc.consulted)
		}
		if tc.repairsRef && !strings.Contains(h.load(t, tc.id).RepairRef, decisionID.String()) {
			t.Fatalf("%s repair ref = %q, want the decision %s", tc.id, h.load(t, tc.id).RepairRef, decisionID)
		}
		releasedMu.Lock()
		gotRelease := false
		for _, r := range released {
			gotRelease = gotRelease || r == tc.id
		}
		releasedMu.Unlock()
		if gotRelease != tc.release {
			t.Fatalf("%s admission released = %v, want %v", tc.id, gotRelease, tc.release)
		}
	}
	fake.mu.Lock()
	first := fake.calls[0]
	fake.mu.Unlock()
	if first.IntentID != cleanID || first.CorrelationID != "seed-correlation-"+cleanID || first.RequestedBy != "lifecycle-test-subject" ||
		first.Reason != "reason:withdrawn" || first.Tenant != lifecycleTestTenant || first.RecordedAt.IsZero() {
		t.Fatalf("bound cancellation request = %+v", first)
	}

	// A failing workflow decision fails the call and changes nothing.
	if _, err := cancel(failID, "idem-fail", 1); err == nil {
		t.Fatal("a failing workflow decision was reported as a cancellation")
	}
	if got := h.load(t, failID); got.Lifecycle.Request != lifecycle.RequestSubmitted || got.InstanceVersion != 1 {
		t.Fatalf("intent after a failed decision = %+v", got.Lifecycle)
	}

	// An already-cancelled intent is refused before the workflow is asked.
	calls := fake.callsFor(cleanID)
	_, err := cancel(cleanID, "idem-again", 2)
	var owned *envelope.Error
	if !errors.As(err, &owned) || owned.ReasonRef() != reasonAlreadyTerminal {
		t.Fatalf("re-cancel = %v, want %s", err, reasonAlreadyTerminal)
	}
	if fake.callsFor(cleanID) != calls {
		t.Fatal("a terminal intent reached the workflow cancellation")
	}
}

// TestTodo_WF_RUN_010_GovernedDisposition pins the verdict-to-disposition
// mapping, including instances that ended before the request.
func TestTodo_WF_RUN_010_GovernedDisposition(t *testing.T) {
	id, instance := uuid.New(), uuid.New()
	cases := []struct {
		v         BoundCancellationVerdict
		executing bool
		point     intent.CancellationPoint
		ref       string
		decided   intent.CancellationDisposition
	}{
		{BoundCancellationVerdict{Bound: true, Decision: workflow.Cancelled}, false, intent.CancellationPointClean, "", ""},
		{BoundCancellationVerdict{Bound: true, Decision: workflow.CannotCancel}, true, intent.CancellationPointUnknown, "", intent.DispositionTooLate},
		{BoundCancellationVerdict{Bound: true, Decision: workflow.CompensationRequired}, true, intent.CancellationPointMidFlight, "", intent.DispositionCancellationPending},
		{BoundCancellationVerdict{Bound: true, Decision: workflow.RepairRequired, DecisionID: id}, true, intent.CancellationPointPartialEffect, "workflow-cancellation:" + id.String(), ""},
		{BoundCancellationVerdict{Bound: true, Decision: workflow.RepairRequired, DecisionID: id}, false, intent.CancellationPointUnknown, "", intent.DispositionRepairRequired},
		{BoundCancellationVerdict{Bound: true, TerminalStatus: runtime.InstanceCancelled}, true, intent.CancellationPointClean, "", ""},
		{BoundCancellationVerdict{Bound: true, TerminalStatus: runtime.InstanceCompleted}, true, intent.CancellationPointUnknown, "", intent.DispositionTooLate},
		{BoundCancellationVerdict{Bound: true, InstanceID: instance, TerminalStatus: runtime.InstanceRepairRequired}, true, intent.CancellationPointPartialEffect, "workflow-instance:" + instance.String(), ""},
		{BoundCancellationVerdict{Bound: true, TerminalStatus: runtime.InstanceQuarantined}, true, intent.CancellationPointUnknown, "", intent.DispositionCancellationPending},
	}
	for i, tc := range cases {
		point, ref, decided := tc.v.governedDisposition(tc.executing)
		if point != tc.point || ref != tc.ref || decided != tc.decided {
			t.Fatalf("case %d = %v %q %q, want %v %q %q", i, point, ref, decided, tc.point, tc.ref, tc.decided)
		}
	}
	if wc, release, err := composeWorkflowCancellation(nil, nil); wc != nil || release != nil || err != nil {
		t.Fatal("a cell without an execution database composed a workflow cancellation")
	}
}

// TestTodo_WF_RUN_010_CancelBoundIntegration proves the production port over
// PostgreSQL: the root instance bound to a correlation is decided and
// cancelled durably, a repeat returns the recorded decision, an unbound or
// already-completed execution is reported as such, and the composed admission
// release closes the intent's promotion window.
func TestTodo_WF_RUN_010_CancelBoundIntegration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, 'wfrun010-app', 'cell-local', 'wfrun010 app', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenantID)
	tenantUUID := func(k values.TenantId) uuid.UUID {
		if k == "wfrun010-app" {
			return tenantID
		}
		return uuid.Nil
	}
	wc, release, err := composeWorkflowCancellation(db.Conn, tenantUUID)
	if err != nil || wc == nil || release == nil {
		t.Fatalf("compose = %v, %v, %v", wc, release, err)
	}
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	withTx := func(fn func(tx dbport.Tx) error) {
		t.Helper()
		tx, err := db.Conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
			t.Fatal(err)
		}
		if err := fn(tx); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	seed := func(correlation string, status runtime.InstanceStatus) uuid.UUID {
		inst, err := runtime.NewInstance(tenantID, uuid.New(), "cell-local", plan, workflow.ModeExecute, "sha256:input", correlation, at)
		if err != nil {
			t.Fatal(err)
		}
		withTx(func(tx dbport.Tx) error {
			store := runtime.Store{}
			created, err := store.CreateInstance(ctx, tx, inst)
			if err != nil {
				return err
			}
			running, err := store.RecordInstanceState(ctx, tx, runtime.InstanceTransition{TenantID: tenantID, InstanceID: inst.InstanceID,
				ExpectedVersion: created.InstanceVersion, Status: runtime.InstanceRunning, CurrentNodeIDs: []string{promotionexec.NodeApproveFinance}})
			if err != nil || status == runtime.InstanceRunning {
				return err
			}
			_, err = store.RecordInstanceState(ctx, tx, runtime.InstanceTransition{TenantID: tenantID, InstanceID: inst.InstanceID,
				ExpectedVersion: running.InstanceVersion, Status: status, CompletedAt: &at})
			return err
		})
		return inst.InstanceID
	}
	live := seed("corr-live", runtime.InstanceRunning)
	seed("corr-done", runtime.InstanceCompleted)
	req := BoundCancellation{Tenant: "wfrun010-app", IntentID: "intent-1", CorrelationID: "corr-live", Reason: "withdrawn", RequestedBy: "principal:op", RecordedAt: at}

	verdict, err := wc.CancelBound(ctx, req)
	if err != nil || !verdict.Bound || verdict.InstanceID != live || verdict.Decision != workflow.Cancelled || verdict.DecisionID == uuid.Nil {
		t.Fatalf("bound cancel = %+v, %v", verdict, err)
	}
	var status string
	if err := db.Conn.QueryRow(ctx, `SELECT runtime_status FROM workflow_instance WHERE instance_id = $1`, live).Scan(&status); err != nil || status != "CANCELLED" {
		t.Fatalf("instance = %s, %v", status, err)
	}
	again, err := wc.CancelBound(ctx, req)
	if err != nil || again.DecisionID != verdict.DecisionID || again.Decision != workflow.Cancelled {
		t.Fatalf("repeat = %+v, %v; want the recorded decision", again, err)
	}
	var rows int
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM workflow_cancellation_decision WHERE instance_id = $1`, live).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("decision rows = %d, %v", rows, err)
	}

	done := req
	done.CorrelationID = "corr-done"
	if v, err := wc.CancelBound(ctx, done); err != nil || !v.Bound || v.TerminalStatus != runtime.InstanceCompleted {
		t.Fatalf("completed execution = %+v, %v", v, err)
	}
	for _, corr := range []string{"corr-none", ""} {
		unbound := req
		unbound.CorrelationID = corr
		if v, err := wc.CancelBound(ctx, unbound); err != nil || v.Bound {
			t.Fatalf("unbound %q = %+v, %v", corr, v, err)
		}
	}
	foreign := req
	foreign.Tenant = "nobody"
	if _, err := wc.CancelBound(ctx, foreign); err == nil {
		t.Fatal("an unmapped tenant was cancelled")
	}

	intentID := uuid.New()
	guardID := uuid.New()
	withTx(func(tx dbport.Tx) error {
		if _, err := promotionguard.Admit(ctx, tx, tenantID, guardID, "EMPLOYMENT:wfrun010", "2027-01-01", "key-wfrun010"); err != nil {
			return err
		}
		return promotionguard.Confirm(ctx, tx, tenantID, guardID, "key-wfrun010", intentID)
	})
	if err := release(ctx, "wfrun010-app", intentID.String(), at); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := db.Conn.QueryRow(ctx, `SELECT status FROM promotion_active_intent_guard WHERE guard_id = $1`, guardID).Scan(&status); err != nil || status != "CLOSED" {
		t.Fatalf("guard after release = %s, %v", status, err)
	}
	if err := release(ctx, "wfrun010-app", "not-a-uuid", at); err == nil {
		t.Fatal("a malformed intent id was released")
	}
}
