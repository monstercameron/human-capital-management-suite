// Package messagingmeta is the tenant-scoped store for the communication
// metadata migration 00031 creates (DB-014).
//
// It owns seven tables: message intents, delivery endpoints, per-recipient
// messages, delivery attempts and the provider receipts that observe them,
// and the conversation thread/participant pair.
//
// Two rules it refuses to let a caller break, both also enforced by the
// schema:
//
//   - An endpoint stores the digest of its address, never the address. A
//     value that still looks like an address (an "@", a "+" dialling prefix,
//     a URL scheme) is rejected as a raw contact datum.
//   - Provider acceptance is not business completion. A recipient message
//     reaches SATISFIED only through [SatisfyRecipientMessage], which
//     requires the delivery receipt whose normalized result actually
//     satisfies the intent's delivery requirement. A PROVIDER_ACCEPTED
//     attempt never satisfies anything on its own.
package messagingmeta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

var (
	// ErrNilTenant is returned when a row omits its tenant or its own id.
	ErrNilTenant = errors.New("messagingmeta: tenant or row id is nil")
	// ErrRawAddress is returned when an endpoint carries a raw address
	// instead of its digest (DB-014 RED: no raw contact datum in an
	// ordinary relational row).
	ErrRawAddress = errors.New("messagingmeta: raw address rejected, store the address digest")
	// ErrMissingCorrelation is returned when a row omits the correlation or
	// idempotency key that makes it replay-safe.
	ErrMissingCorrelation = errors.New("messagingmeta: correlation or idempotency key missing")
	// ErrMissingDeadline is returned when an attempt omits its deadline.
	ErrMissingDeadline = errors.New("messagingmeta: deadline missing")
	// ErrMissingDigest is returned when a rendered or payload digest is
	// absent.
	ErrMissingDigest = errors.New("messagingmeta: content digest missing")
	// ErrMissingClassification is returned when a row omits its
	// classification.
	ErrMissingClassification = errors.New("messagingmeta: classification missing")
	// ErrProviderAcceptanceNotCompletion is returned when a caller tries to
	// satisfy a delivery requirement without a qualifying receipt.
	ErrProviderAcceptanceNotCompletion = errors.New("messagingmeta: provider acceptance is not business completion")
	// ErrReplyRequiresThread is returned when an intent that expects a human
	// response has no canonical conversation thread on its recipient copy.
	ErrReplyRequiresThread = errors.New("messagingmeta: response-required recipient message must be bound to a conversation thread")
	// ErrInvalidEnum is returned for a value outside its declared set.
	ErrInvalidEnum = errors.New("messagingmeta: value outside its declared set")
	// ErrInvalidInterval is returned for a reversed or empty interval.
	ErrInvalidInterval = errors.New("messagingmeta: time interval invalid")
)

// ErrDetail names the field behind one of the sentinels above.
type ErrDetail struct {
	Sentinel error
	Detail   string
}

func (e ErrDetail) Error() string { return fmt.Sprintf("%v: %s", e.Sentinel, e.Detail) }

func (e ErrDetail) Unwrap() error { return e.Sentinel }

func detail(sentinel error, format string, args ...any) error {
	return ErrDetail{Sentinel: sentinel, Detail: fmt.Sprintf(format, args...)}
}

// MessagingTables is the exact set of base tables migration 00031 creates for
// the communication half of DB-014, sorted.
var MessagingTables = []string{
	"conversation_thread",
	"delivery_attempt",
	"delivery_endpoint",
	"delivery_receipt",
	"message_intent",
	"recipient_message",
	"thread_participant",
}

// MessagePurposes is the purpose taxonomy the connectivity model declares,
// duplicated by the schema's message_intent_purpose_allowed constraint.
var MessagePurposes = []string{
	"APPROVAL", "TASK", "REMINDER", "NOTICE", "LEGAL_NOTICE", "INCIDENT",
	"EMPLOYEE_MESSAGE", "WORKFLOW_UPDATE", "SURVEY_INVITATION",
	"SURVEY_REMINDER", "ANNOUNCEMENT", "RECOGNITION",
	"ACKNOWLEDGEMENT_REQUEST", "SYSTEM_ALERT", "LEGAL_CORRECTION",
}

