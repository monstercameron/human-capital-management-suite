package application

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/appointment"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	sharedreservation "github.com/monstercameron/human-capital-management-suite/internal/resource/reservation"
)

// The methods in this file form the APPT-001..004 contract surface alongside
// the separate APPT-005/006 event projection. Keeping the adapters here avoids
// a second appointment authority or process-wide state.

// ServedAppointmentSurface exposes the APPT-001..004 appointment contract
// through the application boundary. State remains caller-owned by requiring a
// ledger argument for every reservation and lifecycle operation.
type ServedAppointmentSurface struct{}

// NewServedAppointmentSurface returns the APPT-001..004 capabilities exposed
// by the served application boundary.
func NewServedAppointmentSurface() ServedAppointmentSurface { return ServedAppointmentSurface{} }

// Appointment returns the APPT-001..004 surface exposed by a composed app. A
// nil application has no served capabilities.
func (a *App) Appointment() ServedAppointmentSurface {
	if a == nil {
		return ServedAppointmentSurface{}
	}
	return NewServedAppointmentSurface()
}

func (ServedAppointmentSurface) ValidateRequirement(in appointment.Requirement) error {
	return in.Validate()
}

func (ServedAppointmentSurface) Publish(in appointment.Requirement) (appointment.Publication, error) {
	return appointment.Publish(in)
}

func (ServedAppointmentSurface) NewRequirement(in appointment.Requirement) (appointment.Requirement, error) {
	return appointment.NewAppointmentRequirement(in)
}

func (ServedAppointmentSurface) NewResourceType(in appointment.ResourceType) (appointment.ResourceType, error) {
	return appointment.NewResourceType(in)
}

func (ServedAppointmentSurface) CheckFeasibility(in appointment.Requirement, available []appointment.ResourceAvailability) (appointment.FeasibilityResult, error) {
	return appointment.CheckFeasibility(in, available)
}

func (ServedAppointmentSurface) Explain(in appointment.Requirement) (appointment.AppointmentExplanation, error) {
	return appointment.Explain(in)
}

func (ServedAppointmentSurface) ResolveAvailability(in appointment.Requirement, signals []appointment.CandidateSignal) (appointment.AvailabilityResolution, error) {
	return appointment.ResolveAvailability(in, signals)
}

func (ServedAppointmentSurface) NewLedger() *appointment.ReservationLedger {
	return appointment.NewReservationLedgerWithStore(sharedreservation.NewStore())
}

func (ServedAppointmentSurface) Reserve(ledger *appointment.ReservationLedger, in appointment.Requirement, request appointment.ReservationRequest, now time.Time) (appointment.Reservation, error) {
	return ledger.Reserve(in, request, now)
}

func (ServedAppointmentSurface) GetReservation(ledger *appointment.ReservationLedger, id string) (appointment.Reservation, error) {
	return ledger.Get(id)
}

func (ServedAppointmentSurface) ActiveReservations(ledger *appointment.ReservationLedger) int {
	return ledger.Active()
}

func (ServedAppointmentSurface) Release(ledger *appointment.ReservationLedger, id string, now time.Time) (appointment.Reservation, error) {
	return ledger.Release(id, now)
}

func (ServedAppointmentSurface) Expire(ledger *appointment.ReservationLedger, id string, now time.Time) (appointment.Reservation, error) {
	return ledger.Expire(id, now)
}

func (ServedAppointmentSurface) Confirm(ledger *appointment.ReservationLedger, id, expectedDigest, idempotencyKey string, now time.Time) (appointment.Reservation, error) {
	return ledger.Confirm(id, expectedDigest, idempotencyKey, now)
}

func (ServedAppointmentSurface) Reschedule(ledger *appointment.ReservationLedger, id, expectedDigest string, slot values.EffectiveInterval, idempotencyKey string, now time.Time) (appointment.Reservation, error) {
	return ledger.Reschedule(id, expectedDigest, slot, idempotencyKey, now)
}

func (ServedAppointmentSurface) Cancel(ledger *appointment.ReservationLedger, id, expectedDigest, idempotencyKey string, now time.Time) (appointment.Reservation, error) {
	return ledger.Cancel(id, expectedDigest, idempotencyKey, now)
}

func (ServedAppointmentSurface) Outbox(ledger *appointment.ReservationLedger) []appointment.Notification {
	return ledger.Outbox()
}
