package execution

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
)

// WF-STEP-018: the served approval kernel. executeDriverAdapter hands the
// journey's approval votes and invalidations to execute.Driver's generic
// CompleteApproval and InvalidateApproval, so a served decision and its
// workflow advancement commit in one transaction.

var _ app.ApprovalKernel = executeDriverAdapter{}

// recheckAuthority adapts the journey's in-transaction recheck to the
// driver's CurrentApprovalAuthority port. The journey's refusal travels as
// the error, so its typed cause survives; an admitted vote stands on the
// decision's own authority reference.
type recheckAuthority func(context.Context, workitem.Executor, workitem.WorkItem) error

func (r recheckAuthority) Recheck(ctx context.Context, ex workitem.Executor, in execute.CurrentApprovalAuthorityRequest) (execute.CurrentApprovalAuthorityDecision, error) {
	if r != nil {
		if err := r(ctx, ex, in.Item); err != nil {
			return execute.CurrentApprovalAuthorityDecision{}, err
		}
	}
	return execute.CurrentApprovalAuthorityDecision{Allowed: true, DecisionRef: in.Decision.AuthorityDecisionRef}, nil
}

// CompleteApproval implements app.ApprovalKernel.
func (a executeDriverAdapter) CompleteApproval(ctx context.Context, req app.ApprovalVoteRequest) (app.ApprovalVoteResult, error) {
	result, err := a.driver.CompleteApproval(ctx, execute.ApprovalCompletionRequest{
		Start: req.Start, InstanceID: req.InstanceID, ExpectedInstanceVersion: req.ExpectedInstanceVersion,
		WorkItemID: req.WorkItemID, ExpectedWorkItemVersion: req.ExpectedWorkItemVersion,
		Continuation: req.Continuation, Decision: req.Decision, RecordedAt: req.RecordedAt, Meta: req.Meta,
		Authority: recheckAuthority(req.Recheck), Prepare: req.Prepare,
		// WF-RUN-034: the promotion's GOVERN-002 record is appended in this
		// same vote transaction, after the caller's own decision evidence, so
		// an approval and the governance its revalidation will recompose can
		// never disagree about whether they happened.
		Record: a.withApprovalGovernance(req),
	})
	if err != nil {
		return app.ApprovalVoteResult{}, approvalKernelError(err)
	}
	return app.ApprovalVoteResult{
		Execution: adaptExecutionResult(result.Result, req.InstanceID.String()),
		Item:      result.CompletedItem, Pending: result.Pending, Replay: result.Replay,
	}, nil
}

// InvalidateApproval implements app.ApprovalKernel.
func (a executeDriverAdapter) InvalidateApproval(ctx context.Context, req app.ApprovalInvalidationRequest) (app.ExecutionResult, error) {
	result, err := a.driver.InvalidateApproval(ctx, execute.ApprovalInvalidationRequest{
		Start: req.Start, InstanceID: req.InstanceID, ExpectedInstanceVersion: req.ExpectedInstanceVersion,
		Continuation: req.Continuation, Requirements: req.Requirements, Change: req.Change,
		Reason: req.Reason, EvidenceRef: req.EvidenceRef, RecordedAt: req.RecordedAt, Meta: req.Meta,
	})
	if err != nil {
		return app.ExecutionResult{}, approvalKernelError(err)
	}
	return adaptExecutionResult(result.Result, req.InstanceID.String()), nil
}

// approvalKernelError names a vote the durable record no longer admits in the
// decision vocabulary internal/intent/app reports; every other error travels
// unchanged.
func approvalKernelError(err error) error {
	if errors.Is(err, execute.ErrApprovalCompletionConflict) {
		return fmt.Errorf("%w: %w", app.ErrProposalDecisionConflict, err)
	}
	return err
}
