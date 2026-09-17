// APPT-005: handle attendance and no-show.
//
// Attendance evidence is recorded against the APPT-004 reservation lifecycle
// under an injected clock. Attended and no-show are the only recordable
// outcomes: a cancelled reservation carries no attendance evidence (its
// CANCELLED state is distinct), and the absence of a record is the UNKNOWN
// state, which never implies a downstream action. First evidence is trusted
// only for CONFIRMED reservations at or after the slot start; later
// corrections supersede it in a digest-linked history. The downstream
// fee/case/workflow action is derived from the contracted no-show policy,
// never from assumption.
//
// The ledger is kernel-pure: time arrives as an explicit `now` argument, so
// there is no clock, database, or network dependency. All mutations hold one
// mutex.
package appointment

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrAttendanceRejected matches every APPT-005 attendance refusal. It
// unwraps together with the *Rejection carrying code, field and version.
var ErrAttendanceRejected = errors.New("appointment: attendance rejected")

func rejectAttendance(field, state, version, reason string) error {
	return fmt.Errorf("%w: %w", ErrAttendanceRejected, &Rejection{Code: "APPT_005_REJECTED", Field: field, State: state, Version: version, Reason: reason})
}

// AttendanceOutcome is the trusted evidence recorded for one reservation.
// CANCELLED is deliberately absent: cancellation is a reservation state, not
// attendance evidence. UNKNOWN is the absence of a record, never a value.
type AttendanceOutcome string

const (
	AttendanceAttended AttendanceOutcome = "ATTENDED"
	AttendanceNoShow   AttendanceOutcome = "NO_SHOW"
)

func (o AttendanceOutcome) valid() bool {
	return o == AttendanceAttended || o == AttendanceNoShow
}

// AttendanceAction is the downstream fee/case/workflow action derived from
// the contracted no-show policy.
type AttendanceAction string

const (
	AttendanceActionNone     AttendanceAction = "NONE"
	AttendanceActionFollowUp AttendanceAction = "FOLLOW_UP"
	AttendanceActionReview   AttendanceAction = "REVIEW"
)

// AttendanceActionFor maps evidence to its downstream action under the
// contracted policy. Unknown (empty) evidence never implies an action.
func AttendanceActionFor(outcome AttendanceOutcome, policy CancellationRules) AttendanceAction {
	switch outcome {
	case AttendanceAttended:
		return AttendanceActionNone
	case AttendanceNoShow:
		switch policy.NoShow {
		case NoShowFollowUp:
			return AttendanceActionFollowUp
		case NoShowReview:
			return AttendanceActionReview
		default:
			return AttendanceActionNone
		}
	default:
		return AttendanceActionNone
	}
}

// AttendanceRecord is one trusted evidence entry. Sequence 1 is the first
// evidence; higher sequences are corrections linked to the prior digest via
// Supersedes. Digest covers the whole record.
type AttendanceRecord struct {
	ReservationID string
	Outcome       AttendanceOutcome
	Action        AttendanceAction
	Sequence      uint64
	Supersedes    string
	At            values.Instant
	Digest        string
}

