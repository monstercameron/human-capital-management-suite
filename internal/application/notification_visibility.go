package application

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// requiredWorkflowNotificationReader enforces the notification authorization
// contract for a configured execution journey. A cell without execution may
// omit its feed; an execution cell must not silently start without current
// recipient authorization.
func requiredWorkflowNotificationReader(engine any, executionRequired bool) (workspace.WorkflowNotificationReader, error) {
	reader, ok := engine.(workspace.WorkflowNotificationReader)
	if ok {
		return reader, nil
	}
	if executionRequired {
		return nil, errors.New("application: served journey engine does not expose current notification authorization")
	}
	return nil, nil
}

// journeyNotificationVisibility uses the served journey engine as the current
// authorization authority. The engine re-evaluates journey visibility and
// the current work-item assignee before it returns any notification.
type journeyNotificationVisibility struct {
	engine interface {
		ListJourneys(context.Context) ([]workspace.JourneySummary, error)
	}
	notifications workspace.WorkflowNotificationReader
}

func (v journeyNotificationVisibility) VisibleWorkflows(ctx context.Context, tenant uuid.UUID, subject string, instanceIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	principal, err := trust.MustFromContext(ctx)
	if err != nil || principal.Subject() != subject {
		return nil, errors.New("application: notification principal does not match authenticated context")
	}
	if v.engine == nil {
		return nil, errors.New("application: current journey authorization is unavailable")
	}
	journeys, err := v.engine.ListJourneys(ctx)
	if err != nil {
		return nil, err
	}
	allowedIDs := make(map[uuid.UUID]bool, len(instanceIDs))
	for _, id := range instanceIDs {
		allowedIDs[id] = true
	}
	reader := v.notifications
	if reader == nil {
		return nil, errors.New("application: current work-item authorization is unavailable")
	}
	byIntent := make(map[string]uuid.UUID, len(journeys))
	candidates := make([]workspace.JourneySummary, 0, len(instanceIDs))
	for _, journey := range journeys {
		id, parseErr := uuid.Parse(journey.InstanceID)
		if parseErr == nil && allowedIDs[id] {
			byIntent[journey.IntentID] = id
			candidates = append(candidates, journey)
		}
	}
	notifications, err := reader.WorkflowNotifications(ctx, candidates)
	if err != nil {
		return nil, err
	}
	visible := make(map[uuid.UUID]bool)
	for _, notification := range notifications {
		if id, ok := byIntent[notification.JourneyID]; ok {
			visible[id] = true
		}
	}
	return visible, nil
}
