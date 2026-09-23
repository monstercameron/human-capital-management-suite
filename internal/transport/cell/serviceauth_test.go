package cell

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
	transportworkflow "github.com/monstercameron/human-capital-management-suite/internal/transport/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// principalFor builds an authenticated principal with the given subject and
// credential roles. Whether the hook admits it depends only on the durable
// store, never on roles: passing hcm_admin here while the store knows
// nothing about the subject must still lose.
func principalFor(t *testing.T, subject string, roles ...string) *trust.Principal {
	t.Helper()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "session-" + subject, IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(1000, 0),
		CredentialDigest: "credential-digest", Roles: roles,
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return principal
}

// TestTodo_RBAC_RT_004_Integration drives the real cell authorizers over a
// durable role store: every Work and Workflow call in production composition
// resolves through these hooks, so the mapping from durable assignment to
// admitted action is proven here rather than against a copy of the table.
func TestTodo_RBAC_RT_004_Integration(t *testing.T) {
	ctx := context.Background()
	store := hookStore{snapshot: roleaccess.Snapshot{Assignments: []roleaccess.Assignment{
		{WorkerRef: "case-worker", RoleIDs: []string{"manager"}},
		{WorkerRef: "designer-a", RoleIDs: []string{"intent_author"}},
		{WorkerRef: "operator-o", RoleIDs: []string{"hcmnext.trust.role.operator"}},
	}}}
	authorizer := workflowAuthorizer(store)

	worker := principalFor(t, "case-worker", "worker")
	designerA := principalFor(t, "designer-a")
	operatorO := principalFor(t, "operator-o")
	outsider := principalFor(t, "outsider-x", "hcm_admin")

	// Instance and catalog reads are a workforce capability: any durable
	// assignment admits the call, with the subject rule enforced downstream.
	for _, action := range []string{
		transportworkflow.ActionGetWorkflow,
		transportworkflow.ActionListNodeExecutions,
		transportworkflow.ActionListWorkflowPublications,
		transportworkflow.ActionGetWorkflowDefinitionView,
	} {
		if !authorizer(ctx, worker, action) {
			t.Errorf("assigned worker refused %q", action)
		}
		if authorizer(ctx, outsider, action) {
			t.Errorf("outsider admitted %q", action)
		}
	}

	// Draft authoring needs a durable designer grant. A durable manager
	// grant is not one; a token naming designer roles without a durable
	// row is not one either.
	for _, action := range []string{
		transportworkflow.ActionCreateWorkflowDraft,
		transportworkflow.ActionCompileWorkflowDraft,
		transportworkflow.ActionListWorkflowBlocks,
	} {
		if !authorizer(ctx, designerA, action) {
			t.Errorf("assigned designer refused %q", action)
		}
		if authorizer(ctx, worker, action) {
			t.Errorf("manager admitted %q", action)
		}
		if authorizer(ctx, outsider, action) {
			t.Errorf("outsider admitted %q", action)
		}
	}

	// Controls and any-subject inspection need the durable operator or
	// administrator grant.
	for _, action := range []string{
		transportworkflow.ActionPauseWorkflow,
		transportworkflow.ActionRetryNode,
		transportworkflow.ActionInspectAnySubject,
	} {
		if !authorizer(ctx, operatorO, action) {
			t.Errorf("durable operator refused %q", action)
		}
		if authorizer(ctx, designerA, action) {
			t.Errorf("designer admitted %q", action)
		}
		if authorizer(ctx, worker, action) {
			t.Errorf("manager admitted %q", action)
		}
	}

	// Unknown actions deny even for the operator: the table is closed.
	if authorizer(ctx, operatorO, "approve_anything") {
		t.Error("operator admitted an unknown action")
	}

	// The Work hook admits every work action on any durable assignment and
	// nothing without one.
	work := workAuthorizer(store)
	workActions := []string{
		transporthumanwork.ActionListWorkItems,
		transporthumanwork.ActionGetWorkItem,
		transporthumanwork.ActionGetThresholdTable,
		transporthumanwork.ActionWorkItemGovernance,
		transporthumanwork.ActionClaimWorkItem,
		transporthumanwork.ActionReleaseWorkItem,
		transporthumanwork.ActionCompleteWorkItem,
		transporthumanwork.ActionDecideApproval,
	}
	for _, action := range workActions {
		if !work(ctx, worker, action) {
			t.Errorf("assigned worker refused work action %q", action)
		}
		if work(ctx, outsider, action) {
			t.Errorf("outsider admitted work action %q", action)
		}
		if work(ctx, nil, action) {
			t.Errorf("nil principal admitted work action %q", action)
		}
	}
	if work(ctx, worker, "close_any_item") {
		t.Error("assigned worker admitted an unknown work action")
	}

	// A missing store, a failing store, and an unknown action all deny:
	// every degradation points at closed.
	broken := hookStore{err: errors.New("role store down")}
	if workflowAuthorizer(broken)(ctx, operatorO, transportworkflow.ActionGetWorkflow) {
		t.Error("failing store admitted a workflow read")
	}
	if workAuthorizer(nil)(ctx, worker, transporthumanwork.ActionListWorkItems) {
		t.Error("nil store admitted a work read")
	}
	if workAuthorizer(broken)(ctx, worker, transporthumanwork.ActionListWorkItems) {
		t.Error("failing store admitted a work read")
	}

	// The composed workflow dependencies carry the durable hook, not the
	// old token-role check: the same designer the store knows is admitted,
	// the token-only claimant is not.
	composed := workflowDependencies(&app.Cell{RoleAccess: store}, nil, nil, nil)
	if composed.Authorize == nil {
		t.Fatal("composed workflow dependencies carry no authorizer")
	}
	if !composed.Authorize(ctx, designerA, transportworkflow.ActionCreateWorkflowDraft) {
		t.Error("composed hook refused the durably assigned designer")
	}
	if composed.Authorize(ctx, outsider, transportworkflow.ActionCreateWorkflowDraft) {
		t.Error("composed hook admitted token-only authority")
	}
}