func (r AttendanceRecord) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.appointment.AttendanceRecord", schemaVersion).
		String("reservation_id", r.ReservationID).String("outcome", string(r.Outcome)).
		String("action", string(r.Action)).Int("sequence", int64(r.Sequence)).
		String("supersedes", r.Supersedes).Value("at", r.At)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r AttendanceRecord) computedDigest() string {
	b := r.body()
	if b == nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// DispatchedAction is the single downstream effect emitted per evidence
// entry. Corrections dispatch a superseding action; rejections dispatch
// nothing.
type DispatchedAction struct {
	ReservationID string
	Action        AttendanceAction
	Sequence      uint64
	At            values.Instant
	Digest        string
}

// AttendanceLedger is the kernel-pure, mutex-guarded attendance store. It
// reads reservation state through the reservation ledger and never mutates
// it. The zero value is unusable; build it with NewAttendanceLedger.
type AttendanceLedger struct {
	mu           sync.Mutex
	reservations *ReservationLedger
	records      map[string]AttendanceRecord
	history      map[string][]AttendanceRecord
	replayed     map[string]AttendanceRecord
	dispatched   []DispatchedAction
}

// NewAttendanceLedger returns an empty attendance ledger over the given
// reservation ledger.
func NewAttendanceLedger(reservations *ReservationLedger) *AttendanceLedger {
	return &AttendanceLedger{
		reservations: reservations,
		records:      make(map[string]AttendanceRecord),
		history:      make(map[string][]AttendanceRecord),
		replayed:     make(map[string]AttendanceRecord),
	}
}

func attendanceVersion(res Reservation) string {
	if res.RequirementVersion != "" {
		return res.RequirementVersion
	}
	return "v1"
}

func (l *AttendanceLedger) replayKey(id, key string) (AttendanceRecord, bool) {
	if key == "" {
		return AttendanceRecord{}, false
	}
	rec, ok := l.replayed[id+"\x00"+key]
	return rec, ok
}

// store commits one evidence entry and its downstream action. Callers must
// hold l.mu.
func (l *AttendanceLedger) store(rec AttendanceRecord, key string) AttendanceRecord {
	rec.Digest = rec.computedDigest()
	l.records[rec.ReservationID] = rec
	l.history[rec.ReservationID] = append(l.history[rec.ReservationID], rec)
	if key != "" {
		l.replayed[rec.ReservationID+"\x00"+key] = rec
	}
	l.dispatched = append(l.dispatched, DispatchedAction{
		ReservationID: rec.ReservationID,
		Action:        rec.Action,
		Sequence:      rec.Sequence,
		At:            rec.At,
		Digest:        rec.Digest,
	})
	return rec
}

// checkTrust validates the reservation and the evidence time without
// mutating anything. Callers must hold l.mu.
func (l *AttendanceLedger) checkTrust(id string, outcome AttendanceOutcome, now time.Time) (Reservation, error) {
	if l.reservations == nil {
		return Reservation{}, rejectAttendance("ledger", "UNKNOWN", "v1", "no reservation ledger is bound")
	}
	res, err := l.reservations.Get(id)
	if err != nil {
		return Reservation{}, rejectAttendance("reservation", "UNKNOWN", "v1", fmt.Sprintf("reservation %s is unknown", id))
	}
	version := attendanceVersion(res)
	if res.State != ReservationConfirmed {
		return Reservation{}, rejectAttendance("state", string(res.State), version, fmt.Sprintf("only a CONFIRMED reservation carries attendance, not %s", res.State))
	}
	if !outcome.valid() {
		return Reservation{}, rejectAttendance("outcome", string(res.State), version, "only ATTENDED or NO_SHOW evidence is recordable")
	}
	start, ok := res.Slot.StartInstant()
	if !ok {
		return Reservation{}, rejectAttendance("slot", string(res.State), version, "the reservation slot has no usable start")
	}
	if nowTime := now.UTC(); nowTime.Before(start.Time()) {
		return Reservation{}, rejectAttendance("at", string(res.State), version, "attendance before the slot start is assumption")
	}
	return res, nil
}

// Record stores the first trusted evidence for a reservation and dispatches
// its policy-derived action. A repeated idempotency key replays the stored
// record without a duplicate dispatch; any other second record must use
// Correct.
func (l *AttendanceLedger) Record(id string, outcome AttendanceOutcome, idempotencyKey string, now time.Time) (AttendanceRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if rec, ok := l.replayKey(id, idempotencyKey); ok {
		return rec, nil
	}
	res, err := l.checkTrust(id, outcome, now)
	if err != nil {
		return AttendanceRecord{}, err
	}
	if _, exists := l.records[id]; exists {
		return AttendanceRecord{}, rejectAttendance("sequence", string(res.State), attendanceVersion(res), "attendance is already recorded; correct it instead")
	}
	return l.store(AttendanceRecord{
		ReservationID: id,
		Outcome:       outcome,
		Action:        AttendanceActionFor(outcome, res.Cancellation),
		Sequence:      1,
		At:            values.NewInstant(now),
	}, idempotencyKey), nil
}

// Correct supersedes recorded evidence with a different outcome and
// dispatches the superseding policy-derived action. The prior digest stays
// linked through Supersedes so the full history remains auditable.
func (l *AttendanceLedger) Correct(id string, outcome AttendanceOutcome, idempotencyKey string, now time.Time) (AttendanceRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if rec, ok := l.replayKey(id, idempotencyKey); ok {
		return rec, nil
	}
	res, err := l.checkTrust(id, outcome, now)
	if err != nil {
		return AttendanceRecord{}, err
	}
	prior, exists := l.records[id]
	if !exists {
		return AttendanceRecord{}, rejectAttendance("sequence", string(res.State), attendanceVersion(res), "no attendance is recorded to correct")
	}
	if outcome == prior.Outcome {
		return AttendanceRecord{}, rejectAttendance("outcome", string(res.State), attendanceVersion(res), "the correction repeats the recorded outcome")
	}
	return l.store(AttendanceRecord{
		ReservationID: id,
		Outcome:       outcome,
		Action:        AttendanceActionFor(outcome, res.Cancellation),
		Sequence:      prior.Sequence + 1,
		Supersedes:    prior.Digest,
		At:            values.NewInstant(now),
	}, idempotencyKey), nil
}

// Get returns the current evidence for a reservation, or false when the
// attendance is still UNKNOWN.
func (l *AttendanceLedger) Get(id string) (AttendanceRecord, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, ok := l.records[id]
	return rec, ok
}

// History returns the full evidence chain for a reservation in sequence
// order, oldest first.
func (l *AttendanceLedger) History(id string) []AttendanceRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]AttendanceRecord(nil), l.history[id]...)
}

// Dispatched returns a defensive copy of the downstream actions emitted so
// far, in emit order.
func (l *AttendanceLedger) Dispatched() []DispatchedAction {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]DispatchedAction(nil), l.dispatched...)
}
