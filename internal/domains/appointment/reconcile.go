// APPT-006: reconcile external calendars.
//
// The reconciler compares the expected appointment (slot, participants and
// status drawn from the APPT-004 lifecycle) against a provider observation
// and returns one verdict plus at most one repair. Stale, partial,
// duplicate and deleted events each have their own verdict and never move
// local state by assumption: only an explicit repair entry is ever created,
// and repairs carry identifiers alone — never provider notes, slot detail
// or participant content.
//
// A missing time or resource contract, an unsequenced or future-dated event,
// or an observation that would double-book a competing fenced hold is
// rejected with APPT_006_REJECTED and persists zero decisions and zero
// repairs.
//
// The reconciler is kernel-pure: time arrives as an explicit `now` argument,
// so there is no clock, database, or network dependency. All mutations hold
// one mutex.
package appointment

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrReconcileRejected matches every APPT-006 reconcile refusal. It unwraps
// together with the *Rejection carrying code, field and version. A refusal
// caused by a competing fenced hold additionally matches
// ErrReservationConflict.
var ErrReconcileRejected = errors.New("appointment: external reconcile rejected")

func rejectReconcile(field, state, version, reason string) error {
	return fmt.Errorf("%w: %w", ErrReconcileRejected, &Rejection{Code: "APPT_006_REJECTED", Field: field, State: state, Version: version, Reason: reason})
}

func conflictReconcile(field, state, version, reason string) error {
	return fmt.Errorf("%w: %w: %w", ErrReconcileRejected, ErrReservationConflict, &Rejection{Code: "APPT_006_REJECTED", Field: field, State: state, Version: version, Reason: reason})
}

// ProviderStatus is the lifecycle state a calendar provider reports.
type ProviderStatus string

const (
	ProviderConfirmed ProviderStatus = "CONFIRMED"
	ProviderCancelled ProviderStatus = "CANCELLED"
	ProviderDeleted   ProviderStatus = "DELETED"
)

func (s ProviderStatus) valid() bool {
	return s == ProviderConfirmed || s == ProviderCancelled || s == ProviderDeleted
}

// ProviderObservation is one provider event. Slot is the observed time and
// HasSlot reports whether the provider supplied it; a status-only event is
// partial, never assumed. Note is opaque provider text that is never copied
// into a verdict, repair or digest.
type ProviderObservation struct {
	EventID       string
	ReservationID string
	Slot          values.EffectiveInterval
	HasSlot       bool
	Status        ProviderStatus
	Seq           uint64
	ObservedAt    time.Time
	Note          string
}

// ReconcileVerdict names the reconciliation outcome for one observation.
type ReconcileVerdict string

const (
	ReconcileMatch           ReconcileVerdict = "MATCH"
	ReconcileDriftSlot       ReconcileVerdict = "DRIFT_SLOT"
	ReconcileDriftStatus     ReconcileVerdict = "DRIFT_STATUS"
	ReconcileStale           ReconcileVerdict = "STALE"
	ReconcileDuplicate       ReconcileVerdict = "DUPLICATE"
	ReconcilePartial         ReconcileVerdict = "PARTIAL"
	ReconcileDeletedExternal ReconcileVerdict = "DELETED_EXTERNAL"
)

func (v ReconcileVerdict) String() string { return string(v) }

// RepairKind names the governed follow-up a verdict creates.
type RepairKind string

const (
	RepairNone              RepairKind = "NONE"
	RepairRequestResync     RepairKind = "REQUEST_RESYNC"
	RepairProposeReschedule RepairKind = "PROPOSE_RESCHEDULE"
	RepairFlagReview        RepairKind = "FLAG_REVIEW"
)

// Repair is the durable follow-up for one verdict. It carries identifiers
// only: no slot detail, no participant content, no provider note.
type Repair struct {
	ReservationID string
	EventID       string
	Kind          RepairKind
	Seq           uint64
}

func (r Repair) String() string {
	return fmt.Sprintf("repair reservation=%s event=%s kind=%s seq=%d", r.ReservationID, r.EventID, r.Kind, r.Seq)
}

