package application

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/truststore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
)

// TestGovernedWorkflowControlsOnComposedServer proves EP-WF-002's controls are
// live on the real serve composition: with the operator's JIT grant recorded
// in the durable trust store, PauseWorkflow and ResumeWorkflow apply over gRPC
// with a receipt; the same operator without a grant, and a cancel without the
// dual control it demands, are governed denials that change nothing. A retry is
// dry-run first so it is judged on the node's real state, and a grant narrowed
// to the instance and approved by a second person cancels it.
func TestGovernedWorkflowControlsOnComposedServer(t *testing.T) {
	h := promoux015Compose(t)
	h.proposeAndExecute()
	ctx := context.Background()
	var instanceID string
	var version int64
	if err := h.pool.QueryRow(ctx, `SELECT instance_id::text, instance_version FROM workflow_instance ORDER BY created_at DESC LIMIT 1`).Scan(&instanceID, &version); err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(h.composed.GRPCAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := workflowv1.NewWorkflowServiceClient(conn)

	// No grant yet: denied, nothing changes.
	denied, err := client.PauseWorkflow(h.rpc("admin"), &workflowv1.PauseWorkflowRequest{IdempotencyKey: "pause-no-grant", InstanceId: instanceID, ExpectedInstanceVersion: uint64(version), ReasonRef: "INC-1"})
	if err != nil || denied.GetReceipt().GetOutcome() != workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_DENIED || denied.GetReceipt().GetResultCode() != "OPERATOR_AUTHORITY_REQUIRED" {
		t.Fatalf("pause without a grant = %v, %v", denied, err)
	}

	admin := h.principals["admin"]
	tenant := pgstore.TenantID(demoworkforce.CompanyKey)
	now := time.Now().UTC()
	scope, _ := json.Marshal(truststore.JITGrantScope{Role: string(jit.RoleIncidentResponder), TicketRef: "INC-1", Justification: "stuck promotion",
		Capabilities: []string{"WORKFLOW_PAUSE", "WORKFLOW_RESUME", "WORKFLOW_CANCEL", "WORKFLOW_RETRY_NODE"}, Purpose: "incident repair"})
	if err := truststore.New(h.pool).PutJITGrant(ctx, tenant, truststore.JITGrantRecord{TenantID: tenant, RowID: uuid.New(), GrantID: "jit-e2e",
		Revision: 1, State: "ACTIVE", Requester: admin.Subject(), Approver: "principal:security-lead", Scope: scope,
		NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(2 * time.Hour)}); err != nil {
		t.Fatalf("record grant: %v", err)
	}

	paused, err := client.PauseWorkflow(h.rpc("admin"), &workflowv1.PauseWorkflowRequest{IdempotencyKey: "pause-1", InstanceId: instanceID, ExpectedInstanceVersion: uint64(version), ReasonRef: "INC-1"})
	if err != nil {
		t.Fatalf("PauseWorkflow: %v", err)
	}
	receipt := paused.GetReceipt()
	if receipt.GetOutcome() != workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_APPLIED || receipt.GetIntentInstanceId() == "" || receipt.GetInstanceStatus() != "PAUSED" {
		t.Fatalf("governed pause = %v", paused)
	}
	resumed, err := client.ResumeWorkflow(h.rpc("admin"), &workflowv1.ResumeWorkflowRequest{IdempotencyKey: "resume-1", InstanceId: instanceID, ExpectedInstanceVersion: receipt.GetInstanceVersion(), ReasonRef: "INC-1"})
	if err != nil || resumed.GetReceipt().GetOutcome() != workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_APPLIED {
		t.Fatalf("governed resume = %v, %v", resumed, err)
	}
	cancel, err := client.CancelWorkflow(h.rpc("admin"), &workflowv1.CancelWorkflowRequest{IdempotencyKey: "cancel-1", InstanceId: instanceID, ExpectedInstanceVersion: resumed.GetReceipt().GetInstanceVersion(), ReasonRef: "INC-1"})
	if err != nil || cancel.GetReceipt().GetOutcome() != workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_DENIED || cancel.GetReceipt().GetResultCode() != "OPERATOR_DUAL_CONTROL_REQUIRED" {
		t.Fatalf("cancel without dual control = %v, %v", cancel, err)
	}
	var status string
	if err := h.pool.QueryRow(ctx, `SELECT runtime_status FROM workflow_instance WHERE instance_id = $1::uuid`, instanceID).Scan(&status); err != nil || status != "RUNNING" && status != "WAITING" {
		t.Fatalf("instance after governed controls = %s, %v", status, err)
	}

	// A retry is dry-run first, so it is judged on the node's real state
	// rather than refused for missing simulation evidence.
	var nodeID string
	var attempt int
	if err := h.pool.QueryRow(ctx, `SELECT node_id, attempt FROM workflow_node_execution WHERE instance_id = $1::uuid ORDER BY recorded_at DESC LIMIT 1`, instanceID).Scan(&nodeID, &attempt); err != nil {
		t.Fatal(err)
	}
	retry, err := client.RetryNode(h.rpc("admin"), &workflowv1.RetryNodeRequest{IdempotencyKey: "retry-1", InstanceId: instanceID, NodeId: nodeID, ExpectedAttempt: uint32(attempt), ReasonRef: "INC-1"})
	if err != nil || retry.GetReceipt().GetIntentInstanceId() == "" || strings.HasPrefix(retry.GetReceipt().GetResultCode(), "OPERATOR_") || retry.GetReceipt().GetOutcome() == workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_UNSPECIFIED {
		t.Fatalf("retry with preflight simulation = %v, %v", retry, err)
	}

	// A grant narrowed to this instance, approved by a second person, is
	// the dual control a cancel needs: it applies.
	scoped, _ := json.Marshal(truststore.JITGrantScope{Role: string(jit.RoleIncidentResponder), TicketRef: "INC-1", Justification: "cancel stuck promotion",
		Capabilities: []string{"WORKFLOW_CANCEL"}, Fields: []string{"workflow_instance:" + instanceID}, Purpose: "incident repair"})
	if err := truststore.New(h.pool).PutJITGrant(ctx, tenant, truststore.JITGrantRecord{TenantID: tenant, RowID: uuid.New(), GrantID: "jit-e2e-cancel",
		Revision: 1, State: "ACTIVE", Requester: admin.Subject(), Approver: "principal:security-lead", Scope: scoped,
		NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(2 * time.Hour)}); err != nil {
		t.Fatalf("record instance-scoped grant: %v", err)
	}
	var liveVersion int64
	if err := h.pool.QueryRow(ctx, `SELECT instance_version FROM workflow_instance WHERE instance_id = $1::uuid`, instanceID).Scan(&liveVersion); err != nil {
		t.Fatal(err)
	}
	cancelled, err := client.CancelWorkflow(h.rpc("admin"), &workflowv1.CancelWorkflowRequest{IdempotencyKey: "cancel-2", InstanceId: instanceID, ExpectedInstanceVersion: uint64(liveVersion), ReasonRef: "INC-1"})
	if err != nil {
		t.Fatalf("CancelWorkflow: %v", err)
	}
	got := cancelled.GetReceipt()
	t.Logf("cancel %s %s; retry %s %s", got.GetOutcome(), got.GetResultCode(), retry.GetReceipt().GetOutcome(), retry.GetReceipt().GetResultCode())
	if got.GetOutcome() != workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_APPLIED || got.GetIntentInstanceId() == "" || got.GetInstanceStatus() != "CANCELLED" {
		t.Fatalf("instance-scoped dual-control cancel = %v", got)
	}
	if err := h.pool.QueryRow(ctx, `SELECT runtime_status FROM workflow_instance WHERE instance_id = $1::uuid`, instanceID).Scan(&status); err != nil || status != "CANCELLED" {
		t.Fatalf("instance after governed cancel = %s, %v", status, err)
	}

	// The control receipt is durable: it is a row in operator_control_receipt,
	// and re-sending the same idempotency key replays it without a second
	// transition.
	var outcome, digest string
	rtx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rtx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, rtx, tenant); err != nil {
		t.Fatal(err)
	}
	if err := rtx.QueryRow(ctx, `SELECT outcome, receipt->>'digest' FROM operator_control_receipt WHERE idempotency_key = 'cancel-2'`).Scan(&outcome, &digest); err != nil || outcome != "APPLIED" || digest != got.GetReceiptDigest() {
		t.Fatalf("durable cancel receipt = %s %s, %v; want APPLIED %s", outcome, digest, err, got.GetReceiptDigest())
	}
	var versionBefore int64
	if err := h.pool.QueryRow(ctx, `SELECT instance_version FROM workflow_instance WHERE instance_id = $1::uuid`, instanceID).Scan(&versionBefore); err != nil {
		t.Fatal(err)
	}
	replayed, err := client.CancelWorkflow(h.rpc("admin"), &workflowv1.CancelWorkflowRequest{IdempotencyKey: "cancel-2", InstanceId: instanceID, ExpectedInstanceVersion: uint64(liveVersion), ReasonRef: "INC-1"})
	if err != nil || replayed.GetReceipt().GetReceiptDigest() != got.GetReceiptDigest() || replayed.GetReceipt().GetOutcome() != got.GetOutcome() {
		t.Fatalf("replayed cancel = %v, %v; want the recorded receipt", replayed, err)
	}
	var versionAfter int64
	if err := h.pool.QueryRow(ctx, `SELECT instance_version FROM workflow_instance WHERE instance_id = $1::uuid`, instanceID).Scan(&versionAfter); err != nil || versionAfter != versionBefore {
		t.Fatalf("a replayed cancel changed the instance version %d -> %d (%v)", versionBefore, versionAfter, err)
	}
}
