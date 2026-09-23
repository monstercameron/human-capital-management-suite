package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/inbox"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

var _ workspace.WorkflowNotificationReader = (*journeyEngine)(nil)

func (e *journeyEngine) WorkflowNotifications(ctx context.Context, visible []workspace.JourneySummary) (result []workspace.WorkflowNotification, retErr error) {
	ctx, op := observe.Begin(e.observed(ctx), "workflow.notification.list", nil)
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
	records, err := (inbox.Store{}).WorkflowNotices(ctx, tx, tenantID, principal.Subject(), 100)
	if err != nil {
		return nil, err
	}
	byInstance := make(map[string]workspace.JourneySummary, len(visible))
	for _, journey := range visible {
		if journey.InstanceID != "" {
			byInstance[journey.InstanceID] = journey
		}
	}
	instanceIDs := make([]uuid.UUID, 0, len(records))
	seen := map[uuid.UUID]bool{}
	for _, record := range records {
		if _, allowed := byInstance[record.InstanceID.String()]; allowed && !seen[record.InstanceID] {
			instanceIDs = append(instanceIDs, record.InstanceID)
			seen[record.InstanceID] = true
		}
	}
	items, err := (workitem.Store{}).ListForInstances(ctx, tx, tenantID, instanceIDs)
	if err != nil {
		return nil, err
	}
	byItem := make(map[uuid.UUID]workitem.WorkItem)
	for _, group := range items {
		for _, item := range group {
			byItem[item.WorkItemID] = item
		}
	}
	return projectWorkflowNotifications(principal.Subject(), records, byInstance, byItem), nil
}

func projectWorkflowNotifications(subject string, records []inbox.WorkflowRecord, byInstance map[string]workspace.JourneySummary, byItem map[uuid.UUID]workitem.WorkItem) []workspace.WorkflowNotification {
	var result []workspace.WorkflowNotification
	if subject == "" {
		return result
	}
	for _, record := range records {
		journey, allowed := byInstance[record.InstanceID.String()]
		if !allowed {
			continue
		}
		item, exists := byItem[record.WorkItemID]
		if !exists || item.WorkflowInstanceID != record.InstanceID || item.TenantID != record.TenantID || record.SubjectRef != subject {
			continue
		}
		// Reassignment revokes the old recipient's notification even if they
		// retain general visibility of the journey as an HR administrator.
		if item.Assignment.ChosenOwner != subject {
			continue
		}
		result = append(result, workspace.WorkflowNotification{ID: record.InboxRecordID.String(), JourneyID: journey.IntentID,
			WorkItemID: record.WorkItemID.String(), WorkerName: journey.WorkerName, Purpose: record.Purpose,
			Status: string(item.Status), CreatedAt: record.CreatedAt, Read: record.ReadState == inbox.Read})
	}
	return result
}
