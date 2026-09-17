// FILING-002: reconcile acknowledgements and append amendments.
//
// ReconcileAcknowledgement advances one filing through prepared,
// submitted, accepted, rejected, paid, amended and repair-required as
// government acceptance is observed — acceptance is an external
// observation, never inferred success. A rejected, partial, late or
// payment-mismatched filing is never complete. ApplyAmendment appends an
// amendment that references the original package and balances to source
// facts; the original is never overwritten.
package filing

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// LifecycleVersion is the rejection version for FILING-002.
const LifecycleVersion = "filing-lifecycle/v1"

var (
	// ErrLifecycleRejected is the FILING-002 sentinel. A rejected,
	// partial, late or payment-mismatched filing that reads complete, or
	// an amendment that overwrites its original, fails with this error.
	ErrLifecycleRejected = errors.New("FILING_002_REJECTED")
)

// LifecycleRejection is the stable FILING-002 failure shape.
type LifecycleRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *LifecycleRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrLifecycleRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the FILING_002_REJECTED sentinel to errors.Is.
func (r *LifecycleRejection) Unwrap() error { return ErrLifecycleRejected }

func lifecycleReject(field, state, reason string) error {
	return &LifecycleRejection{Field: field, State: state, Version: LifecycleVersion, Reason: reason}
}

// FilingState is the closed FILING-002 lifecycle vocabulary.
type FilingState string

const (
	FilingPrepared       FilingState = "PREPARED"
	FilingSubmitted      FilingState = "SUBMITTED"
	FilingAccepted       FilingState = "ACCEPTED"
	FilingRejected       FilingState = "REJECTED"
	FilingPaid           FilingState = "PAID"
	FilingAmended        FilingState = "AMENDED"
	FilingRepairRequired FilingState = "REPAIR_REQUIRED"
)

// Valid reports whether the state is declared.
func (s FilingState) Valid() bool {
	switch s {
	case FilingPrepared, FilingSubmitted, FilingAccepted, FilingRejected,
		FilingPaid, FilingAmended, FilingRepairRequired:
		return true
	default:
		return false
	}
}

// AckObservation is one government acknowledgement.
type AckObservation struct {
	Status        string
	AmountDue     values.Decimal
	AmountPaid    values.Decimal
	Partial       bool
	Late          bool
	ObservedAt    time.Time
	ReferenceHash string
}

// FilingRecord is the lifecycle state for one package digest.
type FilingRecord struct {
	PackageDigest string
	State         FilingState
	AmountDue     values.Decimal
	AmountPaid    values.Decimal
	AmendmentOf   string
	UpdatedAt     time.Time
	Digest        string
}

