package application

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

// personaRunLeaseRenewals is how many times per lease term the holder renews.
const personaRunLeaseRenewals = 3

// startLeaseRenewal keeps the claimed run's lease alive while its worker works.
// A model call may outlast one lease term; without renewal another process
// would find the lease expired and take over a worker that is still running.
// The returned function stops the renewal and waits for it to finish.
//
// The lease is never extended past the run's deadline, so a wedged worker holds
// a run for at most the deadline and recovery then finishes it. Renewal stops by
// itself when the lease is no longer this worker's: the run was taken over, or it
// reached a final state.
func (e *personaAdmittedRunExecutor) startLeaseRenewal(ctx context.Context, run runstate.Run) func() {
	renewer, ok := e.store.(runstate.LeaseRenewer)
	if !ok || e.leaseTTL <= 0 || e.now == nil || run.ID == "" {
		return func() {}
	}
	interval := e.leaseTTL / personaRunLeaseRenewals
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	renewCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	var done sync.WaitGroup
	done.Add(1)
	go func() {
		defer done.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-renewCtx.Done():
				return
			case <-ticker.C:
				if !e.renewLeaseOnce(renewCtx, renewer, run.ID, run.Fence) {
					return
				}
			}
		}
	}()
	return func() {
		cancel()
		done.Wait()
	}
}

// renewLeaseOnce extends the lease once and reports whether renewal should go on.
func (e *personaAdmittedRunExecutor) renewLeaseOnce(ctx context.Context, renewer runstate.LeaseRenewer, id string, fence uint64) bool {
	current, err := e.store.Get(ctx, id)
	if err != nil {
		return ctx.Err() == nil
	}
	// Worker identities are shared by processes, so the fence is what says this
	// claim is still the current one.
	if current.State != runstate.StateRunning || current.Fence != fence || current.Lease == nil || current.Lease.Owner != e.workerID {
		return false
	}
	now := e.now().UTC()
	until := now.Add(e.leaseTTL)
	if until.After(current.Deadline) {
		until = current.Deadline
	}
	if !until.After(current.Lease.Until) {
		return now.Before(current.Deadline)
	}
	if err := renewer.RenewLease(ctx, id, e.workerID, fence, until, now); err != nil {
		return !errors.Is(err, runstate.ErrLease) && ctx.Err() == nil
	}
	return true
}
