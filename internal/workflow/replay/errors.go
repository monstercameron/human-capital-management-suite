package replay

import (
	"errors"
	"fmt"
)

// Sentinels. Classify with [errors.Is]; read [Error.Code] for the exact
// reason. Never match message text.
var (
	// ErrReplay is the sentinel every refusal from this package unwraps to.
	ErrReplay = errors.New("replay: run refused")
)

// Stable refusal codes.
const (
	// CodeArtifactUnavailable is the contract code WF-RUN-013's GREEN clause
	// names: "missing artifact returns REPLAY_ARTIFACT_UNAVAILABLE". It is
	// raised when the frontier reaches a node the record holds no output,
	// signal or timer settlement for. The call still returns a [Result]
	// carrying a [Divergence] that names that node, because the FAULT clause
	// asks for a divergence rather than a crash.
	CodeArtifactUnavailable = "REPLAY_ARTIFACT_UNAVAILABLE"

	// CodeEffectForbidden reports an attempt to commit domain truth, reach an
	// external destination or consume a live approval from inside a replay.
	// It always names the node that tried.
	CodeEffectForbidden = "REPLAY_EFFECT_FORBIDDEN"

	// CodeModeRefused reports a run admitted under a contract that is not
	// REPLAY, or a REPLAY contract whose own guarantees have been weakened --
	// a contract that would permit a domain commit, an external effect or a
	// live approval, that reads a clock other than the historical one, or
	// that binds anything but the null adapter profile.
	CodeModeRefused = "REPLAY_MODE_REFUSED"

	// CodeCausalSeparation reports an instance that does not name the
	// historical intent it re-derives, or names itself as that intent.
	CodeCausalSeparation = "REPLAY_CAUSAL_SEPARATION"

	// CodeDivergence reports that the replay and the record disagree: a node
	// the record does not place on the frontier, an outcome the plan cannot
	// route, a resulting frontier the record contradicts, or a trace digest
	// that does not reproduce [Record.TraceDigest].
	CodeDivergence = "REPLAY_DIVERGENCE"

	// CodeRecordInvalid reports a durable record this package refuses to
	// replay because it is incomplete or self-contradictory.
	CodeRecordInvalid = "REPLAY_RECORD_INVALID"

	// CodePlanMismatch reports a compiled plan whose digest is not the one the
	// record pins. An instance replays against the plan it ran on, never a
	// migration in disguise.
	CodePlanMismatch = "REPLAY_PLAN_MISMATCH"

	// CodeInvalidOptions reports unusable [Options].
	CodeInvalidOptions = "REPLAY_INVALID_OPTIONS"

	// CodeSourceFailed reports a [Source] that could not hand back a record.
	CodeSourceFailed = "REPLAY_SOURCE_FAILED"

	// CodeStepBudgetExceeded reports a walk that exceeded its bounded step
	// budget, which is how a declared cycle stays bounded without a timer.
	CodeStepBudgetExceeded = "REPLAY_STEP_BUDGET_EXCEEDED"
)

// Error is one typed refusal, naming the code, the node it happened at (when a
// node is implicated) and the underlying cause.
type Error struct {
	Code   string
	NodeID string
	Detail string
	Err    error
}

func (e *Error) Error() string {
	loc := ""
	if e.NodeID != "" {
		loc = " at node " + e.NodeID
	}
	msg := fmt.Sprintf("replay: %s%s: %s", e.Code, loc, e.Detail)
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap exposes the wrapped cause, and always the package sentinel.
func (e *Error) Unwrap() []error {
	if e.Err != nil {
		return []error{ErrReplay, e.Err}
	}
	return []error{ErrReplay}
}

// refuse builds a typed refusal.
func refuse(code, nodeID, format string, args ...any) *Error {
	return &Error{Code: code, NodeID: nodeID, Detail: fmt.Sprintf(format, args...)}
}

// wrap builds a typed refusal around an underlying cause.
func wrap(code, nodeID string, err error, format string, args ...any) *Error {
	return &Error{Code: code, NodeID: nodeID, Detail: fmt.Sprintf(format, args...), Err: err}
}

// CodeOf returns the refusal code carried by err, or "" when err is not a
// refusal from this package.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// NodeOf returns the node a refusal names, or "" when err is not a refusal
// from this package or names no node.
//
// It is exported because "which node tried to reach out" is the load-bearing
// half of [CodeEffectForbidden]: a security test that could only read the
// message text would be asserting on prose.
func NodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.NodeID
	}
	return ""
}

// ErrorCode reports the refusal's stable code for telemetry classification
// (internal/workflow/observe.ErrorCode); it never carries message text.
func (e *Error) ErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}
