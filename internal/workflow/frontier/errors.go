package frontier

import (
	"errors"
	"fmt"
)

// ErrFrontier is the sentinel every refusal from this package unwraps to.
// Classify with [errors.Is] and read [Error.Code] for the exact reason; never
// match message text.
var ErrFrontier = errors.New("frontier: advancement refused")

// Stable refusal codes. A transport, a runtime or a test matches these; they
// never change spelling once published.
const (
	// CodeInvalidPlan reports an absent or unusable compiled plan.
	CodeInvalidPlan = "INVALID_PLAN"
	// CodePlanMismatch reports a state snapshot pinned to a different plan
	// digest than the plan supplied. Advancing an instance against a plan it
	// was not started on is a migration, not a transition.
	CodePlanMismatch = "PLAN_MISMATCH"
	// CodeUnknownNode reports an outcome for a node the plan does not declare.
	CodeUnknownNode = "UNKNOWN_NODE"
	// CodeNodeNotActive reports an outcome for a node that is not on the
	// frontier: it never started, or it already settled. Completing a settled
	// node twice is a duplicate delivery, and this package refuses rather than
	// advancing the instance a second time.
	CodeNodeNotActive = "NODE_NOT_ACTIVE"
	// CodeAlreadyComplete reports an advancement attempt on an instance that
	// already reached its terminal.
	CodeAlreadyComplete = "ALREADY_COMPLETE"
	// CodeUnknownOutcome reports an outcome route key the node's compiled step
	// type cannot produce.
	CodeUnknownOutcome = "UNKNOWN_OUTCOME"
	// CodeMissingRoute reports an admissible outcome with no outgoing edge.
	// There is no implicit first edge at advancement time.
	CodeMissingRoute = "MISSING_ROUTE"
	// CodeNoMatchingRoute reports a DECISION whose produced route key matches
	// no declared route and whose node declares no default_route.
	CodeNoMatchingRoute = "NO_MATCHING_ROUTE"
	// CodeNoFailureRoute reports a failed node with its retry budget exhausted
	// and no declared failure_route.
	CodeNoFailureRoute = "NO_FAILURE_ROUTE"
	// CodeAwaitNotAdmitted reports an awaiting-work marker whose kind the
	// node's step type cannot raise, or an outcome that both completes and
	// awaits.
	CodeAwaitNotAdmitted = "AWAIT_NOT_ADMITTED"
	// CodeJoinNotDeclared reports a JOIN reached with no declared strategy in
	// the instance state. A join strategy is declared, never inferred.
	CodeJoinNotDeclared = "JOIN_NOT_DECLARED"
	// CodeInvalidJoinDeclaration reports a join declaration that names a
	// non-JOIN node, an undeclared strategy, or a count its branch set cannot
	// meet.
	CodeInvalidJoinDeclaration = "INVALID_JOIN_DECLARATION"
	// CodeMissingTerminal reports an END node with no compiled terminal
	// artifact. The plan, not the handler, states the lifecycle dimensions.
	CodeMissingTerminal = "MISSING_TERMINAL"
	// CodeTerminalMismatch reports a handler-supplied terminal result that
	// contradicts the terminal the plan compiled for that END node.
	CodeTerminalMismatch = "TERMINAL_MISMATCH"
	// CodeTerminalFrontierRemains reports an END whose completion would leave
	// other nodes on the frontier. The refusal names them.
	CodeTerminalFrontierRemains = "TERMINAL_FRONTIER_REMAINS"
	// CodeInvalidState reports a state snapshot this package cannot interpret:
	// an unknown node id, an undeclared node state, or a frontier that
	// disagrees with the node states beside it.
	CodeInvalidState = "INVALID_STATE"
)

// Error is one typed refusal: the code, the node it happened at when a node is
// implicated, and a detail written for a person. Code is for a program.
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
	msg := fmt.Sprintf("frontier: %s%s: %s", e.Code, loc, e.Detail)
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap exposes the wrapped cause, and always the package sentinel.
func (e *Error) Unwrap() []error {
	if e.Err != nil {
		return []error{ErrFrontier, e.Err}
	}
	return []error{ErrFrontier}
}

// refuse builds a typed refusal.
func refuse(code, nodeID, format string, args ...any) *Error {
	return &Error{Code: code, NodeID: nodeID, Detail: fmt.Sprintf(format, args...)}
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
