package execute

import (
	"context"

	transactioncancel "github.com/monstercameron/human-capital-management-suite/internal/transaction/cancel"
	transactioncommit "github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/plan"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// GovernedCommitResult is the execute-layer handoff for a terminal governed
// transaction. It leaves ordinary workflow cancellation unchanged while
// exposing TX-008 to callers that own a prepared transaction plan.
type GovernedCommitResult struct {
	Decision transactioncancel.Result
	Receipt  transactioncommit.Receipt
}

// CommitGoverned commits a prepared plan through the driver's database port.
func (d *Driver) CommitGoverned(ctx context.Context, prepared plan.TransactionPlan, req transactioncancel.Request) (ret0 GovernedCommitResult, retErr error) {
	ctx, obsOp := observe.Begin(d.observed(ctx), "workflow.execute.commit_governed", prepared, req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if d == nil || d.opts.DB == nil {
		return GovernedCommitResult{}, invalid("governed commit requires a database")
	}
	result, err := transactioncommit.New(d.opts.DB, transactioncommit.Options{
		Clock: d.opts.Clock, ConflictFence: d.opts.ConflictFence,
	}).CommitGoverned(ctx, prepared, req)
	return GovernedCommitResult{Decision: result.Decision, Receipt: result.Receipt}, err
}

// CancelGoverned resolves a prepared plan's cancellation and invokes the
// caller-owned governed compensation/correction launcher after AFTER_COMMIT.
func (d *Driver) CancelGoverned(ctx context.Context, req transactioncancel.Request, compensator transactioncancel.Compensator) (ret0 GovernedCommitResult, retErr error) {
	ctx, obsOp := observe.Begin(d.observed(ctx), "workflow.execute.cancel_governed", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if d == nil || d.opts.DB == nil {
		return GovernedCommitResult{}, invalid("governed cancellation requires a database")
	}
	result, err := transactioncommit.New(d.opts.DB, transactioncommit.Options{Clock: d.opts.Clock}).CancelGoverned(ctx, req, compensator)
	return GovernedCommitResult{Decision: result.Decision, Receipt: result.Receipt}, err
}
