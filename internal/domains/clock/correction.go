// CLOCK-006: append-only correction of prior time punches.
//
// A correction binds the original signed observation, a reason with its
// reason record, attestation and approval authority, the corrected occurred
// time and the downstream consumers that must recalculate. The original is
// received by value and never mutated: the correction is a new signed record
// that references it. The package is kernel-pure: clocks arrive as
// parameters, never from the wall; nothing is persisted or emitted.
package clock

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrCorrectionRejected is the CLOCK-006 seeded-defect sentinel. A test
	// that probes an unattested, unapproved, unreasoned or no-op correction
	// must see this error with the offending field, state and version.
	ErrCorrectionRejected = errors.New("CLOCK_006_REJECTED")
	// ErrCorrectionEvidence identifies an invalid correction result that
	// cannot be used as time evidence.
	ErrCorrectionEvidence = errors.New("clock: correction evidence is invalid")
)

// CorrectionRejection is the stable CLOCK-006 failure shape.
type CorrectionRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *CorrectionRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrCorrectionRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the CLOCK_006_REJECTED sentinel to errors.Is.
func (r *CorrectionRejection) Unwrap() error { return ErrCorrectionRejected }

func correctionReject(field, state, version, reason string) error {
	return &CorrectionRejection{Field: field, State: state, Version: version, Reason: reason}
}

// CorrectionRequest is the complete append-only correction question.
type CorrectionRequest struct {
	Original            TimeObservation
	Reason              string
	ReasonRef           string
	AttestationRef      string
	ApprovalRef         string
	CorrectedOccurredAt time.Time
	Impacts             []string
	Now                 time.Time
}

// PunchCorrection is the new signed record. OriginalDigest and
// OriginalOccurredAt bind the immutable original; nothing in the original is
// rewritten by producing this correction.
type PunchCorrection struct {
	OriginalDigest      string
	OriginalOccurredAt  time.Time
	Reason              string
	ReasonRef           string
	AttestationRef      string
	ApprovalRef         string
	CorrectedOccurredAt time.Time
	CorrectedAt         time.Time
	Impacts             []string
	Digest              string
}

// CorrectObservation appends a correction to an accepted observation. The
// original keeps its digest and instants; the correction carries its own
// server-stamped digest over the original digest, the corrected instant and
// the bound authority.
func CorrectObservation(req CorrectionRequest) (PunchCorrection, error) {
	const version = "clock-correction/v1"
	if !req.Original.Accepted || strings.TrimSpace(req.Original.Digest) == "" {
		return PunchCorrection{}, correctionReject("original", "MISSING", version, "correction requires an accepted signed observation")
	}
	if strings.TrimSpace(req.Reason) == "" {
		return PunchCorrection{}, correctionReject("reason", "MISSING", version, "correction reason is required")
	}
	if strings.TrimSpace(req.ReasonRef) == "" {
		return PunchCorrection{}, correctionReject("reason_ref", "MISSING", version, "correction reason record is required")
	}
	if strings.TrimSpace(req.AttestationRef) == "" {
		return PunchCorrection{}, correctionReject("attestation_ref", "MISSING", version, "correction attestation is required")
	}
	if strings.TrimSpace(req.ApprovalRef) == "" {
		return PunchCorrection{}, correctionReject("approval_ref", "MISSING", version, "correction approval is required")
	}
	if req.CorrectedOccurredAt.IsZero() {
		return PunchCorrection{}, correctionReject("corrected_occurred_at", "MISSING", version, "corrected occurred time is required")
	}
	if req.CorrectedOccurredAt.Equal(req.Original.OccurredAt) {
		return PunchCorrection{}, correctionReject("corrected_occurred_at", "UNCHANGED", version, "correction changes no observed instant")
	}
	if req.Now.IsZero() {
		return PunchCorrection{}, correctionReject("now", "MISSING", version, "server clock is required")
	}
	if req.CorrectedOccurredAt.After(req.Now.Add(observationSkew)) {
		return PunchCorrection{}, correctionReject("corrected_occurred_at", "FUTURE", version, "corrected time is beyond clock skew")
	}
	if len(req.Impacts) == 0 {
		return PunchCorrection{}, correctionReject("impacts", "MISSING", version, "recalculation impacts are required")
	}
	for _, impact := range req.Impacts {
		if strings.TrimSpace(impact) == "" {
			return PunchCorrection{}, correctionReject("impacts", "INVALID", version, "recalculation impacts must name consumers")
		}
	}
	correctedAt := req.Now.UTC()
	impacts := append([]string(nil), req.Impacts...)
	body := strings.Join([]string{
		req.Original.Digest,
		req.CorrectedOccurredAt.UTC().Format(time.RFC3339Nano),
		req.ReasonRef,
		req.AttestationRef,
		req.ApprovalRef,
		strings.Join(impacts, ","),
		correctedAt.Format(time.RFC3339Nano),
	}, "\x00")
	sum := sha256.Sum256([]byte(body))
	return PunchCorrection{
		OriginalDigest:      req.Original.Digest,
		OriginalOccurredAt:  req.Original.OccurredAt.UTC(),
		Reason:              req.Reason,
		ReasonRef:           req.ReasonRef,
		AttestationRef:      req.AttestationRef,
		ApprovalRef:         req.ApprovalRef,
		CorrectedOccurredAt: req.CorrectedOccurredAt.UTC(),
		CorrectedAt:         correctedAt,
		Impacts:             impacts,
		Digest:              "sha256:" + hex.EncodeToString(sum[:]),
	}, nil
}

// ExplainCorrection validates a correction result for operator display.
func ExplainCorrection(c PunchCorrection) error {
	if c.OriginalDigest == "" || c.Digest == "" || c.CorrectedAt.IsZero() {
		return ErrCorrectionEvidence
	}
	return nil
}
