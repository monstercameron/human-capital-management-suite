package paymethod

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrInvalidReconciliation identifies an incomplete or incoherent record.
	ErrInvalidReconciliation = errors.New("paymethod: invalid settlement reconciliation")
	// ErrSettlementDuplicate refuses a repeated settlement submission.
	ErrSettlementDuplicate = errors.New("paymethod: duplicate settlement submission")
	// ErrReconciliationRefused is the typed PAYMETHOD-003 refusal boundary.
	ErrReconciliationRefused = errors.New("PAYMETHOD_003_REJECTED")
)

// ReconciliationError reports the offending field and reason without creating
// an authoritative side effect.
type ReconciliationError struct {
	Code   string
	Field  string
	Reason string
	Cause  error
}

func (e *ReconciliationError) Error() string {
	return fmt.Sprintf("%s: field=%s: %s", e.Code, e.Field, e.Reason)
}

// Is reports the typed PAYMETHOD-003 boundary and any wrapped cause.
func (e *ReconciliationError) Is(target error) bool {
	return target == ErrReconciliationRefused || target == e.Cause
}

// Unwrap returns the wrapped cause, if any.
func (e *ReconciliationError) Unwrap() error { return e.Cause }

func reconciliationRefusal(field, reason string, cause error) error {
	return &ReconciliationError{Code: ErrReconciliationRefused.Error(), Field: field, Reason: reason, Cause: cause}
}

// PrenoteState is the closed vocabulary for a provider prenote observation.
// It is deliberately distinct from VerificationState: a prenote never
// verifies a destination.
type PrenoteState string

const (
	PrenoteSubmitted PrenoteState = "SUBMITTED"
	PrenoteConfirmed PrenoteState = "CONFIRMED"
	PrenoteFailed    PrenoteState = "FAILED"
)

// Valid reports whether s is a declared prenote state.
func (s PrenoteState) Valid() bool {
	switch s {
	case PrenoteSubmitted, PrenoteConfirmed, PrenoteFailed:
		return true
	default:
		return false
	}
}

// PrenoteObservation is one external provider observation about a prenote.
// It carries references only: account detail never enters this package.
type PrenoteObservation struct {
	ObservationID     string
	DestinationID     string
	DestinationDigest string
	State             PrenoteState
	ObservedAt        values.Instant
	ProviderRef       string
}

func (o PrenoteObservation) validate() error {
	if strings.TrimSpace(o.ObservationID) == "" {
		return reconciliationRefusal("observation_id", "observation id is required", ErrInvalidReconciliation)
	}
	if strings.TrimSpace(o.DestinationID) == "" || strings.TrimSpace(o.DestinationDigest) == "" {
		return reconciliationRefusal("destination", "destination id and digest are required", ErrInvalidReconciliation)
	}
	if !o.State.Valid() {
		return reconciliationRefusal("state", fmt.Sprintf("unknown prenote state %q", o.State), ErrInvalidReconciliation)
	}
	if err := o.ObservedAt.Validate(); err != nil {
		return reconciliationRefusal("observed_at", "observed-at instant is required", err)
	}
	if strings.TrimSpace(o.ProviderRef) == "" {
		return reconciliationRefusal("provider_ref", "provider reference is required", ErrInvalidReconciliation)
	}
	return nil
}

// PrenoteRecord is the recorded observation. Verified is always false: a
// prenote submission never counts as destination verification, which only a
// verification challenge event can confer.
type PrenoteRecord struct {
	ObservationID     string
	DestinationID     string
	DestinationDigest string
	State             PrenoteState
	ObservedAt        values.Instant
	ProviderRef       string
	Verified          bool
	RecordDigest      string
}

func (r PrenoteRecord) body() *canonicalbytes.Writer {
	return canonicalbytes.New("hcmnext.domains.paymethod.PrenoteRecord", 1).
		String("observation_id", r.ObservationID).
		String("destination_id", r.DestinationID).
		String("destination_digest", r.DestinationDigest).
		String("state", string(r.State)).
		Value("observed_at", r.ObservedAt).
		String("provider_ref", r.ProviderRef).
		Bool("verified", r.Verified)
}

