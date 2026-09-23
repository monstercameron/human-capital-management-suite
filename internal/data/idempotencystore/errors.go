package idempotencystore

import (
	"errors"
	"fmt"
)

const (
	statusInProgress = "in_progress"
	statusCompleted  = "completed"
)

// Code is a typed store failure callers can branch on.
type Code string

const (
	CodeInvalid        Code = "INVALID"
	CodeNotFound       Code = "NOT_FOUND"
	CodeConflict       Code = "CONFLICT"
	CodeDatabase       Code = "DATABASE"
	CodeTenantRequired Code = "TENANT_REQUIRED"
)

// ErrDigestConflict is returned when a key is reused with a different
// request: the original claim stands and the conflicting write is refused.
var ErrDigestConflict = errors.New("idempotency key reused with a different request")

// ErrNoClaim is returned when completing a key with no in-progress claim:
// unknown keys, foreign-tenant keys and already-completed keys.
var ErrNoClaim = errors.New("no in-progress idempotency claim")

// Error is a typed store failure. Callers can branch on CodeOf without
// parsing messages.
type Error struct {
	Code    Code
	Table   string
	Key     string
	Wrapped error
}

func (e *Error) Error() string {
	return fmt.Sprintf("idempotencystore: %s %s %s: %v", e.Code, e.Table, e.Key, e.Wrapped)
}

func (e *Error) Unwrap() error { return e.Wrapped }

func failure(code Code, table, key string, err error) error {
	return &Error{Code: code, Table: table, Key: key, Wrapped: err}
}

// CodeOf returns the nearest store error code, or CodeDatabase for foreign
// failures.
func CodeOf(err error) Code {
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return CodeDatabase
}
