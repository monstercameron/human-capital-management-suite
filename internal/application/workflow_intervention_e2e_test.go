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
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/truststore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator/workflowcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/intervention"
)

// TestTodo_WF_RUN_015_Served proves typed workflow interventions are live on
// the real serve composition and reachable only through its governed operator
// gateway: on a promotion waiting for finance approval, a no-op RESUME and the
// forbidden force kind are typed denials that write nothing; a REWIND without
// the dual control its repair authority demands is INTERVENTION_UNAUTHORIZED;
// and under an instance-scoped integrity-repair grant approved by a second
// person the paused instance is rewound to its earlier decision node -- a new
// attempt, the waiting approval cancelled, every earlier attempt kept -- with
// an immutable decision row and a durable operator receipt, and a replay of
// the same key changes nothing.
func TestTodo_WF_RUN_015_Served(t *testing.T) {
	h := promoux015Compose(t)
	h.proposeAndExecute()
	ctx := context.Background()
	var instanceID string
	var version int64
	if err := h.pool.QueryRow(ctx, `SELECT instance_id::text, instance_version FROM workflow_instance ORDER BY created_at DESC LIMIT 1`).Scan(&instanceID, &version); err != nil {
		t.Fatal(err)
	}
	cell := h.composed.Cell()
	if cell == nil || cell.WorkflowControl == nil || cell.WorkflowTenantIDs == nil {
		t.Fatal("serve composed no governed workflow control")
	}
	admin := h.principals["admin"]
	tenantID, err := cell.WorkflowTenantIDs(admin.Tenant())
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.MustParse(instanceID)
	command := func(key string, v int64, spec workflowcontrol.InterventionSpec) workflowcontrol.Command {
		if spec.EvidenceRefs == nil {
			spec.EvidenceRefs = []string{"ticket:INC-15", "trace:promotion-stuck"}
		}
		return workflowcontrol.Command{TenantID: tenantID, Tenant: admin.Tenant(), InstanceID: id, ExpectedVersion: v,
			IdempotencyKey: key, ReasonRef: "INC-15", Operator: admin.Subject(), Intervention: &spec}
	}
	receiptOutcome := func(key string) (outcome, digest string) {
		t.Helper()
		tx, err := h.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
			t.Fatal(err)
		}
		_ = tx.QueryRow(ctx, `SELECT outcome, receipt->>'digest' FROM operator_control_receipt WHERE idempotency_key = $1`, key).Scan(&outcome, &digest)
		return outcome, digest
	}

	// A no-op and the forbidden force kind: typed denials, nothing written.
	for key, spec := range map[string]workflowcontrol.InterventionSpec{
		"served-resume-noop": {Kind: intervention.Resume},
		"served-force":       {Kind: "FORCE_COMPLETE_WITH_EVIDENCE"},
	} {
		res, err := cell.WorkflowControl.Intervene(ctx, command(key, version, spec))
		want := intervention.CodeNoOp
		if spec.Kind != intervention.Resume {
			want = intervention.CodeNotSupported
		}
		if err != nil || res.Outcome != workflowcontrol.OutcomeDenied || res.Code != want {
			t.Fatalf("%s = %+v, %v; want DENIED %s", key, res, err, want)
		}
		if outcome, _ := receiptOutcome(key); outcome != "" {
			t.Fatalf("%s journaled a %s receipt", key, outcome)
		}
	}

	// A broad integrity-repair grant pauses the instance over gRPC but carries
	// no second approval for this instance, so a rewind is unauthorized.
	tenant := pgstore.TenantID(demoworkforce.CompanyKey)
	now := time.Now().UTC()
	put := func(grantID string, fields []string) {
		t.Helper()
		scope, _ := json.Marshal(truststore.JITGrantScope{Role: string(jit.RoleIntegrityRepair), TicketRef: "INC-15", Justification: "rewind stuck promotion",
			Capabilities: []string{string(operator.KindWorkflowPause), string(operator.KindWorkflowRewind)}, Fields: fields, Purpose: "incident repair"})
		if err := truststore.New(h.pool).PutJITGrant(ctx, tenant, truststore.JITGrantRecord{TenantID: tenant, RowID: uuid.New(), GrantID: grantID,
			Revision: 1, State: "ACTIVE", Requester: admin.Subject(), Approver: "principal:security-lead", Scope: scope,
			NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(2 * time.Hour)}); err != nil {
			t.Fatalf("record grant %s: %v", grantID, err)
		}
	}
	put("jit-served-broad", nil)
	conn, err := grpc.NewClient(h.composed.GRPCAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	paused, err := workflowv1.NewWorkflowServiceClient(conn).PauseWorkflow(h.rpc("admin"), &workflowv1.PauseWorkflowRequest{
		IdempotencyKey: "served-pause", InstanceId: instanceID, ExpectedInstanceVersion: uint64(version), ReasonRef: "INC-15"})
	if err != nil || paused.GetReceipt().GetInstanceStatus() != "PAUSED" {
		t.Fatalf("governed pause = %v, %v", paused, err)
	}
	pausedVersion := int64(paused.GetReceipt().GetInstanceVersion())
	rewind := workflowcontrol.InterventionSpec{Kind: intervention.Rewind, TargetNodeID: "raise_threshold"}
	undual, err := cell.WorkflowControl.Intervene(ctx, command("served-rewind-undual", pausedVersion, rewind))
	if err != nil || undual.Outcome != workflowcontrol.OutcomeDenied || undual.Code != intervention.CodeUnauthorized || undual.Cause != operator.CodeDualControlRequired {
		t.Fatalf("rewind without dual control = %+v, %v", undual, err)
	}

	// The instance-scoped grant is the second approval: the rewind applies.
	put("jit-served-scoped", []string{workflowcontrol.InstanceApprovalField(instanceID)})
	res, err := cell.WorkflowControl.Intervene(ctx, command("served-rewind", pausedVersion, rewind))
	if err != nil || res.Outcome != workflowcontrol.OutcomeApplied || res.DecisionID == "" || res.IntentInstanceID == "" {
		t.Fatalf("governed rewind = %+v, %v", res, err)
	}
	outcome, digest := receiptOutcome("served-rewind")
	if outcome != string(operator.OutcomeApplied) || digest != res.ReceiptDigest {
		t.Fatalf("durable rewind receipt = %s %s; want APPLIED %s", outcome, digest, res.ReceiptDigest)
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatal(err)
	}
	decisions, err := intervention.DecisionStore{}.Load(ctx, tx, tenantID, id)
	if err != nil || len(decisions) != 1 {
		t.Fatalf("decisions = %+v, %v; want one", decisions, err)
	}
	d := decisions[0]
	if d.DecisionID.String() != res.DecisionID || d.Kind != intervention.Rewind || d.OperatorKind != string(operator.KindWorkflowRewind) ||
		d.IntentInstanceID != res.IntentInstanceID || d.Plan.ExpectedVersion != pausedVersion || d.Observed.Node.NodeID != "raise_threshold" || d.Observed.Node.Attempt != 2 {
		t.Fatalf("decision = %+v, result %+v", d, res)
	}
	var status string
	var frontier []string
	if err := tx.QueryRow(ctx, `SELECT runtime_status, current_node_ids FROM workflow_instance WHERE instance_id = $1`, id).Scan(&status, &frontier); err != nil {
		t.Fatal(err)
	}
	var earlier, restaged, cancelled string
	if err := tx.QueryRow(ctx, `SELECT
			(SELECT status FROM workflow_node_execution WHERE instance_id = $1 AND node_id = 'raise_threshold' AND attempt = 1),
			(SELECT status FROM workflow_node_execution WHERE instance_id = $1 AND node_id = 'raise_threshold' AND attempt = 2),
			(SELECT status FROM workflow_node_execution WHERE instance_id = $1 AND node_id = 'approve_finance' ORDER BY attempt DESC LIMIT 1)`, id).
		Scan(&earlier, &restaged, &cancelled); err != nil {
		t.Fatal(err)
	}
	if status != "PAUSED" || len(frontier) != 1 || frontier[0] != "raise_threshold" || earlier != "SUCCEEDED" || restaged != "READY" || cancelled != "CANCELLED" {
		t.Fatalf("after rewind: %s %v, raise_threshold #1 %s #2 %s, approve_finance %s", status, frontier, earlier, restaged, cancelled)
	}
	var versionAfter int64
	if err := tx.QueryRow(ctx, `SELECT instance_version FROM workflow_instance WHERE instance_id = $1`, id).Scan(&versionAfter); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback(ctx)

	replayed, err := cell.WorkflowControl.Intervene(ctx, command("served-rewind", pausedVersion, rewind))
	if err != nil || !replayed.Replayed || replayed.DecisionID != res.DecisionID || replayed.ReceiptDigest != res.ReceiptDigest {
		t.Fatalf("replayed rewind = %+v, %v", replayed, err)
	}
	var versionReplayed int64
	if err := h.pool.QueryRow(ctx, `SELECT instance_version FROM workflow_instance WHERE instance_id = $1::uuid`, instanceID).Scan(&versionReplayed); err != nil || versionReplayed != versionAfter {
		t.Fatalf("a replayed rewind moved the instance version %d -> %d (%v)", versionAfter, versionReplayed, err)
	}
}
