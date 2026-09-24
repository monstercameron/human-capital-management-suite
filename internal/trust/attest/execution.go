package attest

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ExecutionRequirement is the exact attestation obligation a dependent effect
// must satisfy. It binds the effect to one statement and one evidence binding.
type ExecutionRequirement struct {
	Tenant           values.TenantId
	ObligationID     string
	StatementID      string
	StatementVersion uint64
	StatementDigest  string
	BindingDigest    string
	ResponseID       string
	ResponseRevision uint64
	TransactionID    string
}

// ExecutionDecision is an audit-safe result of the execution gate.
type ExecutionDecision struct {
	Allowed         bool
	Reason          string
	ObligationID    string
	ResponseDigest  string
	StatementDigest string
	BindingDigest   string
	CheckedAt       TrustedTime
}

// ValidateRequired checks a response against the exact obligation at the
// execution boundary. Refused and unknown are never agreement, and a
// correction/revocation does not become valid merely because it exists.
func ValidateRequired(req ExecutionRequirement, response Response, now TrustedTime) (ExecutionDecision, error) {
	if err := now.Validate(); err != nil {
		return ExecutionDecision{}, err
	}
	if strings.TrimSpace(req.Tenant.String()) == "" || strings.TrimSpace(req.ObligationID) == "" || strings.TrimSpace(req.StatementID) == "" || strings.TrimSpace(req.StatementDigest) == "" || strings.TrimSpace(req.BindingDigest) == "" || strings.TrimSpace(req.ResponseID) == "" || req.StatementVersion == 0 || req.ResponseRevision == 0 {
		return ExecutionDecision{}, fmt.Errorf("%w: incomplete execution requirement", ErrRequiredAttestation)
	}
	refuse := func(reason string) (ExecutionDecision, error) {
		return ExecutionDecision{Allowed: false, Reason: reason, ObligationID: req.ObligationID, ResponseDigest: response.Digest, StatementDigest: req.StatementDigest, BindingDigest: req.BindingDigest, CheckedAt: now}, nil
	}
	if response.Tenant != req.Tenant || response.ResponseID != req.ResponseID || response.Revision != req.ResponseRevision {
		return refuse("response_identity_mismatch")
	}
	if response.StatementID != req.StatementID || response.StatementVersion != req.StatementVersion || response.StatementDigest != req.StatementDigest || response.BindingDigest != req.BindingDigest {
		return refuse("statement_or_binding_mismatch")
	}
	if response.Status != ResponseAccepted {
		return refuse("response_not_accepted")
	}
	if response.Kind != AssertionResponse {
		return refuse("response_is_not_original_acceptance")
	}
	if !slices.Contains(response.AffectedObligations, req.ObligationID) {
		return refuse("obligation_not_covered")
	}
	if req.TransactionID != "" && response.TransactionID != req.TransactionID {
		return refuse("transaction_mismatch")
	}
	if response.RecordedAt.At.After(now.At) {
		return refuse("response_recorded_in_future")
	}
	if response.RecordedAt.At.Before(now.At) && response.RecordedAt.Health == "" {
		return refuse("response_time_evidence_missing")
	}
	// The durable receipt is content-addressed. Recompute both digests at the
	// boundary so corrupt or internally inconsistent rows are refused.
	if err := ValidateResponse(response); err != nil || response.RequestDigest != requestDigest(responseRequest(response)) || response.Digest == "" || response.Digest != responseDigest(response) {
		return refuse("response_integrity_mismatch")
	}
	return ExecutionDecision{Allowed: true, Reason: "ATTESTATION_ACCEPTED", ObligationID: req.ObligationID, ResponseDigest: response.Digest, StatementDigest: req.StatementDigest, BindingDigest: req.BindingDigest, CheckedAt: now}, nil
}

func responseRequest(response Response) ResponseRequest {
	return ResponseRequest{
		Tenant: response.Tenant, ResponseID: response.ResponseID,
		StatementID: response.StatementID, StatementVersion: response.StatementVersion,
		StatementDigest: response.StatementDigest, BindingDigest: response.BindingDigest,
		Status: response.Status, Kind: response.Kind, Reason: response.Reason,
		EvidenceReceipt: response.EvidenceReceipt, IdempotencyKey: response.IdempotencyKey,
		CorrectsResponseID: response.CorrectsResponseID, Authority: response.Authority,
		AffectedObligations: append([]string(nil), response.AffectedObligations...),
		TransactionID:       response.TransactionID,
	}
}

// RevalidateAtExecution loads the exact persisted receipt and applies the
// execution gate. A store failure blocks the effect rather than guessing.
func RevalidateAtExecution(ctx context.Context, store ResponseStore, clock TrustedClock, req ExecutionRequirement) (ExecutionDecision, error) {
	if store == nil || clock == nil {
		return ExecutionDecision{}, fmt.Errorf("%w: response store and trusted clock are required", ErrRequiredAttestation)
	}
	response, err := store.GetResponse(ctx, req.Tenant, req.ResponseID, req.ResponseRevision)
	if err != nil {
		return ExecutionDecision{}, fmt.Errorf("%w: response lookup: %v", ErrRequiredAttestation, err)
	}
	now, err := clock.TrustedNow()
	if err != nil {
		return ExecutionDecision{}, fmt.Errorf("%w: execution time: %v", ErrRequiredAttestation, err)
	}
	return ValidateRequired(req, response, now)
}

// EnforceRequired is the concise name for the execution gate.
func EnforceRequired(ctx context.Context, store ResponseStore, clock TrustedClock, req ExecutionRequirement) (ExecutionDecision, error) {
	return RevalidateAtExecution(ctx, store, clock, req)
}

// EnforceBeforeEffect revalidates the required receipt immediately before
// invoking effect. Any lookup, clock, integrity, or policy failure prevents
// the callback from running.
func EnforceBeforeEffect(ctx context.Context, store ResponseStore, clock TrustedClock, req ExecutionRequirement, effect func(context.Context, ExecutionDecision) error) (ExecutionDecision, error) {
	if effect == nil {
		return ExecutionDecision{}, fmt.Errorf("%w: dependent effect is required", ErrRequiredAttestation)
	}
	decision, err := EnforceRequired(ctx, store, clock, req)
	if err != nil {
		return decision, err
	}
	if !decision.Allowed {
		return decision, fmt.Errorf("%w: %s", ErrRequiredAttestation, decision.Reason)
	}
	if err := effect(ctx, decision); err != nil {
		return decision, err
	}
	return decision, nil
}

// FixedTrustedTime is useful for deterministic conformance callers.
func FixedTrustedTime(at time.Time) TrustedTime {
	return TrustedTime{At: at.UTC(), Source: "conformance", EvidenceID: "ev:time:conformance", Health: "TRUSTED"}
}
