package application

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/appointment"
)

// ServedAppointmentEventsSurface exposes attendance and external-calendar
// reconciliation through the application boundary used by shipped commands.
// The appointment package remains the semantic owner; this surface only
// binds its pure operations and keeps all ledger state caller-owned.
type ServedAppointmentEventsSurface struct {
	NewAttendanceLedger func(*appointment.ReservationLedger) *appointment.AttendanceLedger
	RecordAttendance    func(*appointment.AttendanceLedger, string, appointment.AttendanceOutcome, string, time.Time) (appointment.AttendanceRecord, error)
	CorrectAttendance   func(*appointment.AttendanceLedger, string, appointment.AttendanceOutcome, string, time.Time) (appointment.AttendanceRecord, error)
	AttendanceActionFor func(appointment.AttendanceOutcome, appointment.CancellationRules) appointment.AttendanceAction
	NewReconciler       func() *appointment.Reconciler
	ReconcileCalendar   func(*appointment.Reconciler, appointment.ExpectedAppointment, []appointment.ExpectedAppointment, appointment.ProviderObservation, time.Time) (appointment.ReconcileDecision, error)
}

// NewServedAppointmentEventsSurface returns the attendance and reconciliation
// capabilities reachable from the serve composition.
func NewServedAppointmentEventsSurface() ServedAppointmentEventsSurface {
	return ServedAppointmentEventsSurface{
		NewAttendanceLedger: appointment.NewAttendanceLedger,
		RecordAttendance:    (*appointment.AttendanceLedger).Record,
		CorrectAttendance:   (*appointment.AttendanceLedger).Correct,
		AttendanceActionFor: appointment.AttendanceActionFor,
		NewReconciler:       appointment.NewReconciler,
		ReconcileCalendar:   (*appointment.Reconciler).Reconcile,
	}
}

// AppointmentEvents returns the attendance and reconciliation capabilities
// exposed by a composed application. A nil application has no capabilities.
func (a *App) AppointmentEvents() ServedAppointmentEventsSurface {
	if a == nil {
		return ServedAppointmentEventsSurface{}
	}
	return NewServedAppointmentEventsSurface()
}
