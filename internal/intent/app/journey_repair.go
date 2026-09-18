package app

// UXLIVE-006: a journey stopped in REPAIR_REQUIRED showed the chip "Needs
// repair" and the next step "Governed repair", and its Actions section
// offered Withdraw, Request cancellation and Edit proposal -- nothing that
// asked about, tracked or explained a repair. Naming a governed repair as
// the next step while offering no way to reach it strands the run and its
// subject.
//
// The repair door itself already exists
// (internal/intent/operator/workflowcontrol.RepairController) and is
// composed on every cell with an execution database. What was missing was
// its projection: the journey never said the repair exists, what it
// requires, who may run it, or what it left behind.
//
// This file answers exactly that, and deliberately does not do more. The
// corrective effect is driven against an authored RepairPlan through the
// operator door, not from a promotion page -- and on this build it cannot
// run at all, because none of the three external-system seams the executor
// needs has a production adapter (see composeWorkflowRepair's own doc: the
// connectivity plane is read-only). A page that offered a button for that
// would be the same defect as the free-text position field UXLIVE-011
// removed: a control whose own help predicts its refusal. So the repair is
// presented, its requirements are stated from the operator policy rather
// than restated here, and the refusal names the authority that is needed.

import (
	"context"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator/workflowcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
)

// Reason references the repair projection owns. Like the three interventions
// above them, each is computed from the journey's own durable stage and from
// policy that is identical for every caller, so the answer's shape discloses
// nothing a viewer who reached this far was not already entitled to.
const (
	// reasonRepairNotRequired is every stage other than REPAIR_REQUIRED: the
	// journey has not asked for a repair, so there is nothing to run.
	reasonRepairNotRequired = "journey.repair.unavailable.not_required"
	// reasonRepairDoorUnavailable is a cell composed without the governed
	// repair door at all. It is not "you may not" -- it is "this deployment
	// cannot", which is a different thing to tell somebody who is trying to
	// unstick a promotion.
	reasonRepairDoorUnavailable = "journey.repair.unavailable.door_unavailable"
	// reasonRepairAuthorityRequired is a viewer holding no current JIT grant
	// for the repair kind. The roles that would authorize it are named
	// alongside, because "you cannot" without "who can" leaves the run
	// stranded just as surely as offering nothing did.
	reasonRepairAuthorityRequired = "journey.repair.unavailable.operator_authority_required"
	// reasonRepairPlanRequired is a viewer who does hold the authority. The
	// repair still does not start here: it runs against an authored,
	// simulated RepairPlan through the operator door, and this page authors
	// no plan.
	reasonRepairPlanRequired = "journey.repair.unavailable.plan_required"
)

// repairConsequence is what a governed repair would do, stated once. It is
// the same sentence whether or not the viewer may run it: what the action
// does is not a function of who is asking.
const repairConsequence = "A governed repair revalidates this journey's failed step against independently loaded current truth " +
	"and, only if that still holds, drives one corrective effect behind an admission fence. " +
	"The fence can answer that it could not establish whether the effect reached the external system, " +
	"and that answer is reported as indeterminate rather than as success or failure."

// repairPolicy is the operator policy for the repair kind: which roles may
// authorize it, and whether it demands dual control and a simulation. It is
// read from the operator package rather than restated, so a page cannot
// describe a lighter action than the door actually enforces.
func repairPolicy() (operator.Policy, bool) {
	return operator.PolicyFor(workflowcontrol.RepairKind)
}

// repairAuthorityRoleRefs names the roles whose grant may authorize a
// repair. Which roles a kind requires is policy, identical for every caller.
func repairAuthorityRoleRefs() []string {
	policy, ok := repairPolicy()
	if !ok {
		return nil
	}
	refs := make([]string, 0, len(policy.Roles))
	for _, role := range policy.Roles {
		refs = append(refs, string(role))
	}
	return refs
}

