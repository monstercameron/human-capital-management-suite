package workspace

import (
	"context"
	"time"
)

// WorkflowNotification links one recipient's durable inbox record to an
// authorized workflow. Status is current work state, not delivery evidence.
type WorkflowNotification struct {
	ID, JourneyID, WorkItemID, WorkerName, Purpose, Status string
	CreatedAt                                              time.Time
	Read                                                   bool
}

// WorkflowNotificationReader rechecks recipient and current visibility against
// the caller's already-authorized journeys. It grants no approval authority.
type WorkflowNotificationReader interface {
	WorkflowNotifications(context.Context, []JourneySummary) ([]WorkflowNotification, error)
}
