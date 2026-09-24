// Package delivery dispatches committed MessageIntents asynchronously through
// a replaceable provider port.
package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/idempotency"
)

// Version is the contract version of asynchronous delivery.
func Version() int { return 1 }

var (
	ErrInvalidIntent         = errors.New("delivery: invalid message intent")
	ErrNotCommitted          = errors.New("delivery: intent/outbox commit is required before provider call")
	ErrIdempotencyConflict   = errors.New("delivery: idempotency key is bound to different request")
	ErrIdempotencyInProgress = errors.New("delivery: idempotent dispatch is already in progress")
	ErrDLPBlocked            = errors.New("delivery: protected content cannot be sent through provider")
	ErrProviderFailed        = errors.New("delivery: provider attempt failed")
)

// Intent is the semantic message committed by the caller's transaction.
type Intent struct {
	TenantID         string
	IntentID         string
	RecipientRef     string
	Purpose          string
	Subject          string
	Body             string
	Classification   string
	IdempotencyKey   string
	CanonicalRequest []byte
	Committed        bool
	CreatedAt        time.Time
}

// Policy controls the provider-visible content boundary.
type Policy struct {
	ProviderMaximumClassification string
	AttentionOnlyClassifications  map[string]bool
}

// Delivery is the provider-facing attention message. Protected content is
// represented by an empty body and cannot bypass this boundary.
type Delivery struct {
	TenantID       string
	IntentID       string
	RecipientRef   string
	Purpose        string
	Subject        string
	Body           string
	Classification string
	AttentionOnly  bool
	IdempotencyKey string
}

// Provider is deliberately small and replaceable; it is called only after
// the semantic intent has been committed.
type Provider interface {
	Send(context.Context, Delivery) (ProviderResult, error)
}

type ProviderResult struct {
	Reference string
	Accepted  bool
}

type State string

const (
	Queued             State = "QUEUED"
	Submitted          State = "SUBMITTED"
	AcceptedByProvider State = "ACCEPTED_BY_PROVIDER"
	Failed             State = "FAILED"
)

