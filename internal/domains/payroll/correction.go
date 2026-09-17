package payroll

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var (
	// ErrInvalidCorrection identifies a malformed payroll correction request.
	ErrInvalidCorrection = errors.New("payroll: invalid payroll correction")
	// ErrCorrectionDuplicate refuses a repeated correction of the same scope.
	ErrCorrectionDuplicate = errors.New("payroll: payroll correction is a duplicate")
	// ErrCorrectionRejected is the typed PAYRUN-008 refusal boundary.
	ErrCorrectionRejected = errors.New("PAYRUN_008_REJECTED")
)

// CorrectionError reports the offending field and reason without creating an
// authoritative side effect.
type CorrectionError struct {
	Code   string
	Field  string
	Reason string
	Cause  error
}

func (e *CorrectionError) Error() string {
	return fmt.Sprintf("%s: field=%s: %s", e.Code, e.Field, e.Reason)
}

// Is reports the typed PAYRUN-008 boundary and any wrapped cause.
func (e *CorrectionError) Is(target error) bool {
	return target == ErrCorrectionRejected || target == e.Cause
}

// Unwrap returns the wrapped cause, if any.
func (e *CorrectionError) Unwrap() error { return e.Cause }

func correctionRefusal(field, reason string, cause error) error {
	return &CorrectionError{Code: ErrCorrectionRejected.Error(), Field: field, Reason: reason, Cause: cause}
}

// CorrectionType is the closed correction vocabulary. A CORRECTION restates
// effects of the finalized release; an OFF_CYCLE records a separate
// out-of-cycle run linked to the same causal release.
type CorrectionType string

const (
	CorrectionTypeCorrection CorrectionType = "CORRECTION"
	CorrectionTypeOffCycle   CorrectionType = "OFF_CYCLE"
)

// Valid reports whether t is a declared correction type.
func (t CorrectionType) Valid() bool {
	return t == CorrectionTypeCorrection || t == CorrectionTypeOffCycle
}

// correctionLegs is the closed effect-leg vocabulary a correction restates.
var correctionLegs = []string{"payments", "statements", "balances", "accounting", "reporting"}

func correctionLegDigest(effects ReleaseEffects, leg string) string {
	switch leg {
	case "payments":
		return effects.PaymentsDigest
	case "statements":
		return effects.StatementsDigest
	case "balances":
		return effects.BalancesDigest
	case "accounting":
		return effects.AccountingDigest
	case "reporting":
		return effects.ReportingDigest
	default:
		return ""
	}
}

// EffectDelta names one restated effect leg: its prior digest from the
// finalized release and its restated digest going forward.
type EffectDelta struct {
	Leg            string
	PriorDigest    string
	RestatedDigest string
}

// CorrectionRequest is the complete material for correcting one finalized
// payroll release append-only: the causal release, the restated effects and
// obligations, the governing approval, and the caller correction key.
type CorrectionRequest struct {
	CorrectionKey       string
	Type                CorrectionType
	Release             PayrollRelease
	Reason              string
	RestatedEffects     ReleaseEffects
	RestatedObligations ReleaseObligations
	ApprovalDigest      string
	ReversalOf          string
}

// PayrollCorrection is the immutable, digested correction of one finalized
// release. The causal release is never mutated.
type PayrollCorrection struct {
	CorrectionID        string
	CorrectionKey       string
	Type                CorrectionType
	ReleaseID           string
	RunID               string
	RunRevision         uint64
	PriorDigest         string
	Deltas              []EffectDelta
	RestatedEffects     ReleaseEffects
	RestatedObligations ReleaseObligations
	ApprovalDigest      string
	ReversalOf          string
	Reason              string
	CorrectionDigest    string
}

