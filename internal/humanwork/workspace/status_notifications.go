package workspace

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/notifyplan"
)

// WorkflowStatusReader returns the caller's own status notices for journeys
// they can still see. A status notice has no work item and grants nothing. Its
// Purpose is notifyplan.PurposeUpdate and its Status one of notifyplan's
// statuses.
type WorkflowStatusReader interface {
	WorkflowStatusNotifications(context.Context, []JourneySummary) ([]WorkflowNotification, error)
}

// FinishedNotificationStatus names how a finished request ended. It reads the
// stage the viewer is already authorized to see, never stored copy.
func FinishedNotificationStatus(stage JourneyStage) string {
	switch stage {
	case JourneyStageCompleted, JourneyStageExecuted, JourneyStageRecorded, JourneyStageObservingEffects, JourneyStageAwaitingAcknowledgement:
		return notifyplan.StatusFinishedApproved
	case JourneyStageRejected:
		return notifyplan.StatusFinishedDeclined
	case JourneyStageFailed, JourneyStageRepairRequired, JourneyStageBlocked:
		return notifyplan.StatusFinishedFailed
	default:
		return notifyplan.StatusFinished
	}
}
