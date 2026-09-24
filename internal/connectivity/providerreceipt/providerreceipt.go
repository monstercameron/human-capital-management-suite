// Package providerreceipt parses and verifies one signed provider RESULT
// callback: the HTTP request a payroll or IAM provider sends to report what
// became of an outbound change.
//
// The replay window, the schema allowlist, the tenant check and duplicate
// detection are delegated to internal/connectivity/webhook's Store.Receive;
// this package adds the header contract, signature verification against an
// ordered list of secrets (for rotation, see [Verifier]), the event-type to
// outcome mapping and the typed decoding of the two provider payloads. It
// touches no database: the caller persists the [Parsed] result (see
// internal/data/providerreceipts).
package providerreceipt

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/idempotency"
)

// Outcome is the provider's statement about one change.
type Outcome string

const (
	OutcomeApplied  Outcome = "APPLIED"
	OutcomeGranted  Outcome = "GRANTED"
	OutcomeRejected Outcome = "REJECTED"
	// OutcomeReversed and OutcomeRevoked report an earlier APPLIED or
	// GRANTED change undone by a reversal or revocation.
	OutcomeReversed Outcome = "REVERSED"
	OutcomeRevoked  Outcome = "REVOKED"
)

// Providers this package understands.
const (
	ProviderPayroll = "payroll"
	ProviderIAM     = "iam"
)

// Wire contract of the two simulated providers.
const (
	PayrollSchema        = "payrollsim.change_result/v1"
	PayrollEventApplied  = "payroll.change.applied"
	PayrollEventRejected = "payroll.change.rejected"
	PayrollEventReversed = "payroll.change.reversed"

	IAMSchema        = "iamsim.access_result/v1"
	IAMEventGranted  = "access.change.granted"
	IAMEventRejected = "access.change.rejected"
	IAMEventRevoked  = "access.change.revoked"
)

// Header names of the signed callback envelope.
const (
	HeaderID        = "Webhook-Id"
	HeaderEvent     = "Webhook-Event"
	HeaderSchema    = "Webhook-Schema"
	HeaderTimestamp = "Webhook-Timestamp"
	HeaderTenant    = "Webhook-Tenant"
	HeaderSignature = "Webhook-Signature"
)

// Defaults applied by [NewVerifier] when an Endpoint leaves them zero.
const (
	DefaultMaxBytes = 64 << 10
	DefaultWindow   = 5 * time.Minute
)

// MinSecretLen is the shortest callback signing secret [NewVerifier]
// accepts, in bytes (the HMAC-SHA256 output size).
const MinSecretLen = 32

var (
	ErrInvalidEndpoint    = errors.New("providerreceipt: invalid endpoint")
	ErrBadSignature       = errors.New("providerreceipt: signature is invalid")
	ErrOutsideWindow      = errors.New("providerreceipt: timestamp outside replay window")
	ErrUnknownSchema      = errors.New("providerreceipt: schema is not allowed")
	ErrWrongTenant        = errors.New("providerreceipt: tenant does not match endpoint")
	ErrMalformed          = errors.New("providerreceipt: malformed callback")
	ErrUnknownEvent       = errors.New("providerreceipt: event type is not mapped")
	ErrDuplicateDifferent = errors.New("providerreceipt: event id redelivered with different bytes")
	ErrTooLarge           = errors.New("providerreceipt: payload exceeds endpoint limit")
	// ErrOutcomeMismatch is a malformed callback whose payload outcome
	// contradicts its event type; errors.Is(err, ErrMalformed) also holds.
	ErrOutcomeMismatch = fmt.Errorf("%w: outcome does not match event type", ErrMalformed)
)

// Endpoint is the receive policy for one provider's callbacks to one tenant.
type Endpoint struct {
	Provider   string
	EndpointID string
	TenantID   string
	// Secrets are the accepted signing secrets in order: the current one
	// first, then any previous ones still honoured during a rotation. A
	// callback signed with any of them verifies; Parsed.SecretIndex says
	// which. Every secret must be at least MinSecretLen bytes.
	Secrets [][]byte
	// Secret is the single-secret form kept for compatibility: it is used
	// as Secrets[0] when Secrets is empty. When both are set, Secret must
	// equal Secrets[0].
	Secret        []byte
	WebhookSchema string
	// Events maps each accepted Webhook-Event value to the outcome its
	// payload must state.
	Events   map[string]Outcome
	MaxBytes int
	Window   time.Duration
}

