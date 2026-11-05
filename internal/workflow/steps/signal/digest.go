package signal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Canonicalization profile identities, following the same profile-prefixed
// sha256-over-canonical-JSON convention as internal/workflow/frontier/digest.go
// and internal/workflow/steps/wait/digest.go.
const (
	subscriptionDigestProfile = "hcmnext.workflow.steps.signal.SignalSubscription/v1"
	logEntryDigestProfile     = "hcmnext.workflow.steps.signal.LogEntry/v1"
)

func canonicalDigest(profile string, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// Every canonical view here is plain data (strings, bytes rendered as
		// hex, and safe String() forms of value types), so this is
		// unreachable in practice; produce bytes that cannot collide with a
		// real digest rather than panicking.
		b = []byte("unencodable:" + err.Error())
	}
	h := sha256.New()
	h.Write([]byte(profile))
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// subscriptionDigestView is the canonical digest input for a
// SignalSubscription. Temporal values are rendered as explicit text (never a
// raw values.Instant, whose MarshalText errors on the legitimate "never
// closes" zero value) and AcceptedSources is sorted before digesting.
type subscriptionDigestView struct {
	Tenant             string   `json:"tenant"`
	WorkflowInstanceID string   `json:"workflow_instance_id"`
	NodeID             string   `json:"node_id"`
	EventType          string   `json:"event_type"`
	CorrelationKey     string   `json:"correlation_key"`
	CorrelationValue   string   `json:"correlation_value"`
	ExpectedSchemaRef  string   `json:"expected_schema_ref"`
	AcceptedSources    []string `json:"accepted_sources"`
	Ordering           string   `json:"ordering"`
	ClosesAt           string   `json:"closes_at"`
	// The default halves omit when empty so a subscription without a
	// declared default keeps its historical digest byte for byte.
	DefaultSchema  string `json:"default_schema,omitempty"`
	DefaultPayload string `json:"default_payload,omitempty"`
}

func computeSubscriptionDigest(s SignalSubscription) string {
	view := subscriptionDigestView{
		Tenant:             s.Tenant.String(),
		WorkflowInstanceID: s.WorkflowInstanceID,
		NodeID:             s.NodeID,
		EventType:          s.EventType,
		CorrelationKey:     s.CorrelationKey,
		CorrelationValue:   s.CorrelationValue,
		ExpectedSchemaRef:  s.ExpectedSchemaRef,
		AcceptedSources:    s.sortedSources(),
		Ordering:           string(s.Ordering),
		ClosesAt:           s.ClosesAtText(),
		DefaultSchema:      s.DeadlineDefault.Schema,
		DefaultPayload:     s.DeadlineDefault.CanonicalJSON,
	}
	return canonicalDigest(subscriptionDigestProfile, view)
}

// signalDigestView is the canonical digest input for the Signal half of a
// LogEntry. Payload/signature are rendered as hex so no raw byte slice
// (unordered as JSON, and not safely comparable) reaches encoding/json.
type signalDigestView struct {
	Tenant           string `json:"tenant"`
	Source           string `json:"source"`
	EventType        string `json:"event_type"`
	SchemaRef        string `json:"schema_ref"`
	CorrelationKey   string `json:"correlation_key"`
	CorrelationValue string `json:"correlation_value"`
	SequenceNumber   uint64 `json:"sequence_number"`
	IdempotencyKey   string `json:"idempotency_key"`
	PayloadHex       string `json:"payload_hex"`
	Taint            string `json:"taint"`
	SignatureHex     string `json:"signature_hex"`
	ReceivedAt       string `json:"received_at"`
}

func signalView(s Signal) signalDigestView {
	return signalDigestView{
		Tenant:           s.Tenant.String(),
		Source:           s.Source,
		EventType:        s.EventType,
		SchemaRef:        s.SchemaRef,
		CorrelationKey:   s.CorrelationKey,
		CorrelationValue: s.CorrelationValue,
		SequenceNumber:   s.SequenceNumber,
		IdempotencyKey:   s.IdempotencyKey,
		PayloadHex:       hex.EncodeToString(s.Payload),
		Taint:            string(s.Taint),
		SignatureHex:     hex.EncodeToString(s.Signature),
		ReceivedAt:       instantText(s.ReceivedAt),
	}
}

// logEntryDigestView is the canonical digest input for a LogEntry.
type logEntryDigestView struct {
	SubscriptionDigest string           `json:"subscription_digest"`
	Signal             signalDigestView `json:"signal"`
	Status             string           `json:"status"`
	Continuation       bool             `json:"continuation"`
	Reason             string           `json:"reason"`
	RecordedAt         string           `json:"recorded_at"`
}

func logEntryView(e LogEntry) logEntryDigestView {
	return logEntryDigestView{
		SubscriptionDigest: e.SubscriptionDigest,
		Signal:             signalView(e.Signal),
		Status:             string(e.Status),
		Continuation:       e.Continuation,
		Reason:             e.Reason,
		RecordedAt:         e.RecordedAtText(),
	}
}

func computeLogEntryDigest(e LogEntry) string {
	return canonicalDigest(logEntryDigestProfile, logEntryView(e))
}

// JSON renders a LogEntry as indented, deterministic JSON with its digest
// attached, suitable for a checked-in golden fixture: two identical
// evaluations render identical bytes.
func (e LogEntry) JSON() ([]byte, error) {
	type rendered struct {
		logEntryDigestView
		Digest string `json:"digest"`
	}
	b, err := json.MarshalIndent(rendered{logEntryDigestView: logEntryView(e), Digest: e.Digest}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
