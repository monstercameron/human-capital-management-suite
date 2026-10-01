package agencytime

import (
	"errors"
	"fmt"
)

// ErrRejected is the sentinel every rejection in this package wraps.
var ErrRejected = errors.New("agencytime: request rejected")

// Rejection is a typed rejection: which field, what state, and why.
type Rejection struct {
	Field  string
	State  string
	Reason string
}

func (r Rejection) Error() string {
	return fmt.Sprintf("agencytime: field %q in state %q: %s", r.Field, r.State, r.Reason)
}

func reject(field, state, reason string) error {
	return errors.Join(ErrRejected, Rejection{Field: field, State: state, Reason: reason})
}
