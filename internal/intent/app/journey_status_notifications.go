package app

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/data/inbox"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/notifyplan"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

var _ workspace.WorkflowStatusReader = (*journeyEngine)(nil)

// WorkflowStatusNotifications lists the caller's status notices for requests
// they started. It discloses only journeys already in visible, the caller's
// authorized list, so losing sight of a request also hides its notices.
func (e *journeyEngine) WorkflowStatusNotifications(ctx context.Context, visible []workspace.JourneySummary) (result []workspace.WorkflowNotification, retErr error) {
	ctx, op := observe.Begin(e.observed(ctx), "workflow.notification.status_list", nil)
	defer func() { observe.Done(op, retErr) }()
	principal, err := journeyPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	tenantID := e.svc.tenantUUID(principal.Tenant())
	op.Set(observe.KeyTenant, tenantID.String())
	tx, err := e.beginTenant(ctx, principal)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	records, err := (inbox.Store{}).WorkflowStatusNotices(ctx, tx, tenantID, principal.Subject(), 100)
	if err != nil {
		return nil, err
	}
	return projectWorkflowStatusNotifications(principal.Subject(), records, visible), nil
}

func projectWorkflowStatusNotifications(subject string, records []inbox.WorkflowStatusRecord, visible []workspace.JourneySummary) []workspace.WorkflowNotification {
	var result []workspace.WorkflowNotification
	if subject == "" {
		return result
	}
	byInstance := make(map[string]workspace.JourneySummary, len(visible))
	for _, journey := range visible {
		if journey.InstanceID != "" {
			byInstance[journey.InstanceID] = journey
		}
	}
	// Records arrive newest first. A request that passes through several
	// reviewers publishes one notice per routing; the bell shows each request
	// once, at its latest update, rather than a run of identical rows.
	shown := make(map[string]bool, len(records))
	for _, record := range records {
		journey, allowed := byInstance[record.InstanceID.String()]
		if !allowed || record.SubjectRef != subject || shown[journey.IntentID] {
			continue
		}
		shown[journey.IntentID] = true
		status := notifyplan.StatusSentForReview
		if record.Event == inbox.StatusFinished {
			status = workspace.FinishedNotificationStatus(journey.Stage)
		}
		result = append(result, workspace.WorkflowNotification{ID: record.InboxRecordID.String(), JourneyID: journey.IntentID,
			WorkerName: journey.WorkerName, Purpose: notifyplan.PurposeUpdate, Status: status,
			CreatedAt: record.CreatedAt, Read: record.ReadState == inbox.Read})
	}
	return result
}
