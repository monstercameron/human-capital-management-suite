package timesession

import (
	"fmt"
	"time"
)

// ExitPolicy is the pinned site policy an exit decision is evaluated
// against. It is ordinary data supplied by the caller (typically the
// FTIME-010 site policy version in effect when the session opened), never a
// package constant.
type ExitPolicy struct {
	AccuracyThresholdMeters float64
	SustainedMinimum        time.Duration
	GraceDuration           time.Duration
}

func (p ExitPolicy) Validate() error {
	if p.AccuracyThresholdMeters <= 0 || p.SustainedMinimum < 0 || p.GraceDuration <= 0 {
		return fmt.Errorf("%w: exit policy needs a positive accuracy threshold and grace duration", ErrInvalidPolicy)
	}
	return nil
}

// ExitEvidence is one location signal. Exited distinguishes a signal that
// suggests the worker left the assigned site from one that suggests they
// returned; resolving that from raw location readings is the caller's job,
// not this package's.
type ExitEvidence struct {
	Exited            bool
	Consent           bool
	AccuracyMeters    float64
	SustainedDuration time.Duration
	Source            string
}

// ExitDecisionKind is EvaluateExit's outcome.
type ExitDecisionKind string

const (
	ExitNone           ExitDecisionKind = "NONE"
	ExitGraceStarted   ExitDecisionKind = "GRACE_STARTED"
	ExitGraceCancelled ExitDecisionKind = "GRACE_CANCELLED"
	ExitUncertain      ExitDecisionKind = "UNCERTAIN_LOCATION"
)

// ExitDecision is EvaluateExit's pure result.
type ExitDecision struct {
	Kind      ExitDecisionKind
	ExpiresAt time.Time
	Exception *Exception
}

// EvaluateExit is FTIME-011's per-signal evaluator. Sustained, sufficiently
// accurate exit evidence starts (or refreshes) a server-owned grace timer;
// a re-entry signal cancels a pending grace; missing consent or
// insufficient accuracy never counts as a confirmed exit and instead leaves
// the session open with a visible UNCERTAIN_LOCATION exception.
func EvaluateExit(policy ExitPolicy, evidence ExitEvidence, session Session, now time.Time) (Session, ExitDecision, error) {
	if session.State != StateOpen && session.State != StateOnBreak {
		return session, ExitDecision{}, fmt.Errorf("%w: session %s is %s", ErrNotOpen, session.SessionID, orNone(session.State))
	}
	if err := policy.Validate(); err != nil {
		return session, ExitDecision{}, err
	}
	if now.IsZero() {
		return session, ExitDecision{}, fmt.Errorf("%w: now is required", ErrInvalidPunch)
	}
	next := cloneSession(session)
	if !evidence.Exited {
		if next.PendingAutoOut == nil {
			return next, ExitDecision{Kind: ExitNone}, nil
		}
		next.PendingAutoOut = nil
		return next, ExitDecision{Kind: ExitGraceCancelled}, nil
	}
	if !evidence.Consent || evidence.AccuracyMeters <= 0 || evidence.AccuracyMeters > policy.AccuracyThresholdMeters {
		exc := Exception{Kind: ExceptionUncertainLocation, Reason: "exit evidence lacks consent or sufficient accuracy", At: now}
		next.OpenExceptions = append(next.OpenExceptions, exc)
		return next, ExitDecision{Kind: ExitUncertain, Exception: &next.OpenExceptions[len(next.OpenExceptions)-1]}, nil
	}
	if evidence.SustainedDuration < policy.SustainedMinimum {
		return next, ExitDecision{Kind: ExitNone}, nil
	}
	expiresAt := now.Add(policy.GraceDuration)
	next.PendingAutoOut = &AutoOutGrace{StartedAt: now, ExpiresAt: expiresAt}
	return next, ExitDecision{Kind: ExitGraceStarted, ExpiresAt: expiresAt}, nil
}

// ExpireGrace is FTIME-011's grace-timer expiry. It atomically closes only
// the matching open session with an idempotent AUTO_OUT outcome and
// provisional effective time pinned to the grace's own expiry instant under
// the policy that started it; it never guesses "now" as the close time. A
// second expiry call carrying the same idempotency key against the
// already-closed result is a no-op replay of the first outcome, never a
// second close.
func ExpireGrace(session Session, now time.Time, idempotencyKey string) (Session, Outcome, error) {
	if session.State == StateAutoClosed && idempotencyKey != "" && session.LastIdempotencyKey == idempotencyKey && session.LastOutcome != nil {
		return session, *session.LastOutcome, nil
	}
	if session.State != StateOpen && session.State != StateOnBreak {
		return session, Outcome{}, fmt.Errorf("%w: session %s is %s", ErrNotOpen, session.SessionID, orNone(session.State))
	}
	if session.PendingAutoOut == nil {
		return session, Outcome{}, ErrNoGraceStarted
	}
	if now.Before(session.PendingAutoOut.ExpiresAt) {
		return session, Outcome{}, ErrGraceNotExpired
	}
	next := cloneSession(session)
	closeAt := session.PendingAutoOut.ExpiresAt
	closeOpenSegments(&next, closeAt)
	next.State = StateAutoClosed
	next.PendingAutoOut = nil
	next.Revision++
	next.OpenExceptions = append(next.OpenExceptions, Exception{
		Kind: ExceptionAutoOutProvisional, Reason: "auto clock-out on confirmed jobsite exit; provisional pending confirmation", At: closeAt,
	})
	exc := &next.OpenExceptions[len(next.OpenExceptions)-1]
	return finish(next, OutcomeAutoClosed, Punch{IdempotencyKey: idempotencyKey}, exc)
}