// PayrollEndpoint is the standard policy for payroll result callbacks.
func PayrollEndpoint(endpointID, tenantID string, secret []byte) Endpoint {
	return Endpoint{Provider: ProviderPayroll, EndpointID: endpointID, TenantID: tenantID, Secret: secret, WebhookSchema: PayrollSchema,
		Events: map[string]Outcome{PayrollEventApplied: OutcomeApplied, PayrollEventRejected: OutcomeRejected, PayrollEventReversed: OutcomeReversed}}
}

// IAMEndpoint is the standard policy for IAM access result callbacks.
func IAMEndpoint(endpointID, tenantID string, secret []byte) Endpoint {
	return Endpoint{Provider: ProviderIAM, EndpointID: endpointID, TenantID: tenantID, Secret: secret, WebhookSchema: IAMSchema,
		Events: map[string]Outcome{IAMEventGranted: OutcomeGranted, IAMEventRejected: OutcomeRejected, IAMEventRevoked: OutcomeRevoked}}
}

// Parsed is one verified, decoded callback.
type Parsed struct {
	Provider       string
	EventID        string
	EventType      string
	Schema         string
	TenantID       string
	ChangeRef      string
	CorrelationKey string
	ProviderRef    string
	Reason         string
	Outcome        Outcome
	// Payload is a copy of the exact signed body bytes.
	Payload []byte
	// PayloadDigest is "sha256:" + hex of Payload.
	PayloadDigest string
	// Details carries the provider-specific fields: payroll amount,
	// currency, effective_date, occurred_at; IAM job_code, grade,
	// effective_date, occurred_at; and, on a reversal or revocation event,
	// reversal_reason.
	Details map[string]string
	// SecretIndex is the index in Endpoint.Secrets of the secret that
	// verified this delivery (0 = current), for rotation telemetry. It is
	// not part of the event's identity: the same event signed with another
	// listed secret is the same receipt.
	SecretIndex int
}

// Verifier verifies callbacks for one endpoint. It is safe for concurrent
// use. Its duplicate memory lives in its own in-memory webhook.Store, so it
// spans only this Verifier's lifetime; durable dedupe is the caller's store.
//
// Rotation: the Verifier checks the signature against each of the
// endpoint's secrets itself, then hands the store a copy of the request
// re-signed with a private per-Verifier relay key, the only key the store
// knows. The store therefore keeps judging tenant, size, replay window,
// schema and duplicates exactly as before, keyed by event id over the event
// bytes (never the signature), so one event is one receipt whichever
// listed secret signed it. A request no secret verifies is passed on
// unchanged and fails the store's signature check after its tenant, window
// and schema checks, preserving their precedence.
type Verifier struct {
	ep      Endpoint
	secrets [][]byte
	// relayKey is the random key the store verifies against; it never
	// leaves the Verifier.
	relayKey []byte
	store    *webhook.Store
}

// NewVerifier validates ep, applies defaults and registers it.
func NewVerifier(ep Endpoint) (*Verifier, error) {
	return NewVerifierWithRegistry(ep, idempotency.NewRegistry())
}

// NewVerifierWithRegistry composes callback verification with a shared
// idempotency lifecycle. Runtime receivers must pass a registry backed by a
// durable store; this constructor refuses a nil registry rather than silently
// falling back to process-local replay protection.
func NewVerifierWithRegistry(ep Endpoint, lifecycle *idempotency.Registry) (*Verifier, error) {
	if lifecycle == nil {
		return nil, fmt.Errorf("%w: durable idempotency registry is required", ErrInvalidEndpoint)
	}
	if ep.MaxBytes == 0 {
		ep.MaxBytes = DefaultMaxBytes
	}
	if ep.Window == 0 {
		ep.Window = DefaultWindow
	}
	var allowed map[Outcome]bool
	switch ep.Provider {
	case ProviderPayroll:
		allowed = map[Outcome]bool{OutcomeApplied: true, OutcomeRejected: true, OutcomeReversed: true}
	case ProviderIAM:
		allowed = map[Outcome]bool{OutcomeGranted: true, OutcomeRejected: true, OutcomeRevoked: true}
	default:
		return nil, fmt.Errorf("%w: provider %q", ErrInvalidEndpoint, ep.Provider)
	}
	switch {
	case strings.TrimSpace(ep.EndpointID) == "", strings.TrimSpace(ep.TenantID) == "":
		return nil, fmt.Errorf("%w: endpoint id and tenant id are required", ErrInvalidEndpoint)
	case strings.TrimSpace(ep.WebhookSchema) == "":
		return nil, fmt.Errorf("%w: schema is required", ErrInvalidEndpoint)
	case ep.MaxBytes < 0 || ep.Window < 0:
		return nil, fmt.Errorf("%w: limits must be positive", ErrInvalidEndpoint)
	case len(ep.Events) == 0:
		return nil, fmt.Errorf("%w: events are required", ErrInvalidEndpoint)
	}
	secrets, err := endpointSecrets(ep)
	if err != nil {
		return nil, err
	}
	events := make(map[string]Outcome, len(ep.Events))
	for name, outcome := range ep.Events {
		if strings.TrimSpace(name) == "" || !allowed[outcome] {
			return nil, fmt.Errorf("%w: event %q -> %q not valid for %s", ErrInvalidEndpoint, name, outcome, ep.Provider)
		}
		events[name] = outcome
	}
	ep.Events = events
	ep.Secrets, ep.Secret = nil, nil // the copies in secrets are authoritative
	relayKey := make([]byte, 32)
	if _, err := rand.Read(relayKey); err != nil {
		return nil, fmt.Errorf("%w: relay key: %v", ErrInvalidEndpoint, err)
	}
	store := webhook.NewStoreWithRegistry(lifecycle)
	if err := store.RegisterEndpoint(webhook.Endpoint{ID: ep.EndpointID, TenantID: ep.TenantID, ConnectionID: ep.Provider + ":" + ep.EndpointID,
		Secret: relayKey, AllowedSchemas: []string{ep.WebhookSchema}, MaxPayloadBytes: ep.MaxBytes, ReplayWindow: ep.Window}); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidEndpoint, err)
	}
	return &Verifier{ep: ep, secrets: secrets, relayKey: relayKey, store: store}, nil
}

