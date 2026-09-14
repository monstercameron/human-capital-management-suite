package task

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// OpenInput is [Open]'s request: the compiled TASK node a WORK_ITEM_REQUIRED
// intent named, the exact workflow instance it belongs to, and the fixed
// work-item framing (policy route, visibility, organization scope, subjects)
// FORM-001..003 requires every governed task to carry.
type OpenInput struct {
	TenantID   uuid.UUID
	WorkItemID uuid.UUID // zero value: minted here

	WorkflowInstanceID uuid.UUID
	CorrelationID      string
	SubjectRefs        []string

	Node                CompiledTaskNode
	PolicyRouteRef      string
	Visibility          workitem.Visibility
	OrganizationScopeID string
	DeadlineAt          time.Time

	Now  time.Time
	Meta workitem.TransitionMeta
}

// Open creates the governed TASK work item a WORK_ITEM_REQUIRED intent named:
// kind TASK, the compiled node's exact work type and node id. It performs no
// candidate resolution or assignment -- that is [workitem.Store.Route], run
// separately -- and no form validation, which is [Submit]'s job once a
// candidate submits. Open's only job is minting the one durable record
// [NewContinuation] and [Submit] both bind against.
func Open(ctx context.Context, tx workitem.Executor, store workitem.Port, in OpenInput) (ret0 workitem.WorkItem, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.steps.task.open", in)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := validateNode(in.Node); err != nil {
		return workitem.WorkItem{}, err
	}
	if in.WorkflowInstanceID == uuid.Nil || in.TenantID == uuid.Nil {
		return workitem.WorkItem{}, ErrInvalidContinuation
	}
	if in.Now.IsZero() || in.DeadlineAt.IsZero() {
		return workitem.WorkItem{}, fmt.Errorf("%w: current instant and deadline are required", ErrInvalidContinuation)
	}

	item, err := workitem.NewWorkItem(workitem.NewWorkItemInput{
		TenantID:            in.TenantID,
		WorkItemID:          in.WorkItemID,
		Kind:                workitem.KindTask,
		WorkType:            in.Node.WorkType,
		CorrelationID:       in.CorrelationID,
		WorkflowInstanceID:  in.WorkflowInstanceID,
		NodeID:              in.Node.NodeID,
		SubjectRefs:         in.SubjectRefs,
		PolicyRouteRef:      in.PolicyRouteRef,
		Visibility:          in.Visibility,
		OrganizationScopeID: in.OrganizationScopeID,
		DeadlineAt:          in.DeadlineAt,
		CreatedAt:           in.Now,
	})
	if err != nil {
		return workitem.WorkItem{}, err
	}
	return store.Create(ctx, tx, item, in.Meta)
}