func (r PrenoteRecord) computedDigest() string {
	digest, err := r.body().Digest()
	if err != nil {
		return ""
	}
	return digest
}

// Validate checks the record bindings and self-digest.
func (r PrenoteRecord) Validate() error {
	if strings.TrimSpace(r.ObservationID) == "" || strings.TrimSpace(r.DestinationID) == "" || strings.TrimSpace(r.DestinationDigest) == "" {
		return reconciliationRefusal("destination", "record identity is incomplete", ErrInvalidReconciliation)
	}
	if !r.State.Valid() {
		return reconciliationRefusal("state", "prenote state is unknown", ErrInvalidReconciliation)
	}
	if err := r.ObservedAt.Validate(); err != nil {
		return reconciliationRefusal("observed_at", "observed-at instant is required", err)
	}
	if r.Verified {
		return reconciliationRefusal("verified", "a prenote observation never confers verification", ErrInvalidReconciliation)
	}
	if r.RecordDigest == "" || r.RecordDigest != r.computedDigest() {
		return reconciliationRefusal("record_digest", "prenote record digest mismatch", ErrInvalidReconciliation)
	}
	return nil
}

// RecordPrenoteObservation records one external prenote observation against
// the expected destination revision. The destination value is never changed:
// in particular its verification state is untouched.
func RecordPrenoteObservation(expected Destination, obs PrenoteObservation) (PrenoteRecord, error) {
	if err := expected.Validate(); err != nil {
		return PrenoteRecord{}, reconciliationRefusal("destination", "expected destination is invalid", err)
	}
	if err := obs.validate(); err != nil {
		return PrenoteRecord{}, err
	}
	if obs.DestinationID != expected.DestinationID || obs.DestinationDigest != expected.CanonicalDigest {
		return PrenoteRecord{}, reconciliationRefusal("destination", "observation does not bind the expected destination revision", ErrInvalidReconciliation)
	}
	record := PrenoteRecord{
		ObservationID: obs.ObservationID, DestinationID: obs.DestinationID,
		DestinationDigest: obs.DestinationDigest, State: obs.State,
		ObservedAt: obs.ObservedAt, ProviderRef: obs.ProviderRef,
	}
	record.RecordDigest = record.computedDigest()
	if err := record.Validate(); err != nil {
		return PrenoteRecord{}, err
	}
	return record, nil
}

// SettlementSubmission is one payroll settlement instruction recorded for
// acceptance tracking. The idempotency key deduplicates provider-timeout
// retries: the same key never records twice.
type SettlementSubmission struct {
	InstructionID     string
	IdempotencyKey    string
	DestinationID     string
	DestinationDigest string
	SubmittedAt       values.Instant
}

func (s SettlementSubmission) validate() error {
	if strings.TrimSpace(s.InstructionID) == "" {
		return reconciliationRefusal("instruction_id", "instruction id is required", ErrInvalidReconciliation)
	}
	if strings.TrimSpace(s.IdempotencyKey) == "" {
		return reconciliationRefusal("idempotency_key", "idempotency key is required", ErrInvalidReconciliation)
	}
	if strings.TrimSpace(s.DestinationID) == "" || strings.TrimSpace(s.DestinationDigest) == "" {
		return reconciliationRefusal("destination", "destination id and digest are required", ErrInvalidReconciliation)
	}
	if err := s.SubmittedAt.Validate(); err != nil {
		return reconciliationRefusal("submitted_at", "submitted-at instant is required", err)
	}
	return nil
}

// RecordedSubmission is the accepted submission with its digest.
type RecordedSubmission struct {
	InstructionID     string
	IdempotencyKey    string
	DestinationID     string
	DestinationDigest string
	SubmittedAt       values.Instant
	SubmissionDigest  string
}

func (s RecordedSubmission) body() *canonicalbytes.Writer {
	return canonicalbytes.New("hcmnext.domains.paymethod.RecordedSubmission", 1).
		String("instruction_id", s.InstructionID).
		String("idempotency_key", s.IdempotencyKey).
		String("destination_id", s.DestinationID).
		String("destination_digest", s.DestinationDigest).
		Value("submitted_at", s.SubmittedAt)
}

func (s RecordedSubmission) computedDigest() string {
	digest, err := s.body().Digest()
	if err != nil {
		return ""
	}
	return digest
}

