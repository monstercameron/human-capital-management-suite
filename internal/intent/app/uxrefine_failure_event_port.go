package app

import (
	"context"
	"errors"
	"strings"
	"time"
)

// JourneyFailureSchemaRef identifies the deterministic protobuf Struct payload
// used for a failed approval-start attempt on an intent ledger stream.
const JourneyFailureSchemaRef = "hcmnext.journey.v1.ApprovalStartFailure@1"

// ErrJourneyFailureRecord identifies a failed attempt to persist the
// user-visible approval-start failure. Its stable text is safe to expose;
// the private cause remains available to internal errors.Is checks only.
var ErrJourneyFailureRecord = errors.New("journey failure record unavailable")

type journeyFailureRecordError struct {
	cause error
}

func (e journeyFailureRecordError) Error() string { return ErrJourneyFailureRecord.Error() }

func (e journeyFailureRecordError) Unwrap() error { return e.cause }

func (e journeyFailureRecordError) Is(target error) bool { return target == ErrJourneyFailureRecord }

// JourneyFailureEvent is the bounded, authorized fact recorded when an
// approval start fails after the intent has passed its visibility checks.
type JourneyFailureEvent struct {
	Tenant         string
	IntentID       string
	Actor          string
	ReasonRef      string
	RevisionID     string
	OccurredAt     time.Time
	IdempotencyKey string
}

// JourneyFailureRecorder durably records failed approval starts. Implementers
// must scope the write to Tenant and preserve exact idempotent retries.
type JourneyFailureRecorder interface {
	AppendJourneyFailure(context.Context, JourneyFailureEvent) error
}

// Validate checks the event's structural and owned-value boundaries before a
// persistence adapter is allowed to open a stream or write a row.
func (e JourneyFailureEvent) Validate() error {
	if strings.TrimSpace(e.Tenant) == "" || strings.TrimSpace(e.IntentID) == "" {
		return errors.New("journey failure event requires tenant and intent")
	}
	if len(e.Tenant) > 128 || len(e.IntentID) > 128 {
		return errors.New("journey failure event tenant and intent are too large")
	}
	if strings.TrimSpace(e.Actor) == "" {
		return errors.New("journey failure event requires actor")
	}
	if len(e.Actor) > 256 || len(e.RevisionID) > 256 {
		return errors.New("journey failure event actor and revision are too large")
	}
	if !journeyFailureReasonAllowed(e.ReasonRef) {
		return errors.New("journey failure event has an unknown reason")
	}
	if strings.TrimSpace(e.IdempotencyKey) == "" || len(e.IdempotencyKey) > 256 {
		return errors.New("journey failure event requires a bounded idempotency key")
	}
	if e.OccurredAt.IsZero() {
		return errors.New("journey failure event requires occurred time")
	}
	return nil
}

func journeyFailureReasonAllowed(reason string) bool {
	switch strings.TrimSpace(reason) {
	case journeyFailureDomainUnavailable, journeyFailureStorageFailed, journeyFailureStagePrecondition:
		return true
	default:
		return false
	}
}
