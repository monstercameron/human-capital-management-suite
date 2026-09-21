package logging

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// maxPrincipalRefLen bounds the opaque principal reference so a caller
// cannot smuggle an oversized payload through it.
const maxPrincipalRefLen = 128

type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
	ctxKeyCorrelationID
	ctxKeyPrincipalRef
	ctxKeyEvidenceIDs
)

// WithRequestID attaches a per-call request identifier that the Handler
// copies onto every envelope emitted while ctx (or a descendant) is in
// scope.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyRequestID, id)
}

// RequestID returns the request identifier attached to ctx, if any.
func RequestID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxKeyRequestID).(string)
	return v, ok
}

// WithCorrelationID attaches a business correlation identifier that may span
// several requests or a long-running workflow.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyCorrelationID, id)
}

// CorrelationID returns the correlation identifier attached to ctx, if any.
func CorrelationID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxKeyCorrelationID).(string)
	return v, ok
}

// WithEvidenceIDs appends one or more evidence identifiers (references into
// an evidence store, never the evidence payload itself) to any already
// attached to ctx.
func WithEvidenceIDs(ctx context.Context, ids ...string) context.Context {
	if len(ids) == 0 {
		return ctx
	}
	existing, _ := EvidenceIDs(ctx)
	merged := make([]string, 0, len(existing)+len(ids))
	merged = append(merged, existing...)
	merged = append(merged, ids...)
	return context.WithValue(ctx, ctxKeyEvidenceIDs, merged)
}

// EvidenceIDs returns the evidence identifiers attached to ctx, if any.
func EvidenceIDs(ctx context.Context) ([]string, bool) {
	v, ok := ctx.Value(ctxKeyEvidenceIDs).([]string)
	return v, ok
}

// ErrRawIdentity is returned by WithPrincipalRef when the supplied value
// looks like a raw identity (an email address, or something exceeding the
// bound expected of an opaque reference) rather than an opaque reference.
var ErrRawIdentity = errors.New("logging: principal reference must be an opaque reference, not a raw identity")

// WithPrincipalRef attaches a principal reference: an opaque, bounded
// identifier for "who acted" (for example "principal:worker:<uuid>"), never
// the raw identity (name, email, external ID) itself. ValidatePrincipalRef
// gates the value before it is stored, so a raw identity can never reach the
// envelope through this path.
func WithPrincipalRef(ctx context.Context, ref string) (context.Context, error) {
	if err := ValidatePrincipalRef(ref); err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, ctxKeyPrincipalRef, ref), nil
}

// PrincipalRef returns the principal reference attached to ctx, if any.
func PrincipalRef(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxKeyPrincipalRef).(string)
	return v, ok
}

// ValidatePrincipalRef rejects empty values, oversized values, and values
// that look like a raw identity rather than an opaque reference. It is a
// narrow heuristic (no "@", no embedded whitespace, bounded length) scoped
// to this package; the owned classifier for identity-shaped data lives in
// internal/platform/telemetry per the structured-logging spec.
func ValidatePrincipalRef(ref string) error {
	if ref == "" {
		return fmt.Errorf("%w: empty", ErrRawIdentity)
	}
	if len([]rune(ref)) > maxPrincipalRefLen {
		return fmt.Errorf("%w: exceeds %d characters", ErrRawIdentity, maxPrincipalRefLen)
	}
	if strings.ContainsAny(ref, "@ \t\n") {
		return fmt.Errorf("%w: %q", ErrRawIdentity, ref)
	}
	return nil
}
