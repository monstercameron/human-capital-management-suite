package journeyclient

import journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"

// NextStep is the stable code for the single next step a journey's server
// stage names (UXAUDIT-017). It is a workflow fact read off the stage, never a
// viewer permission: whether the reader may take that step is decided by the
// server when the canonical intent workspace is opened, and no surface renders
// a control from this code.
type NextStep string

// The closed NextStep vocabulary. Each is justified by the stage's own
// contract comment in schema/proto/hcmnext/journey/v1/journey_service.proto.
const (
	// NextStepNone: the journey is terminal, or its stage is unknown.
	NextStepNone NextStep = ""
	// NextStepStartApproval: PROPOSED -- simulated, no execution requested.
	NextStepStartApproval NextStep = "start_approval"
	// NextStepCorrectProposal: BLOCKED -- the simulation produced no
	// executable plan.
	NextStepCorrectProposal NextStep = "correct_proposal"
	// NextStepApprovalDecision: AWAITING_APPROVAL -- parked on its approval
	// work item.
	NextStepApprovalDecision NextStep = "approval_decision"
	// NextStepManagerDecision: MANAGER_APPROVAL.
	NextStepManagerDecision NextStep = "manager_decision"
	// NextStepFinanceDecision: FINANCE_APPROVAL.
	NextStepFinanceDecision NextStep = "finance_decision"
	// NextStepReapprovalDecision: REAPPROVAL -- a material change requires an
	// approval decision again.
	NextStepReapprovalDecision NextStep = "reapproval_decision"
	// NextStepRepair: REPAIR_REQUIRED -- governed repair before a consistent
	// terminal state.
	NextStepRepair NextStep = "repair"
	// NextStepAwaitEffectiveDate: WAITING_EFFECTIVE_DATE -- approvals are
	// complete; the workflow waits for the effective-date safe point.
	NextStepAwaitEffectiveDate NextStep = "await_effective_date"
	// NextStepSystemProcessing: REVALIDATION, EXECUTED, OBSERVING_EFFECTS --
	// the workflow itself is working; no person holds the step.
	NextStepSystemProcessing NextStep = "system_processing"
)

// StageActor is the role class a stage says the journey is waiting on. It is
// never a person: the journey list carries no assignee, so a named owner is
// only available on the journey's own work items.
type StageActor string

// The closed StageActor vocabulary.
const (
	// StageActorUnstated: the stage does not say who holds the step (terminal,
	// unknown, or REPAIR_REQUIRED, whose contract names no actor).
	StageActorUnstated StageActor = ""
	StageActorProposer StageActor = "proposer"
	StageActorApprover StageActor = "approver"
	StageActorManager  StageActor = "manager"
	StageActorFinance  StageActor = "finance"
	StageActorSystem   StageActor = "system"
)

// StatusDimension is the task-facing reading of one journey stage that My Work
// (an action queue) and Journeys (a lifecycle tracker) share, so both surfaces
// say the same thing about the same stage while composing it differently.
type StatusDimension struct {
	NextStep  NextStep
	WaitingOn StageActor
	// AwaitsPerson is true when the next step is held by a person rather than
	// by the workflow or the calendar. My Work ranks these ahead of work the
	// system is carrying, which is the only ownership signal the list wire
	// supports.
	AwaitsPerson bool
}

// nextStepLabels is the Journeys surface's English wording for each code.
// The product My Work page localizes the same codes through its own message
// catalog (internal/humanwork/productui "work.next_step.<code>").
var nextStepLabels = map[NextStep]string{
	NextStepStartApproval:      "Start approval",
	NextStepCorrectProposal:    "Correct the proposal",
	NextStepApprovalDecision:   "Approval decision",
	NextStepManagerDecision:    "Manager decision",
	NextStepFinanceDecision:    "Finance decision",
	NextStepReapprovalDecision: "Approval decision again",
	NextStepRepair:             "Governed repair",
	NextStepAwaitEffectiveDate: "Wait for the effective date",
	NextStepSystemProcessing:   "Workflow processing",
}

// NextStepLabel is code's English wording, or "" for NextStepNone and any
// code outside the closed vocabulary.
func NextStepLabel(code NextStep) string { return nextStepLabels[code] }

// StageStatusDimension maps a server stage to its shared status dimension.
// It is total: an unknown stage reads as no next step and no actor, never as a
// guessed one.
func StageStatusDimension(stage journeyv1.JourneyStage) StatusDimension {
	switch stage {
	case journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED:
		return StatusDimension{NextStep: NextStepStartApproval, WaitingOn: StageActorProposer, AwaitsPerson: true}
	case journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED:
		return StatusDimension{NextStep: NextStepCorrectProposal, WaitingOn: StageActorProposer, AwaitsPerson: true}
	case journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL:
		return StatusDimension{NextStep: NextStepApprovalDecision, WaitingOn: StageActorApprover, AwaitsPerson: true}
	case journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL:
		return StatusDimension{NextStep: NextStepManagerDecision, WaitingOn: StageActorManager, AwaitsPerson: true}
	case journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL:
		return StatusDimension{NextStep: NextStepFinanceDecision, WaitingOn: StageActorFinance, AwaitsPerson: true}
	case journeyv1.JourneyStage_JOURNEY_STAGE_REAPPROVAL:
		return StatusDimension{NextStep: NextStepReapprovalDecision, WaitingOn: StageActorApprover, AwaitsPerson: true}
	case journeyv1.JourneyStage_JOURNEY_STAGE_REPAIR_REQUIRED:
		return StatusDimension{NextStep: NextStepRepair, WaitingOn: StageActorUnstated, AwaitsPerson: true}
	case journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE:
		return StatusDimension{NextStep: NextStepAwaitEffectiveDate, WaitingOn: StageActorSystem}
	case journeyv1.JourneyStage_JOURNEY_STAGE_REVALIDATION,
		journeyv1.JourneyStage_JOURNEY_STAGE_EXECUTED,
		journeyv1.JourneyStage_JOURNEY_STAGE_OBSERVING_EFFECTS:
		return StatusDimension{NextStep: NextStepSystemProcessing, WaitingOn: StageActorSystem}
	default:
		return StatusDimension{}
	}
}
