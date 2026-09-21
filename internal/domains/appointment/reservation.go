// APPT-003: reserve an appointment slot and its resources atomically.
//
// A reservation commits every participant and resource hold together or
// nothing at all: validation and conflict checks run before a single ledger
// insert, so a rejected request persists zero holds. Conflict is fenced at
// the instance level — one participant or one resource instance in one
// overlapping active hold wins; type-level capacity multiplexing stays with
// the APPT-001 feasibility check. Holds expire and release only through
// fenced transitions, and a byte-identical replay returns the committed
// record instead of double-booking.
//
// The ledger is kernel-pure: time arrives as an explicit `now` argument, so
// there is no clock, database, or network dependency. All mutations hold one
// mutex, giving single-winner semantics under concurrency.
package appointment

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrReservationRejected matches every APPT-003 validation refusal.It
	// unwraps together with the *Rejection carrying code, field and version.
	ErrReservationRejected = errors.New("appointment: reservation rejected")
	// ErrReservationConflict matches an accepted-contract request that loses
	// to a competing active hold. The ledger is unchanged.
	ErrReservationConflict = errors.New("appointment: reservation conflict")
	// ErrReservationStateConflict matches an expiry or release attempted from
	// a state that does not allow it.
	ErrReservationStateConflict = errors.New("appointment: reservation state conflict")
)

// ReservationState is the lifecycle of one atomic hold set. HELD is the only
// state that fences a slot; CONFIRMED (APPT-004) keeps fencing it while
// EXPIRED, RELEASED and CANCELLED free it.
type ReservationState string

const (
	ReservationHeld      ReservationState = "HELD"
	ReservationExpired   ReservationState = "EXPIRED"
	ReservationReleased  ReservationState = "RELEASED"
	ReservationConfirmed ReservationState = "CONFIRMED"
	ReservationCancelled ReservationState = "CANCELLED"
)

// active reports whether the state still fences its slot and holds.
func (s ReservationState) active() bool {
	return s == ReservationHeld || s == ReservationConfirmed
}

func rejectReservation(field, state, version, reason string) error {
	return fmt.Errorf("%w: %w", ErrReservationRejected, &Rejection{Code: "APPT_003_REJECTED", Field: field, State: state, Version: version, Reason: reason})
}

func conflictReservation(field, state, version, reason string) error {
	return fmt.Errorf("%w: %w", ErrReservationConflict, &Rejection{Code: "APPT_003_REJECTED", Field: field, State: state, Version: version, Reason: reason})
}

func stateConflict(field, state, version, reason string) error {
	return fmt.Errorf("%w: %w", ErrReservationStateConflict, &Rejection{Code: "APPT_003_REJECTED", Field: field, State: state, Version: version, Reason: reason})
}

// ResourceHold pins one concrete resource instance, in one quantity, for the
// reservation window. The instance ref is what the conflict fence compares.
type ResourceHold struct {
	ResourceRef     values.EntityRef
	ResourceTypeRef values.EntityRef
	ResourceTypeID  string
	Quantity        values.Quantity
}

func (h ResourceHold) validate() error {
	if err := h.ResourceRef.Validate(); err != nil {
		return err
	}
	if h.ResourceTypeRef == (values.EntityRef{}) && h.ResourceTypeID == "" {
		return errors.New("resource type ref or id is required")
	}
	if err := h.Quantity.Validate(); err != nil {
		return err
	}
	if h.Quantity.Value().Sign() <= 0 {
		return errors.New("resource quantity must be positive")
	}
	return nil
}

// ReservationRequest is one atomic booking attempt: a slot inside the
// requirement window, the contracted participants, the contracted resource
// instances, and how long the hold lives past the injected `now`.
type ReservationRequest struct {
	Slot         values.EffectiveInterval
	Participants []values.EntityRef
	Resources    []ResourceHold
	HoldTTL      time.Duration
}