// digestPattern is the shape of a sha256 content digest, the only form an
// address may take in delivery_endpoint.
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func ensureTenant(ctx context.Context, tx dbport.Execer, tenantID uuid.UUID) error {
	if tenantID == uuid.Nil {
		return ErrNilTenant
	}
	return tenancy.WithTenant(ctx, tx, tenantID)
}

func object(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

func oneOf(value string, allowed ...string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// message_intent
// ---------------------------------------------------------------------------

// MessageIntent is one communication a tenant means to make.
type MessageIntent struct {
	TenantID            uuid.UUID       `json:"tenant_id"`
	MessageIntentID     uuid.UUID       `json:"message_intent_id"`
	Purpose             string          `json:"purpose"`
	OrgScopeID          *uuid.UUID      `json:"org_scope_id"`
	AudienceExpression  json.RawMessage `json:"audience_expression"`
	TemplateKey         string          `json:"template_key"`
	TemplateVersion     int64           `json:"template_version"`
	Classification      string          `json:"classification"`
	Urgency             string          `json:"urgency"`
	DeliveryRequirement string          `json:"delivery_requirement"`
	ResponseRequirement string          `json:"response_requirement"`
	WorkflowRef         string          `json:"workflow_ref"`
	CorrelationKey      string          `json:"correlation_key"`
	ExpiresAt           *time.Time      `json:"expires_at"`
	CreatedAt           time.Time       `json:"created_at"`
}

func (m MessageIntent) Validate() error {
	if m.TenantID == uuid.Nil || m.MessageIntentID == uuid.Nil {
		return ErrNilTenant
	}
	if m.CorrelationKey == "" {
		return detail(ErrMissingCorrelation, "message_intent.correlation_key")
	}
	if m.Classification == "" {
		return detail(ErrMissingClassification, "message_intent.classification")
	}
	if m.TemplateVersion < 1 {
		return detail(ErrInvalidEnum, "message_intent.template_version=%d", m.TemplateVersion)
	}
	if !oneOf(m.Purpose, MessagePurposes...) {
		return detail(ErrInvalidEnum, "message_intent.purpose=%q", m.Purpose)
	}
	if !oneOf(m.DeliveryRequirement, "BEST_EFFORT", "DELIVERED", "READ", "ACKNOWLEDGED", "SIGNED") {
		return detail(ErrInvalidEnum, "message_intent.delivery_requirement=%q", m.DeliveryRequirement)
	}
	if m.ExpiresAt != nil && !m.ExpiresAt.After(m.CreatedAt) {
		return detail(ErrInvalidInterval, "message_intent.expires_at does not follow created_at")
	}
	return nil
}

func InsertMessageIntent(ctx context.Context, tx dbport.Tx, m MessageIntent) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, m.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO message_intent (tenant_id, message_intent_id, purpose, org_scope_id, audience_expression, template_key, template_version, classification, urgency, delivery_requirement, response_requirement, workflow_ref, correlation_key, expires_at, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		m.TenantID, m.MessageIntentID, m.Purpose, m.OrgScopeID, object(m.AudienceExpression), m.TemplateKey, m.TemplateVersion, m.Classification, m.Urgency, m.DeliveryRequirement, m.ResponseRequirement, m.WorkflowRef, m.CorrelationKey, m.ExpiresAt, m.CreatedAt)
	return err
}

func LoadMessageIntent(ctx context.Context, q dbport.Querier, tenantID, intentID uuid.UUID) (MessageIntent, error) {
	var m MessageIntent
	var org *uuid.UUID
	var audience []byte
	var expires *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, message_intent_id, purpose, org_scope_id, audience_expression, template_key, template_version, classification, urgency, delivery_requirement, response_requirement, workflow_ref, correlation_key, expires_at, created_at FROM message_intent WHERE tenant_id=$1 AND message_intent_id=$2`, tenantID, intentID).
		Scan(&m.TenantID, &m.MessageIntentID, &m.Purpose, &org, &audience, &m.TemplateKey, &m.TemplateVersion, &m.Classification, &m.Urgency, &m.DeliveryRequirement, &m.ResponseRequirement, &m.WorkflowRef, &m.CorrelationKey, &expires, &m.CreatedAt)
	if err != nil {
		return MessageIntent{}, err
	}
	m.OrgScopeID = org
	m.AudienceExpression = audience
	m.ExpiresAt = expires
	return m, nil
}

// ---------------------------------------------------------------------------
// delivery_endpoint
// ---------------------------------------------------------------------------

// DeliveryEndpoint is one addressable destination. AddressDigest is the
// sha256 of the address; AddressHint is a short redacted display fragment.
type DeliveryEndpoint struct {
	TenantID          uuid.UUID       `json:"tenant_id"`
	EndpointID        uuid.UUID       `json:"endpoint_id"`
	PrincipalRef      string          `json:"principal_ref"`
	Channel           string          `json:"channel"`
	AddressDigest     string          `json:"address_digest"`
	AddressHint       string          `json:"address_hint"`
	ProviderAccount   string          `json:"provider_account"`
	Ownership         string          `json:"ownership"`
	VerificationState string          `json:"verification_state"`
	PurposeScope      json.RawMessage `json:"purpose_scope"`
	Locale            string          `json:"locale"`
	EffectiveFrom     time.Time       `json:"effective_from"`
	EffectiveTo       *time.Time      `json:"effective_to"`
	Status            string          `json:"status"`
}

// Validate rejects a raw address in either the digest column or the hint.
func (e DeliveryEndpoint) Validate() error {
	if e.TenantID == uuid.Nil || e.EndpointID == uuid.Nil {
		return ErrNilTenant
	}
	if !digestPattern.MatchString(e.AddressDigest) {
		return detail(ErrRawAddress, "delivery_endpoint.address_digest=%q is not a sha256 digest", e.AddressDigest)
	}
	if len(e.AddressHint) > 32 {
		return detail(ErrRawAddress, "delivery_endpoint.address_hint is %d bytes, at most 32 allowed", len(e.AddressHint))
	}
	if strings.Contains(e.AddressHint, "://") {
		return detail(ErrRawAddress, "delivery_endpoint.address_hint %q looks like a URL, not a redacted fragment", e.AddressHint)
	}
	if !oneOf(e.Channel, "EMAIL", "SMS", "PUSH", "INBOX", "VOICE", "POSTAL", "WEBHOOK") {
		return detail(ErrInvalidEnum, "delivery_endpoint.channel=%q", e.Channel)
	}
	if !oneOf(e.Ownership, "BUSINESS", "PERSONAL") {
		return detail(ErrInvalidEnum, "delivery_endpoint.ownership=%q", e.Ownership)
	}
	if !oneOf(e.VerificationState, "UNVERIFIED", "PENDING", "VERIFIED", "EXPIRED") {
		return detail(ErrInvalidEnum, "delivery_endpoint.verification_state=%q", e.VerificationState)
	}
	if e.EffectiveTo != nil && !e.EffectiveTo.After(e.EffectiveFrom) {
		return detail(ErrInvalidInterval, "delivery_endpoint effective interval is not half-open")
	}
	return nil
}

func InsertDeliveryEndpoint(ctx context.Context, tx dbport.Tx, e DeliveryEndpoint) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, e.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO delivery_endpoint (tenant_id, endpoint_id, principal_ref, channel, address_digest, address_hint, provider_account, ownership, verification_state, purpose_scope, locale, effective_from, effective_to, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		e.TenantID, e.EndpointID, e.PrincipalRef, e.Channel, e.AddressDigest, e.AddressHint, e.ProviderAccount, e.Ownership, e.VerificationState, object(e.PurposeScope), e.Locale, e.EffectiveFrom, e.EffectiveTo, e.Status)
	return err
}

func LoadDeliveryEndpoint(ctx context.Context, q dbport.Querier, tenantID, endpointID uuid.UUID) (DeliveryEndpoint, error) {
	var e DeliveryEndpoint
	var scope []byte
	var to *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, endpoint_id, principal_ref, channel, address_digest, address_hint, provider_account, ownership, verification_state, purpose_scope, locale, effective_from, effective_to, status FROM delivery_endpoint WHERE tenant_id=$1 AND endpoint_id=$2`, tenantID, endpointID).
		Scan(&e.TenantID, &e.EndpointID, &e.PrincipalRef, &e.Channel, &e.AddressDigest, &e.AddressHint, &e.ProviderAccount, &e.Ownership, &e.VerificationState, &scope, &e.Locale, &e.EffectiveFrom, &to, &e.Status)
	if err != nil {
		return DeliveryEndpoint{}, err
	}
	e.PurposeScope = scope
	e.EffectiveTo = to
	return e, nil
}

