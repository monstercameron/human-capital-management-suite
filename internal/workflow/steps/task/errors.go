package task

import "errors"

var (
	ErrInvalidNode         = errors.New("workflow task: invalid compiled node")
	ErrInvalidContinuation = errors.New("workflow task: invalid continuation")
	ErrInvalidSubmission   = errors.New("workflow task: invalid submission")
	ErrBindingMismatch     = errors.New("workflow task: binding mismatch")
	ErrClaimExpired        = errors.New("workflow task: claim expired before submission")
	ErrValidationFailed    = errors.New("workflow task: submission validation failed")
	ErrNoDeadline          = errors.New("workflow task: node declares no deadline expression")
)
