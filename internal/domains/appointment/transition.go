// APPT-004: confirm, reschedule and cancel appointments.
//
// Transitions run against the APPT-003 reservation ledger under its mutex,
// so concurrent attempts serialize to exactly one winner. Every transition
// binds the contracted requirement digest (a changed availability contract or
// version fails instead of moving stale state), honors the requirement's
// cancellation policy, and appends exactly one governed communication to the
// ledger outbox. A repeated idempotency key replays the current record
// without appending a duplicate or re-moving state; callers that need
// per-attempt accounting must use distinct keys.
//
// Time is injected: `now` is the only clock. No database, network, or wall
// clock is touched.
package appointment

import (
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrTransitionRejected matches every APPT-004 transition refusal. It
// unwraps together with the *Rejection carrying code, field and version.
var ErrTransitionRejected = errors.New("appointment: transition rejected")

func rejectTransition(field, state, version, reason string) error {
	return fmt.Errorf("%w: %w", ErrTransitionRejected, &Rejection{Code: "APPT_004_REJECTED", Field: field, State: state, Version: version, Reason: reason})
}

func conflictTransition(field, state, version, reason string) error {
	return fmt.Errorf("%w: %w", ErrReservationConflict, &Rejection{Code: "APPT_004_REJECTED", Field: field, State: state, Version: version, Reason: reason})
}

func notificationDigest(reservationID string, kind NotificationKind, key string, at values.Instant) string {
	w := canonicalbytes.New("hcmnext.domains.appointment.Notification", schemaVersion).
		String("reservation_id", reservationID).String("kind", string(kind)).
		String("idempotency_key", key).Value("at", at)
	b, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// Outbox returns a defensive copy of the governed communications appended so
// far, in append order.
func (l *ReservationLedger) Outbox() []Notification {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Notification(nil), l.outbox...)
}

// replayed reports whether this idempotency key already produced a
// notification for this reservation. Callers must hold l.mu.
func (l *ReservationLedger) replayed(id, key string) bool {
	if key == "" {
		return false
	}
	for _, n := range l.outbox {
		if n.ReservationID == id && n.IdempotencyKey == key {
			return true
		}
	}
	return false
}

// appendNotification records one governed communication. Callers must hold
// l.mu and must have checked replayed first.
func (l *ReservationLedger) appendNotification(id string, kind NotificationKind, key string, now time.Time) Notification {
	n := Notification{
		ReservationID:  id,
		Kind:           kind,
		IdempotencyKey: key,
		At:             values.NewInstant(now),
	}
	n.Digest = notificationDigest(id, kind, key, n.At)
	l.outbox = append(l.outbox, n)
	return n
}

// bindDigest refuses a transition when the caller quotes a stale requirement
// contract. Changed availability or a new requirement version must
// re-reserve, never slide under a moving reservation.
func bindDigest(res Reservation, expectedDigest string) error {
	version := res.RequirementVersion
	if version == "" {
		version = "v1"
	}
	if expectedDigest == "" || expectedDigest != res.RequirementDigest {
		return rejectTransition("requirement_digest", "STALE", version, "the quoted requirement contract does not match the reservation")
	}
	return nil
}

// expireIfDue lazily moves a HELD reservation past its fence time to
// EXPIRED. Callers must hold l.mu. It reports whether the hold is still live.
func (l *ReservationLedger) expireIfDue(res *Reservation, now time.Time) bool {
	if res.State == ReservationHeld && !values.NewInstant(now).Before(res.ExpiresAt) {
		res.State = ReservationExpired
		res.Digest = res.computedDigest()
		l.holds[res.ID] = *res
		return false
	}
	return res.State == ReservationHeld || res.State == ReservationConfirmed
}

// Confirm moves a live HELD reservation to CONFIRMED and emits one CONFIRMED
// communication. A stale digest, an expired hold, or any non-HELD state is a
// refusal with the record left exactly as found (apart from lazy expiry).
func (l *ReservationLedger) Confirm(id, expectedDigest, idempotencyKey string, now time.Time) (Reservation, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	res, ok := l.holds[id]
	if !ok {
		return Reservation{}, fmt.Errorf("%w: reservation %s: %w", ErrTransitionRejected, id, errReservationNotFound())
	}
	if l.replayed(id, idempotencyKey) {
		return res, nil
	}
	if err := bindDigest(res, expectedDigest); err != nil {
		return Reservation{}, err
	}
	version := res.RequirementVersion
	if version == "" {
		version = "v1"
	}
	if !l.expireIfDue(&res, now) {
		return Reservation{}, rejectTransition("state", string(res.State), version, fmt.Sprintf("only a live HELD reservation can confirm, not %s", res.State))
	}
	if res.State != ReservationHeld {
		return Reservation{}, rejectTransition("state", string(res.State), version, fmt.Sprintf("only a HELD reservation can confirm, not %s", res.State))
	}
	res.State = ReservationConfirmed
	res.Digest = res.computedDigest()
	l.holds[id] = res
	l.appendNotification(id, NotificationConfirmed, idempotencyKey, now)
	return res, nil
}

// Reschedule swaps the slot of a live HELD or CONFIRMED reservation
// atomically: the new slot must sit inside the contracted window and fence
// no competing hold, or the record and outbox are unchanged. State is
// preserved; one RESCHEDULED communication is emitted.
func (l *ReservationLedger) Reschedule(id, expectedDigest string, newSlot values.EffectiveInterval, idempotencyKey string, now time.Time) (Reservation, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	res, ok := l.holds[id]
	if !ok {
		return Reservation{}, fmt.Errorf("%w: reservation %s: %w", ErrTransitionRejected, id, errReservationNotFound())
	}
	if l.replayed(id, idempotencyKey) {
		return res, nil
	}
	if err := bindDigest(res, expectedDigest); err != nil {
		return Reservation{}, err
	}
	version := res.RequirementVersion
	if version == "" {
		version = "v1"
	}
	if !l.expireIfDue(&res, now) {
		return Reservation{}, rejectTransition("state", string(res.State), version, fmt.Sprintf("only a live reservation can reschedule, not %s", res.State))
	}
	if res.State != ReservationHeld && res.State != ReservationConfirmed {
		return Reservation{}, rejectTransition("state", string(res.State), version, fmt.Sprintf("only a HELD or CONFIRMED reservation can reschedule, not %s", res.State))
	}
	if err := newSlot.Validate(); err != nil || newSlot.Kind() != values.IntervalKindInstant {
		return Reservation{}, rejectTransition("slot", string(res.State), version, "a valid INSTANT slot is required")
	}
	if !coversInstantWindow(res.RequirementWindow, newSlot) {
		return Reservation{}, rejectTransition("slot", "OUTSIDE_WINDOW", version, "the new slot is outside the contracted requirement window")
	}
	for _, existing := range l.holds {
		if existing.ID == id || !existing.State.active() || !intervalsOverlap(existing.Slot, newSlot) {
			continue
		}
		for _, h := range res.Resources {
			for _, other := range existing.Resources {
				if h.ResourceRef == other.ResourceRef {
					return Reservation{}, conflictTransition("slot", "CONFLICT", version, fmt.Sprintf("resource %s is already held by reservation %s", h.ResourceRef.String(), existing.ID))
				}
			}
		}
		for _, p := range res.Participants {
			for _, other := range existing.Participants {
				if p == other {
					return Reservation{}, conflictTransition("slot", "CONFLICT", version, fmt.Sprintf("participant %s is already held by reservation %s", p.String(), existing.ID))
				}
			}
		}
	}
	res.Slot = newSlot
	res.Digest = res.computedDigest()
	l.holds[id] = res
	l.appendNotification(id, NotificationRescheduled, idempotencyKey, now)
	return res, nil
}

// Cancel moves a live HELD or CONFIRMED reservation to CANCELLED under its
// contracted cancellation policy and emits one CANCELLED communication. A
// disabled policy, a late cancel inside the notice window, a stale digest,
// or a non-live state is a refusal with the record unchanged.
func (l *ReservationLedger) Cancel(id, expectedDigest, idempotencyKey string, now time.Time) (Reservation, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	res, ok := l.holds[id]
	if !ok {
		return Reservation{}, fmt.Errorf("%w: reservation %s: %w", ErrTransitionRejected, id, errReservationNotFound())
	}
	if l.replayed(id, idempotencyKey) {
		return res, nil
	}
	if err := bindDigest(res, expectedDigest); err != nil {
		return Reservation{}, err
	}
	version := res.RequirementVersion
	if version == "" {
		version = "v1"
	}
	if !l.expireIfDue(&res, now) {
		return Reservation{}, rejectTransition("state", string(res.State), version, fmt.Sprintf("only a live reservation can cancel, not %s", res.State))
	}
	if res.State != ReservationHeld && res.State != ReservationConfirmed {
		return Reservation{}, rejectTransition("state", string(res.State), version, fmt.Sprintf("only a HELD or CONFIRMED reservation can cancel, not %s", res.State))
	}
	if res.Cancellation.Kind == CancellationDisabled {
		return Reservation{}, rejectTransition("cancellation_policy", string(res.State), version, "the contracted policy forbids cancellation")
	}
	start, ok := res.Slot.StartInstant()
	if !ok {
		return Reservation{}, rejectTransition("cancellation_policy", string(res.State), version, "the reservation slot has no usable start")
	}
	if nowTime := now.UTC(); nowTime.After(start.Time().Add(-res.Cancellation.Notice)) {
		return Reservation{}, rejectTransition("cancellation_policy", "LATE", version, "cancellation falls inside the contracted notice window")
	}
	res.State = ReservationCancelled
	res.Digest = res.computedDigest()
	l.holds[id] = res
	l.appendNotification(id, NotificationCancelled, idempotencyKey, now)
	return res, nil
}
