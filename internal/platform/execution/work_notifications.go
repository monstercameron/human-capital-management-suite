package execution

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/audience"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// notifyingWorkItems decorates routing, not approval decisions. Notification
// failure rolls back the caller's entire work-item/advancement transaction.
type notifyingWorkItems struct{ next execute.WorkItemFactory }

type currentWorkItemAudienceResolver interface {
	CurrentWorkItemAudience(context.Context, workitem.Executor, workitem.WorkItem) (string, string, error)
}

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
	if err := publishWorkNotification(ctx, ex, item, f.next); err != nil {
		return workitem.WorkItem{}, err
	}
	return item, nil
}

func publishWorkNotification(ctx context.Context, ex workitem.Executor, item workitem.WorkItem, authorities ...execute.WorkItemFactory) (retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.notification.publish", item)
	defer func() { observe.Done(op, retErr) }()
	tx, ok := ex.(dbport.Tx)
	if !ok {
		return fmt.Errorf("workflow notification: routing requires a transaction")
	}
	if len(authorities) != 1 || authorities[0] == nil {
		return fmt.Errorf("workflow notification: current audience authority is unavailable")
	}
	resolver, ok := authorities[0].(currentWorkItemAudienceResolver)
	if !ok {
		return fmt.Errorf("workflow notification: current audience authority is unavailable")
	}
	owner, policy, err := resolver.CurrentWorkItemAudience(ctx, tx, item)
	if err != nil {
		return err
	}
	if owner == "" || policy == "" {
		return fmt.Errorf("workflow notification: current audience authority returned no recipient")
	}
	tenant := values.TenantId(item.TenantID.String())
	ref := values.EntityRef{Tenant: tenant, Kind: "principal", Id: uuid.NewSHA1(item.TenantID, []byte("notification-principal/v1\x00"+owner)).String()}
	spec := audience.AudienceSpec{ExplicitSubjects: []values.EntityRef{ref}}
	resolution, err := audience.Resolve(ctx, audience.Request{
		Tenant: tenant, AsOf: values.NewInstant(item.RecordedAt), ResolvedAt: values.NewInstant(item.RecordedAt), Spec: spec,
		Scope: func(candidate values.EntityRef, source audience.SourceKind) audience.DisclosureDecision {
			return audience.DisclosureDecision{Allowed: candidate == ref && source == audience.SourceExplicit, Reason: "current_work_item_route", PolicyVersion: policy}
		},
	})
	if err != nil {
		return err
	}
	if resolution.Expression != spec.Expression() {
		return fmt.Errorf("workflow notification: audience expression changed during resolution")
	}
	return execute.PublishWorkItemMessage(ctx, tx, item, resolution)
}