func (r FilingRecord) computedDigest() string {
	w := canonicalbytes.New("hcmnext.domains.filing.FilingRecord", 1).
		String("package", r.PackageDigest).
		String("state", string(r.State)).
		Value("due", r.AmountDue).
		Value("paid", r.AmountPaid).
		String("amendment_of", r.AmendmentOf).
		String("updated_at", r.UpdatedAt.UTC().Format(time.RFC3339))
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

func zeroAmount() values.Decimal {
	return values.MustDecimal("0.00", 2, values.RoundingHalfUp)
}

// OpenFiling starts the lifecycle for one sealed package.
func OpenFiling(pkg FilingPackage, amountDue values.Decimal, at time.Time) (FilingRecord, error) {
	if pkg.Digest == "" || pkg.Digest != pkg.computedDigest() {
		return FilingRecord{}, lifecycleReject("filing.package", "UNSEALED", "only a sealed package opens a lifecycle")
	}
	if err := amountDue.Validate(); err != nil || amountDue.Sign() < 0 {
		return FilingRecord{}, lifecycleReject("filing.amount_due", "INVALID", "amount due must be a valid non-negative decimal")
	}
	if at.IsZero() {
		return FilingRecord{}, lifecycleReject("filing.at", "MISSING", "lifecycle instant is required")
	}
	rec := FilingRecord{PackageDigest: pkg.Digest, State: FilingPrepared, AmountDue: amountDue, AmountPaid: zeroAmount(), UpdatedAt: at.UTC()}
	rec.Digest = rec.computedDigest()
	return rec, nil
}

// MarkSubmitted records the gateway dispatch: ACCEPTED and FAILED map to
// SUBMITTED and REPAIR_REQUIRED, while AMBIGUOUS leaves PREPARED with a
// repair flag rather than guessing.
func MarkSubmitted(rec FilingRecord, outcome DispatchOutcome, at time.Time) (FilingRecord, error) {
	if rec.State != FilingPrepared {
		return FilingRecord{}, lifecycleReject("filing.state", string(rec.State), "only a prepared filing submits")
	}
	if outcome.PackageDigest != rec.PackageDigest {
		return FilingRecord{}, lifecycleReject("filing.dispatch", "MISMATCH", "dispatch outcome binds another package")
	}
	if at.IsZero() {
		return FilingRecord{}, lifecycleReject("filing.at", "MISSING", "lifecycle instant is required")
	}
	next := rec
	switch outcome.State {
	case DispatchAccepted:
		next.State = FilingSubmitted
	case DispatchFailed:
		next.State = FilingRepairRequired
	case DispatchAmbiguous:
		next.State = FilingRepairRequired
	default:
		return FilingRecord{}, lifecycleReject("filing.dispatch", "UNDECLARED", fmt.Sprintf("dispatch state %q is not declared", outcome.State))
	}
	next.UpdatedAt = at.UTC()
	next.Digest = next.computedDigest()
	return next, nil
}

// ReconcileAcknowledgement advances the lifecycle on a government
// acknowledgement. Government acceptance is an external observation: a
// rejected, partial, late or payment-mismatched filing never completes.
func ReconcileAcknowledgement(rec FilingRecord, ack AckObservation) (FilingRecord, error) {
	if rec.State != FilingSubmitted && rec.State != FilingAccepted {
		return FilingRecord{}, lifecycleReject("filing.state", string(rec.State), "acknowledgements reconcile submitted or accepted filings")
	}
	if ack.ObservedAt.IsZero() {
		return FilingRecord{}, lifecycleReject("filing.ack.observed_at", "MISSING", "observation instant is required")
	}
	if strings.TrimSpace(ack.ReferenceHash) == "" {
		return FilingRecord{}, lifecycleReject("filing.ack.reference", "MISSING", "acknowledgement reference is required")
	}
	if err := ack.AmountDue.Validate(); err != nil || ack.AmountDue.Sign() < 0 {
		return FilingRecord{}, lifecycleReject("filing.ack.amount_due", "INVALID", "acknowledged amount due must be valid and non-negative")
	}
	if err := ack.AmountPaid.Validate(); err != nil || ack.AmountPaid.Sign() < 0 {
		return FilingRecord{}, lifecycleReject("filing.ack.amount_paid", "INVALID", "acknowledged amount paid must be valid and non-negative")
	}
	next := rec
	next.UpdatedAt = ack.ObservedAt.UTC()
	switch strings.ToUpper(strings.TrimSpace(ack.Status)) {
	case "REJECTED":
		next.State = FilingRejected
	case "ACCEPTED":
		switch {
		case ack.Partial:
			next.State = FilingRepairRequired
		case ack.Late:
			next.State = FilingAccepted
		case !ack.AmountDue.Equal(rec.AmountDue):
			return FilingRecord{}, lifecycleReject("filing.ack.amount_due", "MISMATCH", "acknowledged liability diverges from source facts")
		case !ack.AmountPaid.Equal(ack.AmountDue):
			next.State = FilingAccepted
		default:
			next.State = FilingPaid
			next.AmountPaid = ack.AmountPaid
		}
	default:
		return FilingRecord{}, lifecycleReject("filing.ack.status", "UNDECLARED", fmt.Sprintf("acknowledgement status %q is not declared", ack.Status))
	}
	next.Digest = next.computedDigest()
	return next, nil
}

// ApplyAmendment appends an amendment package that references its
// original and balances to source facts. The original record is never
// overwritten; it resolves to AMENDED alongside the new filing.
func ApplyAmendment(original FilingRecord, amendment FilingPackage, amountDue values.Decimal, at time.Time) (FilingRecord, FilingRecord, error) {
	if original.State != FilingAccepted && original.State != FilingPaid && original.State != FilingRejected {
		return FilingRecord{}, FilingRecord{}, lifecycleReject("filing.state", string(original.State), "amendments follow accepted, paid or rejected filings")
	}
	if amendment.Digest == "" || amendment.Digest != amendment.computedDigest() {
		return FilingRecord{}, FilingRecord{}, lifecycleReject("filing.amendment", "UNSEALED", "only a sealed package amends")
	}
	if amendment.Digest == original.PackageDigest {
		return FilingRecord{}, FilingRecord{}, lifecycleReject("filing.amendment", "IDENTICAL", "amendment must differ from its original")
	}
	if err := amountDue.Validate(); err != nil || amountDue.Sign() < 0 {
		return FilingRecord{}, FilingRecord{}, lifecycleReject("filing.amount_due", "INVALID", "amended amount must balance to source facts")
	}
	if at.IsZero() {
		return FilingRecord{}, FilingRecord{}, lifecycleReject("filing.at", "MISSING", "lifecycle instant is required")
	}
	superseded := original
	superseded.State = FilingAmended
	superseded.UpdatedAt = at.UTC()
	superseded.Digest = superseded.computedDigest()
	next := FilingRecord{
		PackageDigest: amendment.Digest, State: FilingPrepared,
		AmountDue: amountDue, AmountPaid: zeroAmount(),
		AmendmentOf: original.PackageDigest, UpdatedAt: at.UTC(),
	}
	next.Digest = next.computedDigest()
	return superseded, next, nil
}