// Validate checks the submission bindings and self-digest.
func (s RecordedSubmission) Validate() error {
	if err := (SettlementSubmission{InstructionID: s.InstructionID, IdempotencyKey: s.IdempotencyKey, DestinationID: s.DestinationID, DestinationDigest: s.DestinationDigest, SubmittedAt: s.SubmittedAt}).validate(); err != nil {
		return err
	}
	if s.SubmissionDigest == "" || s.SubmissionDigest != s.computedDigest() {
		return reconciliationRefusal("submission_digest", "submission digest mismatch", ErrInvalidReconciliation)
	}
	return nil
}

// RecordSettlementSubmission records one instruction unless its idempotency
// key or instruction id was already recorded. A provider-timeout retry
// reuses the original record instead of creating a duplicate instruction.
func RecordSettlementSubmission(sub SettlementSubmission, prior []RecordedSubmission) (RecordedSubmission, error) {
	if err := sub.validate(); err != nil {
		return RecordedSubmission{}, err
	}
	for _, recorded := range prior {
		if recorded.IdempotencyKey == sub.IdempotencyKey || recorded.InstructionID == sub.InstructionID {
			return RecordedSubmission{}, fmt.Errorf("%w: instruction %q", ErrSettlementDuplicate, sub.InstructionID)
		}
	}
	recorded := RecordedSubmission{
		InstructionID: sub.InstructionID, IdempotencyKey: sub.IdempotencyKey,
		DestinationID: sub.DestinationID, DestinationDigest: sub.DestinationDigest,
		SubmittedAt: sub.SubmittedAt,
	}
	recorded.SubmissionDigest = recorded.computedDigest()
	if err := recorded.Validate(); err != nil {
		return RecordedSubmission{}, err
	}
	return recorded, nil
}

// SettlementStatus is the closed vocabulary for a provider settlement
// observation. It is deliberately distinct from destination verification.
type SettlementStatus string

const (
	SettlementSettled  SettlementStatus = "SETTLED"
	SettlementReturned SettlementStatus = "RETURNED"
	SettlementFailed   SettlementStatus = "FAILED"
	SettlementPending  SettlementStatus = "PENDING"
)

// Valid reports whether s is a declared settlement status.
func (s SettlementStatus) Valid() bool {
	switch s {
	case SettlementSettled, SettlementReturned, SettlementFailed, SettlementPending:
		return true
	default:
		return false
	}
}

// SettlementObservation is one external provider observation about a recorded
// instruction. Observations remain external: they are compared, never
// trusted as payroll facts.
type SettlementObservation struct {
	InstructionID string
	Status        SettlementStatus
	ObservedAt    values.Instant
	ProviderRef   string
	ReturnCode    string
}

func (o SettlementObservation) validate() error {
	if strings.TrimSpace(o.InstructionID) == "" {
		return reconciliationRefusal("instruction_id", "instruction id is required", ErrInvalidReconciliation)
	}
	if !o.Status.Valid() {
		return reconciliationRefusal("status", fmt.Sprintf("unknown settlement status %q", o.Status), ErrInvalidReconciliation)
	}
	if err := o.ObservedAt.Validate(); err != nil {
		return reconciliationRefusal("observed_at", "observed-at instant is required", err)
	}
	if strings.TrimSpace(o.ProviderRef) == "" {
		return reconciliationRefusal("provider_ref", "provider reference is required", ErrInvalidReconciliation)
	}
	if o.Status == SettlementReturned && strings.TrimSpace(o.ReturnCode) == "" {
		return reconciliationRefusal("return_code", "a returned payment requires a return code", ErrInvalidReconciliation)
	}
	return nil
}

// ReconciliationState is the exact outcome of comparing the expected,
// accepted, and settled destinations.
type ReconciliationState string

const (
	ReconciliationMatched           ReconciliationState = "MATCHED"
	ReconciliationSuperseded        ReconciliationState = "SUPERSEDED_DESTINATION"
	ReconciliationMismatched        ReconciliationState = "DESTINATION_MISMATCH"
	ReconciliationReturned          ReconciliationState = "RETURNED"
	ReconciliationFailed            ReconciliationState = "SETTLEMENT_FAILED"
	ReconciliationStale             ReconciliationState = "OBSERVATION_STALE"
	ReconciliationPendingSettlement ReconciliationState = "PENDING_SETTLEMENT"
)

