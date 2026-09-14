package execution

// PROMOUX-003: "Enforce and explain separation of duties across promotion
// approvals." RED clause 2 names a real defect in this package's own
// promotionWorkItems.CreateAndRoute: the finance and manager approval
// WorkItems it creates for the executable plan were both pinned to the
// identical configured approver. This file proves the fix -- each node's
// compiled candidate and routed owner now derive their own
// authority-class-scoped principal (promotionexec.FinanceApproverFor /
// ManagerApproverFor) from the one configured base -- directly against
// promotionWorkItems.CreateAndRoute, which no other test in this package
// exercises for the executable plan's finance/manager nodes.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/approverclass"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func TestPromotionWorkItemsCreateAndRouteDerivesDistinctApproversForFinanceAndManager(t *testing.T) {
	ctx := context.Background()
	database := pgtest.New(t)
	tenant := uuid.New()
	at := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	database.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,'promoux003','cell-local','PROMOUX-003 tenant','ACTIVE',$2)`, tenant, at.Add(-time.Hour))
	instance := uuid.New()
	database.Exec(t, `INSERT INTO workflow_instance (
			tenant_id, instance_id, cell_id, workflow_id, workflow_version,
			compiled_plan_hash, execution_mode, runtime_status, input_ref,
			current_node_ids, correlation_id, created_at)
		VALUES ($1, $2, 'cell-local', 'wf.promoux003', 1,
			'0000000000000000000000000000000000000000000000000000000000000000', 'EXECUTE', 'RUNNING', 'sha256:input',
			ARRAY['approve_finance'], $3, $4)`,
		tenant, instance, "corr-"+instance.String(), at)
	conn := database.NewConn(t)
	defer conn.Close(ctx)

	const base = "principal:promoux003-execution-base"
	factory := promotionWorkItems{approver: base, plan: PLAN_EXECUTE}

	requestFor := func(nodeID string, workItemID uuid.UUID) execute.WorkItemRequest {
		return execute.WorkItemRequest{
			WorkItemID: workItemID,
			Continuation: runtime.ContinuationRecord{
				TenantID: tenant, InstanceID: instance, TargetNodeID: nodeID,
			},
			Proposal: runtime.ProposalBinding{
				Revision: proposalRevisionFixture(),
			},
			CorrelationID: "corr-" + instance.String(),
			SubjectRefs:   []string{"worker:jane"},
			CreatedAt:     at,
		}
	}

	financeItem, err := factory.CreateAndRoute(ctx, conn, requestFor(promotionexec.NodeApproveFinance, uuid.New()))
	if err != nil {
		t.Fatalf("CreateAndRoute(finance): %v", err)
	}
	managerItem, err := factory.CreateAndRoute(ctx, conn, requestFor(promotionexec.NodeApproveManager, uuid.New()))
	if err != nil {
		t.Fatalf("CreateAndRoute(manager): %v", err)
	}

	wantFinance, err := promotionexec.FinanceApproverFor(base)
	if err != nil {
		t.Fatalf("FinanceApproverFor: %v", err)
	}
	wantManager, err := promotionexec.ManagerApproverFor(base)
	if err != nil {
		t.Fatalf("ManagerApproverFor: %v", err)
	}

	if financeItem.OwnerRef != wantFinance {
		t.Fatalf("finance item owner_ref = %q, want the derived %q", financeItem.OwnerRef, wantFinance)
	}
	if managerItem.OwnerRef != wantManager {
		t.Fatalf("manager item owner_ref = %q, want the derived %q", managerItem.OwnerRef, wantManager)
	}
	if financeItem.OwnerRef == managerItem.OwnerRef {
		t.Fatalf("finance and manager items share an owner %q; RED clause 2 is not closed", financeItem.OwnerRef)
	}
	if err := approverclass.RequireDistinct(financeItem.OwnerRef, managerItem.OwnerRef); err != nil {
		t.Fatalf("RequireDistinct(finance, manager) = %v, want nil", err)
	}

	for name, item := range map[string]workitem.WorkItem{"finance": financeItem, "manager": managerItem} {
		if len(item.Assignment.Resolution.Candidates) != 1 || item.Assignment.Resolution.Candidates[0].PrincipalID != item.OwnerRef {
			t.Fatalf("%s item's compiled candidate = %+v, want exactly the routed owner %q",
				name, item.Assignment.Resolution.Candidates, item.OwnerRef)
		}
	}
}

func proposalRevisionFixture() intent.ProposalRevision {
	intentID, revisionID := "intent:promoux003-execution", "revision:promoux003-execution"
	return intent.ProposalRevision{
		IntentID: intentID, ProposalRevisionID: revisionID, Revision: 1,
		OrganizationScopeID: "org:acme:engineering",
		// PROMOUX-015: routing now refuses a proposal whose requester is
		// unknown (approverclass.RequireSeparated), so the fixture names one.
		CreatedBy: intent.PrincipalReference{PrincipalID: "principal:promoux003-execution-requester"},
		MaterialDigest: digest.Reference{
			ProfileID: "PROPOSAL", ProfileVersion: 1, AlgorithmID: "sha256",
			Digest: strings.Repeat("a", 64), ScopeBindingDigest: strings.Repeat("b", 64),
			IntentID: &intentID, ProposalRevisionID: &revisionID,
		},
	}
}
