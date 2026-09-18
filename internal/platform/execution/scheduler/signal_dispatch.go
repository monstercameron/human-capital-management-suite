package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/signals"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// WF-RUN-005's scheduler half. internal/data/signals commits an ACCEPTED
// signal's receipt, disposition, READY continuation and ready-work row in one
// transaction; this dispatcher is what turns that committed row into exactly
// one resume. It claims the ready-work row the receipt enqueued under the
// instance lease and the row's own compare-and-swap version -- the same two
// independent refusals [Scheduler.claim] relies on -- calls a [SignalResumer]
// once, and settles the row. A replica that loses either race resumes
// nothing, and a row once settled DONE is never selected again, so a
// duplicate, a late or a concurrently received signal can never produce a
// second wakeup: at most one row per node attempt exists to be claimed.
//
// The same tick sweeps waits whose close time passed with no accepted
// signal: ExpireDue marks each EXPIRED with its TIMED_OUT continuation and
// ready work in one transaction, and the dispatcher claims each expiry and
// calls a [SignalExpirer] once, so an expired wait reaches its declared
// TIMED_OUT edge exactly once through the same claim/settle machinery.

// SignalWork is one claimed signal continuation handed to a [SignalResumer]:
// the ready-work row, the instance fence it was claimed under, and the
// committed receipt that settled the node attempt. The receipt carries the
// continuation reference, never the payload.
type SignalWork struct {
	Row     runtimestate.ReadyWork
	Fence   lease.Fence
	Receipt signals.MatchedReceipt
}

// SignalResumer resumes the waiting SIGNAL node one claimed continuation names
// -- in production through execute.Driver.ResumeSignal -- and reports how the
// row settles. It is called outside the claiming transaction, with the
// instance fence already on its context (execute.WithFence).
type SignalResumer interface {
	ResumeSignal(ctx context.Context, work SignalWork) (Disposition, error)
}

// SignalResumerFunc adapts a function to [SignalResumer].
type SignalResumerFunc func(ctx context.Context, work SignalWork) (Disposition, error)

// ResumeSignal calls f.
func (f SignalResumerFunc) ResumeSignal(ctx context.Context, work SignalWork) (Disposition, error) {
	return f(ctx, work)
}

// ExpiredSignalWork is one claimed expiry continuation handed to a
// [SignalExpirer]: the ready-work row, the instance fence it was claimed
// under, and the committed EXPIRED wait that settled the node attempt. The
// wait carries the timeout continuation reference, never any signal payload:
// an expiry has no signal.
type ExpiredSignalWork struct {
	Row          runtimestate.ReadyWork
	Fence        lease.Fence
	Subscription signals.ExpiredSubscription
}

// SignalExpirer resumes the waiting SIGNAL node one claimed expiry names
// with its TIMED_OUT outcome -- in production through
// execute.Driver.ResumeSignalTimeout -- and reports how the row settles. It
// is called outside the claiming transaction, with the instance fence already
// on its context (execute.WithFence).
type SignalExpirer interface {
	ResumeExpiredSignal(ctx context.Context, work ExpiredSignalWork) (Disposition, error)
}

// SignalExpirerFunc adapts a function to [SignalExpirer].
type SignalExpirerFunc func(ctx context.Context, work ExpiredSignalWork) (Disposition, error)

// ResumeExpiredSignal calls f.
func (f SignalExpirerFunc) ResumeExpiredSignal(ctx context.Context, work ExpiredSignalWork) (Disposition, error) {
	return f(ctx, work)
}

// SignalDispatcherConfig is what [NewSignalDispatcher] needs.
type SignalDispatcherConfig struct {
	DB      Beginner
	Resumer SignalResumer
	Expirer SignalExpirer
	// Leases and Store are usable as their zero values.
	Leases lease.Manager
	Store  signals.Store
	// BatchSize bounds how many continuations one role tick claims; zero means
	// [DefaultBatchSize]. InstanceTTL bounds a claim's instance lease; zero
	// means [DefaultInstanceTTL].
	BatchSize   int
	InstanceTTL time.Duration
	// Clock stamps settlement; nil means time.Now in UTC.
	Clock  func() time.Time
	Logger Logger
}

// SignalDispatcher is the scheduler-hosted signal role. It implements
// [SignalRole] and [FencedSignalRole]; only the fenced form runs work.
type SignalDispatcher struct {
	cfg SignalDispatcherConfig
}

var (
	_ SignalRole       = (*SignalDispatcher)(nil)
	_ FencedSignalRole = (*SignalDispatcher)(nil)
)