// ---------------------------------------------------------------------------
// recipient_message
// ---------------------------------------------------------------------------

// RecipientMessage is one recipient's independent copy of an intent, with its
// own lifecycle.
type RecipientMessage struct {
	TenantID             uuid.UUID  `json:"tenant_id"`
	RecipientMessageID   uuid.UUID  `json:"recipient_message_id"`
	MessageIntentID      uuid.UUID  `json:"message_intent_id"`
	ConversationThreadID *uuid.UUID `json:"conversation_thread_id,omitempty"`
	RecipientRef         string     `json:"recipient_ref"`
	EndpointID           uuid.UUID  `json:"endpoint_id"`
	RenderedDigest       string     `json:"rendered_digest"`
	Classification       string     `json:"classification"`
	CorrelationKey       string     `json:"correlation_key"`
	RecipientState       string     `json:"recipient_state"`
	SatisfactionState    string     `json:"satisfaction_state"`
	SatisfyingReceiptID  *uuid.UUID `json:"satisfying_receipt_id"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

func (r RecipientMessage) Validate() error {
	if r.TenantID == uuid.Nil || r.RecipientMessageID == uuid.Nil || r.MessageIntentID == uuid.Nil || r.EndpointID == uuid.Nil {
		return ErrNilTenant
	}
	if r.CorrelationKey == "" {
		return detail(ErrMissingCorrelation, "recipient_message.correlation_key")
	}
	if r.RenderedDigest == "" {
		return detail(ErrMissingDigest, "recipient_message.rendered_digest")
	}
	if r.Classification == "" {
		return detail(ErrMissingClassification, "recipient_message.classification")
	}
	if !oneOf(r.RecipientState, "UNSEEN", "SEEN", "READ", "ACKNOWLEDGED", "RESPONDED") {
		return detail(ErrInvalidEnum, "recipient_message.recipient_state=%q", r.RecipientState)
	}
	if !oneOf(r.SatisfactionState, "PENDING", "SATISFIED", "INVALIDATED") {
		return detail(ErrInvalidEnum, "recipient_message.satisfaction_state=%q", r.SatisfactionState)
	}
	if r.SatisfactionState == "SATISFIED" && r.SatisfyingReceiptID == nil {
		return ErrProviderAcceptanceNotCompletion
	}
	return nil
}

func InsertRecipientMessage(ctx context.Context, tx dbport.Tx, r RecipientMessage) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, r.TenantID); err != nil {
		return err
	}
	var responseRequirement string
	if err := tx.QueryRow(ctx, `SELECT response_requirement FROM message_intent WHERE tenant_id=$1 AND message_intent_id=$2`, r.TenantID, r.MessageIntentID).Scan(&responseRequirement); err != nil {
		return fmt.Errorf("messagingmeta: load response requirement for message intent %s: %w", r.MessageIntentID, err)
	}
	if responseRequirement != "NONE" && r.ConversationThreadID == nil {
		return ErrReplyRequiresThread
	}
	_, err := tx.Exec(ctx, `INSERT INTO recipient_message (tenant_id, recipient_message_id, message_intent_id, conversation_thread_id, recipient_ref, endpoint_id, rendered_digest, classification, correlation_key, recipient_state, satisfaction_state, satisfying_receipt_id, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		r.TenantID, r.RecipientMessageID, r.MessageIntentID, r.ConversationThreadID, r.RecipientRef, r.EndpointID, r.RenderedDigest, r.Classification, r.CorrelationKey, r.RecipientState, r.SatisfactionState, r.SatisfyingReceiptID, r.CreatedAt, r.UpdatedAt)
	return err
}

