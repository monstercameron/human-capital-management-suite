package docquery

import "errors"

// Sentinel errors returned by the registry, validator, and query builder.
// All wrap with %w and machine-readable context; none carries document
// payload bytes (payloads may hold restricted fields).
var (
	// ErrInvalidTypeName rejects document-type names outside the dotted
	// lowercase-segment shape.
	ErrInvalidTypeName = errors.New("docquery: invalid document type name")
	// ErrInvalidClassification rejects labels outside the closed
	// classification vocabulary.
	ErrInvalidClassification = errors.New("docquery: invalid classification")
	// ErrInvalidSchema rejects registrations with no fields or duplicate
	// field names.
	ErrInvalidSchema = errors.New("docquery: invalid document schema")
	// ErrInvalidFieldName rejects field names outside identifier shape.
	ErrInvalidFieldName = errors.New("docquery: invalid field name")
	// ErrUnknownWireType rejects fields declared with a wire type the
	// validator cannot check values against.
	ErrUnknownWireType = errors.New("docquery: unknown wire type")
	// ErrTypeExists rejects re-registration of a published type name.
	ErrTypeExists = errors.New("docquery: document type already registered")
	// ErrUnknownType is returned when no such type is registered.
	ErrUnknownType = errors.New("docquery: unknown document type")
	// ErrUndeclaredField quarantines documents carrying fields the type
	// does not declare.
	ErrUndeclaredField = errors.New("docquery: undeclared field")
	// ErrTypeMismatch quarantines documents whose field value does not
	// match the declared wire type.
	ErrTypeMismatch = errors.New("docquery: value does not match declared wire type")
	// ErrMissingField quarantines documents missing a required field.
	ErrMissingField = errors.New("docquery: missing required field")
	// ErrInvalidDocument rejects payloads that are not JSON objects.
	ErrInvalidDocument = errors.New("docquery: document is not a JSON object")
	// ErrUnindexedField rejects predicates over fields the type does not
	// declare Indexed.
	ErrUnindexedField = errors.New("docquery: predicate over non-indexed field")
	// ErrInvalidPredicate rejects predicates with non-scalar values,
	// empty ranges, or operators outside the Eq/Range builders.
	ErrInvalidPredicate = errors.New("docquery: invalid predicate")
	// ErrInvalidLimit rejects limits outside 1..MaxLimit.
	ErrInvalidLimit = errors.New("docquery: invalid limit")
)