func (c PayrollCorrection) body() *canonicalbytes.Writer {
	w := canonicalbytes.New("hcmnext.domains.payroll.PayrollCorrection", 1).
		String("correction_id", c.CorrectionID).
		String("correction_key", c.CorrectionKey).
		String("type", string(c.Type)).
		String("release_id", c.ReleaseID).
		String("run_id", c.RunID).
		Int("run_revision", int64(c.RunRevision)).
		String("prior_digest", c.PriorDigest).
		String("approval_digest", c.ApprovalDigest).
		String("reversal_of", c.ReversalOf).
		String("reason", c.Reason).
		String("effects.payments_digest", c.RestatedEffects.PaymentsDigest).
		String("effects.statements_digest", c.RestatedEffects.StatementsDigest).
		String("effects.balances_digest", c.RestatedEffects.BalancesDigest).
		String("effects.accounting_digest", c.RestatedEffects.AccountingDigest).
		String("effects.reporting_digest", c.RestatedEffects.ReportingDigest).
		String("obligations.funding_digest", c.RestatedObligations.FundingDigest).
		String("obligations.filing_digest", c.RestatedObligations.FilingDigest).
		String("obligations.settlement_digest", c.RestatedObligations.SettlementDigest).
		Int("deltas", int64(len(c.Deltas)))
	for _, d := range c.Deltas {
		prefix := "delta." + d.Leg + "."
		w = w.String(prefix+"prior", d.PriorDigest).
			String(prefix+"restated", d.RestatedDigest)
	}
	return w
}

func (c PayrollCorrection) computedDigest() string {
	digest, err := c.body().Digest()
	if err != nil {
		return ""
	}
	return digest
}

// Validate checks correction bindings, delta coherence, and the self-digest.
func (c PayrollCorrection) Validate() error {
	if strings.TrimSpace(c.CorrectionKey) == "" || strings.TrimSpace(c.ReleaseID) == "" || strings.TrimSpace(c.RunID) == "" || c.RunRevision == 0 {
		return correctionRefusal("correction", "correction identity is incomplete", ErrInvalidCorrection)
	}
	if c.CorrectionID != "payroll-correction/"+c.CorrectionKey {
		return correctionRefusal("correction_id", "correction id is not key-bound", ErrInvalidCorrection)
	}
	if !c.Type.Valid() {
		return correctionRefusal("type", "correction type is unknown", ErrInvalidCorrection)
	}
	if strings.TrimSpace(c.Reason) == "" {
		return correctionRefusal("reason", "correction reason is required", ErrInvalidCorrection)
	}
	if strings.TrimSpace(c.ApprovalDigest) == "" {
		return correctionRefusal("approval_digest", "correction approval is required", ErrInvalidCorrection)
	}
	if strings.TrimSpace(c.PriorDigest) == "" {
		return correctionRefusal("prior_digest", "causal release digest is required", ErrInvalidCorrection)
	}
	if err := c.RestatedEffects.Validate(); err != nil {
		return correctionRefusal("restated_effects", "restated effects are incomplete", err)
	}
	if err := c.RestatedObligations.Validate(); err != nil {
		return correctionRefusal("restated_obligations", "restated obligations are incomplete", err)
	}
	if len(c.Deltas) == 0 {
		return correctionRefusal("deltas", "a correction restates at least one effect leg", ErrInvalidCorrection)
	}
	seen := map[string]bool{}
	for _, d := range c.Deltas {
		validLeg := false
		for _, leg := range correctionLegs {
			if d.Leg == leg {
				validLeg = true
			}
		}
		if !validLeg || seen[d.Leg] {
			return correctionRefusal("deltas", "delta leg is unknown or repeated", ErrInvalidCorrection)
		}
		seen[d.Leg] = true
		if strings.TrimSpace(d.PriorDigest) == "" || strings.TrimSpace(d.RestatedDigest) == "" || d.PriorDigest == d.RestatedDigest {
			return correctionRefusal("deltas", "delta "+d.Leg+" is incoherent", ErrInvalidCorrection)
		}
		if correctionLegDigest(c.RestatedEffects, d.Leg) != d.RestatedDigest {
			return correctionRefusal("deltas", "delta "+d.Leg+" does not match the restated effects", ErrInvalidCorrection)
		}
	}
	if c.CorrectionDigest == "" || c.CorrectionDigest != c.computedDigest() {
		return correctionRefusal("correction_digest", "correction digest mismatch", ErrInvalidCorrection)
	}
	return nil
}

// Canonical returns the correction evidence bytes, or nil when invalid.
func (c PayrollCorrection) Canonical() []byte {
	if err := c.Validate(); err != nil {
		return nil
	}
	raw, err := c.body().Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the correction digest.
func (c PayrollCorrection) Digest() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	return c.CorrectionDigest, nil
}

// CorrectionExplanation is the read-only summary of one correction.
type CorrectionExplanation struct {
	CorrectionID string
	ReleaseID    string
	PriorDigest  string
	Deltas       int
	Digest       string
}