func LoadRecipientMessage(ctx context.Context, q dbport.Querier, tenantID, messageID uuid.UUID) (RecipientMessage, error) {
	var r RecipientMessage
	var receipt *uuid.UUID
	err := q.QueryRow(ctx, `SELECT tenant_id, recipient_message_id, message_intent_id, conversation_thread_id, recipient_ref, endpoint_id, rendered_digest, classification, correlation_key, recipient_state, satisfaction_state, satisfying_receipt_id, created_at, updated_at FROM recipient_message WHERE tenant_id=$1 AND recipient_message_id=$2`, tenantID, messageID).
		Scan(&r.TenantID, &r.RecipientMessageID, &r.MessageIntentID, &r.ConversationThreadID, &r.RecipientRef, &r.EndpointID, &r.RenderedDigest, &r.Classification, &r.CorrelationKey, &r.RecipientState, &r.SatisfactionState, &receipt, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return RecipientMessage{}, err
	}
	r.SatisfyingReceiptID = receipt
	return r, nil
}

// ---------------------------------------------------------------------------
// delivery_attempt and delivery_receipt
// ---------------------------------------------------------------------------

// DeliveryAttempt is one immutable submission to a provider.
type DeliveryAttempt struct {
	TenantID           uuid.UUID `json:"tenant_id"`
	AttemptID          uuid.UUID `json:"attempt_id"`
	RecipientMessageID uuid.UUID `json:"recipient_message_id"`
	EndpointID         uuid.UUID `json:"endpoint_id"`
	Provider           string    `json:"provider"`
	IdempotencyKey     string    `json:"idempotency_key"`
	PayloadDigest      string    `json:"payload_digest"`
	State              string    `json:"state"`
	ProviderMessageID  string    `json:"provider_message_id"`
	SubmittedAt        time.Time `json:"submitted_at"`
	DeadlineAt         time.Time `json:"deadline_at"`
}

