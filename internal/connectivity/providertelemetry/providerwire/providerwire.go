// Package providerwire is the wire half of provider observability: the
// header names HCM and the provider simulators exchange for trace and
// correlation context, and the validation both sides apply to values that
// arrive from the other party.
//
// It depends only on the standard library and the telemetry boundary
// parser, so the simulators can share it without pulling in the
// OpenTelemetry SDK. Nothing here grants trust: a traceparent or
// correlation id that passes validation is well-formed, not authentic.
package providerwire

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/boundary"
)

// Header names. TraceParentHeader is the W3C Trace Context header;
// CorrelationIDHeader carries the business correlation id.
const (
	TraceParentHeader   = "traceparent"
	CorrelationIDHeader = "X-Correlation-Id"
	// MaxCorrelationIDLen bounds a correlation id, matching the
	// correlation_id context_propagation_keys max_length in
	// definitions/telemetry/resource-contract.yaml.
	MaxCorrelationIDLen = 128
)

// ValidCorrelationID reports whether v is a non-empty correlation id of at
// most MaxCorrelationIDLen visible ASCII characters (0x21-0x7e). Spaces and
// control characters are refused so a value can neither split a header nor
// a log line.
func ValidCorrelationID(v string) bool {
	if v == "" || len(v) > MaxCorrelationIDLen {
		return false
	}
	for i := 0; i < len(v); i++ {
		if c := v[i]; c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}

// ValidTraceParent returns the canonical form of a well-formed W3C
// traceparent (version 00, lowercase hex, non-zero ids, flags 00 or 01) and
// true, or "" and false for anything else.
func ValidTraceParent(v string) (string, bool) {
	t, err := boundary.ParseTraceParent(v)
	if err != nil {
		return "", false
	}
	out, err := boundary.FormatTraceParent(t)
	if err != nil {
		return "", false
	}
	return out, true
}

// ChildTraceParent returns a traceparent in the same trace as parent, with
// the same sampled flag and a fresh random span id: the context a provider
// uses when it calls back as a child of the caller's span. It returns false
// when parent is malformed or randomness is unavailable.
func ChildTraceParent(parent string) (string, bool) {
	t, err := boundary.ParseTraceParent(parent)
	if err != nil {
		return "", false
	}
	var id [8]byte
	for {
		if _, err := rand.Read(id[:]); err != nil {
			return "", false
		}
		if id != [8]byte{} && hex.EncodeToString(id[:]) != t.SpanID {
			break
		}
	}
	t.SpanID = hex.EncodeToString(id[:])
	out, err := boundary.FormatTraceParent(t)
	if err != nil {
		return "", false
	}
	return out, true
}

// Context is the validated trace and correlation context one inbound
// request carried. Empty fields were absent or malformed.
type Context struct {
	TraceParent   string
	CorrelationID string
}

// FromHeader extracts and validates the trace and correlation headers of
// an inbound request. A malformed value is dropped, never repaired.
func FromHeader(h http.Header) Context {
	var c Context
	if tp, ok := ValidTraceParent(h.Get(TraceParentHeader)); ok {
		c.TraceParent = tp
	}
	if id := h.Get(CorrelationIDHeader); ValidCorrelationID(id) {
		c.CorrelationID = id
	}
	return c
}

// IsZero reports whether c carries nothing.
func (c Context) IsZero() bool { return c.TraceParent == "" && c.CorrelationID == "" }

// ApplyEcho sets the echo of c on an outbound callback: traceparent as a
// child of the stored parent (a new span id per call) and the correlation
// id unchanged. Absent or malformed values are not set.
func (c Context) ApplyEcho(h http.Header) {
	if child, ok := ChildTraceParent(c.TraceParent); ok {
		h.Set(TraceParentHeader, child)
	}
	if ValidCorrelationID(c.CorrelationID) {
		h.Set(CorrelationIDHeader, c.CorrelationID)
	}
}