// Attempt is the durable-shaped logical delivery attempt. A repeated intent
// returns this same attempt rather than invoking the provider twice.
type Attempt struct {
	ID             string    `json:"attempt_id"`
	TenantID       string    `json:"tenant_id"`
	IntentID       string    `json:"intent_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	RequestHash    string    `json:"request_hash"`
	State          State     `json:"state"`
	ProviderRef    string    `json:"provider_ref,omitempty"`
	AttentionOnly  bool      `json:"attention_only"`
	ErrorCode      string    `json:"error_code,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type DispatchResult struct{ Attempt Attempt }

type Dispatcher struct {
	provider  Provider
	policy    Policy
	mu        sync.Mutex
	attempts  map[string]Attempt
	lifecycle *idempotency.Registry
}

func NewDispatcher(provider Provider, policy Policy) (*Dispatcher, error) {
	return NewDispatcherWithRegistry(provider, policy, idempotency.NewRegistry())
}

// NewDispatcherWithRegistry binds dispatch dedupe to the caller's lifecycle.
// Production composition must inject a registry backed by the durable
// platform store; NewDispatcher remains a process-local convenience for
// isolated tests and simulations.
func NewDispatcherWithRegistry(provider Provider, policy Policy, lifecycle *idempotency.Registry) (*Dispatcher, error) {
	if provider == nil {
		return nil, errors.New("delivery: nil provider")
	}
	if lifecycle == nil {
		return nil, errors.New("delivery: idempotency registry is required")
	}
	if strings.TrimSpace(policy.ProviderMaximumClassification) == "" {
		policy.ProviderMaximumClassification = "INTERNAL"
	}
	if policy.AttentionOnlyClassifications == nil {
		policy.AttentionOnlyClassifications = map[string]bool{"CONFIDENTIAL": true, "RESTRICTED": true}
	}
	return &Dispatcher{provider: provider, policy: policy, attempts: make(map[string]Attempt), lifecycle: lifecycle}, nil
}

// Dispatch performs admission synchronously but invokes the provider only as
// the asynchronous worker boundary. It is safe to call repeatedly for the
// same logical intent.
func (d *Dispatcher) Dispatch(ctx context.Context, in Intent) (DispatchResult, error) {
	if d == nil || d.provider == nil || strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.IntentID) == "" || strings.TrimSpace(in.RecipientRef) == "" || strings.TrimSpace(in.Purpose) == "" || strings.TrimSpace(in.IdempotencyKey) == "" || len(in.CanonicalRequest) == 0 {
		return DispatchResult{}, ErrInvalidIntent
	}
	if !in.Committed {
		return DispatchResult{}, ErrNotCommitted
	}
	hash := requestHash(in.CanonicalRequest)
	identity := in.TenantID + "\x00" + in.IdempotencyKey
	d.mu.Lock()
	if old, ok := d.attempts[identity]; ok {
		if old.RequestHash != hash {
			d.mu.Unlock()
			return DispatchResult{}, ErrIdempotencyConflict
		}
		d.mu.Unlock()
		if old.State == Failed {
			return DispatchResult{Attempt: old}, ErrProviderFailed
		}
		return DispatchResult{Attempt: old}, nil
	}
	now := in.CreatedAt.UTC()
	if now.IsZero() {
		now = time.Unix(0, 0).UTC()
	}
	lifecycleIdentity := idempotency.Identity{Tenant: in.TenantID, Capability: "communications.notify", EffectScope: "intent:" + in.IntentID, Key: in.IdempotencyKey}
	reservation, reserveErr := d.lifecycle.Reserve(idempotency.Request{Identity: lifecycleIdentity, Layer: "message-intent", Canonical: in.CanonicalRequest, Retention: idempotency.RetentionPolicy{ExpiresAt: now.Add(24 * time.Hour), Mode: idempotency.RejectReuse, Tombstone: true}, Now: now})
	if reserveErr != nil {
		d.mu.Unlock()
		if errors.Is(reserveErr, idempotency.ErrConflict) {
			return DispatchResult{}, ErrIdempotencyConflict
		}
		return DispatchResult{}, reserveErr
	}
	switch reservation.Decision {
	case idempotency.Replay:
		attempt := Attempt{ID: reservation.Record.EffectRef, TenantID: in.TenantID, IntentID: in.IntentID, IdempotencyKey: in.IdempotencyKey, RequestHash: hash, CreatedAt: reservation.Record.CreatedAt.UTC()}
		switch reservation.Record.ResultRef {
		case "dlp-blocked":
			attempt.State, attempt.ErrorCode = Failed, "DLP_BLOCKED"
			d.attempts[identity] = attempt
			d.mu.Unlock()
			return DispatchResult{Attempt: attempt}, ErrDLPBlocked
		case "provider-failed":
			attempt.State, attempt.ErrorCode = Failed, "PROVIDER_FAILED"
			d.attempts[identity] = attempt
			d.mu.Unlock()
			return DispatchResult{Attempt: attempt}, ErrProviderFailed
		default:
			attempt.State, attempt.ProviderRef = AcceptedByProvider, reservation.Record.ResultRef
			if message, messageErr := d.providerMessage(in); messageErr == nil {
				attempt.AttentionOnly = message.AttentionOnly
			}
			d.attempts[identity] = attempt
			d.mu.Unlock()
			return DispatchResult{Attempt: attempt}, nil
		}
	case idempotency.InFlight:
		d.mu.Unlock()
		return DispatchResult{}, ErrIdempotencyInProgress
	case idempotency.Reserved:
	default:
		d.mu.Unlock()
		return DispatchResult{}, ErrIdempotencyConflict
	}
	attempt := Attempt{ID: digest(identity + "\x00" + hash), TenantID: in.TenantID, IntentID: in.IntentID, IdempotencyKey: in.IdempotencyKey, RequestHash: hash, State: Queued, CreatedAt: in.CreatedAt.UTC()}
	if attempt.CreatedAt.IsZero() {
		attempt.CreatedAt = time.Unix(0, 0).UTC()
	}
	d.attempts[identity] = attempt
	d.mu.Unlock()

	msg, err := d.providerMessage(in)
	if err != nil {
		d.mu.Lock()
		attempt.State = Failed
		attempt.ErrorCode = "DLP_BLOCKED"
		d.attempts[identity] = attempt
		d.mu.Unlock()
		_, _ = d.lifecycle.Complete(lifecycleIdentity, reservation.Record.RequestDigest, "dlp-blocked", attempt.ID, now)
		return DispatchResult{Attempt: attempt}, err
	}
	d.mu.Lock()
	attempt.State = Submitted
	attempt.AttentionOnly = msg.AttentionOnly
	d.attempts[identity] = attempt
	d.mu.Unlock()
	result, err := d.provider.Send(ctx, msg)
	d.mu.Lock()
	if err != nil || !result.Accepted {
		attempt.State = Failed
		attempt.ErrorCode = "PROVIDER_FAILED"
		d.attempts[identity] = attempt
		d.mu.Unlock()
		_, _ = d.lifecycle.Complete(lifecycleIdentity, reservation.Record.RequestDigest, "provider-failed", attempt.ID, now)
		return DispatchResult{Attempt: attempt}, ErrProviderFailed
	}
	attempt.State = AcceptedByProvider
	attempt.ProviderRef = result.Reference
	d.attempts[identity] = attempt
	d.mu.Unlock()
	_, _ = d.lifecycle.Complete(lifecycleIdentity, reservation.Record.RequestDigest, result.Reference, attempt.ID, now)
	return DispatchResult{Attempt: attempt}, nil
}

func (d *Dispatcher) providerMessage(in Intent) (Delivery, error) {
	attention := rank(in.Classification) > rank(d.policy.ProviderMaximumClassification)
	if attention {
		if !d.policy.AttentionOnlyClassifications[in.Classification] {
			return Delivery{}, ErrDLPBlocked
		}
		return Delivery{TenantID: in.TenantID, IntentID: in.IntentID, RecipientRef: in.RecipientRef, Purpose: in.Purpose, Subject: in.Subject, Classification: in.Classification, AttentionOnly: true, IdempotencyKey: in.IdempotencyKey}, nil
	}
	return Delivery{TenantID: in.TenantID, IntentID: in.IntentID, RecipientRef: in.RecipientRef, Purpose: in.Purpose, Subject: in.Subject, Body: in.Body, Classification: in.Classification, IdempotencyKey: in.IdempotencyKey}, nil
}

// Explain returns an operational summary with no message body.
func Explain(a Attempt) string {
	return fmt.Sprintf("attempt=%s intent=%s state=%s provider_ref=%s attention_only=%t", a.ID, a.IntentID, a.State, a.ProviderRef, a.AttentionOnly)
}

func rank(value string) int {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "PUBLIC":
		return 0
	case "INTERNAL":
		return 1
	case "CONFIDENTIAL":
		return 2
	case "RESTRICTED":
		return 3
	default:
		return 4
	}
}

func requestHash(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