// Reservation is the committed atomic hold set. RequirementDigest binds the
// exact contracted requirement bytes; Digest covers the whole record.
type Reservation struct {
	ID                 string
	RequirementID      string
	RequirementDigest  string
	RequirementVersion string
	RequirementWindow  values.EffectiveInterval
	Cancellation       CancellationRules
	Slot               values.EffectiveInterval
	Participants       []values.EntityRef
	Resources          []ResourceHold
	State              ReservationState
	HeldAt             values.Instant
	ExpiresAt          values.Instant
	Digest             string
}

func (r Reservation) body() []byte {
	participants := append([]values.EntityRef(nil), r.Participants...)
	sort.Slice(participants, func(i, j int) bool { return participants[i].String() < participants[j].String() })
	resources := append([]ResourceHold(nil), r.Resources...)
	sort.Slice(resources, func(i, j int) bool { return resources[i].ResourceRef.String() < resources[j].ResourceRef.String() })
	w := canonicalbytes.New("hcmnext.domains.appointment.Reservation", schemaVersion).
		String("id", r.ID).String("requirement_id", r.RequirementID).
		String("requirement_digest", r.RequirementDigest).String("requirement_version", r.RequirementVersion).
		Value("window", r.RequirementWindow).Value("slot", r.Slot).
		String("state", string(r.State)).Value("held_at", r.HeldAt).Value("expires_at", r.ExpiresAt).
		Count("participants", len(participants))
	for _, p := range participants {
		w.Value("participant", p)
	}
	w.Count("resources", len(resources))
	for _, h := range resources {
		w.Value("resource", h.ResourceRef).Value("quantity", h.Quantity)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r Reservation) computedDigest() string {
	b := r.body()
	if b == nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// fingerprint identifies the booking attempt without time: identical requests
// replay to the same ID, so a retry can never double-book.
func reservationFingerprint(requirementDigest string, r ReservationRequest) string {
	participants := append([]values.EntityRef(nil), r.Participants...)
	sort.Slice(participants, func(i, j int) bool { return participants[i].String() < participants[j].String() })
	resources := append([]ResourceHold(nil), r.Resources...)
	sort.Slice(resources, func(i, j int) bool { return resources[i].ResourceRef.String() < resources[j].ResourceRef.String() })
	w := canonicalbytes.New("hcmnext.domains.appointment.ReservationRequest", schemaVersion).
		String("requirement_digest", requirementDigest).Value("slot", r.Slot).
		Count("participants", len(participants))
	for _, p := range participants {
		w.Value("participant", p)
	}
	w.Count("resources", len(resources))
	for _, h := range resources {
		w.Value("resource", h.ResourceRef).Value("quantity", h.Quantity)
	}
	b, err := w.Bytes()
	if err != nil {
		return ""
	}
	return "rsv-" + canonicalbytes.Digest(b)
}

// NotificationKind names the governed communication emitted by a lifecycle
// transition. Emission behavior belongs to APPT-004; the type and the ledger
// outbox live here so the record shape is fixed before transitions use it.
type NotificationKind string

const (
	NotificationConfirmed   NotificationKind = "CONFIRMED"
	NotificationRescheduled NotificationKind = "RESCHEDULED"
	NotificationCancelled   NotificationKind = "CANCELLED"
)

// Notification is one governed communication appended by a transition. At is
// the injected transition time; Digest covers the whole record.
type Notification struct {
	ReservationID  string
	Kind           NotificationKind
	IdempotencyKey string
	At             values.Instant
	Digest         string
}

// ReservationLedger is the kernel-pure, mutex-guarded hold store. The zero
// value is unusable; build it with NewReservationLedger.
type ReservationLedger struct {
	mu     sync.Mutex
	holds  map[string]Reservation
	outbox []Notification
}

// NewReservationLedger returns an empty reservation ledger.
func NewReservationLedger() *ReservationLedger {
	return &ReservationLedger{holds: make(map[string]Reservation)}
}

// requestState names the lifecycle state observed by validation.
func requestState(req Requirement) string {
	if req.State != "" {
		return req.State
	}
	return "DRAFT"
}

// Reserve validates the requirement, the slot and every hold, fences
// competing active holds, then commits all holds in one insert. A
// byte-identical replay returns the committed record unchanged.
func (l *ReservationLedger) Reserve(req Requirement, r ReservationRequest, now time.Time) (Reservation, error) {
	version := req.Version
	if version == "" {
		version = "v1"
	}
	state := requestState(req)
	if err := req.Validate(); err != nil {
		return Reservation{}, rejectReservation("requirement", state, version, err.Error())
	}
	if err := r.Slot.Validate(); err != nil || r.Slot.Kind() != values.IntervalKindInstant {
		return Reservation{}, rejectReservation("slot", state, version, "a valid INSTANT slot is required")
	}
	window, err := req.modernWindow()
	if err != nil {
		return Reservation{}, rejectReservation("slot", state, version, "the requirement carries no usable window")
	}
	if !coversInstantWindow(window, r.Slot) {
		return Reservation{}, rejectReservation("slot", "OUTSIDE_WINDOW", version, "the slot is outside the contracted requirement window")
	}
	if len(r.Resources) == 0 {
		return Reservation{}, rejectReservation("resources", "MISSING", version, "at least one resource hold is required")
	}
	for i, h := range r.Resources {
		if err := h.validate(); err != nil {
			return Reservation{}, rejectReservation(fmt.Sprintf("resources[%d]", i), state, version, err.Error())
		}
	}
	if len(r.Participants) == 0 {
		return Reservation{}, rejectReservation("participants", "EMPTY", version, "at least one participant hold is required")
	}
	known := make(map[string]struct{}, len(req.ParticipantRefs))
	for _, ref := range req.ParticipantRefs {
		known[ref.String()] = struct{}{}
	}
	seen := make(map[string]struct{}, len(r.Participants))
	tenant := req.RequestTenant()
	for i, p := range r.Participants {
		field := fmt.Sprintf("participants[%d]", i)
		if err := p.Validate(); err != nil {
			return Reservation{}, rejectReservation(field, state, version, err.Error())
		}
		if tenant != "tenant-placeholder" && p.Tenant != tenant {
			return Reservation{}, rejectReservation(field, "CROSS_TENANT", version, "participant holds must share the requirement tenant")
		}
		if _, ok := known[p.String()]; !ok {
			return Reservation{}, rejectReservation(field, "UNKNOWN", version, "participant is not in the contracted requirement")
		}
		if _, dup := seen[p.String()]; dup {
			return Reservation{}, rejectReservation(field, "DUPLICATE", version, "duplicate participant hold")
		}
		seen[p.String()] = struct{}{}
	}
	if r.HoldTTL <= 0 {
		return Reservation{}, rejectReservation("hold_ttl", state, version, "a positive hold TTL is required")
	}

	requirementDigest := req.computedDigest()
	id := reservationFingerprint(requirementDigest, r)
	if id == "" {
		return Reservation{}, rejectReservation("slot", state, version, "the reservation fingerprint is not encodable")
	}
	heldAt := values.NewInstant(now)
	expiresAt := values.NewInstant(now.Add(r.HoldTTL))

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.holds == nil {
		l.holds = make(map[string]Reservation)
	}
	if existing, ok := l.holds[id]; ok {
		return existing, nil
	}
	for _, existing := range l.holds {
		if !existing.State.active() || !intervalsOverlap(existing.Slot, r.Slot) {
			continue
		}
		for _, h := range r.Resources {
			for _, other := range existing.Resources {
				if h.ResourceRef == other.ResourceRef {
					return Reservation{}, conflictReservation("resources", "CONFLICT", version, fmt.Sprintf("resource %s is already held by reservation %s", h.ResourceRef.String(), existing.ID))
				}
			}
		}
		for _, p := range r.Participants {
			for _, other := range existing.Participants {
				if p == other {
					return Reservation{}, conflictReservation("participants", "CONFLICT", version, fmt.Sprintf("participant %s is already held by reservation %s", p.String(), existing.ID))
				}
			}
		}
	}
	res := Reservation{
		ID:                 id,
		RequirementID:      req.ID,
		RequirementDigest:  requirementDigest,
		RequirementVersion: req.Version,
		RequirementWindow:  window,
		Cancellation:       req.CancellationRules,
		Slot:               r.Slot,
		Participants:       append([]values.EntityRef(nil), r.Participants...),
		Resources:          append([]ResourceHold(nil), r.Resources...),
		State:              ReservationHeld,
		HeldAt:             heldAt,
		ExpiresAt:          expiresAt,
	}
	res.Digest = res.computedDigest()
	l.holds[id] = res
	return res, nil
}

// Get returns a defensive copy of one reservation.
func (l *ReservationLedger) Get(id string) (Reservation, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	res, ok := l.holds[id]
	if !ok {
		return Reservation{}, fmt.Errorf("%w: reservation %s: %w", ErrReservationRejected, id, errReservationNotFound())
	}
	return res, nil
}

// Active counts holds that still fence their slot (HELD or CONFIRMED).
func (l *ReservationLedger) Active() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, res := range l.holds {
		if res.State.active() {
			n++
		}
	}
	return n
}

// Release frees a HELD reservation without deleting its record. Any other
// state is a fenced refusal and the record is unchanged.
func (l *ReservationLedger) Release(id string) (Reservation, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	res, ok := l.holds[id]
	if !ok {
		return Reservation{}, fmt.Errorf("%w: reservation %s: %w", ErrReservationRejected, id, errReservationNotFound())
	}
	if res.State != ReservationHeld {
		return Reservation{}, stateConflict("state", string(res.State), res.RequirementVersion, fmt.Sprintf("only a HELD reservation can release, not %s", res.State))
	}
	res.State = ReservationReleased
	res.Digest = res.computedDigest()
	l.holds[id] = res
	return res, nil
}

// Expire moves a HELD reservation past its fence time to EXPIRED. Expiring
// early or from a terminal state is a fenced refusal.
func (l *ReservationLedger) Expire(id string, now time.Time) (Reservation, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	res, ok := l.holds[id]
	if !ok {
		return Reservation{}, fmt.Errorf("%w: reservation %s: %w", ErrReservationRejected, id, errReservationNotFound())
	}
	if res.State != ReservationHeld {
		return Reservation{}, stateConflict("state", string(res.State), res.RequirementVersion, fmt.Sprintf("only a HELD reservation can expire, not %s", res.State))
	}
	if values.NewInstant(now).Compare(res.ExpiresAt) < 0 {
		return Reservation{}, stateConflict("state", string(res.State), res.RequirementVersion, "the hold has not reached its expiry yet")
	}
	res.State = ReservationExpired
	res.Digest = res.computedDigest()
	l.holds[id] = res
	return res, nil
}

func errReservationNotFound() error {
	return errors.New("appointment: reservation not found")
}

// intervalsOverlap reports whether two INSTANT intervals share any instant
// under half-open [start, end) semantics.
func intervalsOverlap(a, b values.EffectiveInterval) bool {
	aStart, ok := a.StartInstant()
	if !ok {
		return false
	}
	bStart, ok := b.StartInstant()
	if !ok {
		return false
	}
	aEnd, aHas := a.EndInstant()
	bEnd, bHas := b.EndInstant()
	if bHas && aStart.Compare(bEnd) >= 0 {
		return false
	}
	if aHas && bStart.Compare(aEnd) >= 0 {
		return false
	}
	return true
}
