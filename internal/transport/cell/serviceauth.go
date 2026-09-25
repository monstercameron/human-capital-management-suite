package cell

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
	transportworkflow "github.com/monstercameron/human-capital-management-suite/internal/transport/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// workflowDesignerRoles holds the durable grants that may author workflow
// drafts, compile them and read the designer catalog. They are the same
// three grants the previous token-role hook enforced, now resolved from
// durable assignments (RBAC-RT-002) instead of credential claims.
var workflowDesignerRoles = []string{"hcm_admin", "comp_admin", "intent_author"}

// workflowOperatorRoles holds the durable grants that may inspect any
// workflow instance or run governed controls: administrators and the
// operator profile (RBAC-RT-009 derives operator authority from durable
// bindings, so the hook reads the binding, never the token).
var workflowOperatorRoles = []string{"hcm_admin", "comp_admin", "hcmnext.trust.role.operator"}

// durableRoles resolves the caller's strictly durable role set: the roles
// carried by a durable assignment row for the subject, with no fallback to
// credential claims. RBAC-RT-002 requires token roles to be ignored for
// authorization, and these authorizers are the capability gates the Work
// and Workflow services call before their own visibility checks, so the
// admitted-claims fallback [roleaccess.AssignedRoles] keeps for rollout
// would reopen exactly the revoked-user gap RBAC-RT-002 closed. A nil
// store, a load failure, or no assignment row resolves to nothing, and
// nothing authorizes nothing: every authorizer built on this fails closed.
func durableRoles(ctx context.Context, store roleaccess.Store, p *trust.Principal) []string {
	if store == nil || p == nil {
		return nil
	}
	snapshot, err := store.Load(ctx, p.Tenant(), p.OrganizationScopeID())
	if err != nil {
		return nil
	}
	subject := strings.TrimSpace(p.Subject())
	var roles []string
	for _, assignment := range snapshot.Assignments {
		if strings.EqualFold(strings.TrimSpace(assignment.WorkerRef), subject) {
			roles = append(roles, assignment.RoleIDs...)
		}
	}
	return roleaccess.NormalizeRoleIDs(roles)
}

func hasAnyDurableRole(roles []string, want ...string) bool {
	for _, w := range want {
		if roleaccess.ContainsRole(roles, w) {
			return true
		}
	}
	return false
}

// workAuthorizer builds the WorkService capability hook over the durable
// role store. Any durable assignment admits the wire-level capability for
// every work action: reads, claims, releases, completions and approval
// decisions. The hook answers capability ("is acting on work items this
// principal's job"), never membership: the domain ports behind
// [transporthumanwork.Dependencies] re-establish current authority,
// separation of duties and the exclusive version compare-and-swap fresh
// against the loaded row on every call. Unknown actions deny.
func workAuthorizer(store roleaccess.Store) func(context.Context, *trust.Principal, string) bool {
	return func(ctx context.Context, p *trust.Principal, action string) bool {
		switch action {
		case transporthumanwork.ActionListWorkItems,
			transporthumanwork.ActionGetWorkItem,
			transporthumanwork.ActionGetThresholdTable,
			transporthumanwork.ActionWorkItemGovernance,
			transporthumanwork.ActionClaimWorkItem,
			transporthumanwork.ActionReleaseWorkItem,
			transporthumanwork.ActionCompleteWorkItem,
			transporthumanwork.ActionDecideApproval:
		default:
			return false
		}
		return len(durableRoles(ctx, store, p)) > 0
	}
}

// WorkActionAuthorizer exposes the same durable action check to composition
// ports that project a safe WorkItem reference outside WorkService.
func WorkActionAuthorizer(store roleaccess.Store) func(context.Context, *trust.Principal, string) bool {
	return workAuthorizer(store)
}

// workflowAuthorizer builds the WorkflowService capability hook over the
// durable role store. Draft authoring, compilation and the designer catalog
// need a durable designer grant; governed controls and reading an instance
// the caller neither participates in nor supervises need a durable
// operator or administrator grant; instance and catalog reads need any
// durable assignment, with the participation, supervision and operator
// admissions enforced in the handlers after the record loads. Unknown
// actions deny.
func workflowAuthorizer(store roleaccess.Store) func(context.Context, *trust.Principal, string) bool {
	return func(ctx context.Context, p *trust.Principal, action string) bool {
		roles := durableRoles(ctx, store, p)
		if len(roles) == 0 {
			return false
		}
		switch action {
		case transportworkflow.ActionListWorkflowPublications,
			transportworkflow.ActionGetWorkflowDefinitionView,
			transportworkflow.ActionGetWorkflow,
			transportworkflow.ActionListNodeExecutions:
			return true
		case transportworkflow.ActionCompileWorkflowDraft,
			transportworkflow.ActionListWorkflowBlocks,
			transportworkflow.ActionCreateWorkflowDraft,
			transportworkflow.ActionGetWorkflowDraft,
			transportworkflow.ActionInsertWorkflowPaletteEntry,
			transportworkflow.ActionUpdateWorkflowDraftNode,
			transportworkflow.ActionSetWorkflowDraftOutcome,
			transportworkflow.ActionBindWorkflowDraftInput,
			transportworkflow.ActionMoveWorkflowDraftNode,
			transportworkflow.ActionRemoveWorkflowDraftNode,
			transportworkflow.ActionClearWorkflowDraftOutcome,
			transportworkflow.ActionRenameWorkflowDraft,
			transportworkflow.ActionNavigateWorkflowDraftHistory,
			transportworkflow.ActionApplyWorkflowTemplateOverlay:
			return hasAnyDurableRole(roles, workflowDesignerRoles...)
		case transportworkflow.ActionPauseWorkflow,
			transportworkflow.ActionResumeWorkflow,
			transportworkflow.ActionCancelWorkflow,
			transportworkflow.ActionRetryNode,
			transportworkflow.ActionInspectAnySubject:
			return hasAnyDurableRole(roles, workflowOperatorRoles...)
		default:
			return false
		}
	}
}