// Explain returns the correction facts.
func (c PayrollCorrection) Explain() (CorrectionExplanation, error) {
	if err := c.Validate(); err != nil {
		return CorrectionExplanation{}, err
	}
	return CorrectionExplanation{
		CorrectionID: c.CorrectionID, ReleaseID: c.ReleaseID,
		PriorDigest: c.PriorDigest, Deltas: len(c.Deltas), Digest: c.CorrectionDigest,
	}, nil
}

// CorrectFinalizedRun records an append-only correction of one finalized
// payroll release. The causal release is validated and never mutated: the
// correction links the release, the exact per-leg deltas, the governing
// approval, the optional reversal, the reason, and the restated obligations.
// A no-op restatement, an invalid release, or a duplicate correction fails
// with a typed refusal and yields no correction. Prior corrections are
// caller-held idempotency evidence. Nothing is mutated.
func CorrectFinalizedRun(req CorrectionRequest, prior []PayrollCorrection) (PayrollCorrection, error) {
	if strings.TrimSpace(req.CorrectionKey) == "" {
		return PayrollCorrection{}, correctionRefusal("correction_key", "correction key is required", ErrInvalidCorrection)
	}
	if !req.Type.Valid() {
		return PayrollCorrection{}, correctionRefusal("type", "correction type is unknown", ErrInvalidCorrection)
	}
	if err := req.Release.Validate(); err != nil {
		return PayrollCorrection{}, correctionRefusal("release", "causal release is invalid", err)
	}
	if strings.TrimSpace(req.Reason) == "" {
		return PayrollCorrection{}, correctionRefusal("reason", "correction reason is required", ErrInvalidCorrection)
	}
	if strings.TrimSpace(req.ApprovalDigest) == "" {
		return PayrollCorrection{}, correctionRefusal("approval_digest", "correction approval is required", ErrInvalidCorrection)
	}
	if err := req.RestatedEffects.Validate(); err != nil {
		return PayrollCorrection{}, correctionRefusal("restated_effects", "restated effects are incomplete", err)
	}
	if err := req.RestatedObligations.Validate(); err != nil {
		return PayrollCorrection{}, correctionRefusal("restated_obligations", "restated obligations are incomplete", err)
	}
	var deltas []EffectDelta
	for _, leg := range correctionLegs {
		priorDigest, restated := correctionLegDigest(req.Release.Effects, leg), correctionLegDigest(req.RestatedEffects, leg)
		if restated != priorDigest {
			deltas = append(deltas, EffectDelta{Leg: leg, PriorDigest: priorDigest, RestatedDigest: restated})
		}
	}
	if len(deltas) == 0 {
		return PayrollCorrection{}, correctionRefusal("deltas", "restated effects change nothing; there is nothing to correct", ErrInvalidCorrection)
	}
	for _, recorded := range prior {
		if recorded.CorrectionKey == req.CorrectionKey {
			return PayrollCorrection{}, correctionRefusal("correction_key", "correction key was already recorded", ErrCorrectionDuplicate)
		}
		if recorded.PriorDigest == req.Release.ReleaseDigest && restatedEqual(recorded, req) {
			return PayrollCorrection{}, correctionRefusal("restated_effects", "this restatement was already recorded", ErrCorrectionDuplicate)
		}
	}
	correction := PayrollCorrection{
		CorrectionID:  "payroll-correction/" + req.CorrectionKey,
		CorrectionKey: req.CorrectionKey, Type: req.Type,
		ReleaseID: req.Release.ReleaseID, RunID: req.Release.RunID, RunRevision: req.Release.RunRevision,
		PriorDigest:     req.Release.ReleaseDigest,
		Deltas:          deltas,
		RestatedEffects: req.RestatedEffects, RestatedObligations: req.RestatedObligations,
		ApprovalDigest: req.ApprovalDigest, ReversalOf: req.ReversalOf, Reason: req.Reason,
	}
	correction.CorrectionDigest = correction.computedDigest()
	if err := correction.Validate(); err != nil {
		return PayrollCorrection{}, err
	}
	return correction, nil
}

// restatedEqual reports whether a recorded correction already restates the
// requested effects, obligations, and causal release.
func restatedEqual(recorded PayrollCorrection, req CorrectionRequest) bool {
	return recorded.ReleaseID == req.Release.ReleaseID &&
		recorded.RestatedEffects == req.RestatedEffects &&
		recorded.RestatedObligations == req.RestatedObligations
}