// ReconcileDecision is the verdict plus its repair. Digest covers the whole
// decision; provider notes never enter it.
type ReconcileDecision struct {
	Verdict ReconcileVerdict
	Repair  Repair
	Digest  string
}

func (d ReconcileDecision) body(reservationID, eventID string) []byte {
	w := canonicalbytes.New("hcmnext.domains.appointment.ReconcileDecision", schemaVersion).
		String("reservation_id", reservationID).String("event_id", eventID).
		String("verdict", string(d.Verdict)).String("repair_kind", string(d.Repair.Kind)).
		Int("seq", int64(d.Repair.Seq))
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// ExpectedAppointment is the local truth an observation is checked against:
// the reservation identity, its fenced slot and its lifecycle status.
type ExpectedAppointment struct {
	ReservationID string
	Slot          values.EffectiveInterval
	Status        ReservationState
	Version       string
}

// Reconciler is the kernel-pure, mutex-guarded reconcile store. The zero
// value is unusable; build it with NewReconciler.
type Reconciler struct {
	mu      sync.Mutex
	seen    map[string]ReconcileDecision
	lastSeq map[string]uint64
	repairs []Repair
}

// NewReconciler returns an empty reconciler.
func NewReconciler() *Reconciler {
	return &Reconciler{seen: make(map[string]ReconcileDecision), lastSeq: make(map[string]uint64)}
}

func reconcileVersion(expected ExpectedAppointment) string {
	if expected.Version != "" {
		return expected.Version
	}
	return "v1"
}

// sameSlot reports whether two INSTANT intervals cover the same instants.
func sameSlot(a, b values.EffectiveInterval) bool {
	aStart, ok := a.StartInstant()
	if !ok {
		return false
	}
	bStart, ok := b.StartInstant()
	if !ok {
		return false
	}
	if aStart.Compare(bStart) != 0 {
		return false
	}
	aEnd, aHas := a.EndInstant()
	bEnd, bHas := b.EndInstant()
	if aHas != bHas {
		return false
	}
	if aHas && aEnd.Compare(bEnd) != 0 {
		return false
	}
	return true
}

// statusMatches reports whether the provider status agrees with the local
// lifecycle status.
func statusMatches(local ReservationState, remote ProviderStatus) bool {
	switch remote {
	case ProviderConfirmed:
		return local == ReservationHeld || local == ReservationConfirmed
	case ProviderCancelled:
		return local == ReservationCancelled
	default:
		return false
	}
}

// accept commits one decision and, when it carries a repair, appends the
// repair. Callers must hold r.mu.
func (r *Reconciler) accept(key string, dec ReconcileDecision) ReconcileDecision {
	dec.Digest = canonicalbytes.Digest(dec.body(dec.Repair.ReservationID, dec.Repair.EventID))
	r.seen[key] = dec
	if dec.Repair.Seq > r.lastSeq[dec.Repair.ReservationID] {
		r.lastSeq[dec.Repair.ReservationID] = dec.Repair.Seq
	}
	if dec.Repair.Kind != RepairNone {
		r.repairs = append(r.repairs, dec.Repair)
	}
	return dec
}

// Reconcile checks one provider observation against the expected appointment.
// competing lists the other currently fenced holds; an observation whose
// slot would double-book one of them is refused. now is the only clock:
// future-dated observations are untrusted. Rejections persist nothing.
func (r *Reconciler) Reconcile(expected ExpectedAppointment, competing []ExpectedAppointment, obs ProviderObservation, now time.Time) (ReconcileDecision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	version := reconcileVersion(expected)
	state := string(expected.Status)
	if state == "" {
		state = "UNKNOWN"
	}
	if expected.ReservationID == "" {
		return ReconcileDecision{}, rejectReconcile("reservation", "UNKNOWN", version, "no expected appointment is bound")
	}
	if obs.EventID == "" {
		return ReconcileDecision{}, rejectReconcile("event", state, version, "the provider event carries no identity")
	}
	if obs.ReservationID == "" || obs.ReservationID != expected.ReservationID {
		return ReconcileDecision{}, rejectReconcile("reservation", state, version, "the observation names no contracted reservation")
	}
	if !obs.HasSlot && !obs.Status.valid() {
		return ReconcileDecision{}, rejectReconcile("slot", state, version, "the observation carries no usable time")
	}
	if !obs.Status.valid() {
		return ReconcileDecision{}, rejectReconcile("status", state, version, "the provider status is not declared")
	}
	if obs.Seq == 0 {
		return ReconcileDecision{}, rejectReconcile("seq", state, version, "the provider event carries no ordering")
	}
	if obs.ObservedAt.IsZero() || obs.ObservedAt.After(now.UTC()) {
		return ReconcileDecision{}, rejectReconcile("observed_at", state, version, "the observation is undated or future-dated")
	}
	key := obs.EventID + "\x00" + fmt.Sprintf("%d", obs.Seq)
	if prior, ok := r.seen[key]; ok {
		return ReconcileDecision{Verdict: ReconcileDuplicate, Repair: Repair{ReservationID: expected.ReservationID, EventID: obs.EventID, Kind: RepairNone, Seq: obs.Seq}, Digest: prior.Digest}, nil
	}
	if obs.Seq <= r.lastSeq[expected.ReservationID] {
		dec := ReconcileDecision{Verdict: ReconcileStale, Repair: Repair{ReservationID: expected.ReservationID, EventID: obs.EventID, Kind: RepairNone, Seq: obs.Seq}}
		dec.Digest = canonicalbytes.Digest(dec.body(expected.ReservationID, obs.EventID))
		r.seen[key] = dec
		return dec, nil
	}
	if obs.HasSlot {
		if err := obs.Slot.Validate(); err != nil || obs.Slot.Kind() != values.IntervalKindInstant {
			return ReconcileDecision{}, rejectReconcile("slot", state, version, "the observed slot is not a valid INSTANT interval")
		}
		for _, other := range competing {
			if other.ReservationID == "" || other.ReservationID == expected.ReservationID {
				continue
			}
			if !other.Status.active() {
				continue
			}
			if intervalsOverlap(other.Slot, obs.Slot) {
				return ReconcileDecision{}, conflictReconcile("slot", "CONFLICT", version, fmt.Sprintf("the observed slot is fenced by competing reservation %s", other.ReservationID))
			}
		}
	}
	if !obs.HasSlot {
		return r.accept(key, ReconcileDecision{
			Verdict: ReconcilePartial,
			Repair:  Repair{ReservationID: expected.ReservationID, EventID: obs.EventID, Kind: RepairRequestResync, Seq: obs.Seq},
		}), nil
	}
	if obs.Status == ProviderDeleted {
		if expected.Status.active() {
			return r.accept(key, ReconcileDecision{
				Verdict: ReconcileDeletedExternal,
				Repair:  Repair{ReservationID: expected.ReservationID, EventID: obs.EventID, Kind: RepairFlagReview, Seq: obs.Seq},
			}), nil
		}
		return r.accept(key, ReconcileDecision{
			Verdict: ReconcileMatch,
			Repair:  Repair{ReservationID: expected.ReservationID, EventID: obs.EventID, Kind: RepairNone, Seq: obs.Seq},
		}), nil
	}
	if !sameSlot(expected.Slot, obs.Slot) {
		return r.accept(key, ReconcileDecision{
			Verdict: ReconcileDriftSlot,
			Repair:  Repair{ReservationID: expected.ReservationID, EventID: obs.EventID, Kind: RepairProposeReschedule, Seq: obs.Seq},
		}), nil
	}
	if !statusMatches(expected.Status, obs.Status) {
		return r.accept(key, ReconcileDecision{
			Verdict: ReconcileDriftStatus,
			Repair:  Repair{ReservationID: expected.ReservationID, EventID: obs.EventID, Kind: RepairFlagReview, Seq: obs.Seq},
		}), nil
	}
	return r.accept(key, ReconcileDecision{
		Verdict: ReconcileMatch,
		Repair:  Repair{ReservationID: expected.ReservationID, EventID: obs.EventID, Kind: RepairNone, Seq: obs.Seq},
	}), nil
}

// Repairs returns a defensive copy of the repairs created so far, in create
// order.
func (r *Reconciler) Repairs() []Repair {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Repair(nil), r.repairs...)
}
