// GARN-006: handle order release, amendment and termination.
//
// ApplyRelease, ApplyAmendment and ApplyTermination move one ACTIVE order
// forward on its effective date while preserving the prior version by
// digest chain. Dates arrive as explicit parameters, so the transition
// stays kernel-pure. An effective date outside the order's validity is
// refused, which prevents deductions outside validity; notice arriving
// after the effective date seals a correction obligation covering the
// late window. IsDeductible reports whether one order may withhold on
// one date.
package garnishment

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

const lifecycleSchemaVersion = 1

// LifecycleVersion is the rejection version carried by LifecycleRejection.
const LifecycleVersion = "garnishment-lifecycle/v1"

var (
	// ErrLifecycleRejected is the GARN-006 sentinel. Effective-dated
	// transitions that would rewrite history or withhold outside
	// validity must fail with this error carrying the offending field,
	// state and version.
	ErrLifecycleRejected = errors.New("GARN_006_REJECTED")
)

// OrderTerminated is the terminal GARN-006 lifecycle state. Terminated
// orders never reactivate and never deduct again.
const OrderTerminated OrderStatus = "TERMINATED"

// TransitionKind is the closed GARN-006 transition vocabulary.
type TransitionKind string

const (
	TransitionAmend     TransitionKind = "AMEND"
	TransitionRelease   TransitionKind = "RELEASE"
	TransitionTerminate TransitionKind = "TERMINATE"
)

func (k TransitionKind) valid() bool {
	return k == TransitionAmend || k == TransitionRelease || k == TransitionTerminate
}

// LifecycleRejection is the stable GARN-006 failure shape.
type LifecycleRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *LifecycleRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrLifecycleRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the GARN_006_REJECTED sentinel to errors.Is.
func (r *LifecycleRejection) Unwrap() error { return ErrLifecycleRejected }

func lifecycleReject(field, state, reason string) error {
	return &LifecycleRejection{Field: field, State: state, Version: LifecycleVersion, Reason: reason}
}

// DatedNotice carries the effective date of a lifecycle change and the
// moment notice arrived. Both are caller-supplied: there is no clock
// read here.
type DatedNotice struct {
	EffectiveAt time.Time
	NoticedAt   time.Time
	Actor       string
	Reason      string
}

// CorrectionObligation covers the window a late notice left exposed:
// withholdings taken from the effective date until notice arrived must
// be corrected.
type CorrectionObligation struct {
	PeriodFrom time.Time
	PeriodTo   time.Time
	Reason     string
}

// LifecycleReceipt seals one effective-dated transition: the prior and
// new order digests, the effective date, and any correction obligation
// a late notice produced.
type LifecycleReceipt struct {
	OrderID         string
	Version         int
	Kind            TransitionKind
	PriorDigest     string
	NewDigest       string
	Status          OrderStatus
	EffectiveAt     time.Time
	NoticedAt       time.Time
	LateNotice      bool
	Correction      *CorrectionObligation
	Actor           string
	Reason          string
	CanonicalDigest string
}

func validateTransition(order AttachmentOrder, notice DatedNotice) error {
	if order.Status != OrderActive {
		return lifecycleReject("order.status", "NOT_ACTIVE", "only active orders transition")
	}
	if notice.EffectiveAt.IsZero() || notice.NoticedAt.IsZero() {
		return lifecycleReject("order.dates", "MISSING", "effective and notice dates are required")
	}
	if strings.TrimSpace(notice.Actor) == "" || strings.TrimSpace(notice.Reason) == "" {
		return lifecycleReject("order.notice", "MISSING", "transitions need an actor and a reason")
	}
	if notice.EffectiveAt.Before(order.EffectiveFrom) {
		return lifecycleReject("order.validity", "PREMATURE", "effective date precedes the order validity")
	}
	if !order.EffectiveTo.IsZero() && !notice.EffectiveAt.Before(order.EffectiveTo) {
		return lifecycleReject("order.validity", "EXPIRED", "effective date is outside the order validity")
	}
	return nil
}

func sealReceipt(orderID string, version int, kind TransitionKind, prior, next AttachmentOrder, notice DatedNotice) LifecycleReceipt {
	receipt := LifecycleReceipt{
		OrderID: orderID, Version: version, Kind: kind,
		PriorDigest: prior.Digest, NewDigest: next.Digest, Status: next.Status,
		EffectiveAt: notice.EffectiveAt.UTC(), NoticedAt: notice.NoticedAt.UTC(),
		Actor: notice.Actor, Reason: notice.Reason,
	}
	if receipt.NoticedAt.After(receipt.EffectiveAt) {
		receipt.LateNotice = true
		receipt.Correction = &CorrectionObligation{
			PeriodFrom: receipt.EffectiveAt,
			PeriodTo:   receipt.NoticedAt,
			Reason:     "late notice: withholdings on or after the effective date must be corrected",
		}
	}
	receipt.CanonicalDigest = receipt.computedDigest()
	return receipt
}

// ApplyRelease closes an ACTIVE order on its effective date.
func ApplyRelease(order AttachmentOrder, notice DatedNotice) (AttachmentOrder, LifecycleReceipt, error) {
	if err := validateTransition(order, notice); err != nil {
		return AttachmentOrder{}, LifecycleReceipt{}, err
	}
	next, err := order.Release(notice.Actor, notice.Reason)
	if err != nil {
		return AttachmentOrder{}, LifecycleReceipt{}, err
	}
	return next, sealReceipt(order.OrderID, next.Version, TransitionRelease, order, next, notice), nil
}

