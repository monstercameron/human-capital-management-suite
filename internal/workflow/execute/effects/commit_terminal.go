package effects

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	transactioncommit "github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/plan"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// TransactionPlanProvider binds the immutable TX-003 plan to the terminal
// request that reached COMPLETE. It runs inside the same transaction as the
// commit, so a provider can re-read a durable plan binding without opening a
// second database transaction.
type TransactionPlanProvider func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (plan.TransactionPlan, error)

// InTxCommitter is the small seam the adapter needs from transaction/commit.
// Keeping it local lets workflow tests use a deterministic fake without
// importing PostgreSQL types or making the execute port depend on a concrete
// coordinator.
type InTxCommitter interface {
	CommitInTx(context.Context, dbport.Tx, plan.TransactionPlan) (transactioncommit.Receipt, error)
}

// CommitTerminalWriter is an additive TerminalWriter adapter. When Plan and
// Committer are supplied it commits the prepared plan through the TX-004
// coordinator. Next is an optional compatibility delegate for existing
// terminal fixtures while callers migrate their plan preparation to TX-003.
type CommitTerminalWriter struct {
	Committer InTxCommitter
	Plan      TransactionPlanProvider
	Next      execute.TerminalWriter
}

var _ execute.TerminalWriter = (*CommitTerminalWriter)(nil)

// Write commits a prepared transaction plan inside tx and returns the receipt
// identities expected by TX-006. A configured Next delegate is used only when
// this adapter has not yet been given a plan provider, preserving the existing
// TerminalWriter port during additive rollout.
func (w *CommitTerminalWriter) Write(ctx context.Context, tx dbport.Tx, req execute.TerminalWriteRequest) (ret0 idempotency.ResultIdentity, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.effects.commit_terminal_write", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if w == nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("effects: commit terminal writer is nil")
	}
	if w.Committer == nil || w.Plan == nil {
		if w.Next == nil {
			return idempotency.ResultIdentity{}, fmt.Errorf("effects: commit terminal writer requires committer and plan provider")
		}
		return w.Next.Write(ctx, tx, req)
	}
	prepared, err := w.Plan(ctx, tx, req)
	if err != nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("effects: prepare terminal transaction plan: %w", err)
	}
	if prepared.Tenant != "" && string(prepared.Tenant) != req.TenantID.String() {
		return idempotency.ResultIdentity{}, fmt.Errorf("effects: terminal plan tenant %q does not match request tenant %q", prepared.Tenant, req.TenantID)
	}
	receipt, err := w.Committer.CommitInTx(ctx, tx, prepared)
	if err != nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("effects: commit terminal transaction plan: %w", err)
	}
	identity := idempotency.ResultIdentity{
		ResultRef:      transactioncommit.ExplainReceipt(receipt),
		EffectIdentity: prepared.IdempotencyKey,
		EvidenceID:     receipt.ReceiptID.String(),
	}
	if len(receipt.Events) > 0 {
		last := receipt.Events[len(receipt.Events)-1]
		identity.EventRef = fmt.Sprintf("%s@%d", last.StreamKey, last.Sequence)
	} else {
		identity.EventRef = uuid.Nil.String()
	}
	return identity, nil
}
