package wait

import "errors"

// Sentinel errors. All are matchable with errors.Is.
var (
	// ErrWorkflowIdentityRequired says a CompiledWaitNode did not name the
	// workflow/node it belongs to.
	ErrWorkflowIdentityRequired = errors.New("wait: workflow id, version and node id are required")
	// ErrNoWakeCondition says a CompiledWaitNode declared neither a fixed
	// instant nor a local-date wake condition.
	ErrNoWakeCondition = errors.New("wait: no wake condition declared")
	// ErrConflictingWakeCondition says a CompiledWaitNode declared both a
	// fixed instant and a local-date wake condition; exactly one is legal.
	ErrConflictingWakeCondition = errors.New("wait: instant and local-date wake conditions are mutually exclusive")
	// ErrInvalidRequirement says Resolve was called with a TimerRequirement
	// that was never produced by ComputeTimerRequirement (no digest).
	ErrInvalidRequirement = errors.New("wait: requirement has no digest; it was not produced by ComputeTimerRequirement")
	// ErrDigestMismatch says a prior resolution was supplied for a different
	// requirement than the one being resolved.
	ErrDigestMismatch = errors.New("wait: prior resolution belongs to a different requirement")
	// ErrNowRequired says Resolve was called for a WAKE event without a valid
	// caller-supplied "now".
	ErrNowRequired = errors.New("wait: a WAKE event requires a valid caller-supplied instant")
	// ErrEarlyWake says the caller-supplied "now" is before the timer's
	// fire-at instant. A runtime that woke early is refused, not honored.
	ErrEarlyWake = errors.New("wait: early wake refused: now is before fire_at")
	// ErrUnknownEventKind says the WakeEvent named a kind Resolve does not
	// recognize.
	ErrUnknownEventKind = errors.New("wait: unknown wake event kind")
	// ErrAdvanceInstantsRequired says a local-development advance did not name
	// both sides of its explicit clock movement.
	ErrAdvanceInstantsRequired = errors.New("wait: local-dev advance requires caller-supplied now and target instants")
	// ErrAdvanceBackwards says a development clock request attempted to move
	// backwards, which could make a wait appear to complete inconsistently.
	ErrAdvanceBackwards = errors.New("wait: local-dev advance cannot move backwards")
)
