package application

// PROMO-EXEC-004: drive the work queue over a live promotion approval and
// prove the control branches. Against one live promotion the finance item is
// listed, claimed and released over the WorkService RPCs and the journey
// decision advances the run; on the manager approval a plain completion is
// refused, separation of duties refuses the sibling decider's queue
// decision, and the current manager's queue decision completes the item.
// Pause, resume, cancel and retry-node report their real outcome branches on
// the instance.
//
// The served composition wires the queue write ports (workqueue_writes.go):
// without them Claim/Decide answer UNAVAILABLE.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	humanworkv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/humanwork/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/truststore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// promoexec004Work dials the served cell's WorkService.
func promoexec004Work(t *testing.T, h *promoux015Harness) humanworkv1.WorkServiceClient {
	t.Helper()
	conn, err := grpc.NewClient(h.composed.GRPCAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc client: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return humanworkv1.NewWorkServiceClient(conn)
}

// promoexec004AssignedItem finds the caller's assigned item in one queue
// listing, failing when the queue does not route it.
func promoexec004AssignedItem(t *testing.T, work humanworkv1.WorkServiceClient, rpc func() (context.Context, context.CancelFunc), assignee, what string) (itemID string, version uint64, proposalRev string) {
	t.Helper()
	ctx, cancel := rpc()
	defer cancel()
	listed, err := work.ListWorkItems(ctx, &humanworkv1.ListWorkItemsRequest{})
	if err != nil {
		t.Fatalf("ListWorkItems(%s): %v", what, err)
	}
	for _, item := range listed.GetWorkItems() {
		if item.GetAssignedPrincipalId() != assignee {
			continue
		}
		if item.GetStatus() != humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_ASSIGNED {
			t.Fatalf("%s item status = %s, want ASSIGNED", what, item.GetStatus())
		}
		return item.GetWorkItemId(), item.GetItemVersion(), item.GetProposalRef()
	}
	t.Fatalf("%s lists no assigned item among %d", what, len(listed.GetWorkItems()))
	return "", 0, ""
}

// TestTodo_PROMO_EXEC_004_LiveWorkQueue drives the finance approval through
// the served work queue (list, get, claim, release) before the journey
// decision advances the run; on the manager approval a plain completion is
// refused, the sibling decider is refused by separation of duties, and the
// current manager's queue decision completes the item.
func TestTodo_PROMO_EXEC_004_LiveWorkQueue(t *testing.T) {
	h := promoux015Compose(t)
	intentID := h.proposeAndExecute()
	work := promoexec004Work(t, h)

	financeRPC := func() (context.Context, context.CancelFunc) { return context.WithCancel(h.rpc("finance-partner")) }
	itemID, version, proposalRev := promoexec004AssignedItem(t, work, financeRPC, promoux015Finance, "finance")
	if proposalRev == "" {
		t.Fatal("the finance item names no proposal revision")
	}
	got, err := work.GetWorkItem(h.rpc("finance-partner"), &humanworkv1.GetWorkItemRequest{WorkItemId: itemID})
	if err != nil {
		t.Fatalf("GetWorkItem as the finance partner: %v", err)
	}
	if got.GetWorkItem().GetWorkItemId() != itemID || got.GetWorkItem().GetItemVersion() != version {
		t.Fatalf("GetWorkItem = %+v, want the listed finance item", got.GetWorkItem())
	}

	claimed, err := work.ClaimWorkItem(h.rpc("finance-partner"), &humanworkv1.ClaimWorkItemRequest{
		WorkItemId: itemID, IdempotencyKey: "promo-exec-004-claim-1", ExpectedItemVersion: version})
	if err != nil {
		t.Fatalf("ClaimWorkItem as the finance partner: %v", err)
	}
	if claimed.GetWorkItem().GetStatus() != humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_CLAIMED ||
		claimed.GetWorkItem().GetClaimedBy() != promoux015Finance {
		t.Fatalf("claimed item = %+v, want CLAIMED by %s", claimed.GetWorkItem(), promoux015Finance)
	}

	released, err := work.ReleaseWorkItem(h.rpc("finance-partner"), &humanworkv1.ReleaseWorkItemRequest{
		WorkItemId: itemID, IdempotencyKey: "promo-exec-004-release-1", ExpectedItemVersion: claimed.GetWorkItem().GetItemVersion()})
	if err != nil {
		t.Fatalf("ReleaseWorkItem as the finance partner: %v", err)
	}
	if released.GetWorkItem().GetStatus() != humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_ASSIGNED {
		t.Fatalf("released item status = %s, want ASSIGNED", released.GetWorkItem().GetStatus())
	}

	// The journey decision still owns advancement: it claims the released
	// item fresh and advances the run to the manager approval.
	engine, financeCtx := h.engine("finance-partner")
	decided, err := engine.Decide(financeCtx, intentID, workspace.Decision{Approve: true, Reason: "finance approves"})
	if err != nil {
		t.Fatalf("Decide(finance): %v", err)
	}
	if decided.Summary.Stage != workspace.JourneyStageManagerApproval {
		t.Fatalf("after finance stage = %s, want MANAGER_APPROVAL", decided.Summary.Stage)
	}

	manager := h.items(intentID)[promotionexec.NodeApproveManager]
	if manager.status != "ASSIGNED" || manager.owner != promoux015Manager {
		t.Fatalf("manager approval = %+v, want ASSIGNED to %s", manager, promoux015Manager)
	}
	managerRPC := func() (context.Context, context.CancelFunc) { return context.WithCancel(h.rpc("admin")) }
	managerID, _, managerProposal := promoexec004AssignedItem(t, work, managerRPC, promoux015Manager, "manager")
	// The finance decider joins the manager approval as its assignee, so
	// the later refusal is separation of duties and nothing else.
	h.promoux015Delegate(promotionexec.NodeApproveManager, promoux015Manager, promoux015Finance, promoux015Proposer)
	delegated, err := work.GetWorkItem(h.rpc("finance-partner"), &humanworkv1.GetWorkItemRequest{WorkItemId: managerID})
	if err != nil {
		t.Fatalf("GetWorkItem(manager) after delegation: %v", err)
	}
	if delegated.GetWorkItem().GetAssignedPrincipalId() != promoux015Finance {
		t.Fatalf("delegated manager item assignee = %s, want %s", delegated.GetWorkItem().GetAssignedPrincipalId(), promoux015Finance)
	}
	mclaimed, err := work.ClaimWorkItem(h.rpc("finance-partner"), &humanworkv1.ClaimWorkItemRequest{
		WorkItemId: managerID, IdempotencyKey: "promo-exec-004-claim-2", ExpectedItemVersion: delegated.GetWorkItem().GetItemVersion()})
	if err != nil {
		t.Fatalf("ClaimWorkItem(manager) as the delegate: %v", err)
	}

	// A plain completion cannot bypass the approval decision.
	if _, err := work.CompleteWorkItem(h.rpc("finance-partner"), &humanworkv1.CompleteWorkItemRequest{
		WorkItemId: managerID, IdempotencyKey: "promo-exec-004-complete-1",
		ExpectedItemVersion: mclaimed.GetWorkItem().GetItemVersion(),
		OutputArtifactRef:   "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		EvidenceRefs:        []*commonv1.EvidenceRef{{EvidenceId: "ev:promo-exec-004", Digest: "sha256:aa"}}}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("CompleteWorkItem on an approval = %v, want FAILED_PRECONDITION", err)
	}

	// The delegate already decided finance and is still refused the
	// manager decision: one principal never decides both requirements.
	sibling, err := work.DecideApproval(h.rpc("finance-partner"), &humanworkv1.DecideApprovalRequest{
		WorkItemId: managerID, IdempotencyKey: "promo-exec-004-sibling-1",
		ExpectedItemVersion: mclaimed.GetWorkItem().GetItemVersion(),
		ProposalRevisionId:  managerProposal, Decision: intentsv1.ApprovalDecisionKind_APPROVAL_DECISION_KIND_APPROVE,
		ReasonRef: "manager.approval/v1"})
	if err == nil {
		t.Fatalf("DecideApproval(manager) by the finance decider = %v, want the separation refusal", sibling)
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("DecideApproval(manager) by the finance decider = %v, want PERMISSION_DENIED", err)
	}
	if _, err := work.ReleaseWorkItem(h.rpc("finance-partner"), &humanworkv1.ReleaseWorkItemRequest{
		WorkItemId: managerID, IdempotencyKey: "promo-exec-004-release-2", ExpectedItemVersion: mclaimed.GetWorkItem().GetItemVersion()}); err != nil {
		t.Fatalf("ReleaseWorkItem(manager) as the refused delegate: %v", err)
	}

	// The current manager rejoins and decides through the same queue.
	h.promoux015Delegate(promotionexec.NodeApproveManager, promoux015Finance, promoux015Manager, promoux015Proposer)
	rejoined, err := work.GetWorkItem(h.rpc("admin"), &humanworkv1.GetWorkItemRequest{WorkItemId: managerID})
	if err != nil {
		t.Fatalf("GetWorkItem(manager) after the manager rejoins: %v", err)
	}
	rclaimed, err := work.ClaimWorkItem(h.rpc("admin"), &humanworkv1.ClaimWorkItemRequest{
		WorkItemId: managerID, IdempotencyKey: "promo-exec-004-claim-3", ExpectedItemVersion: rejoined.GetWorkItem().GetItemVersion()})
	if err != nil {
		t.Fatalf("ClaimWorkItem(manager) as the rejoined manager: %v", err)
	}
	approved, err := work.DecideApproval(h.rpc("admin"), &humanworkv1.DecideApprovalRequest{
		WorkItemId: managerID, IdempotencyKey: "promo-exec-004-decide-1",
		ExpectedItemVersion: rclaimed.GetWorkItem().GetItemVersion(),
		ProposalRevisionId:  rclaimed.GetWorkItem().GetProposalRef(), Decision: intentsv1.ApprovalDecisionKind_APPROVAL_DECISION_KIND_APPROVE,
		ReasonRef: "manager.approval/v1"})
	if err != nil {
		t.Fatalf("DecideApproval(manager) as the manager: %v", err)
	}
	if approved.GetDecision().GetDecidingPrincipal().GetPrincipalId() != promoux015Manager {
		t.Fatalf("manager decision = %+v, want it decided by %s", approved.GetDecision(), promoux015Manager)
	}
	final, err := work.GetWorkItem(h.rpc("admin"), &humanworkv1.GetWorkItemRequest{WorkItemId: managerID})
	if err != nil {
		t.Fatalf("GetWorkItem(manager) after the decision: %v", err)
	}
	if final.GetWorkItem().GetStatus() != humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_COMPLETED {
		t.Fatalf("manager item status = %s, want COMPLETED", final.GetWorkItem().GetStatus())
	}
}

// TestTodo_PROMO_EXEC_004_LiveWorkQueue_Security proves the queue refuses
// before any write: no credential, a non-member claimant, a cross-tenant
// scope and a stale version.
func TestTodo_PROMO_EXEC_004_LiveWorkQueue_Security(t *testing.T) {
	h := promoux015Compose(t)
	h.proposeAndExecute()
	work := promoexec004Work(t, h)
	ctx := context.Background()

	financeRPC := func() (context.Context, context.CancelFunc) { return context.WithCancel(h.rpc("finance-partner")) }
	itemID, version, _ := promoexec004AssignedItem(t, work, financeRPC, promoux015Finance, "finance")

	if _, err := work.ClaimWorkItem(ctx, &humanworkv1.ClaimWorkItemRequest{
		WorkItemId: itemID, IdempotencyKey: "promo-exec-004-anon-1", ExpectedItemVersion: version}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("ClaimWorkItem with no credential = %v, want UNAUTHENTICATED", err)
	}
	// The operator holds the execution role but is no member of the finance
	// approval: the queue does not even disclose the item.
	if _, err := work.ClaimWorkItem(h.rpc("admin"), &humanworkv1.ClaimWorkItemRequest{
		WorkItemId: itemID, IdempotencyKey: "promo-exec-004-nonmember-1", ExpectedItemVersion: version}); status.Code(err) != codes.NotFound {
		t.Fatalf("ClaimWorkItem as a non-member = %v, want NOT_FOUND", err)
	}
	// A caller-supplied scope naming another tenant is a forged trusted
	// field, refused before any read.
	if _, err := work.GetWorkItem(h.rpc("finance-partner"), &humanworkv1.GetWorkItemRequest{
		WorkItemId: itemID, Scope: &commonv1.ScopeContext{TenantId: uuid.NewString()}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("GetWorkItem with a forged scope = %v, want INVALID_ARGUMENT", err)
	}
	// A credential naming an unknown tenant authenticates nothing: the
	// verifier admits only the served tenant before any read.
	now := time.Now()
	foreignToken, err := h.verifier.Issue(trust.Claims{
		Issuer: h.cfg.Issuer, Audience: h.cfg.Audience, Subject: promoux015Finance, SubjectKind: "human", Tenant: "foreign-tenant",
		OrganizationScopeID: "org:foreign-tenant:people-ops", Roles: []string{"comp_admin"},
		Purposes: []string{"compensation_review"}, AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "session-promo-exec-004-foreign",
		IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue foreign credential: %v", err)
	}
	foreign, err := h.verifier.Verify(ctx, trust.Credential{Scheme: "Bearer", Token: foreignToken, Audience: h.cfg.Audience})
	if err != nil {
		t.Fatalf("verify foreign credential: %v", err)
	}
	if _, err := work.GetWorkItem(trust.WithPrincipal(ctx, foreign), &humanworkv1.GetWorkItemRequest{WorkItemId: itemID}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("GetWorkItem on an unknown tenant = %v, want UNAUTHENTICATED", err)
	}
	if _, err := work.ClaimWorkItem(h.rpc("finance-partner"), &humanworkv1.ClaimWorkItemRequest{
		WorkItemId: itemID, IdempotencyKey: "promo-exec-004-stale-1", ExpectedItemVersion: version + 100}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("ClaimWorkItem at a stale version = %v, want FAILED_PRECONDITION", err)
	}
	if _, err := work.DecideApproval(h.rpc("finance-partner"), &humanworkv1.DecideApprovalRequest{
		WorkItemId: itemID, IdempotencyKey: "promo-exec-004-stale-2",
		ExpectedItemVersion: version + 100, ProposalRevisionId: "sha256:stale",
		Decision: intentsv1.ApprovalDecisionKind_APPROVAL_DECISION_KIND_APPROVE, ReasonRef: "finance.approval/v1"}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("DecideApproval at a stale version = %v, want FAILED_PRECONDITION", err)
	}
}

// TestTodo_PROMO_EXEC_004_LiveWorkQueue_Integration proves the queue
// responses read the live store and the instance controls report their real
// branches: every listed item exists with its listed version, the claim
// transition is durable, and pause, resume, a retry of a settled node and
// both cancels return the governed outcomes.
func TestTodo_PROMO_EXEC_004_LiveWorkQueue_Integration(t *testing.T) {
	h := promoux015Compose(t)
	h.proposeAndExecute()
	work := promoexec004Work(t, h)
	ctx := context.Background()

	financeRPC := func() (context.Context, context.CancelFunc) { return context.WithCancel(h.rpc("finance-partner")) }
	itemID, version, _ := promoexec004AssignedItem(t, work, financeRPC, promoux015Finance, "finance")
	listed, err := work.ListWorkItems(h.rpc("finance-partner"), &humanworkv1.ListWorkItemsRequest{})
	if err != nil {
		t.Fatalf("ListWorkItems as the finance partner: %v", err)
	}
	var stored int64
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM work_item`).Scan(&stored); err != nil {
		t.Fatalf("count stored work items: %v", err)
	}
	if int64(len(listed.GetWorkItems())) != stored {
		t.Fatalf("the queue lists %d items, the store holds %d", len(listed.GetWorkItems()), stored)
	}
	for _, item := range listed.GetWorkItems() {
		var itemVersion int64
		if err := h.pool.QueryRow(ctx, `SELECT item_version FROM work_item WHERE work_item_id = $1::uuid`, item.GetWorkItemId()).Scan(&itemVersion); err != nil {
			t.Fatalf("read stored %s: %v", item.GetWorkItemId(), err)
		}
		if int64(item.GetItemVersion()) != itemVersion {
			t.Fatalf("listed %s at version %d, the store holds %d", item.GetWorkItemId(), item.GetItemVersion(), itemVersion)
		}
	}

	if _, err := work.ClaimWorkItem(h.rpc("finance-partner"), &humanworkv1.ClaimWorkItemRequest{
		WorkItemId: itemID, IdempotencyKey: "promo-exec-004-claim-3", ExpectedItemVersion: version}); err != nil {
		t.Fatalf("ClaimWorkItem as the finance partner: %v", err)
	}
	var transitions int64
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM work_item_transition WHERE work_item_id = $1::uuid AND reason = 'workitem.claimed_via_endpoint'`,
		itemID).Scan(&transitions); err != nil {
		t.Fatalf("count claim transitions: %v", err)
	}
	if transitions != 1 {
		t.Fatalf("claim transitions for %s = %d, want the durable one", itemID, transitions)
	}

	promoexec004Controls(t, h)
}

// promoexec004Controls proves the instance control branches on the live
// promotion: a grantless pause is denied, a granted pause and resume apply,
// a retry of a settled node is denied on its real state, a cancel without
// dual control is denied, and an instance-scoped dual-control cancel applies
// and is durable.
func promoexec004Controls(t *testing.T, h *promoux015Harness) {
	t.Helper()
	ctx := context.Background()
	var instanceID string
	var version int64
	if err := h.pool.QueryRow(ctx, `SELECT instance_id::text, instance_version FROM workflow_instance ORDER BY created_at DESC LIMIT 1`).Scan(&instanceID, &version); err != nil {
		t.Fatalf("read the live instance: %v", err)
	}
	conn, err := grpc.NewClient(h.composed.GRPCAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc client: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := workflowv1.NewWorkflowServiceClient(conn)

	denied, err := client.PauseWorkflow(h.rpc("admin"), &workflowv1.PauseWorkflowRequest{IdempotencyKey: "promo-exec-004-pause-no-grant", InstanceId: instanceID, ExpectedInstanceVersion: uint64(version), ReasonRef: "INC-004"})
	if err != nil || denied.GetReceipt().GetOutcome() != workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_DENIED ||
		denied.GetReceipt().GetResultCode() != "OPERATOR_AUTHORITY_REQUIRED" {
		t.Fatalf("pause without a grant = %v, %v; want DENIED/OPERATOR_AUTHORITY_REQUIRED", denied, err)
	}

	admin := h.principals["admin"]
	tenant := pgstore.TenantID(h.cfg.Tenant)
	now := time.Now().UTC()
	scope, _ := json.Marshal(truststore.JITGrantScope{Role: string(jit.RoleIncidentResponder), TicketRef: "INC-004", Justification: "promo-exec-004 live queue",
		Capabilities: []string{"WORKFLOW_PAUSE", "WORKFLOW_RESUME", "WORKFLOW_CANCEL", "WORKFLOW_RETRY_NODE"}, Purpose: "incident repair"})
	if err := truststore.New(h.pool).PutJITGrant(ctx, tenant, truststore.JITGrantRecord{TenantID: tenant, RowID: uuid.New(), GrantID: "jit-promo-exec-004",
		Revision: 1, State: "ACTIVE", Requester: admin.Subject(), Approver: "principal:security-lead", Scope: scope,
		NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(2 * time.Hour)}); err != nil {
		t.Fatalf("record grant: %v", err)
	}

	paused, err := client.PauseWorkflow(h.rpc("admin"), &workflowv1.PauseWorkflowRequest{IdempotencyKey: "promo-exec-004-pause-1", InstanceId: instanceID, ExpectedInstanceVersion: uint64(version), ReasonRef: "INC-004"})
	if err != nil {
		t.Fatalf("PauseWorkflow: %v", err)
	}
	receipt := paused.GetReceipt()
	if receipt.GetOutcome() != workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_APPLIED || receipt.GetInstanceStatus() != "PAUSED" {
		t.Fatalf("governed pause = %v, want APPLIED/PAUSED", paused)
	}
	resumed, err := client.ResumeWorkflow(h.rpc("admin"), &workflowv1.ResumeWorkflowRequest{IdempotencyKey: "promo-exec-004-resume-1", InstanceId: instanceID, ExpectedInstanceVersion: receipt.GetInstanceVersion(), ReasonRef: "INC-004"})
	if err != nil || resumed.GetReceipt().GetOutcome() != workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_APPLIED {
		t.Fatalf("governed resume = %v, %v", resumed, err)
	}

	// A retry is dry-run first, so a settled node is denied on its real
	// state rather than refused for missing evidence.
	var nodeID string
	var attempt int
	if err := h.pool.QueryRow(ctx, `SELECT node_id, attempt FROM workflow_node_execution WHERE instance_id = $1::uuid AND status = 'SUCCEEDED' ORDER BY recorded_at DESC LIMIT 1`,
		instanceID).Scan(&nodeID, &attempt); err != nil {
		t.Fatalf("read a settled node: %v", err)
	}
	retry, err := client.RetryNode(h.rpc("admin"), &workflowv1.RetryNodeRequest{IdempotencyKey: "promo-exec-004-retry-1", InstanceId: instanceID, NodeId: nodeID, ExpectedAttempt: uint32(attempt), ReasonRef: "INC-1"})
	if err != nil || retry.GetReceipt().GetOutcome() != workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_DENIED ||
		!strings.Contains(retry.GetReceipt().GetResultCode(), "NOT_FAILED") {
		t.Fatalf("retry of settled %s = %v, %v; want DENIED on its real state", nodeID, retry, err)
	}

	cancel, err := client.CancelWorkflow(h.rpc("admin"), &workflowv1.CancelWorkflowRequest{IdempotencyKey: "promo-exec-004-cancel-1", InstanceId: instanceID, ExpectedInstanceVersion: resumed.GetReceipt().GetInstanceVersion(), ReasonRef: "INC-004"})
	if err != nil || cancel.GetReceipt().GetOutcome() != workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_DENIED ||
		cancel.GetReceipt().GetResultCode() != "OPERATOR_DUAL_CONTROL_REQUIRED" {
		t.Fatalf("cancel without dual control = %v, %v; want DENIED/OPERATOR_DUAL_CONTROL_REQUIRED", cancel, err)
	}

	scoped, _ := json.Marshal(truststore.JITGrantScope{Role: string(jit.RoleIncidentResponder), TicketRef: "INC-004", Justification: "promo-exec-004 live queue",
		Capabilities: []string{"WORKFLOW_CANCEL"}, Fields: []string{"workflow_instance:" + instanceID}, Purpose: "incident repair"})
	if err := truststore.New(h.pool).PutJITGrant(ctx, tenant, truststore.JITGrantRecord{TenantID: tenant, RowID: uuid.New(), GrantID: "jit-promo-exec-004-cancel",
		Revision: 1, State: "ACTIVE", Requester: admin.Subject(), Approver: "principal:security-lead", Scope: scoped,
		NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(2 * time.Hour)}); err != nil {
		t.Fatalf("record instance-scoped grant: %v", err)
	}
	var liveVersion int64
	if err := h.pool.QueryRow(ctx, `SELECT instance_version FROM workflow_instance WHERE instance_id = $1::uuid`, instanceID).Scan(&liveVersion); err != nil {
		t.Fatalf("read the live instance version: %v", err)
	}
	cancelled, err := client.CancelWorkflow(h.rpc("admin"), &workflowv1.CancelWorkflowRequest{IdempotencyKey: "promo-exec-004-cancel-2", InstanceId: instanceID, ExpectedInstanceVersion: uint64(liveVersion), ReasonRef: "INC-004"})
	if err != nil {
		t.Fatalf("CancelWorkflow: %v", err)
	}
	if got := cancelled.GetReceipt(); got.GetOutcome() != workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_APPLIED || got.GetInstanceStatus() != "CANCELLED" {
		t.Fatalf("instance-scoped dual-control cancel = %v, want APPLIED/CANCELLED", cancelled)
	}
	var status string
	if err := h.pool.QueryRow(ctx, `SELECT runtime_status FROM workflow_instance WHERE instance_id = $1::uuid`, instanceID).Scan(&status); err != nil || status != "CANCELLED" {
		t.Fatalf("instance after governed cancel = %s, %v", status, err)
	}
}