func (a DeliveryAttempt) Validate() error {
	if a.TenantID == uuid.Nil || a.AttemptID == uuid.Nil || a.RecipientMessageID == uuid.Nil || a.EndpointID == uuid.Nil {
		return ErrNilTenant
	}
	if a.IdempotencyKey == "" {
		return detail(ErrMissingCorrelation, "delivery_attempt.idempotency_key")
	}
	if a.PayloadDigest == "" {
		return detail(ErrMissingDigest, "delivery_attempt.payload_digest")
	}
	if a.DeadlineAt.IsZero() || !a.DeadlineAt.After(a.SubmittedAt) {
		return detail(ErrMissingDeadline, "delivery_attempt.deadline_at")
	}
	if !oneOf(a.State, "QUEUED", "SUBMITTED", "PROVIDER_ACCEPTED", "DELIVERED", "BOUNCED", "REJECTED", "EXPIRED", "FAILED", "AMBIGUOUS") {
		return detail(ErrInvalidEnum, "delivery_attempt.state=%q", a.State)
	}
	return nil
}

func InsertDeliveryAttempt(ctx context.Context, tx dbport.Tx, a DeliveryAttempt) error {
	if err := a.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, a.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO delivery_attempt (tenant_id, attempt_id, recipient_message_id, endpoint_id, provider, idempotency_key, payload_digest, state, provider_message_id, submitted_at, deadline_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		a.TenantID, a.AttemptID, a.RecipientMessageID, a.EndpointID, a.Provider, a.IdempotencyKey, a.PayloadDigest, a.State, a.ProviderMessageID, a.SubmittedAt, a.DeadlineAt)
	return err
}

func LoadDeliveryAttempt(ctx context.Context, q dbport.Querier, tenantID, attemptID uuid.UUID) (DeliveryAttempt, error) {
	var a DeliveryAttempt
	err := q.QueryRow(ctx, `SELECT tenant_id, attempt_id, recipient_message_id, endpoint_id, provider, idempotency_key, payload_digest, state, provider_message_id, submitted_at, deadline_at FROM delivery_attempt WHERE tenant_id=$1 AND attempt_id=$2`, tenantID, attemptID).
		Scan(&a.TenantID, &a.AttemptID, &a.RecipientMessageID, &a.EndpointID, &a.Provider, &a.IdempotencyKey, &a.PayloadDigest, &a.State, &a.ProviderMessageID, &a.SubmittedAt, &a.DeadlineAt)
	if err != nil {
		return DeliveryAttempt{}, err
	}
	return a, nil
}

