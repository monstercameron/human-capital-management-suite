package application

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/truststore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type wfrun010Instance struct {
	id      string
	version int64
	status  string
}

// wfrun010Instance reads the most recently started workflow instance (the one
// the latest proposeAndExecute bound).
func (h *promoux015Harness) wfrun010Instance() wfrun010Instance {
	h.t.Helper()
	var out wfrun010Instance
	if err := h.pool.QueryRow(context.Background(), `
		SELECT instance_id::text, instance_version, runtime_status
		FROM workflow_instance ORDER BY created_at DESC LIMIT 1`).Scan(&out.id, &out.version, &out.status); err != nil {
		h.t.Fatalf("read the latest workflow instance: %v", err)
	}
	return out
}

func (h *promoux015Harness) wfrun010Count(sql string, args ...any) int64 {
	h.t.Helper()
	var n int64
	if err := h.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		h.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func (h *promoux015Harness) wfrun010Cancel(persona, intentID, key string) *intentsv1.IntentInstance {
	h.t.Helper()
	client := h.intents()
	got, err := client.GetIntent(h.rpc(persona), &intentsv1.GetIntentRequest{IntentId: intentID})
	if err != nil {
		h.t.Fatalf("GetIntent: %v", err)
	}
	resp, err := client.CancelIntent(h.rpc(persona), &intentsv1.CancelIntentRequest{IntentId: intentID, IdempotencyKey: key,
		ExpectedInstanceVersion: got.GetIntent().GetInstanceVersion(), ReasonRef: "WF-RUN-010"})
	if err != nil {
		h.t.Fatalf("CancelIntent(%s): %v", intentID, err)
	}
	return resp.GetIntent()
}

func wfrun010Disposition(msg *intentsv1.IntentInstance) string {
	d := msg.GetCancellationDecisions()
	if len(d) == 0 {
		return ""
	}
	return d[len(d)-1].GetEffectDispositionRef()
}

// TestTodo_WF_RUN_010_Served proves governed cancellation on the real serve
// composition over PostgreSQL:
//
//   - CancelIntent on a served promotion that is running (no effect yet) cancels
//     the bound workflow instance through the governed decision: the instance
//     is CANCELLED with a recorded decision and phase/effect evidence, its node
//     history is intact, and the intent's promotion admission window is
//     released (a fresh proposal for the same worker and date is admitted);
//   - after the promotion's execute_promotion effect has succeeded (it
//     declares no compensation), concurrent operator CancelWorkflow calls under
//     different keys all resolve to the same recorded CANNOT_CANCEL decision
//     (TOO_LATE / EFFECT_COMMITTED), the instance is untouched, and CancelIntent
//     reports TOO_LATE without cancelling the intent or releasing its window.
func TestTodo_WF_RUN_010_Served(t *testing.T) {
	h := promoux015Compose(t)
	ctx := context.Background()
	tenant := pgstore.TenantID(demoworkforce.CompanyKey)

	// Phase 1: cancel before any effect.
	first := h.proposeAndExecute()
	bound := h.wfrun010Instance()
	nodesBefore := h.wfrun010Count(`SELECT count(*) FROM workflow_node_execution WHERE instance_id = $1::uuid`, bound.id)
	cancelled := h.wfrun010Cancel("admin", first, "wfrun010-cancel-first")
	if got := wfrun010Disposition(cancelled); got != "CANCELLED" || cancelled.GetLifecycle().GetRequest() != intentsv1.RequestState_REQUEST_STATE_CANCELLED {
		t.Fatalf("CancelIntent before any effect = %s %s", got, cancelled.GetLifecycle().GetRequest())
	}
	after := h.wfrun010Instance()
	if after.status != "CANCELLED" || after.version != bound.version+2 {
		t.Fatalf("bound instance after CancelIntent = %+v (was %+v)", after, bound)
	}
	var decision, phase, statusBefore string
	var evidence []byte
	if err := h.pool.QueryRow(ctx, `SELECT decision, phase, status_before, evidence FROM workflow_cancellation_decision WHERE instance_id = $1::uuid`, bound.id).
		Scan(&decision, &phase, &statusBefore, &evidence); err != nil {
		t.Fatalf("decision row: %v", err)
	}
	var doc struct {
		Outcome struct {
			Phase  string `json:"phase"`
			Digest string `json:"digest"`
		} `json:"outcome"`
	}
	if err := json.Unmarshal(evidence, &doc); err != nil || decision != "CANCELLED" || phase == "" || statusBefore != bound.status ||
		doc.Outcome.Phase != phase || doc.Outcome.Digest == "" {
		t.Fatalf("decision = %s %s %s %s, %v", decision, phase, statusBefore, evidence, err)
	}
	if n := h.wfrun010Count(`SELECT count(*) FROM workflow_node_execution WHERE instance_id = $1::uuid`, bound.id); n != nodesBefore {
		t.Fatalf("node history %d -> %d rows across cancellation", nodesBefore, n)
	}
	if n := h.wfrun010Count(`SELECT count(*) FROM promotion_active_intent_guard WHERE intent_id = $1::uuid AND status = 'ACTIVE'`, first); n != 0 {
		t.Fatalf("the cancelled intent still holds %d active admission windows", n)
	}

	// Phase 2: the released window admits a fresh proposal for the same
	// worker and date; its core effect then commits.
	second := h.proposeAndExecute()
	live := h.wfrun010Instance()
	instanceID := uuid.MustParse(live.id)
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatal(err)
	}
	func(tx dbport.Tx) {
		defer func() { _ = tx.Rollback(ctx) }()
		store := runtime.Store{}
		exec := runtime.NewNodeExecution(tenant, instanceID, promotionexec.NodeExecutePromotion, 1, "CAPABILITY", runtime.NodeReady)
		exec.RecordedAt = time.Now().UTC()
		_, version, err := store.RecordNodeExecution(ctx, tx, exec, live.version)
		for _, status := range []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeSucceeded} {
			if err != nil {
				break
			}
			_, version, err = store.RecordNodeTransition(ctx, tx, runtime.NodeTransition{TenantID: tenant, InstanceID: instanceID,
				NodeID: promotionexec.NodeExecutePromotion, Attempt: 1, ExpectedInstanceVersion: version, Status: status})
		}
		if err != nil {
			t.Fatalf("record the committed effect: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}(tx)
	effected := h.wfrun010Instance()

	admin := h.principals["admin"]
	now := time.Now().UTC()
	scope, _ := json.Marshal(truststore.JITGrantScope{Role: string(jit.RoleIncidentResponder), TicketRef: "INC-010", Justification: "cancel committed promotion",
		Capabilities: []string{"WORKFLOW_CANCEL"}, Fields: []string{"workflow_instance:" + live.id}, Purpose: "incident repair"})
	if err := truststore.New(h.pool).PutJITGrant(ctx, tenant, truststore.JITGrantRecord{TenantID: tenant, RowID: uuid.New(), GrantID: "jit-wfrun010",
		Revision: 1, State: "ACTIVE", Requester: admin.Subject(), Approver: "principal:security-lead", Scope: scope,
		NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(2 * time.Hour)}); err != nil {
		t.Fatalf("record grant: %v", err)
	}
	conn, err := grpc.NewClient(h.composed.GRPCAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	workflows := workflowv1.NewWorkflowServiceClient(conn)
	const workers = 4
	receipts := make([]*workflowv1.CancelWorkflowResponse, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			receipts[i], errs[i] = workflows.CancelWorkflow(h.rpc("admin"), &workflowv1.CancelWorkflowRequest{IdempotencyKey: "wfrun010-race-" + uuid.NewString(),
				InstanceId: live.id, ExpectedInstanceVersion: uint64(effected.version), ReasonRef: "INC-010"})
		}(i)
	}
	close(start)
	wg.Wait()
	for i := range receipts {
		r := receipts[i].GetReceipt()
		if errs[i] != nil || r.GetOutcome() != workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_TOO_LATE || r.GetResultCode() != "EFFECT_COMMITTED" {
			t.Fatalf("worker %d cancel after the committed effect = %v, %v", i, r, errs[i])
		}
	}
	if n := h.wfrun010Count(`SELECT count(*) FROM workflow_cancellation_decision WHERE instance_id = $1::uuid`, live.id); n != 1 {
		t.Fatalf("concurrent cancels recorded %d decisions, want 1", n)
	}
	if n := h.wfrun010Count(`SELECT count(*) FROM workflow_cancellation_decision WHERE instance_id = $1::uuid AND decision = 'CANNOT_CANCEL'
		AND status_after = status_before AND 'EFFECT_IRREVERSIBLE|execute_promotion|execute_promotion#1' = ANY(reasons)`, live.id); n != 1 {
		t.Fatalf("the refusal is not recorded with its reason")
	}
	if got := h.wfrun010Instance(); got != effected {
		t.Fatalf("a refused cancellation changed the instance %+v -> %+v", effected, got)
	}

	late := h.wfrun010Cancel("admin", second, "wfrun010-cancel-second")
	if got := wfrun010Disposition(late); got != "TOO_LATE" || late.GetLifecycle().GetRequest() == intentsv1.RequestState_REQUEST_STATE_CANCELLED {
		t.Fatalf("CancelIntent after the committed effect = %s %s", got, late.GetLifecycle().GetRequest())
	}
	if got := h.wfrun010Instance(); got != effected {
		t.Fatalf("CancelIntent changed the effected instance %+v -> %+v", effected, got)
	}
	if n := h.wfrun010Count(`SELECT count(*) FROM promotion_active_intent_guard WHERE intent_id = $1::uuid AND status = 'ACTIVE'`, second); n != 1 {
		t.Fatalf("a TOO_LATE cancel released the admission window (active = %d)", n)
	}
}
