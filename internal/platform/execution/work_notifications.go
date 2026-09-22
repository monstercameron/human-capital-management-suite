package execution

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/inbox"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/audience"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// notifyingWorkItems decorates routing, not approval decisions. Notification
// failure rolls back the caller's entire work-item/advancement transaction.
type notifyingWorkItems struct{ next execute.WorkItemFactory }

func (f notifyingWorkItems) CreateAndRoute(ctx context.Context, ex workitem.Executor, req execute.WorkItemRequest) (result workitem.WorkItem, retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.notification.route", req)
	defer func() { observe.DoneWith(op, retErr, result) }()
	if _, ok := ex.(dbport.Tx); !ok {
		return workitem.WorkItem{}, fmt.Errorf("workflow notification: routing requires a transaction")
	}
	item, err := f.next.CreateAndRoute(ctx, ex, req)
	if err != nil {
		return workitem.WorkItem{}, err
	}
	if err := publishWorkNotification(ctx, ex, item); err != nil {
		return workitem.WorkItem{}, err
	}
	return item, nil
}

func publishWorkNotification(ctx context.Context, ex workitem.Executor, item workitem.WorkItem) (retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.notification.publish", item)
	defer func() { observe.Done(op, retErr) }()
	tx, ok := ex.(dbport.Tx)
	if !ok {
		return fmt.Errorf("workflow notification: routing requires a transaction")
	}
	owner := item.Assignment.ChosenOwner
	if owner == "" {
		return fmt.Errorf("workflow notification: resolved recipient is required")
	}
	if _, authorized := item.Assignment.Resolution.Authorizes(owner); !authorized {
		return fmt.Errorf("workflow notification: owner is outside the resolved assignment")
	}
	tenant := values.TenantId(item.TenantID.String())
	// Authentication subject keys need not be UUIDs. Use a tenant-scoped,
	// deterministic opaque reference only for the audience resolver; the
	// inbox remains addressed to the exact authenticated subject key.
	ref := values.EntityRef{Tenant: tenant, Kind: "principal", Id: uuid.NewSHA1(item.TenantID, []byte("notification-principal/v1\x00"+owner)).String()}
	resolution, err := audience.Resolve(ctx, audience.Request{
		Tenant: tenant, AsOf: values.NewInstant(item.RecordedAt), ResolvedAt: values.NewInstant(item.RecordedAt),
		Spec: audience.AudienceSpec{ExplicitSubjects: []values.EntityRef{ref}},
		Scope: func(candidate values.EntityRef, _ audience.SourceKind) audience.DisclosureDecision {
			return audience.DisclosureDecision{Allowed: candidate == ref, Reason: "work_item.assignment", PolicyVersion: item.Assignment.GovernancePolicyRef}
		},
	})
	if err != nil {
		return err
	}
	if len(resolution.Principals) != 1 {
		return fmt.Errorf("workflow notification: no authorized recipient")
	}
	purpose := "TASK"
	if item.Kind == workitem.KindApproval {
		purpose = "APPROVAL"
	}
	_, err = (inbox.Store{}).PublishWorkflow(ctx, tx, inbox.WorkflowNotice{
		TenantID: item.TenantID, WorkItemID: item.WorkItemID, InstanceID: item.WorkflowInstanceID,
		SubjectRef: owner, Purpose: purpose, CorrelationID: item.CorrelationID,
		AudienceDigest: resolution.ResultDigest, CreatedAt: item.RecordedAt,
	})
	return err
}
