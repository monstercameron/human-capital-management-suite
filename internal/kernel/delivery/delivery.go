// Package delivery holds the pure value types of the provider-neutral
// messaging delivery boundary: the semantic envelope, its channels and states,
// the provider result and error, and the claim and observation a delivery
// attempt records.
//
// They live apart from internal/connectivity/delivery, whose Runner, attempt
// store and idempotency registry are server machinery. The browser client
// reaches these types through procurement, so they must not drag the store,
// the tenancy layer or the idempotency registry into the client build. The
// connectivity package re-exports every type here as an alias.
package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Channel string

const (
	ChannelEmail Channel = "EMAIL"
	ChannelSMS   Channel = "SMS"
	ChannelPush  Channel = "PUSH"
	ChannelInbox Channel = "INBOX"
)

type State string

const (
	StateSubmitted       State = "SUBMITTED"
	StateFailed          State = "FAILED"
	StateReviewRequired  State = "REVIEW_REQUIRED"
	StateAlreadyObserved State = "ALREADY_OBSERVED"
)

// ErrInvalidEnvelope is matchable with errors.Is.
var ErrInvalidEnvelope = errors.New("delivery: invalid semantic envelope")

// Envelope contains only semantic references and a content digest. It has no
// provider address, credential, rendered body or arbitrary payload field.
type Envelope struct {
	TenantID       string
	IntentID       string
	RecipientRef   string
	EndpointRef    string
	ContentRef     string
	TemplateRef    string
	ParametersRef  string
	Purpose        string
	Classification string
	Channel        Channel
	IdempotencyKey string
	CorrelationID  string
	ContentDigest  string
	AvailableAt    time.Time
	ExpiresAt      time.Time
}

func (e Envelope) Validate(now time.Time) error {
	for name, value := range map[string]string{
		"tenant_id": e.TenantID, "intent_id": e.IntentID, "recipient_ref": e.RecipientRef,
		"endpoint_ref": e.EndpointRef, "purpose": e.Purpose, "classification": e.Classification,
		"idempotency_key": e.IdempotencyKey, "correlation_id": e.CorrelationID, "content_digest": e.ContentDigest,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidEnvelope, name)
		}
	}
	if e.ContentRef == "" && e.TemplateRef == "" {
		return fmt.Errorf("%w: content_ref or template_ref is required", ErrInvalidEnvelope)
	}
	switch e.Channel {
	case ChannelEmail, ChannelSMS, ChannelPush, ChannelInbox:
	default:
		return fmt.Errorf("%w: unsupported channel %q", ErrInvalidEnvelope, e.Channel)
	}
	if e.ExpiresAt.IsZero() || !e.ExpiresAt.After(now) {
		return fmt.Errorf("%w: envelope is expired or has no expiry", ErrInvalidEnvelope)
	}
	return nil
}

type ProviderResult struct{ ProviderRef, ObservationRef, Code string }

type ProviderError struct {
	Err                  error
	Retryable, Ambiguous bool
}

func (e *ProviderError) Error() string {
	if e == nil || e.Err == nil {
		return "delivery: provider error"
	}
	return e.Err.Error()
}

func (e *ProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type Transport interface {
	Deliver(context.Context, Envelope) (ProviderResult, error)
}

type Claim struct {
	AttemptID       string
	Attempt         int
	AlreadyObserved bool
}

type Observation struct {
	AttemptID      string
	Attempt        int
	State          State
	ProviderRef    string
	ObservationRef string
	ProviderCode   string
	Reason         string
	RecordedAt     time.Time
}
