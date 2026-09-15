// Package promotionterminal composes the bounded domain mutation with the
// workflow terminal ledger write. Both receive the same caller-owned
// transaction, so COMPLETE can never be durable without all local Promotion
// successor facts (or vice versa).
package promotionterminal

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
	domaincommit "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// CommandResolver materializes the exact approved proposal and immutable
// plan into a typed domain command while the terminal transaction is open.
// The production implementation is Resolver in resolve.go; ResolverFunc
// adapts a closure for tests.
type CommandResolver interface {
	Resolve(context.Context, dbport.Tx, execute.TerminalWriteRequest) (domaincommit.Command, error)
}

type ResolverFunc func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (domaincommit.Command, error)

func (f ResolverFunc) Resolve(ctx context.Context, tx dbport.Tx, req execute.TerminalWriteRequest) (domaincommit.Command, error) {
	return f(ctx, tx, req)
}

type Mutator interface {
	Write(context.Context, dbport.Tx, domaincommit.Command) (promotioncommit.Receipt, error)
}

// Writer is an execute.TerminalWriter decorator. Only the approved terminal
// code mutates: every other code records its ledger fact with no successor
// facts behind it, and an unconfigured writer refuses outright instead of
// silently recording outcomes with no facts behind them.
type Writer struct {
	// ApprovedTerminalCode is the promotion workflow's approved END code
	// (PROMOTION_COMPLETE): the only terminal code that may write successor
	// facts. An empty code keeps the pre-gate composer shape the package's
	// own unit tests use, and it is refused the moment a real terminal code
	// arrives, so production -- where the run always records a code -- can
	// never run unconfigured.
	ApprovedTerminalCode string
	Resolver             CommandResolver
	Mutation             Mutator
	Next                 execute.TerminalWriter
}

var _ execute.TerminalWriter = (*Writer)(nil)

func (w *Writer) Write(ctx context.Context, tx dbport.Tx, req execute.TerminalWriteRequest) (ret0 idempotency.ResultIdentity, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_terminal.write", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if w == nil || w.Resolver == nil || w.Mutation == nil || w.Next == nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("promotion terminal: resolver, mutation writer and next writer are required")
	}
	if w.ApprovedTerminalCode == "" {
		if req.TerminalCode != "" {
			return idempotency.ResultIdentity{}, fmt.Errorf("promotion terminal: no approved terminal code is configured")
		}
	} else if req.TerminalCode != w.ApprovedTerminalCode {
		return w.Next.Write(ctx, tx, req)
	}
	cmd, err := w.Resolver.Resolve(ctx, tx, req)
	if err != nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("promotion terminal: resolve mutation: %w", err)
	}
	if cmd.TenantID != req.TenantID.String() || cmd.ProposalRevisionID != req.Proposal.Revision.ProposalRevisionID ||
		cmd.ProposalDigest != req.Proposal.Revision.MaterialDigest.Digest || cmd.WorkflowPlanDigest != req.PlanDigest {
		return idempotency.ResultIdentity{}, fmt.Errorf("%w: terminal request does not match the promotion command", domaincommit.ErrInvalidCommand)
	}
	if _, err := w.Mutation.Write(ctx, tx, cmd); err != nil {
		return idempotency.ResultIdentity{}, err
	}
	return w.Next.Write(ctx, tx, req)
}