// ApplyAmendment issues the next order version on its effective date.
// The prior version is preserved by digest chain and the original
// document digest is immutable across every version.
func ApplyAmendment(order AttachmentOrder, notice DatedNotice, req AmendRequest) (AttachmentOrder, LifecycleReceipt, error) {
	if err := validateTransition(order, notice); err != nil {
		return AttachmentOrder{}, LifecycleReceipt{}, err
	}
	next, err := order.Amend(req)
	if err != nil {
		return AttachmentOrder{}, LifecycleReceipt{}, err
	}
	return next, sealReceipt(order.OrderID, next.Version, TransitionAmend, order, next, notice), nil
}

// ApplyTermination ends an ACTIVE order permanently. Terminated orders
// never reactivate and never deduct again.
func ApplyTermination(order AttachmentOrder, notice DatedNotice) (AttachmentOrder, LifecycleReceipt, error) {
	if err := validateTransition(order, notice); err != nil {
		return AttachmentOrder{}, LifecycleReceipt{}, err
	}
	next := order
	next.Status = OrderTerminated
	next.Digest = orderDigest(next)
	return next, sealReceipt(order.OrderID, next.Version, TransitionTerminate, order, next, notice), nil
}

// IsDeductible reports whether one order may withhold on one date: the
// order is ACTIVE and the date falls inside its validity window.
func IsDeductible(order AttachmentOrder, at time.Time) bool {
	if order.Status != OrderActive {
		return false
	}
	if at.Before(order.EffectiveFrom) {
		return false
	}
	if !order.EffectiveTo.IsZero() && !at.Before(order.EffectiveTo) {
		return false
	}
	return true
}

func (r LifecycleReceipt) computedDigest() string {
	w := canonicalbytes.New("hcmnext.domains.garnishment.LifecycleReceipt", lifecycleSchemaVersion).
		String("order", r.OrderID).Int("version", int64(r.Version)).String("kind", string(r.Kind)).
		String("prior_digest", r.PriorDigest).String("new_digest", r.NewDigest).String("status", string(r.Status)).
		String("effective_at", r.EffectiveAt.UTC().Format(time.RFC3339Nano)).
		String("noticed_at", r.NoticedAt.UTC().Format(time.RFC3339Nano)).
		Bool("late_notice", r.LateNotice).Bool("has_correction", r.Correction != nil)
	if r.Correction != nil {
		w.String("correction_from", r.Correction.PeriodFrom.UTC().Format(time.RFC3339Nano)).
			String("correction_to", r.Correction.PeriodTo.UTC().Format(time.RFC3339Nano)).
			String("correction_reason", r.Correction.Reason)
	}
	w.String("actor", r.Actor).String("reason", r.Reason)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// Verify checks the seal and internal consistency of a previously
// computed receipt: status matches kind, the late flag matches the
// dates, a late notice carries exactly the exposed window, and the
// digest matches.
func (r LifecycleReceipt) Verify() error {
	if strings.TrimSpace(r.OrderID) == "" || r.Version <= 0 || !r.Kind.valid() {
		return lifecycleReject("receipt.identity", "MISSING", "order, version and kind are required")
	}
	want := map[TransitionKind]OrderStatus{
		TransitionAmend: OrderPendingVerification, TransitionRelease: OrderReleased, TransitionTerminate: OrderTerminated,
	}[r.Kind]
	if r.Status != want {
		return lifecycleReject("receipt.status", "MISMATCH", "status does not match the transition kind")
	}
	if strings.TrimSpace(r.PriorDigest) == "" || strings.TrimSpace(r.NewDigest) == "" || r.PriorDigest == r.NewDigest {
		return lifecycleReject("receipt.lineage", "BROKEN", "prior and new digests must chain")
	}
	if r.EffectiveAt.IsZero() || r.NoticedAt.IsZero() || strings.TrimSpace(r.Actor) == "" {
		return lifecycleReject("receipt.dates", "MISSING", "effective date, notice date and actor are required")
	}
	if late := r.NoticedAt.After(r.EffectiveAt); late != r.LateNotice {
		return lifecycleReject("receipt.late_notice", "MISMATCH", "late flag does not match the dates")
	}
	if !r.LateNotice {
		if r.Correction != nil {
			return lifecycleReject("receipt.correction", "MISMATCH", "timely notice carries no correction")
		}
	} else {
		if r.Correction == nil {
			return lifecycleReject("receipt.correction", "MISSING", "late notice must carry a correction obligation")
		}
		if !r.Correction.PeriodFrom.Equal(r.EffectiveAt) || !r.Correction.PeriodTo.Equal(r.NoticedAt) ||
			strings.TrimSpace(r.Correction.Reason) == "" {
			return lifecycleReject("receipt.correction", "MISMATCH", "correction must cover the late window")
		}
	}
	if r.CanonicalDigest == "" || r.computedDigest() != r.CanonicalDigest {
		return lifecycleReject("receipt.seal", "MISMATCH", "canonical digest mismatch")
	}
	return nil
}
