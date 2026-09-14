package task

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// SubmitInput is [Submit]'s request: the compiled node the submission must
// match, the construction input for the immutable [Submission] it mints, the
// [Validator] that checks it against the declared form/schema, and the
// instant Submit runs at.
type SubmitInput struct {
	Node      CompiledTaskNode
	Spec      SubmissionSpec
	Validator Validator
	Now       time.Time
	Meta      workitem.TransitionMeta
}

// Submit is TASK's governed completion boundary: FORM-001..003's "server-side
// validation using same compiled rules" plus the human-work spec's
// completion checklist -- current authority, expected item version, required
// output schema, form version, and accessibility/accommodation evidence --
// applied to one durable WorkItem before it is ever handed to
// [workitem.Store.Complete].
//
// It refuses an item that is not currently claimed (no claim to submit
// against), a claim already expired against now, a submission naming a
// different claim or a different schema/form than the compiled node
// declares, and a submitter who is not the exact candidate this item's
// WORK-002 assignment recorded. [NewSubmission] itself refuses arbitrary
// mutable payload shapes and a missing accessibility or accommodation
// acknowledgement: every field on [Submission] is typed and declared, never
// free-form.
//
// A double claim and an expired claim discovered at write time, and a
// duplicate conflicting completion, are refused by [workitem.Store.Complete]
// itself -- the item-version compare-and-set, the claim-expiry touch, and
// migration 00017's immutable-output trigger (surfaced as an illegal
// transition once the item is already terminal).
func Submit(ctx context.Context, tx workitem.Executor, store workitem.Port, item workitem.WorkItem, in SubmitInput) (ret0 workitem.WorkItem, ret1 Submission, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.steps.task.submit", item, in)
	defer func() { observe.DoneWith(obsOp, retErr, ret0, ret1) }()
	if err := validateNode(in.Node); err != nil {
		return workitem.WorkItem{}, Submission{}, err
	}
	if item.Kind != workitem.KindTask || item.NodeID != in.Node.NodeID || item.WorkType != in.Node.WorkType {
		return workitem.WorkItem{}, Submission{}, fmt.Errorf("%w: work item %s does not belong to this compiled node", ErrBindingMismatch, item.WorkItemID)
	}
	if !item.Status.Claimed() {
		return workitem.WorkItem{}, Submission{}, fmt.Errorf("%w: work item %s is not claimed", ErrInvalidSubmission, item.WorkItemID)
	}
	if item.ClaimExpired(in.Now) {
		return workitem.WorkItem{}, Submission{}, fmt.Errorf("%w: claim on work item %s expired before submission", ErrClaimExpired, item.WorkItemID)
	}
	if item.ClaimID == nil || *item.ClaimID != in.Spec.ClaimID {
		return workitem.WorkItem{}, Submission{}, fmt.Errorf("%w: submission names a different claim than the one held", ErrBindingMismatch)
	}
	if in.Spec.OutputSchema != in.Node.OutputSchema || in.Spec.FormDefinition != in.Node.FormDefinition {
		return workitem.WorkItem{}, Submission{}, fmt.Errorf("%w: submission names a different schema or form than the compiled node declares", ErrBindingMismatch)
	}
	candidate, ok := item.Assignment.Resolution.Authorizes(in.Spec.CompletedBy)
	if !ok {
		return workitem.WorkItem{}, Submission{}, fmt.Errorf("%w: %q is not the recorded candidate for work item %s", ErrBindingMismatch, in.Spec.CompletedBy, item.WorkItemID)
	}
	if candidate.Via != in.Spec.CandidateVia || candidate.DelegationID != in.Spec.DelegationID {
		return workitem.WorkItem{}, Submission{}, fmt.Errorf("%w: submitter route does not match the recorded candidate", ErrBindingMismatch)
	}

	spec := in.Spec
	spec.WorkflowInstanceID = item.WorkflowInstanceID
	spec.NodeID = item.NodeID
	spec.WorkItemID = item.WorkItemID
	// store.Complete always increments item_version by exactly one; this is
	// the version the completed row will carry, which is what Resolve's
	// checkCompletion later verifies the submission against.
	spec.ItemVersion = item.ItemVersion + 1
	sub, err := NewSubmission(spec)
	if err != nil {
		return workitem.WorkItem{}, Submission{}, err
	}
	if in.Validator == nil {
		return workitem.WorkItem{}, Submission{}, fmt.Errorf("%w: no validator supplied", ErrValidationFailed)
	}
	if err := in.Validator.Validate(ValidationRequest{Node: in.Node, Submission: sub}); err != nil {
		return workitem.WorkItem{}, Submission{}, fmt.Errorf("%w: %v", ErrValidationFailed, err)
	}

	updated, err := store.Complete(ctx, tx, workitem.CompleteInput{
		TenantID:              item.TenantID,
		WorkItemID:            item.WorkItemID,
		ExpectedVersion:       item.ItemVersion,
		CompletedBy:           sub.CompletedBy,
		CompletedOutputDigest: sub.Digest(),
		Now:                   in.Now,
		Meta:                  in.Meta,
	})
	if err != nil {
		return workitem.WorkItem{}, Submission{}, err
	}
	// WORK-010: the full submission content is recorded in the same
	// transaction as its completion, digest-verified against
	// updated.CompletedOutputDigest (sub.Digest(), recorded verbatim above). A
	// failure here rolls back the completion too, since both run through tx.
	if err := recordSubmission(ctx, tx, updated, sub); err != nil {
		return workitem.WorkItem{}, Submission{}, err
	}
	return updated, sub, nil
}
