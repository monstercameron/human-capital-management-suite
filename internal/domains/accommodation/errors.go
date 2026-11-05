package accommodation

import "errors"

// Domain sentinels. Classify with errors.Is.
var (
	// ErrInvalidRecord marks a record that fails validation: unknown
	// vocabulary, missing provenance, or broken lifecycle ordering.
	ErrInvalidRecord = errors.New("accommodation: invalid record")
	// ErrSealBroken marks a record whose digest no longer matches its
	// content: a mutation after sealing.
	ErrSealBroken = errors.New("accommodation: record seal is broken")
	// ErrUnknownCase marks an operation against a case the store does not hold.
	ErrUnknownCase = errors.New("accommodation: unknown case")
	// ErrCaseExists marks an attempt to open a case that is already open.
	ErrCaseExists = errors.New("accommodation: case already exists")
)