// Valid reports whether s is a declared reconciliation state.
func (s ReconciliationState) Valid() bool {
	switch s {
	case ReconciliationMatched, ReconciliationSuperseded, ReconciliationMismatched,
		ReconciliationReturned, ReconciliationFailed, ReconciliationStale,
		ReconciliationPendingSettlement:
		return true
	default:
		return false
	}
}

// ReconciliationReport is the immutable outcome of one settlement
// reconciliation. Verified, Accepted, and Settled stay distinct: Paid is
// true only when all three legs hold on a matched comparison. Quarantined
// outcomes carry a separate repair intent; returned payments carry a
// separate payment-return intent. Neither rewrites payroll.
type ReconciliationReport struct {
	InstructionID       string
	ExpectedDestination string
	ExpectedDigest      string
	UsedDigest          string
	Verified            bool
	Accepted            bool
	Settled             bool
	State               ReconciliationState
	Paid                bool
	Quarantined         bool
	RepairIntentID      string
	ReturnIntentID      string
	ReportDigest        string
}

func (r ReconciliationReport) body() *canonicalbytes.Writer {
	return canonicalbytes.New("hcmnext.domains.paymethod.ReconciliationReport", 1).
		String("instruction_id", r.InstructionID).
		String("expected_destination", r.ExpectedDestination).
		String("expected_digest", r.ExpectedDigest).
		String("used_digest", r.UsedDigest).
		Bool("verified", r.Verified).
		Bool("accepted", r.Accepted).
		Bool("settled", r.Settled).
		String("state", string(r.State)).
		Bool("paid", r.Paid).
		Bool("quarantined", r.Quarantined).
		String("repair_intent_id", r.RepairIntentID).
		String("return_intent_id", r.ReturnIntentID)
}

func (r ReconciliationReport) computedDigest() string {
	digest, err := r.body().Digest()
	if err != nil {
		return ""
	}
	return digest
}

// Validate checks the report bindings and self-digest.
func (r ReconciliationReport) Validate() error {
	if strings.TrimSpace(r.InstructionID) == "" || strings.TrimSpace(r.ExpectedDestination) == "" || strings.TrimSpace(r.ExpectedDigest) == "" || strings.TrimSpace(r.UsedDigest) == "" {
		return reconciliationRefusal("report", "report identity is incomplete", ErrInvalidReconciliation)
	}
	if !r.State.Valid() {
		return reconciliationRefusal("state", "reconciliation state is unknown", ErrInvalidReconciliation)
	}
	if r.Paid && (!r.Verified || !r.Accepted || !r.Settled || r.State != ReconciliationMatched || r.Quarantined) {
		return reconciliationRefusal("paid", "paid requires verified, accepted, and settled legs on a matched comparison", ErrInvalidReconciliation)
	}
	if r.Quarantined && strings.TrimSpace(r.RepairIntentID) == "" {
		return reconciliationRefusal("repair_intent_id", "a quarantined outcome requires a repair intent", ErrInvalidReconciliation)
	}
	if r.State == ReconciliationReturned && strings.TrimSpace(r.ReturnIntentID) == "" {
		return reconciliationRefusal("return_intent_id", "a returned payment requires a payment-return intent", ErrInvalidReconciliation)
	}
	if r.ReportDigest == "" || r.ReportDigest != r.computedDigest() {
		return reconciliationRefusal("report_digest", "report digest mismatch", ErrInvalidReconciliation)
	}
	return nil
}

