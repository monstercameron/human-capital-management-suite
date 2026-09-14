package application

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/truststore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
)

// TestGovernedWorkflowControlsOnComposedServer proves EP-WF-002's controls are
// live on the real serve composition: with the operator's JIT grant recorded
// in the durable trust store, PauseWorkflow and ResumeWorkflow apply over gRPC
// with a receipt; the same operator without a grant, and a cancel without the
// dual control it demands, are governed denials that change nothing.
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
		Capabilities: []string{"WORKFLOW_PAUSE", "WORKFLOW_RESUME", "WORKFLOW_CANCEL"}, Purpose: "incident repair"})
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
}
