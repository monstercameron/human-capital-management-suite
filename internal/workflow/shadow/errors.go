package shadow

import (
	"errors"
	"fmt"
)

var (
	ErrShadow          = errors.New("shadow: run refused")
	ErrInvalidOptions  = errors.New("shadow: invalid options")
	ErrModeRefused     = errors.New("shadow: mode contract refused")
	ErrEffectForbidden = errors.New("shadow: effect forbidden")
	ErrDurableMutation = errors.New("shadow: durable mutation detected")
	ErrNoRoute         = errors.New("shadow: no declared route")
)

const (
	CodeModeRefused     = "MODE_REFUSED"
	CodeEffectForbidden = "SHADOW_EFFECT_FORBIDDEN"
	CodeDurableMutation = "SHADOW_DURABLE_MUTATION"
	CodeNoRoute         = "NO_DECLARED_ROUTE"
	CodeStepFailed      = "STEP_FAILED"
	CodeStepBudget      = "STEP_BUDGET_EXCEEDED"
	CodeInvalidOptions  = "INVALID_OPTIONS"
)

type Error struct {
	Code   string
	NodeID string
	Detail string
	Err    error
}

func (e *Error) Error() string {
	where := ""
	if e.NodeID != "" {
		where = " at node " + e.NodeID
	}
	msg := fmt.Sprintf("shadow: %s%s: %s", e.Code, where, e.Detail)
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *Error) Unwrap() []error {
	if e.Err != nil {
		return []error{ErrShadow, e.Err}
	}
	return []error{ErrShadow}
}

func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func refuse(code, node, format string, args ...any) error {
	return &Error{Code: code, NodeID: node, Detail: fmt.Sprintf(format, args...)}
}

func wrap(code, node string, err error, format string, args ...any) error {
	return &Error{Code: code, NodeID: node, Detail: fmt.Sprintf(format, args...), Err: err}
}

// ErrorCode reports the refusal's stable code for telemetry classification
// (internal/workflow/observe.ErrorCode); it never carries message text.
func (e *Error) ErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}