// Canonical returns the report evidence bytes, or nil when invalid.
func (r ReconciliationReport) Canonical() []byte {
	if err := r.Validate(); err != nil {
		return nil
	}
	raw, err := r.body().Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ReconciliationExplanation is the read-only summary of a report.
type ReconciliationExplanation struct {
	InstructionID string
	State         ReconciliationState
	Paid          bool
	Quarantined   bool
	Digest        string
}

// Explain returns the report facts.
func (r ReconciliationReport) Explain() (ReconciliationExplanation, error) {
	if err := r.Validate(); err != nil {
		return ReconciliationExplanation{}, err
	}
	return ReconciliationExplanation{
		InstructionID: r.InstructionID, State: r.State,
		Paid: r.Paid, Quarantined: r.Quarantined, Digest: r.ReportDigest,
	}, nil
}

// ReconcileSettlement compares the expected current destination revision,
// the recorded (accepted) submission, and one fresh external observation,
// and returns the exact state. Ambiguity quarantines with a separate repair
// intent and returned payments carry a separate payment-return intent; the
// worker is marked paid only when verification, acceptance, and settlement
// all hold. Inputs are never mutated.
func ReconcileSettlement(expected Destination, used RecordedSubmission, obs SettlementObservation, now values.Instant, maxObservationAgeSec int64) (ReconciliationReport, error) {
	if err := expected.Validate(); err != nil {
		return ReconciliationReport{}, reconciliationRefusal("destination", "expected destination is invalid", err)
	}
	if expected.CanonicalDigest == "" || expected.CanonicalDigest != expected.computedDigest() {
		return ReconciliationReport{}, reconciliationRefusal("destination", "expected destination digest does not match its revision", ErrInvalidReconciliation)
	}
	if err := used.Validate(); err != nil {
		return ReconciliationReport{}, err
	}
	if err := obs.validate(); err != nil {
		return ReconciliationReport{}, err
	}
	if err := now.Validate(); err != nil {
		return ReconciliationReport{}, reconciliationRefusal("now", "reference instant is required", err)
	}
	if maxObservationAgeSec < 0 {
		return ReconciliationReport{}, reconciliationRefusal("max_observation_age", "maximum observation age must not be negative", ErrInvalidReconciliation)
	}
	if obs.InstructionID != used.InstructionID {
		return ReconciliationReport{}, reconciliationRefusal("instruction_id", "observation is for a different instruction", ErrInvalidReconciliation)
	}
	if obs.ObservedAt.After(now) {
		return ReconciliationReport{}, reconciliationRefusal("observed_at", "observation is newer than the reference instant", ErrInvalidReconciliation)
	}
	report := ReconciliationReport{
		InstructionID:       used.InstructionID,
		ExpectedDestination: expected.DestinationID,
		ExpectedDigest:      expected.CanonicalDigest,
		UsedDigest:          used.DestinationDigest,
		Verified:            expected.Verification == VerificationVerified,
		Accepted:            true,
	}
	finish := func(state ReconciliationState, settled, quarantined bool) (ReconciliationReport, error) {
		report.State, report.Settled, report.Quarantined = state, settled, quarantined
		if quarantined {
			report.RepairIntentID = "settlement-repair/" + used.InstructionID
		}
		if state == ReconciliationReturned {
			report.ReturnIntentID = "payment-return/" + used.InstructionID
		}
		report.Paid = state == ReconciliationMatched && report.Verified && report.Accepted && settled && !quarantined
		report.ReportDigest = report.computedDigest()
		if err := report.Validate(); err != nil {
			return ReconciliationReport{}, err
		}
		return report, nil
	}
	// Freshness first: an observation the provider could not have made, or
	// one older than the window, quarantines as stale.
	nowSec, _ := now.Unix()
	obsSec, _ := obs.ObservedAt.Unix()
	subSec, _ := used.SubmittedAt.Unix()
	if obsSec < subSec || nowSec-obsSec > maxObservationAgeSec {
		return finish(ReconciliationStale, false, true)
	}
	// Payroll must settle to the current revision: a superseded or foreign
	// destination quarantines with a repair intent and never marks paid.
	if used.DestinationID != expected.DestinationID {
		return finish(ReconciliationMismatched, false, true)
	}
	if used.DestinationDigest != expected.CanonicalDigest {
		return finish(ReconciliationSuperseded, obs.Status == SettlementSettled, true)
	}
	switch obs.Status {
	case SettlementReturned:
		return finish(ReconciliationReturned, false, false)
	case SettlementFailed:
		return finish(ReconciliationFailed, false, true)
	case SettlementPending:
		return finish(ReconciliationPendingSettlement, false, false)
	default:
		return finish(ReconciliationMatched, true, false)
	}
}