// DeliveryReceipt is an authenticated provider observation of an attempt.
type DeliveryReceipt struct {
	TenantID          uuid.UUID `json:"tenant_id"`
	ReceiptID         uuid.UUID `json:"receipt_id"`
	AttemptID         uuid.UUID `json:"attempt_id"`
	ProviderEventID   string    `json:"provider_event_id"`
	EventType         string    `json:"event_type"`
	EventTime         time.Time `json:"event_time"`
	ReceivedAt        time.Time `json:"received_at"`
	SignatureResult   string    `json:"signature_result"`
	SequenceNo        int64     `json:"sequence_no"`
	DedupeState       string    `json:"dedupe_state"`
	NormalizedResult  string    `json:"normalized_result"`
	RawArtifactDigest string    `json:"raw_artifact_digest"`
}

func (r DeliveryReceipt) Validate() error {
	if r.TenantID == uuid.Nil || r.ReceiptID == uuid.Nil || r.AttemptID == uuid.Nil {
		return ErrNilTenant
	}
	if r.ProviderEventID == "" {
		return detail(ErrMissingCorrelation, "delivery_receipt.provider_event_id")
	}
	if r.RawArtifactDigest == "" {
		return detail(ErrMissingDigest, "delivery_receipt.raw_artifact_digest")
	}
	if r.SequenceNo < 1 {
		return detail(ErrInvalidEnum, "delivery_receipt.sequence_no=%d", r.SequenceNo)
	}
	if !oneOf(r.SignatureResult, "VALID", "INVALID", "ABSENT") {
		return detail(ErrInvalidEnum, "delivery_receipt.signature_result=%q", r.SignatureResult)
	}
	if !oneOf(r.DedupeState, "FIRST", "DUPLICATE", "OUT_OF_ORDER") {
		return detail(ErrInvalidEnum, "delivery_receipt.dedupe_state=%q", r.DedupeState)
	}
	if !oneOf(r.NormalizedResult, "DELIVERED", "READ", "ACKNOWLEDGED", "BOUNCED", "REJECTED", "EXPIRED", "UNKNOWN") {
		return detail(ErrInvalidEnum, "delivery_receipt.normalized_result=%q", r.NormalizedResult)
	}
	if r.ReceivedAt.Before(r.EventTime) {
		return detail(ErrInvalidInterval, "delivery_receipt.received_at precedes event_time")
	}
	return nil
}

func InsertDeliveryReceipt(ctx context.Context, tx dbport.Tx, r DeliveryReceipt) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, r.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO delivery_receipt (tenant_id, receipt_id, attempt_id, provider_event_id, event_type, event_time, received_at, signature_result, sequence_no, dedupe_state, normalized_result, raw_artifact_digest) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		r.TenantID, r.ReceiptID, r.AttemptID, r.ProviderEventID, r.EventType, r.EventTime, r.ReceivedAt, r.SignatureResult, r.SequenceNo, r.DedupeState, r.NormalizedResult, r.RawArtifactDigest)
	return err
}

func LoadDeliveryReceipt(ctx context.Context, q dbport.Querier, tenantID, receiptID uuid.UUID) (DeliveryReceipt, error) {
	var r DeliveryReceipt
	err := q.QueryRow(ctx, `SELECT tenant_id, receipt_id, attempt_id, provider_event_id, event_type, event_time, received_at, signature_result, sequence_no, dedupe_state, normalized_result, raw_artifact_digest FROM delivery_receipt WHERE tenant_id=$1 AND receipt_id=$2`, tenantID, receiptID).
		Scan(&r.TenantID, &r.ReceiptID, &r.AttemptID, &r.ProviderEventID, &r.EventType, &r.EventTime, &r.ReceivedAt, &r.SignatureResult, &r.SequenceNo, &r.DedupeState, &r.NormalizedResult, &r.RawArtifactDigest)
	if err != nil {
		return DeliveryReceipt{}, err
	}
	return r, nil
}

// satisfyingResults maps a delivery requirement to the normalized receipt
// results that actually satisfy it. PROVIDER_ACCEPTED is not one of them at
// any level: it is an attempt state, not an observed outcome.
var satisfyingResults = map[string][]string{
	"BEST_EFFORT":  {"DELIVERED", "READ", "ACKNOWLEDGED"},
	"DELIVERED":    {"DELIVERED", "READ", "ACKNOWLEDGED"},
	"READ":         {"READ", "ACKNOWLEDGED"},
	"ACKNOWLEDGED": {"ACKNOWLEDGED"},
	"SIGNED":       {"ACKNOWLEDGED"},
}

