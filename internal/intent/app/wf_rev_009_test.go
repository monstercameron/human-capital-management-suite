package app

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator/workflowcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/cancellation"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func TestTodo_WF_REV_009_IntentLifecycle(t *testing.T) {
	const id = "20000000-0000-0000-0000-000000000091"
	h := newLifecycleHarness(t)
	h.seed(t, id, lifecycleDims(lifecycle.RequestSubmitted, lifecycle.ExecutionNotPlanned))
	decisionID := uuid.MustParse("20000000-0000-0000-0000-000000000092")
	fake := &fakeWorkflowCancel{verdicts: map[string]BoundCancellationVerdict{
		id: {
			Bound: true, DecisionID: decisionID, Decision: workflow.CompensationRequired,
			HasSettlement: true, BusinessState: lifecycle.BusinessNotAchieved,
			ConsistencyState: lifecycle.ConsistencyDegraded, SettlementDecision: decisionID,
		},
	}, fail: map[string]error{}}
	h.Service.workflowCancel = fake
	resp, err := h.Service.CancelIntent(lifecycleCtx(t), &intentsv1.CancelIntentRequest{
		IntentId: id, IdempotencyKey: "wf-rev-009-lifecycle", ExpectedInstanceVersion: 1, ReasonRef: "reason:partial-reversal",
	})
	if err != nil {
		t.Fatalf("CancelIntent: %v", err)
	}
	msg := resp.GetIntent()
	want := lifecycle.Dimensions{
		Request: lifecycle.RequestSubmitted, Execution: lifecycle.ExecutionNotPlanned,
		Business: lifecycle.BusinessNotAchieved, Consistency: lifecycle.ConsistencyDegraded,
		Obligation: lifecycle.ObligationNotApplicable,
	}
	got, err := protomap.InstanceFromProto(msg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Lifecycle != want {
		t.Fatalf("intent lifecycle = %+v, want %+v", got.Lifecycle, want)
	}
	if got.InstanceVersion != 2 {
		t.Fatalf("instance version = %d, want 2", got.InstanceVersion)
	}
}

func TestTodo_WF_REV_009_PersistedDecision(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	tenantKey := "wf-rev-009-" + tenantID.String()[:8]
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'wf-rev-009', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenantID, tenantKey)
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}

	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatal(err)
	}
	for i := range plan.Nodes {
		switch plan.Nodes[i].ID {
		case promotionexec.NodeCompensateHold:
			plan.Nodes[i].CompensationRef = &workflow.ResolvedReference{Kind: workflow.RefCompensation, ID: "compensation.hold.supersede", Version: "1"}
		case promotionexec.NodeExecutePromotion:
			plan.Nodes[i].CompensationRef = &workflow.ResolvedReference{Kind: workflow.RefCompensation, ID: "compensation.promotion.reverse", Version: "1"}
		}
	}
	const correlation = "corr-wf-rev-009-persisted"
	instanceID := uuid.New()
	recordedAt := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	inst, err := runtime.NewInstance(tenantID, instanceID, "cell-local", plan, workflow.ModeExecute,
		"sha256:wf-rev-009-input", correlation, recordedAt)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatal(err)
	}
	store := runtime.Store{}
	created, err := store.CreateInstance(ctx, tx, inst)
	if err != nil {
		t.Fatal(err)
	}
	running, err := store.RecordInstanceState(ctx, tx, runtime.InstanceTransition{TenantID: tenantID, InstanceID: instanceID,
		ExpectedVersion: created.InstanceVersion, Status: runtime.InstanceRunning,
		CurrentNodeIDs: []string{promotionexec.NodeObservePayroll}})
	if err != nil {
		t.Fatal(err)
	}
	version := running.InstanceVersion
	for _, nodeID := range []string{promotionexec.NodeCompensateHold, promotionexec.NodeExecutePromotion} {
		node, _ := plan.Node(nodeID)
		exec := runtime.NewNodeExecution(tenantID, instanceID, nodeID, 1, node.Type, runtime.NodeReady)
		exec.RecordedAt = recordedAt
		_, version, err = store.RecordNodeExecution(ctx, tx, exec, version)
		if err != nil {
			t.Fatal(err)
		}
		for _, status := range []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeSucceeded} {
			transition := runtime.NodeTransition{TenantID: tenantID, InstanceID: instanceID, NodeID: nodeID,
				Attempt: 1, ExpectedInstanceVersion: version, Status: status}
			if status == runtime.NodeSucceeded {
				transition.Refs.EffectRefs = []string{"effect:" + nodeID}
			}
			_, version, err = store.RecordNodeTransition(ctx, tx, transition)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	h := newLifecycleHarness(t)
	const intentID = "20000000-0000-0000-0000-000000000093"
	h.seed(t, intentID, lifecycleDims(lifecycle.RequestSubmitted, lifecycle.ExecutionNotPlanned),
		func(i *intent.Instance) { i.CorrelationID = correlation })
	h.Service.workflowCancel = executionWorkflowCancellation{
		db: conn, tenantUUID: func(values.TenantId) uuid.UUID { return tenantID },
		plans: workflowcontrol.PlanSet{plan.Digest(): plan},
	}
	resp, err := h.Service.CancelIntent(lifecycleCtx(t), &intentsv1.CancelIntentRequest{
		IntentId: intentID, IdempotencyKey: "wf-rev-009-persisted", ExpectedInstanceVersion: 1,
		ReasonRef: "reason:partial-reversal",
	})
	if err != nil {
		t.Fatalf("CancelIntent: %v", err)
	}
	projected, err := protomap.InstanceFromProto(resp.GetIntent())
	if err != nil {
		t.Fatal(err)
	}
	if projected.Lifecycle.Business != lifecycle.BusinessNotAchieved || projected.Lifecycle.Consistency != lifecycle.ConsistencyDegraded {
		t.Fatalf("intent did not reflect persisted settlement: %+v", projected.Lifecycle)
	}
	if projected.Lifecycle.Request != lifecycle.RequestSubmitted || projected.Lifecycle.Execution != lifecycle.ExecutionNotPlanned {
		t.Fatalf("settlement rewrote cancellation request or execution: %+v", projected.Lifecycle)
	}

	readTx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = readTx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, readTx, tenantID); err != nil {
		t.Fatal(err)
	}
	rows, err := cancellation.Decisions(ctx, readTx, tenantID, instanceID)
	if err != nil {
		t.Fatal(err)
	}
	settlement := cancellation.ProjectSettlement(rows)
	if len(rows) != 1 || len(settlement.Effects) != 2 || settlement.BusinessState != "NOT_ACHIEVED" || settlement.ConsistencyState != "DEGRADED" {
		t.Fatalf("persisted cancellation settlement = rows:%d %+v", len(rows), settlement)
	}
	states := map[string]cancellation.SettlementState{}
	for _, effect := range settlement.Effects {
		states[effect.EffectID] = effect.State
	}
	if states[promotionexec.NodeCompensateHold+"#1"] != cancellation.SettlementReversed ||
		states[promotionexec.NodeExecutePromotion+"#1"] != cancellation.SettlementUnresolved {
		t.Fatalf("persisted effect settlement states = %v", states)
	}
}

var _ dbport.Beginner = (*pgxadapter.Conn)(nil)