// endpointSecrets resolves and copies the accepted secrets: Secrets, or
// Secret alone when Secrets is empty. It refuses an empty list, a secret
// shorter than MinSecretLen, and a Secret that contradicts Secrets[0].
func endpointSecrets(ep Endpoint) ([][]byte, error) {
	list := ep.Secrets
	if len(list) == 0 {
		if len(ep.Secret) == 0 {
			return nil, fmt.Errorf("%w: at least one secret is required", ErrInvalidEndpoint)
		}
		list = [][]byte{ep.Secret}
	} else if len(ep.Secret) > 0 && !hmac.Equal(ep.Secret, list[0]) {
		return nil, fmt.Errorf("%w: Secret must equal Secrets[0] when both are set", ErrInvalidEndpoint)
	}
	out := make([][]byte, len(list))
	for i, secret := range list {
		if len(secret) < MinSecretLen {
			return nil, fmt.Errorf("%w: secret %d is shorter than %d bytes", ErrInvalidEndpoint, i, MinSecretLen)
		}
		out[i] = append([]byte(nil), secret...)
	}
	return out, nil
}

// matchSecret returns the index of the first secret whose signature over req
// equals req.Signature, or -1. Every secret is always checked, so the time
// taken does not reveal which one matched.
func (v *Verifier) matchSecret(req webhook.Request) int {
	got, err := hex.DecodeString(req.Signature)
	if err != nil {
		return -1
	}
	match := -1
	for i, secret := range v.secrets {
		want, _ := hex.DecodeString(webhook.Sign(secret, req))
		if hmac.Equal(got, want) && match < 0 {
			match = i
		}
	}
	return match
}

// resultBody is the union of the two provider payload shapes.
type resultBody struct {
	EventID        string `json:"event_id"`
	ChangeRef      string `json:"change_ref"`
	CorrelationKey string `json:"correlation_key"`
	ProviderRef    string `json:"provider_ref"`
	Outcome        string `json:"outcome"`
	Reason         string `json:"reason"`
	EffectiveDate  string `json:"effective_date"`
	OccurredAt     string `json:"occurred_at"`
	BasePay        *struct {
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	} `json:"base_pay"`
	JobCode        string `json:"job_code"`
	Grade          string `json:"grade"`
	ReversalReason string `json:"reversal_reason"`
}

