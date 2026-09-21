// Package logging implements the OBS-009 scaffold for the versioned
// structured-log envelope (planning/todos.md `OBS-009`;
// planning/specs/structured-logging-and-opentelemetry.md, "Structured log
// envelope" and "Severity and outcome"). It wraps `log/slog` with a handler
// that emits one JSON object per record with a stable schema version,
// deterministic field order, an RFC 3339 UTC timestamp, redaction of denied
// attribute keys, a principal reference instead of a raw identity, and
// bounded size/cardinality with explicit truncation markers.
//
// Scope note: the full OBS-009 contract is owned by
// internal/platform/telemetry (event/attribute registries, OpenTelemetry
// export, trace propagation, privacy policy compiled from the telemetry
// schema). This package is a smaller platform-layer building block that
// implements the envelope shape and the redaction/cardinality mechanics in
// isolation, so it does not reference OpenTelemetry, service/deployment
// resource attributes, or the owned event-name registry from that spec.
package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// SchemaVersion is the current version of the envelope this package emits.
// A change to the field set or its semantics requires incrementing this
// constant so downstream consumers can detect the shift.
const SchemaVersion = 1

// Default cardinality and size caps. These are conservative defaults for the
// scoped platform logger; callers needing different bounds pass the
// corresponding Option.
const (
	defaultMaxAttrs        = 32
	defaultMaxAttrValueLen = 256
	defaultMaxMessageLen   = 512
	defaultMaxEvidenceIDs  = 8
	truncationSuffix       = "…[TRUNCATED]"
	redactedValue          = "[REDACTED]"
)

// Clock returns the current time. Tests inject a fixed Clock to make the
// envelope's timestamp field byte-stable.
type Clock func() time.Time