// NewSignalDispatcher validates the wiring.
func NewSignalDispatcher(cfg SignalDispatcherConfig) (*SignalDispatcher, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("%w: signal dispatcher has no database Beginner", ErrConfig)
	}
	if cfg.Resumer == nil {
		return nil, fmt.Errorf("%w: signal dispatcher has no SignalResumer", ErrConfig)
	}
	if cfg.Expirer == nil {
		return nil, fmt.Errorf("%w: signal dispatcher has no SignalExpirer", ErrConfig)
	}
	if cfg.BatchSize < 0 || cfg.InstanceTTL < 0 {
		return nil, fmt.Errorf("%w: signal dispatcher batch size and lease window are not negative", ErrConfig)
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = DefaultBatchSize
	}
	if cfg.InstanceTTL == 0 {
		cfg.InstanceTTL = DefaultInstanceTTL
	}
	if cfg.Clock == nil {
		cfg.Clock = func() time.Time { return time.Now().UTC() }
	}
	if cfg.Logger == nil {
		cfg.Logger = discardLogger{}
	}
	return &SignalDispatcher{cfg: cfg}, nil
}

// ErrSignalRoleUnfenced reports an attempt to run the signal dispatcher
// without the queue fence the scheduler tick acquired.
var ErrSignalRoleUnfenced = errors.New("scheduler: the signal dispatcher runs only under the queue fence")

// RunSignalRole refuses: resuming workflow instances without proof that this
// replica holds the queue would let a stalled replica claim signal work.
func (d *SignalDispatcher) RunSignalRole(ctx context.Context, claim lease.AcquireRequest, _ time.Time, shard string) (ret0 int, retErr error) {
	_, obsOp := observe.Begin(ctx, "workflow.scheduler.signal_role_unfenced", claim)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	return 0, fmt.Errorf("%w (shard %s)", ErrSignalRoleUnfenced, shard)
}

// RunFencedSignalRole claims every eligible matched signal continuation for
// the claim's tenant, expires every due wait, claims each expiry, resumes
// each exactly once, and settles it. The count is the number of continuations
// claimed, matched and expired together.
func (d *SignalDispatcher) RunFencedSignalRole(ctx context.Context, claim lease.AcquireRequest, fence lease.Fence, now time.Time, shard string) (ret0 int, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.scheduler.signal_role", claim)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	claimed, err := d.claim(ctx, claim, fence, now)
	if err != nil {
		return 0, err
	}
	expired, err := d.expireAndClaim(ctx, claim, fence, now)
	if err != nil {
		return 0, err
	}
	var lastErr error
	for _, work := range claimed {
		disposition := d.resume(ctx, work)
		if err := d.settle(ctx, work, disposition); err != nil {
			lastErr = err
		}
	}
	for _, work := range expired {
		disposition := d.resumeExpired(ctx, work)
		if err := d.settleExpired(ctx, work, disposition); err != nil {
			lastErr = err
		}
	}
	d.cfg.Logger.Info("scheduler.signal_role_tick",
		"tenant", claim.TenantID.String(), "shard", shard, "claimed", len(claimed)+len(expired), "fence_token", fence.Token)
	return len(claimed) + len(expired), lastErr
}

// Route wraps the scheduler's generic dispatcher so ready work a matched
// signal enqueued is resumed as a signal even when the generic claim loop
// reached it first (a replica that does not hold the queue still drains the
// frontier). Every other row goes to next.
func (d *SignalDispatcher) Route(next Dispatcher) Dispatcher {
	return DispatcherFunc(func(ctx context.Context, work Work) (Disposition, error) {
		var receipt signals.MatchedReceipt
		var matched bool
		var expired signals.ExpiredSubscription
		var found bool
		err := d.inTenantTx(ctx, lease.AcquireRequest{TenantID: work.Row.TenantID}, func(tx dbport.Tx) error {
			var loadErr error
			receipt, matched, loadErr = d.cfg.Store.MatchedForNode(ctx, tx, work.Row.TenantID, work.Row.InstanceID, work.Row.NodeID, work.Row.Attempt)
			if loadErr != nil {
				return loadErr
			}
			if matched {
				return nil
			}
			// No matched receipt: the row may still be an expired wait's
			// TIMED_OUT continuation, which carries no receipt. An EXPIRED
			// wait resumes as a timeout; anything else is not signal work
			// and falls through to the next dispatcher.
			expired, found, loadErr = d.cfg.Store.ExpiredForNode(ctx, tx, work.Row.TenantID, work.Row.InstanceID, work.Row.NodeID, work.Row.Attempt)
			return loadErr
		})
		if err != nil {
			return DispositionRetry, err
		}
		if matched && receipt.Settled() {
			return d.cfg.Resumer.ResumeSignal(ctx, SignalWork{Row: work.Row, Fence: work.Fence, Receipt: receipt})
		}
		if found {
			return d.cfg.Expirer.ResumeExpiredSignal(ctx, ExpiredSignalWork{Row: work.Row, Fence: work.Fence, Subscription: expired})
		}
		if next == nil {
			return DispositionRetry, fmt.Errorf("scheduler: ready work %s/%s has no matched signal and no fallback dispatcher", work.Row.InstanceID, work.Row.NodeID)
		}
		return next.Dispatch(ctx, work)
	})
}

