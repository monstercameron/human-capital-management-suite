package app

import (
	"sort"
	"strings"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
)

// journeyTransition is the next transition one journey stage names: the step,
// the role class holding it, and whether a person (rather than the workflow
// or the calendar) holds it. It is the single owner of this mapping
// (PROMOUX-012 REFACTOR): My Work, tracked requests, Journeys and the person
// profile read it off the wire instead of each keeping a stage table.
type journeyTransition struct {
	step         workspace.JourneyNextStep
	owner        workspace.JourneyStepOwner
	awaitsPerson bool
}

// journeyStageTransition maps a stage to its next transition. It is total:
// a closed or unknown stage has no next step and no owner, never a guessed
// one.
func journeyStageTransition(stage workspace.JourneyStage) journeyTransition {
	switch stage {
	case workspace.JourneyStageProposed:
		return journeyTransition{workspace.JourneyNextStepStartApproval, workspace.JourneyStepOwnerProposer, true}
	case workspace.JourneyStageBlocked:
		return journeyTransition{workspace.JourneyNextStepCorrectProposal, workspace.JourneyStepOwnerProposer, true}
	case workspace.JourneyStageAwaitingApproval:
		return journeyTransition{workspace.JourneyNextStepApprovalDecision, workspace.JourneyStepOwnerApprover, true}
	case workspace.JourneyStageManagerApproval:
		return journeyTransition{workspace.JourneyNextStepManagerDecision, workspace.JourneyStepOwnerManager, true}
	case workspace.JourneyStageFinanceApproval:
		return journeyTransition{workspace.JourneyNextStepFinanceDecision, workspace.JourneyStepOwnerFinance, true}
	case workspace.JourneyStageReapproval:
		return journeyTransition{workspace.JourneyNextStepReapprovalDecision, workspace.JourneyStepOwnerApprover, true}
	case workspace.JourneyStageRepairRequired:
		// Repair is a person's job, but nothing on the journey names whose.
		return journeyTransition{workspace.JourneyNextStepRepair, "", true}
	case workspace.JourneyStageAwaitingAcknowledgement:
		// Recording the verified acknowledgement is a person's job, but
		// nothing on the journey names whose: the attester is whoever
		// verifies it, and must not be the initiator.
		return journeyTransition{workspace.JourneyNextStepAwaitAcknowledgement, "", true}
	case workspace.JourneyStageWaitingEffectiveDate:
		return journeyTransition{workspace.JourneyNextStepAwaitEffectiveDate, workspace.JourneyStepOwnerSystem, false}
	case workspace.JourneyStageRevalidation, workspace.JourneyStageExecuted, workspace.JourneyStageObservingEffects:
		return journeyTransition{workspace.JourneyNextStepSystemProcessing, workspace.JourneyStepOwnerSystem, false}
	default:
		return journeyTransition{}
	}
}

// journeyStageClosed reports whether stage is terminal. It is the same set
// interventionUnavailableAtStage treats as already terminal.
func journeyStageClosed(stage workspace.JourneyStage) bool {
	switch stage {
	case workspace.JourneyStageCompleted, workspace.JourneyStageRejected,
		workspace.JourneyStageFailed, workspace.JourneyStageRecorded:
		return true
	}
	return false
}

// requestEndedBeforeExecution reports whether the intent's own request was
// closed (withdrawn, cancelled, superseded by an edit, rejected or closed).
// With no workflow instance the stage is otherwise derived from the
// simulation alone, which still mints a revision for a withdrawn draft: the
// journey read as PROPOSED forever, kept offering Start and Withdraw, and
// the client's active-journey redirect sent every new proposal for that
// employee back to it.
func requestEndedBeforeExecution(state lifecycle.RequestState) bool {
	switch state {
	case lifecycle.RequestCancelled, lifecycle.RequestWithdrawn, lifecycle.RequestSuperseded,
		lifecycle.RequestRejected, lifecycle.RequestClosed:
		return true
	}
	return false
}

