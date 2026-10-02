package runstate

import (
	"context"
	"fmt"
	"time"
)

// InterruptedCode is the terminal code of a run that was finished because the
// process holding it died, or because it can no longer finish. It is the one
// code a person is told to ask again about.
const InterruptedCode = "ANSWER_INTERRUPTED"

// LeaseRenewer is implemented by stores that can extend a worker's lease
// without a new revision. A heartbeat must not bump the revision: the worker's
// own checkpoints compare against it, and a renewal must never make them fail.
type LeaseRenewer interface {
	// RenewLease moves the lease expiry of a RUNNING run to until, only while
	// owner still holds the current fence and the lease has not already expired.
	// It answers ErrLease when it no longer does.
	RenewLease(ctx context.Context, id, owner string, fence uint64, until, now time.Time) error
}

// InterruptOrphan finishes a run that no worker holds: READY (nobody claimed
// it) or RUNNING with an expired lease (its worker is gone). The run becomes
// FAILED with InterruptedCode and Retryable set, and its fence moves on so a
// late write from the old holder is refused. It is one conditional update on the
// expected revision, so two processes racing to finish the same run produce one
// terminal row and one ErrConflict.
//
// A run with an unresolved effect is refused: the owner of that effect must
// reconcile it first, and recovery never hides it.
func (s *Service) InterruptOrphan(ctx context.Context, id string, expected uint64, now time.Time) (Run, error) {
	run, err := s.get(ctx, id)
	if err != nil {
		return Run{}, err
	}
	if run.Version != expected {
		return Run{}, ErrConflict
	}
	if now.IsZero() {
		return Run{}, fmt.Errorf("%w: time required", ErrInvalid)
	}
	switch {
	case terminal(run.State):
		return Run{}, ErrTerminal
	case run.State == StateReady:
	case run.State == StateRunning:
		if run.Lease != nil && run.Lease.Until.After(now) {
			return Run{}, ErrLease
		}
	default:
		return Run{}, fmt.Errorf("%w: run is not orphaned", ErrInvalid)
	}
	if hasUnknownEffect(run.Effects) {
		return Run{}, fmt.Errorf("%w: unresolved effect needs its owner", ErrInvalid)
	}
	prior := run.Version
	run.Fence++
	run.FailureRequested, run.TerminalCode, run.Retryable = true, InterruptedCode, true
	run.FailureGate, run.FailureOwner, run.FailureLocation = string(FailureGateModelCall), "internal/application", "agentrun_recovery:interrupt"
	run.Lease, run.Version, run.UpdatedAt = nil, run.Version+1, now.UTC()
	run.finishRequestedTerminal()
	if err := s.store.Save(ctx, run, prior); err != nil {
		return Run{}, err
	}
	return run, nil
}
