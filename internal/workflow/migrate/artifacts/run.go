package artifacts

import (
	"context"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// Request is one caller's request to migrate every pending artifact of one
// instance onto the epoch a WF-RUN-018 migration moved it to.
type Request struct {
	Scope Scope
	Ports Ports

	// Barrier, when set, is consulted before each handler runs. It is the
	// declared seam a fault-injection test (and an operator running a
	// migration in stages) uses to stop a run between two artifact kinds and
	// prove nothing was left half-migrated.
	Barrier Barrier
}

// Migrate transforms and deduplicates every pending artifact of one instance
// under the new epoch, inside the caller's transaction.
//
// It is designed to be called in the same transaction as [migrate.Migrate]:
// that call moves the instance's pinned version, digest and frontier, and
// this one moves everything the instance was waiting on. Running them apart
// would leave a window in which the instance is on the new plan and its
// promises are still on the old one.
//
// The handlers run in [Handlers] order and every one of them writes through
// req.Ports only. The first refusal stops the run and returns a typed error,
// having committed nothing of its own: the caller's transaction still holds
// whatever earlier handlers wrote, and rolling it back leaves the instance
// exactly as it was -- PAUSED on its old version, with every promise it
// already had, and therefore runnable. A caller that cannot roll back records
// the failure durably with [MarkRepairRequired] instead.
//
// On success the returned [Receipt] names every artifact and what happened to
// it, canonically ordered and digested. Running the same migration a second
// time produces the same receipt with the re-keys reported as
// [Deduplicated]: every write this package makes addresses a derived identity
// and is a no-op when that identity already exists, so a replay creates no
// duplicate timer, subscription, ready-work row or continuation.
func Migrate(ctx context.Context, tx Executor, req Request) (ret0 Receipt, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.migrate.artifacts", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := req.Scope.validate(); err != nil {
		return Receipt{}, err
	}
	if err := req.Ports.validate(); err != nil {
		return Receipt{}, err
	}

	var entries []Entry
	for _, handler := range Handlers() {
		kind := handler.Kind()
		if req.Barrier != nil {
			if err := req.Barrier.Enter(kind); err != nil {
				return Receipt{}, wrap(CodeBarrierFailed, kind, req.Scope.InstanceID.String(), err,
					"the run's barrier refused to enter the %s handler", kind)
			}
		}
		produced, err := handler.Migrate(ctx, tx, req.Scope, req.Ports)
		if err != nil {
			return Receipt{}, err
		}
		entries = append(entries, produced...)
	}
	sortEntries(entries)

	receipt := Receipt{
		ContractVersion: ContractVersion,
		TenantID:        req.Scope.TenantID, InstanceID: req.Scope.InstanceID,
		From: req.Scope.From, To: req.Scope.To,
		Entries:    entries,
		MigratedBy: req.Scope.MigratedBy, MigratedAt: req.Scope.MigratedAt.UTC(),
	}
	receipt.digest = computeReceiptDigest(receipt)
	return receipt, nil
}
