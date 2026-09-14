package approval

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// CompiledApprovalNode is the compiler's view of one APPROVAL node: the
// workflow/version/node identity and the fixed work-item framing (work type,
// policy route, visibility, organization scope). It never carries the
// requirement content itself -- APPROVAL-001..003 compiles that separately as
// a [humanwork.ApprovalRequirement] -- because the same compiled node can be
// re-opened against a re-derived requirement without changing what kind of
// work item it produces.
type CompiledApprovalNode struct {
	WorkflowID      string
	WorkflowVersion uint32
	NodeID          string
	WorkType        string

	PolicyRouteRef      string
	Visibility          workitem.Visibility
	OrganizationScopeID string
}

func (n CompiledApprovalNode) validate() error {
	if n.WorkflowID == "" || n.WorkflowVersion == 0 || n.NodeID == "" || n.WorkType == "" ||
		n.PolicyRouteRef == "" || !n.Visibility.Valid() || n.OrganizationScopeID == "" {
		return fmt.Errorf("%w: compiled approval node is incomplete", ErrInvalidContinuation)
	}
	return nil
}

// OpenInput is [Open]'s request: the compiled node a WORK_ITEM_REQUIRED intent
// named, the exact workflow instance and proposal it belongs to, and the
// compiled [humanwork.ApprovalRequirement] it must decide.
type OpenInput struct {
	TenantID   uuid.UUID
	WorkItemID uuid.UUID // zero value: minted here

	WorkflowInstanceID uuid.UUID
	CorrelationID      string
	SubjectRefs        []string

	Node        CompiledApprovalNode
	Requirement humanwork.ApprovalRequirement
	Proposal    intent.ProposalRevision

	Now  time.Time
	Meta workitem.TransitionMeta
}

// Open creates the governed ApprovalTask work item a WORK_ITEM_REQUIRED
// intent named for an APPROVAL node: kind APPROVAL, the exact requirement
// reference, and the proposal revision's material digest pinned into
// ProposalRef. It performs no candidate resolution or assignment -- that is
// [workitem.Store.Route] over [workitem.ResolveAssignment], run separately --
// and no decision evaluation -- that is [Resolve]. Open's only job is
// minting the one durable record every later step in this package binds
// against.
func Open(ctx context.Context, tx workitem.Executor, store workitem.Port, in OpenInput) (ret0 workitem.WorkItem, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.steps.approval.open", in)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := in.Node.validate(); err != nil {
		return workitem.WorkItem{}, err
	}
	if in.Requirement.RequirementID == "" || in.Requirement.Digest() == "" {
		return workitem.WorkItem{}, fmt.Errorf("%w: requirement is not compiled", ErrInvalidContinuation)
	}
	if err := in.Requirement.Deadline.Validate(); err != nil {
		return workitem.WorkItem{}, fmt.Errorf("%w: %v", ErrInvalidContinuation, err)
	}
	if in.Proposal.ProposalRevisionID == "" || in.Proposal.MaterialDigest.Digest == "" {
		return workitem.WorkItem{}, fmt.Errorf("%w: proposal is not pinned", ErrInvalidContinuation)
	}
	if in.WorkflowInstanceID == uuid.Nil || in.TenantID == uuid.Nil {
		return workitem.WorkItem{}, ErrInvalidContinuation
	}
	if in.Now.IsZero() {
		return workitem.WorkItem{}, fmt.Errorf("%w: current instant is required", ErrInvalidContinuation)
	}

	item, err := workitem.NewApprovalTask(workitem.NewWorkItemInput{
		TenantID:            in.TenantID,
		WorkItemID:          in.WorkItemID,
		WorkType:            in.Node.WorkType,
		CorrelationID:       in.CorrelationID,
		WorkflowInstanceID:  in.WorkflowInstanceID,
		NodeID:              in.Node.NodeID,
		ProposalRef:         in.Proposal.MaterialDigest.Digest,
		SubjectRefs:         in.SubjectRefs,
		PolicyRouteRef:      in.Node.PolicyRouteRef,
		Visibility:          in.Node.Visibility,
		OrganizationScopeID: in.Node.OrganizationScopeID,
		DeadlineAt:          in.Requirement.Deadline.Expiry.Time(),
		CreatedAt:           in.Now,
	}, in.Requirement.RequirementID)
	if err != nil {
		return workitem.WorkItem{}, err
	}
	return store.Create(ctx, tx, item, in.Meta)
}