// Parse verifies and decodes one callback. wallNow is the receiver's clock,
// judged against Webhook-Timestamp. A byte-identical redelivery of an event
// this Verifier already accepted parses again successfully; the same event
// id with other bytes fails with [ErrDuplicateDifferent].
func (v *Verifier) Parse(h http.Header, body []byte, wallNow time.Time) (Parsed, error) {
	if len(body) > v.ep.MaxBytes {
		return Parsed{}, ErrTooLarge
	}
	req := webhook.Request{EndpointID: v.ep.EndpointID, Payload: body}
	for _, f := range []struct {
		name string
		dst  *string
	}{{HeaderID, &req.EventID}, {HeaderEvent, &req.EventType}, {HeaderSchema, &req.Schema}, {HeaderTenant, &req.TenantID}, {HeaderSignature, &req.Signature}} {
		*f.dst = strings.TrimSpace(h.Get(f.name))
		if *f.dst == "" {
			return Parsed{}, fmt.Errorf("%w: missing %s header", ErrMalformed, f.name)
		}
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(h.Get(HeaderTimestamp)), 10, 64)
	if err != nil || ts <= 0 {
		return Parsed{}, fmt.Errorf("%w: %s must be unix nanoseconds", ErrMalformed, HeaderTimestamp)
	}
	req.Timestamp = time.Unix(0, ts)
	req.Correlation = req.EventID
	secretIndex := v.matchSecret(req)
	relayed := req
	if secretIndex >= 0 {
		relayed.Signature = webhook.Sign(v.relayKey, req)
	}
	if _, err := v.store.Receive(relayed, wallNow); err != nil {
		return Parsed{}, mapWebhookErr(err)
	}
	if secretIndex < 0 {
		// Unreachable while the relay key stays private: the store cannot
		// verify a signature none of our secrets made. Fail closed anyway.
		return Parsed{}, ErrBadSignature
	}

	outcome, ok := v.ep.Events[req.EventType]
	if !ok {
		return Parsed{}, fmt.Errorf("%w: %q", ErrUnknownEvent, req.EventType)
	}
	var b resultBody
	if err := json.Unmarshal(body, &b); err != nil {
		return Parsed{}, fmt.Errorf("%w: body: %v", ErrMalformed, err)
	}
	switch {
	case b.EventID != req.EventID:
		return Parsed{}, fmt.Errorf("%w: event_id %q does not match %s %q", ErrMalformed, b.EventID, HeaderID, req.EventID)
	case strings.TrimSpace(b.ChangeRef) == "":
		return Parsed{}, fmt.Errorf("%w: change_ref is required", ErrMalformed)
	case strings.TrimSpace(b.CorrelationKey) == "":
		return Parsed{}, fmt.Errorf("%w: correlation_key is required", ErrMalformed)
	case Outcome(b.Outcome) != outcome:
		return Parsed{}, fmt.Errorf("%w: event %s requires %s, payload says %q", ErrOutcomeMismatch, req.EventType, outcome, b.Outcome)
	case b.ReversalReason != "" && !isUndo(outcome):
		return Parsed{}, fmt.Errorf("%w: reversal_reason on a %s event", ErrOutcomeMismatch, outcome)
	}
	details := map[string]string{"effective_date": b.EffectiveDate, "occurred_at": b.OccurredAt}
	if isUndo(outcome) {
		details["reversal_reason"] = b.ReversalReason
	}
	switch v.ep.Provider {
	case ProviderPayroll:
		if b.BasePay != nil {
			details["amount"], details["currency"] = b.BasePay.Amount, b.BasePay.Currency
		}
	case ProviderIAM:
		details["job_code"], details["grade"] = b.JobCode, b.Grade
	}
	sum := sha256.Sum256(body)
	return Parsed{
		Provider: v.ep.Provider, EventID: req.EventID, EventType: req.EventType, Schema: req.Schema, TenantID: req.TenantID,
		ChangeRef: b.ChangeRef, CorrelationKey: b.CorrelationKey, ProviderRef: b.ProviderRef, Reason: b.Reason, Outcome: outcome,
		Payload: append([]byte(nil), body...), PayloadDigest: "sha256:" + hex.EncodeToString(sum[:]), Details: details,
		SecretIndex: secretIndex,
	}, nil
}

// isUndo reports the outcomes that undo an earlier APPLIED or GRANTED.
func isUndo(o Outcome) bool { return o == OutcomeReversed || o == OutcomeRevoked }

func mapWebhookErr(err error) error {
	var ours error
	switch {
	case errors.Is(err, webhook.ErrInvalidSignature):
		ours = ErrBadSignature
	case errors.Is(err, webhook.ErrOutsideReplayWindow):
		ours = ErrOutsideWindow
	case errors.Is(err, webhook.ErrUnknownSchema):
		ours = ErrUnknownSchema
	case errors.Is(err, webhook.ErrCrossTenant):
		ours = ErrWrongTenant
	case errors.Is(err, webhook.ErrDuplicateDifferent):
		ours = ErrDuplicateDifferent
	case errors.Is(err, webhook.ErrPayloadTooLarge):
		ours = ErrTooLarge
	case errors.Is(err, webhook.ErrInvalidRequest):
		ours = ErrMalformed
	default:
		return fmt.Errorf("providerreceipt: receive: %w", err)
	}
	return fmt.Errorf("%w (%v)", ours, err)
}