// previewRepair answers whether the governed repair is available to this
// viewer on this journey, and why not when it is not.
//
// Availability is decided in the order a person would ask it: does this
// journey want a repair at all, can this deployment run one, may this viewer
// authorize one, and is there a plan to run. Each step has its own reason,
// because "no repair is needed", "this deployment cannot repair", "you are
// not authorized" and "no plan has been authored" are four different things
// and a reader who is trying to unstick a promotion needs to know which one
// they are looking at.
func (e *journeyEngine) previewRepair(
	ctx context.Context, principal *trust.Principal, stage workspace.JourneyStage, governanceVersion uint64,
) workspace.JourneyInterventionPreview {
	policy, _ := repairPolicy()
	preview := workspace.JourneyInterventionPreview{
		ConsequenceSummary:       repairConsequence,
		LikelyOutcome:            workspace.InterventionIndeterminate,
		RequiresDualControl:      policy.DualControl,
		RequiresSimulation:       policy.SimulationRequired,
		AuthorityRoleRefs:        repairAuthorityRoleRefs(),
		CurrentGovernanceVersion: governanceVersion,
	}

	if stage != workspace.JourneyStageRepairRequired {
		preview.UnavailableReasonRef = reasonRepairNotRequired
		return preview
	}
	if e.repair == nil {
		preview.UnavailableReasonRef = reasonRepairDoorUnavailable
		return preview
	}
	if !e.holdsRepairAuthority(ctx, principal) {
		preview.UnavailableReasonRef = reasonRepairAuthorityRequired
		return preview
	}
	// The viewer may authorize a repair. It still does not start from here:
	// a repair runs against an authored plan, revalidated against current
	// evidence, and this page authors none. Saying so is the whole point --
	// the reader now knows the door is theirs to open and what is missing.
	preview.UnavailableReasonRef = reasonRepairPlanRequired
	return preview
}

// holdsRepairAuthority reports whether this principal currently holds a JIT
// grant that could authorize a repair. A resolver that is absent or that
// fails is answered "no": an authority question that cannot be answered is
// never answered yes.
func (e *journeyEngine) holdsRepairAuthority(ctx context.Context, principal *trust.Principal) bool {
	if e.repairAuthority == nil || principal == nil {
		return false
	}
	tenant := values.TenantId(principal.Tenant())
	if tenant.Validate() != nil {
		return false
	}
	authority, err := e.repairAuthority.ResolveAuthority(ctx, tenant, principal.Subject(), workflowcontrol.RepairKind, "")
	if err != nil {
		return false
	}
	return authority.JIT != nil
}

// repairOutcomeFromStatus projects the REPAIR mode's own typed verdict onto
// the shared intervention vocabulary. The mapping is total, and
// INDETERMINATE is carried through as itself rather than collapsed into
// APPLIED or DENIED: a fence that could not establish whether the effect
// landed must not be reported as either.
func repairOutcomeFromStatus(status execute.RepairStatus) workspace.JourneyInterventionOutcome {
	switch status {
	case execute.RepairCompleted, execute.RepairNoLongerRequired:
		return workspace.InterventionApplied
	case execute.RepairIndeterminate, execute.RepairUnknown:
		return workspace.InterventionIndeterminate
	case execute.RepairReconciliationWait:
		return workspace.InterventionPendingSafePoint
	case execute.RepairReplanRequired, execute.RepairReapprovalRequired, execute.RepairBlocked, execute.RepairFailed:
		return workspace.InterventionRepairRequired
	case execute.RepairSeparationRequired:
		return workspace.InterventionDenied
	default:
		// An unrecognized verdict is not evidence of success. Reporting it
		// as indeterminate is the only answer that is true of every value a
		// future REPAIR mode could add.
		return workspace.InterventionIndeterminate
	}
}

// repairNotRequestableHere refuses a repair submitted through the journey
// port, naming why in the same vocabulary the preview uses.
//
// This is not a stub: the repair door takes an immutable RepairPlan and the
// current evidence it is revalidated against, and neither is a thing a
// promotion page holds or could truthfully invent. Forwarding a request
// without them would either be refused deep inside the executor with a
// message about a missing plan, or -- worse -- be made to pass with a plan
// this page composed, which is exactly the authority this door exists to
// withhold.
func repairNotRequestableHere() error {
	return journeyInputError("kind",
		fmt.Sprintf("REPAIR is previewed here, not requested: a governed repair runs against an authored, simulated repair plan "+
			"through the operator repair door under a %s grant", strings.Join(repairAuthorityRoleRefs(), " or ")))
}
