package operator

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/correction"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// ExecuteLedgerCorrection resolves one governed ledger-correction intent to
// correction.Append (REV-007-03). It re-authorizes the presented request
// through AuthorizeOperatorAction, so a receipt can never be minted around
// the JIT, simulation and idempotency gates, then binds the correction to
// the receipt: the operation must be the ledger-correction kind, the stream
// must equal the authorized target, and the idempotency key must match, so
// the receipt covers exactly this correction and retries replay it instead
// of duplicating it. The tenant scope rides the caller's transaction; the
// caller owns the final commit or rollback.
func ExecuteLedgerCorrection(ctx context.Context, tx dbport.Tx, req intent.OperatorActionRequest, at values.Instant, creq correction.Request, now func() time.Time) (result correction.Result, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "operator.execute_ledger_correction", req.Tenant, req.IntentInstanceID)
	defer func() { observe.DoneWith(obsOp, retErr, result) }()
	receipt, err := intent.AuthorizeOperatorAction(req, at)
	if err != nil {
		return correction.Result{}, err
	}
	if receipt.Operation != intent.OperationLedgerCorrection {
		return correction.Result{}, fmt.Errorf("%w: receipt authorizes %s, not ledger correction", intent.ErrOperatorAction, receipt.Operation)
	}
	if creq.StreamKey != req.Target {
		return correction.Result{}, fmt.Errorf("%w: correction stream %q escapes authorized target %q", intent.ErrOperatorAction, creq.StreamKey, req.Target)
	}
	if creq.IdempotencyKey != req.IdempotencyKey {
		return correction.Result{}, fmt.Errorf("%w: correction idempotency key escapes the authorized receipt", intent.ErrOperatorAction)
	}
	result, retErr = correction.Append(ctx, tx, creq, now)
	return result, retErr
}
