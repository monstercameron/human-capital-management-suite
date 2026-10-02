package chatgate

// FieldError is safe to return across the boundary: it names a declared field
// and a typed refusal, never the rejected answer or directory fact.
type FieldError struct {
	Field string
	Cause error
}

func (e FieldError) Error() string { return "gate field " + e.Field + ": " + e.Cause.Error() }
func (e FieldError) Unwrap() error { return e.Cause }
