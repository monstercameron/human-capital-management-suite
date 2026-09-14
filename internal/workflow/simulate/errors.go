package simulate

import (
	"errors"
	"fmt"
)

// Sentinels. Classify with [errors.Is]; read [Error.Code] for the exact
// reason. Never match message text.
var (
	// ErrSimulate is the sentinel every refusal from this package unwraps to.
	ErrSimulate = errors.New("simulate: run refused")
	// ErrValueType reports a value that does not agree with the type its
	// producer or consumer declared.
	ErrValueType = errors.New("simulate: value type mismatch")
	// ErrMissingInput reports a declared input a node never received.
	ErrMissingInput = errors.New("simulate: input is missing")
)

// Stable refusal codes.
const (
	// CodeSimulationSideEffectForbidden is the contract code WF-RUN-012's
	// GREEN clause names for an attempted mutation: "reads/pure rules/plans/
	// obligations/cost execute and mutation attempt returns
	// SIMULATION_SIDE_EFFECT_FORBIDDEN". It is the load-bearing refusal of
	// this package: SIMULATE suppresses nothing, it refuses.
	CodeSimulationSideEffectForbidden = "SIMULATION_SIDE_EFFECT_FORBIDDEN"
	// CodeWriteEffectInSimulate is the same code under the name this package
	// used before WF-RUN-012 fixed the spelling against the contract. It is
	// kept as an alias so existing callers keep compiling; there is one code,
	// not two.
	CodeWriteEffectInSimulate = CodeSimulationSideEffectForbidden
	// CodeModeNotAdmitted reports a node whose compiled allowed-mode set does
	// not include SIMULATE.
	CodeModeNotAdmitted = "MODE_NOT_ADMITTED"
	// CodePlanNotZeroEffect reports a plan whose own effect summary says it
	// can mutate something.
	CodePlanNotZeroEffect = "PLAN_NOT_ZERO_EFFECT"
	// CodePlanUnverified reports a plan whose content no longer matches the
	// digest it was compiled with.
	CodePlanUnverified = "PLAN_UNVERIFIED"
	// CodeStepNotImplemented reports a step type this P1A interpreter does not
	// execute. WAIT, SIGNAL, COMPENSATE and the structural primitives are
	// durable-runtime work gated behind WF-RUN-000.
	CodeStepNotImplemented = "STEP_NOT_IMPLEMENTED"
	// CodeUnresolvedSource reports an input mapping whose source produced no
	// value at the moment the node ran.
	CodeUnresolvedSource = "UNRESOLVED_SOURCE"
	// CodeOutputTypeMismatch reports a node that produced an output its own
	// declaration does not admit.
	CodeOutputTypeMismatch = "OUTPUT_TYPE_MISMATCH"
	// CodeMissingRoute reports an outcome with no outgoing edge. The compiler
	// proves this cannot happen; the interpreter refuses rather than picking
	// an edge, because there is no implicit first edge at runtime either.
	CodeMissingRoute = "MISSING_ROUTE"
	// CodeStepBudgetExceeded reports a walk that exceeded its bounded step
	// budget, which is how a declared cycle stays bounded without a timer.
	CodeStepBudgetExceeded = "STEP_BUDGET_EXCEEDED"
	// CodeHandlerFailed reports a capability, decision, transform or
	// observation port that failed.
	CodeHandlerFailed = "HANDLER_FAILED"
	// CodeIllegalTerminal reports a terminal whose five-dimension tuple the
	// intent lifecycle rules reject.
	CodeIllegalTerminal = "ILLEGAL_TERMINAL"
	// CodeNoTerminal reports a walk that stopped without reaching an END.
	CodeNoTerminal = "NO_TERMINAL"
	// CodeReceiptInvalid reports a receipt that could not be minted, which in
	// practice means an effect was counted.
	CodeReceiptInvalid = "RECEIPT_INVALID"
	// CodeInvalidOptions reports an unusable Run configuration.
	CodeInvalidOptions = "INVALID_OPTIONS"
)

// Error is one typed refusal. It names the code, the node it happened at (when
// a node is implicated) and the underlying cause.
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
	msg := fmt.Sprintf("simulate: %s%s: %s", e.Code, loc, e.Detail)
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap exposes the wrapped cause, and always the package sentinel.
func (e *Error) Unwrap() []error {
	if e.Err != nil {
		return []error{ErrSimulate, e.Err}
	}
	return []error{ErrSimulate}
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

// ErrorCode reports the refusal's stable code for telemetry classification
// (internal/workflow/observe.ErrorCode); it never carries message text.
func (e *Error) ErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}
