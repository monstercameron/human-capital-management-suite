package fragments

import "errors"

// Fragment sentinels. Classify with errors.Is.
var (
	// ErrInvalidFragment marks a fragment declaration expansion refuses:
	// a bad identity, an unpublished capability version, missing
	// governance, or ungoverned vocabulary.
	ErrInvalidFragment = errors.New("fragments: invalid fragment")
	// ErrInvalidInvocation marks a served handler payload that is not a
	// runner call.
	ErrInvalidInvocation = errors.New("fragments: invalid invocation")
)
