package contractortime

import (
	"errors"
	"fmt"
)

// ErrRejected is the sentinel every rejection in this package wraps. Callers
// match with errors.Is; humans read the joined detail.
var ErrRejected = errors.New("contractortime: request rejected")

// Rejection is a typed rejection with enough structure for a caller to react
// without parsing prose: which field, what state it was in, and why.
type Rejection struct {
	Field  string
	State  string
	Reason string
}

func (r Rejection) Error() string {
	return fmt.Sprintf("contractortime: field %q in state %q: %s", r.Field, r.State, r.Reason)
}

func reject(field, state, reason string) error {
	return errors.Join(ErrRejected, Rejection{Field: field, State: state, Reason: reason})
}
