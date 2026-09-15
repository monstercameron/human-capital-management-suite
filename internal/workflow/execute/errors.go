package execute

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalidConfiguration reports a driver missing a required port or a
	// request missing execution identity.
	ErrInvalidConfiguration = errors.New("workflow execute: invalid configuration")
	// ErrUnsupportedContinuation reports a timer or signal continuation. The
	// prototype has no durable store for either and must never fake one.
	ErrUnsupportedContinuation = errors.New("workflow execute: unsupported continuation")
	// ErrNoProgress reports a non-terminal run with neither READY work nor a
	// durable WorkItem on which it can honestly park.
	ErrNoProgress = errors.New("workflow execute: no progress")
	// ErrWorkItemDrift reports a resumed WorkItem whose reloaded, durable
	// status, item version, completion evidence or instance/node binding does
	// not match what [Driver.Resume] requires (WF-RUN-028). Every check runs
	// against the row [WorkItemReader] loads inside the advancement
	// transaction, never against a struct the caller assembled.
	ErrWorkItemDrift = errors.New("workflow execute: work item drift")
	// ErrTimerDrift reports a resumed WAIT node whose reloaded, durable timer
	// row is not settled, is bound to another instance, or names a node that
	// is not a WAIT in the pinned plan (WF-RUN-004). It is [ErrWorkItemDrift]'s
	// counterpart for durable timers, and it exists for the same reason: an
	// advancement rests on committed evidence, never on a struct the caller
	// assembled.
	ErrTimerDrift = errors.New("workflow execute: timer drift")
	// ErrFenceRefused reports an advancement whose lease fence the configured
	// verifier rejected (WF-RUN-002). The verifier's own typed refusal is
	// wrapped, so LEASE_LOST and FENCE_STALE remain readable off it.
	ErrFenceRefused = errors.New("workflow execute: lease fence refused")
	// ErrModeNotAllowed reports a READY node whose compiled effect class does
	// not admit the run's execution mode (WF-RUN-040). The driver refuses
	// before the step handler runs, so no effect is attempted.
	ErrModeNotAllowed = errors.New("workflow execute: node does not admit the execution mode")
	// ErrCurrencyBlocked reports that [CurrencyGuard] found a material change
	// to the pinned proposal, its approval or its control snapshots while an
	// instance was parked, and moved it to BLOCKED instead of advancing
	// (WF-RUN-029).
	ErrCurrencyBlocked = errors.New("workflow execute: currency guard blocked the instance")
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidConfiguration, fmt.Sprintf(format, args...))
}

func unsupported(kind, nodeID string) error {
	return fmt.Errorf("%w: %s for node %s has no durable prototype store", ErrUnsupportedContinuation, kind, nodeID)
}

func drift(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrWorkItemDrift, fmt.Sprintf(format, args...))
}

func timerDrift(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrTimerDrift, fmt.Sprintf(format, args...))
}