// claim selects the pending matched continuations and takes each under the
// instance lease and the row's compare-and-swap version, after verifying the
// queue fence in the same transaction.
func (d *SignalDispatcher) claim(ctx context.Context, claim lease.AcquireRequest, fence lease.Fence, now time.Time) ([]SignalWork, error) {
	var out []SignalWork
	err := d.inTenantTx(ctx, claim, func(tx dbport.Tx) error {
		if _, err := d.cfg.Leases.Verify(ctx, tx, fence, now); err != nil {
			return err
		}
		pending, err := d.cfg.Store.PendingMatched(ctx, tx, claim.TenantID, now, d.cfg.BatchSize)
		if err != nil {
			return err
		}
		for _, match := range pending {
			if !match.Receipt.Settled() {
				continue
			}
			grant, err := d.cfg.Leases.Acquire(ctx, tx, lease.AcquireRequest{
				TenantID: claim.TenantID, Resource: instanceResource(match.Ready),
				Holder: claim.Holder, Now: now, TTL: d.cfg.InstanceTTL,
			})
			if errors.Is(err, lease.ErrHeld) {
				continue
			}
			if err != nil {
				return err
			}
			row, err := (runtimestate.ReadyWorkStore{}).Claim(ctx, tx, match.Ready.TenantID, match.Ready.ReadyWorkID, match.Ready.Version)
			if err != nil {
				return err
			}
			out = append(out, SignalWork{Row: row, Fence: grant.Fence, Receipt: match.Receipt})
			d.cfg.Logger.Info("scheduler.signal_continuation_claimed",
				"instance", row.InstanceID.String(), "node", row.NodeID, "attempt", row.Attempt,
				"signal", match.Receipt.SignalID.String(), "fence_token", grant.Fence.Token)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// expireAndClaim sweeps every due wait to EXPIRED with its TIMED_OUT
// continuation and ready work, then claims each expiry under the instance
// lease and the row's compare-and-swap version, after verifying the queue
// fence in the same transaction. The sweep and the claim share one tick so an
// expired wait never waits a second tick for its dispatch.
func (d *SignalDispatcher) expireAndClaim(ctx context.Context, claim lease.AcquireRequest, fence lease.Fence, now time.Time) ([]ExpiredSignalWork, error) {
	var out []ExpiredSignalWork
	err := d.inTenantTx(ctx, claim, func(tx dbport.Tx) error {
		if _, err := d.cfg.Leases.Verify(ctx, tx, fence, now); err != nil {
			return err
		}
		if _, err := d.cfg.Store.ExpireDue(ctx, tx, claim.TenantID, now, d.cfg.BatchSize); err != nil {
			return err
		}
		pending, err := d.cfg.Store.PendingExpired(ctx, tx, claim.TenantID, now, d.cfg.BatchSize)
		if err != nil {
			return err
		}
		for _, expiry := range pending {
			grant, err := d.cfg.Leases.Acquire(ctx, tx, lease.AcquireRequest{
				TenantID: claim.TenantID, Resource: instanceResource(expiry.Ready),
				Holder: claim.Holder, Now: now, TTL: d.cfg.InstanceTTL,
			})
			if errors.Is(err, lease.ErrHeld) {
				continue
			}
			if err != nil {
				return err
			}
			row, err := (runtimestate.ReadyWorkStore{}).Claim(ctx, tx, expiry.Ready.TenantID, expiry.Ready.ReadyWorkID, expiry.Ready.Version)
			if err != nil {
				return err
			}
			out = append(out, ExpiredSignalWork{Row: row, Fence: grant.Fence, Subscription: expiry.Subscription})
			d.cfg.Logger.Info("scheduler.signal_expiry_claimed",
				"instance", row.InstanceID.String(), "node", row.NodeID, "attempt", row.Attempt,
				"subscription", expiry.Subscription.SubscriptionID.String(), "fence_token", grant.Fence.Token)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// resumeExpired calls the expirer once. A returned error or an undeclared
// disposition is a retry: the row returns to READY rather than being lost.
func (d *SignalDispatcher) resumeExpired(ctx context.Context, work ExpiredSignalWork) Disposition {
	ctx = execute.WithFence(ctx, work.Fence.RuntimeFence(time.Time{}))
	disposition, err := d.cfg.Expirer.ResumeExpiredSignal(ctx, work)
	if err != nil {
		d.cfg.Logger.Error("scheduler.signal_expiry_resume_failed",
			"instance", work.Row.InstanceID.String(), "node", work.Row.NodeID, "error", err.Error())
		return DispositionRetry
	}
	if !disposition.Valid() {
		d.cfg.Logger.Error("scheduler.signal_expiry_resume_undeclared_disposition",
			"instance", work.Row.InstanceID.String(), "node", work.Row.NodeID, "disposition", string(disposition))
		return DispositionRetry
	}
	return disposition
}

// settleExpired moves the claimed expiry row and releases the instance lease
// in one transaction, exactly as [SignalDispatcher.settle] does.
func (d *SignalDispatcher) settleExpired(ctx context.Context, work ExpiredSignalWork, disposition Disposition) error {
	state, terminal, err := disposition.settleState()
	if err != nil {
		return err
	}
	at := d.cfg.Clock().UTC()
	completedAt := time.Time{}
	if terminal {
		completedAt = at
	}
	return d.inTenantTx(ctx, lease.AcquireRequest{TenantID: work.Row.TenantID}, func(tx dbport.Tx) error {
		if err := (runtimestate.ReadyWorkStore{}).Transition(ctx, tx,
			work.Row.TenantID, work.Row.ReadyWorkID, work.Row.Version, state, completedAt); err != nil {
			return err
		}
		if _, err := d.cfg.Leases.Release(ctx, tx, work.Fence, at); err != nil {
			return err
		}
		d.cfg.Logger.Info("scheduler.signal_expiry_settled",
			"instance", work.Row.InstanceID.String(), "node", work.Row.NodeID,
			"subscription", work.Subscription.SubscriptionID.String(), "disposition", string(disposition), "state", state)
		return nil
	})
}

// resume calls the resumer once. A returned error or an undeclared
// disposition is a retry: the row returns to READY rather than being lost.
func (d *SignalDispatcher) resume(ctx context.Context, work SignalWork) Disposition {
	ctx = execute.WithFence(ctx, work.Fence.RuntimeFence(time.Time{}))
	disposition, err := d.cfg.Resumer.ResumeSignal(ctx, work)
	if err != nil {
		d.cfg.Logger.Error("scheduler.signal_resume_failed",
			"instance", work.Row.InstanceID.String(), "node", work.Row.NodeID, "error", err.Error())
		return DispositionRetry
	}
	if !disposition.Valid() {
		d.cfg.Logger.Error("scheduler.signal_resume_undeclared_disposition",
			"instance", work.Row.InstanceID.String(), "node", work.Row.NodeID, "disposition", string(disposition))
		return DispositionRetry
	}
	return disposition
}

// settle moves the claimed row and releases the instance lease in one
// transaction, exactly as [Scheduler.settle] does.
func (d *SignalDispatcher) settle(ctx context.Context, work SignalWork, disposition Disposition) error {
	state, terminal, err := disposition.settleState()
	if err != nil {
		return err
	}
	at := d.cfg.Clock().UTC()
	completedAt := time.Time{}
	if terminal {
		completedAt = at
	}
	return d.inTenantTx(ctx, lease.AcquireRequest{TenantID: work.Row.TenantID}, func(tx dbport.Tx) error {
		if err := (runtimestate.ReadyWorkStore{}).Transition(ctx, tx,
			work.Row.TenantID, work.Row.ReadyWorkID, work.Row.Version, state, completedAt); err != nil {
			return err
		}
		if _, err := d.cfg.Leases.Release(ctx, tx, work.Fence, at); err != nil {
			return err
		}
		d.cfg.Logger.Info("scheduler.signal_continuation_settled",
			"instance", work.Row.InstanceID.String(), "node", work.Row.NodeID,
			"signal", work.Receipt.SignalID.String(), "disposition", string(disposition), "state", state)
		return nil
	})
}

func (d *SignalDispatcher) inTenantTx(ctx context.Context, scope lease.AcquireRequest, fn func(tx dbport.Tx) error) error {
	tx, err := d.cfg.DB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("scheduler: begin signal dispatch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, scope.TenantID); err != nil {
		return fmt.Errorf("scheduler: scope signal dispatch tenant: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("scheduler: commit signal dispatch: %w", err)
	}
	return nil
}