// requestProtoEndedBeforeExecution is requestEndedBeforeExecution over the
// wire enum ListJourneys reads.
func requestProtoEndedBeforeExecution(state intentsv1.RequestState) bool {
	switch state {
	case intentsv1.RequestState_REQUEST_STATE_CANCELLED, intentsv1.RequestState_REQUEST_STATE_WITHDRAWN,
		intentsv1.RequestState_REQUEST_STATE_SUPERSEDED, intentsv1.RequestState_REQUEST_STATE_REJECTED,
		intentsv1.RequestState_REQUEST_STATE_CLOSED:
		return true
	}
	return false
}

// journeyViewerProjection resolves how the calling viewer stands to one
// journey (PROMOUX-012).
//
// It is built only from what the engine already holds for this viewer:
// whether the viewer is the intent's recorded initiator (a comparison of the
// viewer's own subject with the stored initiator, which is never itself
// disclosed), and the viewer's membership in the current work item as
// journeyWorkItemSummary already disclosed it under the work item read rules
// (nil when those rules admit the viewer to nothing). No other principal's
// identity enters the result, so a viewer with no relationship receives the
// same projection whoever initiated the journey or holds its work item.
//
// Responsibility, in order:
//   - CLOSED for a terminal stage;
//   - ACTION_REQUIRED when the viewer holds or may claim the current work
//     item, or initiated the journey and its next step is the proposer's
//     (start approval, correct a blocked proposal);
//   - TRACKING when the viewer initiated it and someone else, or the
//     workflow, holds the next step -- a passive wait is never actionable;
//   - OBSERVING otherwise.
func journeyViewerProjection(stage workspace.JourneyStage, viewerIsInitiator bool, work *workspace.JourneyWorkItemSummary) workspace.JourneyViewerProjection {
	transition := journeyStageTransition(stage)
	out := workspace.JourneyViewerProjection{
		NextStep: transition.step, NextStepOwner: transition.owner, AwaitsPerson: transition.awaitsPerson,
		Closed: journeyStageClosed(stage),
	}
	holds, mayClaim := false, false
	if work != nil {
		switch work.ViewerMembership {
		case "ASSIGNEE", "CLAIMANT":
			holds = true
		case "CANDIDATE":
			mayClaim = true
		}
	}
	if viewerIsInitiator {
		out.Relationships = append(out.Relationships, workspace.JourneyViewerInitiator)
	}
	if holds {
		out.Relationships = append(out.Relationships, workspace.JourneyViewerAssignee)
	}
	if mayClaim {
		out.Relationships = append(out.Relationships, workspace.JourneyViewerCandidate)
	}
	sort.Slice(out.Relationships, func(i, j int) bool { return out.Relationships[i] < out.Relationships[j] })

	switch {
	case out.Closed:
		out.Responsibility = workspace.JourneyResponsibilityClosed
	case holds || mayClaim:
		out.Responsibility = workspace.JourneyResponsibilityActionRequired
	case viewerIsInitiator && transition.owner == workspace.JourneyStepOwnerProposer:
		out.Responsibility = workspace.JourneyResponsibilityActionRequired
	case viewerIsInitiator:
		out.Responsibility = workspace.JourneyResponsibilityTracking
	default:
		out.Responsibility = workspace.JourneyResponsibilityObserving
	}
	return out
}

// isJourneyInitiator reports whether viewer is the stored initiator. Both
// sides must be non-empty: an intent with no recorded initiator, or a viewer
// with no subject, initiates nothing.
func isJourneyInitiator(initiatorPrincipalID, viewer string) bool {
	initiatorPrincipalID, viewer = strings.TrimSpace(initiatorPrincipalID), strings.TrimSpace(viewer)
	return initiatorPrincipalID != "" && initiatorPrincipalID == viewer
}