// SatisfyRecipientMessage is the only path to SATISFIED. It requires a
// receipt that belongs to an attempt on this very message, whose signature
// verified, and whose normalized result meets the intent's own delivery
// requirement. Anything less is provider acceptance, not completion.
func SatisfyRecipientMessage(ctx context.Context, tx dbport.Tx, tenantID, messageID, receiptID uuid.UUID, at time.Time) error {
	if tenantID == uuid.Nil || messageID == uuid.Nil {
		return ErrNilTenant
	}
	if receiptID == uuid.Nil {
		return ErrProviderAcceptanceNotCompletion
	}
	if err := ensureTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	var owner uuid.UUID
	var normalized, signature string
	err := tx.QueryRow(ctx, `
		SELECT a.recipient_message_id, r.normalized_result, r.signature_result
		FROM delivery_receipt r
		JOIN delivery_attempt a ON a.tenant_id = r.tenant_id AND a.attempt_id = r.attempt_id
		WHERE r.tenant_id=$1 AND r.receipt_id=$2`, tenantID, receiptID).Scan(&owner, &normalized, &signature)
	if err != nil {
		return err
	}
	if owner != messageID {
		return detail(ErrProviderAcceptanceNotCompletion, "receipt %s observes message %s", receiptID, owner)
	}
	if signature != "VALID" {
		return detail(ErrProviderAcceptanceNotCompletion, "receipt %s signature is %s", receiptID, signature)
	}
	var requirement string
	if err := tx.QueryRow(ctx, `
		SELECT i.delivery_requirement
		FROM recipient_message m
		JOIN message_intent i ON i.tenant_id = m.tenant_id AND i.message_intent_id = m.message_intent_id
		WHERE m.tenant_id=$1 AND m.recipient_message_id=$2`, tenantID, messageID).Scan(&requirement); err != nil {
		return err
	}
	if !oneOf(normalized, satisfyingResults[requirement]...) {
		return detail(ErrProviderAcceptanceNotCompletion, "%s result does not satisfy a %s requirement", normalized, requirement)
	}
	affected, err := tx.Exec(ctx, `UPDATE recipient_message SET satisfaction_state='SATISFIED', satisfying_receipt_id=$3, updated_at=$4 WHERE tenant_id=$1 AND recipient_message_id=$2`, tenantID, messageID, receiptID, at)
	if err != nil {
		return err
	}
	if affected == 0 {
		return dbport.ErrNoRows
	}
	return nil
}

// ---------------------------------------------------------------------------
// conversation_thread and thread_participant
// ---------------------------------------------------------------------------

// ConversationThread groups related messages around one subject or matter.
type ConversationThread struct {
	TenantID       uuid.UUID  `json:"tenant_id"`
	ThreadID       uuid.UUID  `json:"thread_id"`
	SubjectRef     string     `json:"subject_ref"`
	MatterRef      string     `json:"matter_ref"`
	WorkflowRef    string     `json:"workflow_ref"`
	Purpose        string     `json:"purpose"`
	Classification string     `json:"classification"`
	OpenedAt       time.Time  `json:"opened_at"`
	ClosedAt       *time.Time `json:"closed_at"`
	Status         string     `json:"status"`
}

func (t ConversationThread) Validate() error {
	if t.TenantID == uuid.Nil || t.ThreadID == uuid.Nil {
		return ErrNilTenant
	}
	if t.Classification == "" {
		return detail(ErrMissingClassification, "conversation_thread.classification")
	}
	if !oneOf(t.Status, "OPEN", "CLOSED", "MERGED", "SPLIT") {
		return detail(ErrInvalidEnum, "conversation_thread.status=%q", t.Status)
	}
	if t.Status == "CLOSED" && t.ClosedAt == nil {
		return detail(ErrInvalidInterval, "conversation_thread closed without a closed_at")
	}
	if t.ClosedAt != nil && t.ClosedAt.Before(t.OpenedAt) {
		return detail(ErrInvalidInterval, "conversation_thread.closed_at precedes opened_at")
	}
	return nil
}

