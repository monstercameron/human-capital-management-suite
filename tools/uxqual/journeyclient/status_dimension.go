package journeyclient

import (
	"strings"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

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
	// NextStepAwaitAcknowledgement: AWAITING_ACKNOWLEDGEMENT -- downstream
	// effects reconciled; a person must record the verified
	// acknowledgement, but no role class on the journey names whose job it
	// is (the attester must not be the initiator).
	NextStepAwaitAcknowledgement NextStep = "await_acknowledgement"
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
	NextStepStartApproval:        "Start approval",
	NextStepCorrectProposal:      "Correct the proposal",
	NextStepApprovalDecision:     "Approval decision",
	NextStepManagerDecision:      "Manager decision",
	NextStepFinanceDecision:      "Finance decision",
	NextStepReapprovalDecision:   "Approval decision again",
	NextStepRepair:               "Governed repair",
	NextStepAwaitEffectiveDate:   "Wait for the effective date",
	NextStepSystemProcessing:     "Workflow processing",
	NextStepAwaitAcknowledgement: "Await acknowledgement",
}

// NextStepLabel is code's English wording, or "" for NextStepNone and any
// code outside the closed vocabulary.
func NextStepLabel(code NextStep) string { return nextStepLabels[code] }

// JourneyStatusDimension reads one journey's shared status dimension off the
// server's PROMOUX-012 viewer projection. The server owns the stage-to-next-
// step mapping (internal/intent/app journeyStageTransition); this is a closed
// enum-to-code copy, so My Work, Journeys and the person profile can never
// disagree about a stage. A journey whose projection is absent reads as no
// next step and no actor, never a guessed one.
func JourneyStatusDimension(j *journeyv1.Journey) StatusDimension {
	viewer := j.GetViewer()
	if viewer == nil {
		return StatusDimension{}
	}
	return StatusDimension{
		NextStep:     nextStepCodes[viewer.GetNextStep()],
		WaitingOn:    stageActorCodes[viewer.GetNextStepOwner()],
		AwaitsPerson: viewer.GetAwaitsPerson(),
	}
}

// JourneyClosed reports the server's lifecycle closure for j. An absent
// projection is not closed: the client never infers closure from a stage.
func JourneyClosed(j *journeyv1.Journey) bool { return j.GetViewer().GetClosed() }

var nextStepCodes = map[journeyv1.JourneyNextStep]NextStep{
	journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_START_APPROVAL:        NextStepStartApproval,
	journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_CORRECT_PROPOSAL:      NextStepCorrectProposal,
	journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_APPROVAL_DECISION:     NextStepApprovalDecision,
	journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_MANAGER_DECISION:      NextStepManagerDecision,
	journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_FINANCE_DECISION:      NextStepFinanceDecision,
	journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_REAPPROVAL_DECISION:   NextStepReapprovalDecision,
	journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_REPAIR:                NextStepRepair,
	journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_AWAIT_EFFECTIVE_DATE:  NextStepAwaitEffectiveDate,
	journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_SYSTEM_PROCESSING:     NextStepSystemProcessing,
	journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_AWAIT_ACKNOWLEDGEMENT: NextStepAwaitAcknowledgement,
}

var stageActorCodes = map[journeyv1.JourneyStepOwner]StageActor{
	journeyv1.JourneyStepOwner_JOURNEY_STEP_OWNER_PROPOSER: StageActorProposer,
	journeyv1.JourneyStepOwner_JOURNEY_STEP_OWNER_APPROVER: StageActorApprover,
	journeyv1.JourneyStepOwner_JOURNEY_STEP_OWNER_MANAGER:  StageActorManager,
	journeyv1.JourneyStepOwner_JOURNEY_STEP_OWNER_FINANCE:  StageActorFinance,
	journeyv1.JourneyStepOwner_JOURNEY_STEP_OWNER_SYSTEM:   StageActorSystem,
}

// Relationship and Responsibility are the viewer half of the projection as
// the stable tokens productui carries (INITIATOR, ASSIGNEE, CANDIDATE;
// ACTION_REQUIRED, TRACKING, OBSERVING, CLOSED).
const (
	RelationshipInitiator            = "INITIATOR"
	RelationshipAssignee             = "ASSIGNEE"
	RelationshipCandidate            = "CANDIDATE"
	ResponsibilityActionRequired     = "ACTION_REQUIRED"
	ResponsibilityTracking           = "TRACKING"
	ResponsibilityObserving          = "OBSERVING"
	ResponsibilityClosed             = "CLOSED"
	relationshipEnumPrefix           = "JOURNEY_VIEWER_RELATIONSHIP_"
	responsibilityEnumPrefix         = "JOURNEY_VIEWER_RESPONSIBILITY_"
	relationshipEnumUnspecifiedToken = "UNSPECIFIED"
)

// JourneyViewerRelationships is the server's relationship set for j as
// tokens, in wire order; unspecified values are dropped.
func JourneyViewerRelationships(j *journeyv1.Journey) []string {
	var out []string
	for _, relationship := range j.GetViewer().GetRelationships() {
		if relationship == journeyv1.JourneyViewerRelationship_JOURNEY_VIEWER_RELATIONSHIP_UNSPECIFIED {
			continue
		}
		out = append(out, strings.TrimPrefix(relationship.String(), relationshipEnumPrefix))
	}
	return out
}

// JourneyViewerResponsibility is the server's responsibility token for j, or
// "" when the projection is absent or unspecified.
func JourneyViewerResponsibility(j *journeyv1.Journey) string {
	token := strings.TrimPrefix(j.GetViewer().GetResponsibility().String(), responsibilityEnumPrefix)
	if token == relationshipEnumUnspecifiedToken {
		return ""
	}
	return token
}
