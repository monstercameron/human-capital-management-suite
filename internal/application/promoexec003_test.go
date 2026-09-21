package application

// PROMO-EXEC-003: pin the workflow inspectors and ADMIN-008 to a live
// promotion execution. Against a real execute-mode promotion instance the
// WorkflowService inspectors must return the live node executions,
// driver-created work items and transitions, governance and terminal evidence
// refs and the execution mode; cross-tenant and unauthorized reads are
// refused.
//
// The live-run fixture is the served PROMOUX-015 composition the control
// tests drive (promoux015Compose + runSeparatedPromotion): embedded
// PostgreSQL, ComposeServe with the executable plan, the demo workforce and
// the real gRPC surface the cell serves.

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	adminpolicy "github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// promoexec003Surfaces dials the served cell's own gRPC listener: the same
// WorkflowService and AdminService the deployment serves, not a test-only
// mount.
func promoexec003Surfaces(t *testing.T, h *promoux015Harness) (workflowv1.WorkflowServiceClient, adminv1.AdminServiceClient) {
	t.Helper()
	conn, err := grpc.NewClient(h.composed.GRPCAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial the served cell: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return workflowv1.NewWorkflowServiceClient(conn), adminv1.NewAdminServiceClient(conn)
}

// promoexec003OperatorCtx mints an operator-profile credential the
// AdminService admits: the reserved operator role on the harness tenant, or
// on another tenant for the cross-tenant refusal case.
func promoexec003OperatorCtx(t *testing.T, h *promoux015Harness, tenant string) context.Context {
	t.Helper()
	now := time.Now()
	token, err := h.verifier.Issue(trust.Claims{
		Issuer: h.cfg.Issuer, Audience: h.cfg.Audience, Subject: "principal:promo-exec-003-operator", SubjectKind: "human", Tenant: tenant,
		OrganizationScopeID: "org:" + h.cfg.Tenant + ":people-ops",
		Roles:               []string{"hcm_admin", adminpolicy.OperatorRole},
		Purposes:            []string{"operator_diagnostics"}, AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "session-promo-exec-003-operator",
		IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue operator credential: %v", err)
	}
	if _, err := h.verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token, Audience: h.cfg.Audience}); err != nil {
		t.Fatalf("verify operator credential: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, "Bearer "+token)
}

// promoexec003Live drives the shared served promotion to
// WAITING_EFFECTIVE_DATE and returns the intent and the live workflow
// instance the inspectors read.
func promoexec003Live(t *testing.T, h *promoux015Harness) (intentID, instanceID string) {
	t.Helper()
	intentID = h.runSeparatedPromotion()
	inspected, err := h.client.InspectJourney(h.rpc("admin"), &journeyv1.InspectJourneyRequest{IntentId: intentID})
	if err != nil {
		t.Fatalf("InspectJourney as the operator: %v", err)
	}
	instanceID = inspected.GetDetail().GetInstance().GetInstanceId()
	if instanceID == "" {
		t.Fatal("the waiting journey names no workflow instance")
	}
	return intentID, instanceID
}

// TestTodo_PROMO_EXEC_003_LiveInspect reads the live execute-mode promotion
// through both inspector surfaces mid-run: the instance with its EXECUTE
// mode and current wait, the node executions with their gateway evidence
// refs, and the operator view with the driver-created approval work items
// and their transitions.
func TestTodo_PROMO_EXEC_003_LiveInspect(t *testing.T) {
	h := promoux015Compose(t)
	_, instanceID := promoexec003Live(t, h)
	workflow, admin := promoexec003Surfaces(t, h)
	ctx := h.rpc("hiring-manager")

	got, err := workflow.GetWorkflow(ctx, &workflowv1.GetWorkflowRequest{InstanceId: instanceID})
	if err != nil {
		t.Fatalf("GetWorkflow on the live run: %v", err)
	}
	inst := got.GetInstance()
	if inst.GetInstanceId() != instanceID {
		t.Fatalf("GetWorkflow instance = %s, want %s", inst.GetInstanceId(), instanceID)
	}
	if inst.GetExecutionMode() != intentsv1.ExecutionMode_EXECUTION_MODE_EXECUTE {
		t.Fatalf("GetWorkflow execution mode = %s, want EXECUTE", inst.GetExecutionMode())
	}
	if inst.GetRuntimeStatus() == workflowv1.RuntimeStatus_RUNTIME_STATUS_UNSPECIFIED || inst.GetCompiledPlanDigest() == "" || inst.GetCorrelationId() == "" {
		t.Fatalf("GetWorkflow instance = %+v, want status, plan digest and correlation", inst)
	}
	current := false
	for _, node := range inst.GetCurrentNodeIds() {
		current = current || node == promotionexec.NodeWaitEffectiveDate
	}
	if !current {
		t.Fatalf("GetWorkflow current nodes = %v, want the live %s wait", inst.GetCurrentNodeIds(), promotionexec.NodeWaitEffectiveDate)
	}

	listed, err := workflow.ListNodeExecutions(ctx, &workflowv1.ListNodeExecutionsRequest{InstanceId: instanceID})
	if err != nil {
		t.Fatalf("ListNodeExecutions on the live run: %v", err)
	}
	byNode := map[string]*workflowv1.NodeExecution{}
	for _, n := range listed.GetNodeExecutions() {
		if n.GetWorkflowInstanceId() != instanceID {
			t.Fatalf("node execution %s belongs to %s, want the live %s", n.GetNodeId(), n.GetWorkflowInstanceId(), instanceID)
		}
		byNode[n.GetNodeId()] = n
	}
	snapshot, ok := byNode[promotionexec.NodeSnapshotWorker]
	if !ok || snapshot.GetStatus() == workflowv1.NodeExecutionStatus_NODE_EXECUTION_STATUS_UNSPECIFIED || snapshot.GetCapabilityExecutionId() == "" || snapshot.GetAuthorizationDecisionId() == "" {
		t.Fatalf("snapshot_worker = %+v, want the live execution with its gateway evidence refs", snapshot)
	}
	for _, node := range []string{promotionexec.NodeApproveFinance, promotionexec.NodeApproveManager} {
		if byNode[node] == nil {
			t.Fatalf("ListNodeExecutions names no %s on the live run: %v", node, byNode)
		}
	}

	view, err := admin.GetWorkflowInstance(promoexec003OperatorCtx(t, h, h.cfg.Tenant), &adminv1.GetWorkflowInstanceRequest{InstanceId: instanceID})
	if err != nil {
		t.Fatalf("GetWorkflowInstance on the live run: %v", err)
	}
	if !view.GetDisclosed() || !view.GetWorkItemsDisclosed() {
		t.Fatalf("operator view disclosed = %t work items = %t, want the live record disclosed",
			view.GetDisclosed(), view.GetWorkItemsDisclosed())
	}
	if view.GetInstance().GetExecutionMode() != "EXECUTE" {
		t.Fatalf("operator view execution mode = %s, want EXECUTE", view.GetInstance().GetExecutionMode())
	}
	var snapshotView *adminv1.NodeProfile
	for _, n := range view.GetNodes() {
		if n.GetNodeId() == promotionexec.NodeSnapshotWorker {
			snapshotView = n
		}
	}
	if snapshotView == nil {
		t.Fatal("operator view names no snapshot_worker on the live run")
	}
	if snapshotView.GetGovernance().GetAuthorizationDecisionId().GetState() != "VALUE" ||
		snapshotView.GetConnector().GetCapabilityExecutionId().GetState() != "VALUE" {
		t.Fatalf("snapshot_worker governance/connector = %+v, want the recorded evidence refs", snapshotView)
	}
	approvals := map[string]*adminv1.WorkItemProfile{}
	for _, w := range view.GetWorkItems() {
		approvals[w.GetNodeId()] = w
	}
	for _, node := range []string{promotionexec.NodeApproveFinance, promotionexec.NodeApproveManager} {
		item := approvals[node]
		if item == nil || item.GetStatus() != "COMPLETED" {
			t.Fatalf("operator work item %s = %+v, want the driver-created COMPLETED approval", node, item)
		}
		if !item.GetTransitionsRecorded() || len(item.GetTransitions()) < 2 {
			t.Fatalf("operator work item %s carries %d transitions, want the recorded claim and decision",
				node, len(item.GetTransitions()))
		}
	}
}

// TestTodo_PROMO_EXEC_003_LiveInspect_Integration pins the inspectors to the
// terminal live run: after the commit the execute_promotion node carries its
// connector effect legs and governance proposal ref, and the served journey
// read serves the terminal ledger fact the same instance closed with.
func TestTodo_PROMO_EXEC_003_LiveInspect_Integration(t *testing.T) {
	h := promoux015Compose(t)
	intentID, instanceID := promoexec003Live(t, h)
	workflow, admin := promoexec003Surfaces(t, h)

	fired, err := h.scheduler(h.afterEffectiveDate()).Tick(context.Background())
	if err != nil || fired.Fired != 1 {
		t.Fatalf("Tick after the effective date = %+v, %v; want one timer fired", fired, err)
	}
	h.acknowledgeParkedPromotion(intentID)

	terminal, err := workflow.GetWorkflow(h.rpc("hiring-manager"), &workflowv1.GetWorkflowRequest{InstanceId: instanceID})
	if err != nil {
		t.Fatalf("GetWorkflow on the terminal run: %v", err)
	}
	if terminal.GetInstance().GetInstanceId() != instanceID || terminal.GetInstance().GetRuntimeStatus() == workflowv1.RuntimeStatus_RUNTIME_STATUS_UNSPECIFIED {
		t.Fatalf("terminal GetWorkflow = %+v, want the closed live instance", terminal.GetInstance())
	}

	view, err := admin.GetWorkflowInstance(promoexec003OperatorCtx(t, h, h.cfg.Tenant), &adminv1.GetWorkflowInstanceRequest{InstanceId: instanceID})
	if err != nil {
		t.Fatalf("GetWorkflowInstance on the terminal run: %v", err)
	}
	if !view.GetDisclosed() || !view.GetComplete() {
		t.Fatalf("terminal operator view disclosed = %t complete = %t, want the closed record",
			view.GetDisclosed(), view.GetComplete())
	}
	var commit *adminv1.NodeProfile
	for _, n := range view.GetNodes() {
		if n.GetNodeId() == promotionexec.NodeExecutePromotion && n.GetStatus() == "SUCCEEDED" {
			commit = n
		}
	}
	if commit == nil {
		t.Fatal("terminal operator view names no SUCCEEDED execute_promotion")
	}
	if commit.GetConnector().GetEffectRefs().GetState() != "VALUE" || len(commit.GetConnector().GetEffectRefs().GetValues()) == 0 {
		t.Fatalf("execute_promotion connector = %+v, want the committed outbox legs", commit.GetConnector())
	}
	if commit.GetGovernance().GetProposalRef().GetState() != "VALUE" {
		t.Fatalf("execute_promotion governance = %+v, want the bound proposal ref", commit.GetGovernance())
	}
	detail, err := h.client.InspectJourney(h.rpc("admin"), &journeyv1.InspectJourneyRequest{IntentId: intentID})
	if err != nil || detail.GetDetail().GetLedger() == nil {
		t.Fatalf("InspectJourney after the commit = %v, ledger %v; want the terminal ledger fact", err, detail.GetDetail().GetLedger())
	}
}

// TestTodo_PROMO_EXEC_003_LiveInspect_Security proves the inspectors refuse
// across the tenant boundary and without authority: another tenant's operator
// finds nothing, a call with no credential is unauthenticated, and a caller
// without the operator profile never reaches ADMIN-008.
func TestTodo_PROMO_EXEC_003_LiveInspect_Security(t *testing.T) {
	h := promoux015Compose(t)
	_, instanceID := promoexec003Live(t, h)
	workflow, admin := promoexec003Surfaces(t, h)

	foreign := promoexec003OperatorCtx(t, h, "other-tenant")
	if _, err := workflow.GetWorkflow(foreign, &workflowv1.GetWorkflowRequest{InstanceId: instanceID}); status.Code(err) != codes.NotFound {
		t.Fatalf("GetWorkflow across tenants = %v, want NOT_FOUND", err)
	}
	if _, err := workflow.ListNodeExecutions(foreign, &workflowv1.ListNodeExecutionsRequest{InstanceId: instanceID}); status.Code(err) != codes.NotFound {
		t.Fatalf("ListNodeExecutions across tenants = %v, want NOT_FOUND", err)
	}
	if _, err := admin.GetWorkflowInstance(foreign, &adminv1.GetWorkflowInstanceRequest{InstanceId: instanceID}); status.Code(err) != codes.NotFound {
		t.Fatalf("GetWorkflowInstance across tenants = %v, want NOT_FOUND", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := workflow.GetWorkflow(ctx, &workflowv1.GetWorkflowRequest{InstanceId: instanceID}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("GetWorkflow with no credential = %v, want UNAUTHENTICATED", err)
	}

	if _, err := admin.GetWorkflowInstance(h.rpc("individual-contributor"), &adminv1.GetWorkflowInstanceRequest{InstanceId: instanceID}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("GetWorkflowInstance as the employee = %v, want PERMISSION_DENIED", err)
	}
}