func InsertConversationThread(ctx context.Context, tx dbport.Tx, t ConversationThread) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, t.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO conversation_thread (tenant_id, thread_id, subject_ref, matter_ref, workflow_ref, purpose, classification, opened_at, closed_at, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		t.TenantID, t.ThreadID, t.SubjectRef, t.MatterRef, t.WorkflowRef, t.Purpose, t.Classification, t.OpenedAt, t.ClosedAt, t.Status)
	return err
}

func LoadConversationThread(ctx context.Context, q dbport.Querier, tenantID, threadID uuid.UUID) (ConversationThread, error) {
	var t ConversationThread
	var closed *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, thread_id, subject_ref, matter_ref, workflow_ref, purpose, classification, opened_at, closed_at, status FROM conversation_thread WHERE tenant_id=$1 AND thread_id=$2`, tenantID, threadID).
		Scan(&t.TenantID, &t.ThreadID, &t.SubjectRef, &t.MatterRef, &t.WorkflowRef, &t.Purpose, &t.Classification, &t.OpenedAt, &closed, &t.Status)
	if err != nil {
		return ConversationThread{}, err
	}
	t.ClosedAt = closed
	return t, nil
}

// ThreadParticipant is one principal's bounded membership in a thread, with
// the authorization snapshot that admitted them and how much history they
// may see.
type ThreadParticipant struct {
	TenantID             uuid.UUID  `json:"tenant_id"`
	ParticipantID        uuid.UUID  `json:"participant_id"`
	ThreadID             uuid.UUID  `json:"thread_id"`
	PrincipalRef         string     `json:"principal_ref"`
	ParticipantRole      string     `json:"participant_role"`
	MembershipFrom       time.Time  `json:"membership_from"`
	MembershipTo         *time.Time `json:"membership_to"`
	AuthorizationDigest  string     `json:"authorization_digest"`
	HistoricalVisibility string     `json:"historical_visibility"`
	Status               string     `json:"status"`
}

func (p ThreadParticipant) Validate() error {
	if p.TenantID == uuid.Nil || p.ParticipantID == uuid.Nil || p.ThreadID == uuid.Nil {
		return ErrNilTenant
	}
	if p.AuthorizationDigest == "" {
		return detail(ErrMissingDigest, "thread_participant.authorization_digest")
	}
	if !oneOf(p.ParticipantRole, "OWNER", "MEMBER", "OBSERVER", "DELEGATE") {
		return detail(ErrInvalidEnum, "thread_participant.participant_role=%q", p.ParticipantRole)
	}
	if !oneOf(p.HistoricalVisibility, "NONE", "FROM_JOIN", "FULL_HISTORY") {
		return detail(ErrInvalidEnum, "thread_participant.historical_visibility=%q", p.HistoricalVisibility)
	}
	if p.MembershipTo != nil && !p.MembershipTo.After(p.MembershipFrom) {
		return detail(ErrInvalidInterval, "thread_participant membership interval is not half-open")
	}
	return nil
}

func InsertThreadParticipant(ctx context.Context, tx dbport.Tx, p ThreadParticipant) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, p.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO thread_participant (tenant_id, participant_id, thread_id, principal_ref, participant_role, membership_from, membership_to, authorization_digest, historical_visibility, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		p.TenantID, p.ParticipantID, p.ThreadID, p.PrincipalRef, p.ParticipantRole, p.MembershipFrom, p.MembershipTo, p.AuthorizationDigest, p.HistoricalVisibility, p.Status)
	return err
}

func LoadThreadParticipant(ctx context.Context, q dbport.Querier, tenantID, participantID uuid.UUID) (ThreadParticipant, error) {
	var p ThreadParticipant
	var to *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, participant_id, thread_id, principal_ref, participant_role, membership_from, membership_to, authorization_digest, historical_visibility, status FROM thread_participant WHERE tenant_id=$1 AND participant_id=$2`, tenantID, participantID).
		Scan(&p.TenantID, &p.ParticipantID, &p.ThreadID, &p.PrincipalRef, &p.ParticipantRole, &p.MembershipFrom, &to, &p.AuthorizationDigest, &p.HistoricalVisibility, &p.Status)
	if err != nil {
		return ThreadParticipant{}, err
	}
	p.MembershipTo = to
	return p, nil
}