// envelope is the exact, ordered set of fields this package emits. Field
// declaration order is preserved by encoding/json for a struct, which is how
// this package satisfies "fixed key order" without hand-rolled encoding.
type envelope struct {
	SchemaVersion int            `json:"schema_version"`
	Timestamp     string         `json:"timestamp"`
	Level         string         `json:"level"`
	Service       string         `json:"service"`
	Message       string         `json:"message"`
	RequestID     string         `json:"request_id,omitempty"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	PrincipalRef  string         `json:"principal_ref,omitempty"`
	EvidenceIDs   []string       `json:"evidence_ids,omitempty"`
	Attrs         map[string]any `json:"attrs,omitempty"`
	Truncated     []string       `json:"truncated,omitempty"`
}

// Handler is a log/slog.Handler that emits the versioned envelope described
// in the package doc. Use NewHandler to construct one.
type Handler struct {
	mu     *sync.Mutex
	writer io.Writer

	clock    Clock
	service  string
	minLevel slog.Level

	denied         map[string]struct{}
	maxAttrs       int
	maxAttrValLen  int
	maxMessageLen  int
	maxEvidenceIDs int

	groupPrefix string
	attrs       []slog.Attr // already group-prefixed, in encounter order
}

// Option configures a Handler at construction time.
type Option func(*Handler)

// WithClock overrides the handler's time source. Tests use this to produce
// byte-stable golden output.
func WithClock(c Clock) Option {
	return func(h *Handler) {
		if c != nil {
			h.clock = c
		}
	}
}

// WithService sets the service name recorded on every envelope.
func WithService(name string) Option {
	return func(h *Handler) { h.service = name }
}

// WithMinLevel sets the minimum level the handler reports as enabled.
func WithMinLevel(l slog.Level) Option {
	return func(h *Handler) { h.minLevel = l }
}

// WithDeniedKeys adds attribute keys (case-insensitive) whose values are
// always redacted, in addition to the built-in default denylist.
func WithDeniedKeys(keys ...string) Option {
	return func(h *Handler) {
		for _, k := range keys {
			h.denied[strings.ToLower(k)] = struct{}{}
		}
	}
}

// WithMaxAttrs bounds how many attributes one record may carry before extras
// are dropped and a truncation marker is recorded.
func WithMaxAttrs(n int) Option {
	return func(h *Handler) {
		if n > 0 {
			h.maxAttrs = n
		}
	}
}

// WithMaxAttrValueLen bounds the rune length of any string attribute value
// (and the message) before it is truncated with a marker suffix.
func WithMaxAttrValueLen(n int) Option {
	return func(h *Handler) {
		if n > 0 {
			h.maxAttrValLen = n
		}
	}
}

// WithMaxMessageLen bounds the rune length of the log message.
func WithMaxMessageLen(n int) Option {
	return func(h *Handler) {
		if n > 0 {
			h.maxMessageLen = n
		}
	}
}

// WithMaxEvidenceIDs bounds how many evidence IDs attached via context are
// emitted on one record.
func WithMaxEvidenceIDs(n int) Option {
	return func(h *Handler) {
		if n > 0 {
			h.maxEvidenceIDs = n
		}
	}
}

// NewHandler constructs a Handler that writes newline-delimited JSON
// envelopes to w.
func NewHandler(w io.Writer, opts ...Option) *Handler {
	h := &Handler{
		mu:             &sync.Mutex{},
		writer:         w,
		clock:          time.Now,
		service:        "unknown-service",
		minLevel:       slog.LevelDebug,
		denied:         defaultDeniedKeys(),
		maxAttrs:       defaultMaxAttrs,
		maxAttrValLen:  defaultMaxAttrValueLen,
		maxMessageLen:  defaultMaxMessageLen,
		maxEvidenceIDs: defaultMaxEvidenceIDs,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Enabled reports whether level is at or above the handler's minimum level.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

// WithAttrs returns a new Handler with attrs appended to every future
// record. The receiver is not mutated.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	next := *h
	merged := make([]slog.Attr, len(h.attrs), len(h.attrs)+len(attrs))
	copy(merged, h.attrs)
	for _, a := range attrs {
		merged = append(merged, prefixAttr(h.groupPrefix, a))
	}
	next.attrs = merged
	return &next
}

// WithGroup returns a new Handler that prefixes subsequent attribute keys
// (including those from WithAttrs) with name.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := *h
	next.groupPrefix = joinGroup(h.groupPrefix, name)
	return &next
}

// Handle builds and writes one envelope for r.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	var truncated []string

	msg, msgTrunc := truncateString(r.Message, h.maxMessageLen)
	if msgTrunc {
		truncated = append(truncated, "message")
	}

	all := make([]slog.Attr, 0, len(h.attrs)+r.NumAttrs())
	all = append(all, h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		all = append(all, prefixAttr(h.groupPrefix, a))
		return true
	})

	fields := make(map[string]any, len(all))
	total := 0
	dropped := 0
	for _, a := range all {
		if a.Key == "" {
			continue
		}
		total++
		if total > h.maxAttrs {
			dropped++
			continue
		}
		if _, isDenied := h.denied[strings.ToLower(a.Key)]; isDenied {
			fields[a.Key] = redactedValue
			continue
		}
		val := a.Value.Resolve().Any()
		if s, ok := val.(string); ok {
			trimmed, wasTrunc := truncateString(s, h.maxAttrValLen)
			if wasTrunc {
				truncated = append(truncated, "attr:"+a.Key)
			}
			val = trimmed
		}
		fields[a.Key] = val
	}
	if dropped > 0 {
		truncated = append(truncated, fmt.Sprintf("attrs:%d_dropped", dropped))
	}

	evidenceIDs, _ := EvidenceIDs(ctx)
	if len(evidenceIDs) > h.maxEvidenceIDs {
		droppedIDs := len(evidenceIDs) - h.maxEvidenceIDs
		evidenceIDs = evidenceIDs[:h.maxEvidenceIDs]
		truncated = append(truncated, fmt.Sprintf("evidence_ids:%d_dropped", droppedIDs))
	}

	reqID, _ := RequestID(ctx)
	corrID, _ := CorrelationID(ctx)
	principal, _ := PrincipalRef(ctx)

	sort.Strings(truncated)

	env := envelope{
		SchemaVersion: SchemaVersion,
		Timestamp:     h.clock().UTC().Format(time.RFC3339),
		Level:         levelString(r.Level),
		Service:       h.service,
		Message:       msg,
		RequestID:     reqID,
		CorrelationID: corrID,
		PrincipalRef:  principal,
		EvidenceIDs:   evidenceIDs,
		Attrs:         fields,
		Truncated:     truncated,
	}

	b, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("logging: marshal envelope: %w", err)
	}
	b = append(b, '\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err = h.writer.Write(b)
	return err
}

func levelString(l slog.Level) string {
	switch {
	case l < slog.LevelInfo:
		return "DEBUG"
	case l < slog.LevelWarn:
		return "INFO"
	case l < slog.LevelError:
		return "WARN"
	default:
		return "ERROR"
	}
}

func prefixAttr(groupPrefix string, a slog.Attr) slog.Attr {
	if groupPrefix == "" || a.Key == "" {
		return a
	}
	return slog.Attr{Key: groupPrefix + "." + a.Key, Value: a.Value}
}

func joinGroup(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// truncateString returns s capped at maxRunes runes. If s was cut, the
// returned string ends with a visible truncation marker and the bool is
// true.
func truncateString(s string, maxRunes int) (string, bool) {
	if maxRunes <= 0 {
		return s, false
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s, false
	}
	return string(r[:maxRunes]) + truncationSuffix, true
}
