package execution

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/inbox"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/audience"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/notifyplan"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// requesterStatusWorkItems tells the person who started a request that it has
// been routed to someone. It wraps routing the way notifyingWorkItems does, so
// the notice commits or rolls back with the work item it reports.
type requesterStatusWorkItems struct{ next execute.WorkItemFactory }

func (f requesterStatusWorkItems) CreateAndRoute(ctx context.Context, ex workitem.Executor, req execute.WorkItemRequest) (result workitem.WorkItem, retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.notification.requester.route", req)
	defer func() { observe.DoneWith(op, retErr, result) }()
	item, err := f.next.CreateAndRoute(ctx, ex, req)
	if err != nil {
		return workitem.WorkItem{}, err
	}
	stepType := "TASK"
	if item.Kind == workitem.KindApproval {
		stepType = "APPROVAL"
	}
	err = publishRequesterStatus(ctx, ex, requesterStatus{
		stepType: stepType, moment: notifyplan.MomentRouted, event: inbox.StatusInReview, eventRef: item.WorkItemID.String(),
		tenant: item.TenantID, instance: item.WorkflowInstanceID, proposal: req.Proposal,
		// The owner of the routed work already has the approval or task
		// notice; a requester who is also the owner is not told twice.
		skip: item.Assignment.ChosenOwner, correlation: item.CorrelationID, at: item.RecordedAt,
	})
	if err != nil {
		return workitem.WorkItem{}, err
	}
	return item, nil
}

// requesterStatusTerminal tells the requester that the run finished, inside
// the transaction of the governed terminal write.
type requesterStatusTerminal struct{ next execute.TerminalWriter }

func (w requesterStatusTerminal) Write(ctx context.Context, tx dbport.Tx, req execute.TerminalWriteRequest) (result idempotency.ResultIdentity, retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.notification.requester.terminal", req)
	defer func() { observe.Done(op, retErr) }()
	identity, err := w.next.Write(ctx, tx, req)
	if err != nil {
		return identity, err
	}
	endRef := req.EndNodeID
	if endRef == "" {
		endRef = req.TerminalCode
	}
	err = publishRequesterStatus(ctx, tx, requesterStatus{
		stepType: "END", moment: notifyplan.MomentFinished, event: inbox.StatusFinished, eventRef: endRef,
		tenant: req.TenantID, instance: req.InstanceID, proposal: req.Proposal, correlation: req.CorrelationID, at: req.RecordedAt,
	})
	return identity, err
}

type requesterStatus struct {
	stepType                           string
	moment                             notifyplan.Moment
	event, eventRef, skip, correlation string
	tenant, instance                   uuid.UUID
	proposal                           runtime.ProposalBinding
	at                                 time.Time
}

// publishRequesterStatus publishes only what notifyplan declares for the step,
// which is also what the workflow editor shows its author. A request with no
// recorded human requester has nobody to tell and publishes nothing.
func publishRequesterStatus(ctx context.Context, ex workitem.Executor, status requesterStatus) (retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.notification.status", nil)
	defer func() { observe.Done(op, retErr) }()
	op.Set(observe.KeyTenant, status.tenant.String())
	op.Set(observe.KeyInstance, status.instance.String())
	op.Set(observe.KeyCorrelation, status.correlation)
	declared := false
	for _, notice := range notifyplan.ForStep(status.stepType) {
		if notice.Audience == notifyplan.AudienceRequester && notice.Moment == status.moment {
			declared = true
		}
	}
	requester := strings.TrimSpace(status.proposal.Revision.CreatedBy.PrincipalID)
	if !declared || requester == "" || requester == status.skip {
		return nil
	}
	tx, ok := ex.(dbport.Tx)
	if !ok {
		return fmt.Errorf("workflow notification: a status notice requires a transaction")
	}
	tenant := values.TenantId(status.tenant.String())
	ref := values.EntityRef{Tenant: tenant, Kind: "principal", Id: uuid.NewSHA1(status.tenant, []byte("notification-principal/v1\x00"+requester)).String()}
	resolution, err := audience.Resolve(ctx, audience.Request{
		Tenant: tenant, AsOf: values.NewInstant(status.at), ResolvedAt: values.NewInstant(status.at),
		Spec: audience.AudienceSpec{ExplicitSubjects: []values.EntityRef{ref}},
		Scope: func(candidate values.EntityRef, _ audience.SourceKind) audience.DisclosureDecision {
			return audience.DisclosureDecision{Allowed: candidate == ref, Reason: "workflow.requester", PolicyVersion: "workflow.status/v1"}
		},
	})
	if err != nil {
		return err
	}
	if len(resolution.Principals) != 1 {
		return fmt.Errorf("workflow notification: no authorized requester")
	}
	_, err = (inbox.Store{}).PublishWorkflowStatus(ctx, tx, inbox.WorkflowStatusNotice{
		TenantID: status.tenant, InstanceID: status.instance, SubjectRef: requester, Event: status.event, EventRef: status.eventRef,
		CorrelationID: status.correlation, AudienceDigest: resolution.ResultDigest, CreatedAt: status.at,
	})
	return err
}
