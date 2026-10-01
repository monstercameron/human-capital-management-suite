package application

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/appointment"
)

// TestTodo_APPT_005_Served proves the attendance contract is reachable from
// the application boundary used by shipped commands.
func TestTodo_APPT_005_Served(t *testing.T) {
	surface := NewServedAppointmentEventsSurface()
	if surface.NewAttendanceLedger == nil || surface.RecordAttendance == nil || surface.CorrectAttendance == nil || surface.AttendanceActionFor == nil {
		t.Fatal("served appointment attendance surface is incomplete")
	}

	ledger := surface.NewAttendanceLedger(nil)
	_, err := surface.RecordAttendance(ledger, "unknown", appointment.AttendanceAttended, "event-1", time.Date(2026, 9, 29, 13, 0, 0, 0, time.UTC))
	if !errors.Is(err, appointment.ErrAttendanceRejected) {
		t.Fatalf("served attendance error = %v, want ErrAttendanceRejected", err)
	}
	var rejection *appointment.Rejection
	if !errors.As(err, &rejection) || rejection.Code != "APPT_005_REJECTED" {
		t.Fatalf("served attendance rejection = %v, want APPT_005_REJECTED", err)
	}
	if got := surface.AttendanceActionFor("", appointment.CancellationRules{NoShow: appointment.NoShowFollowUp}); got != appointment.AttendanceActionNone {
		t.Fatalf("served unknown attendance action = %q, want NONE", got)
	}
}

// TestTodo_APPT_006_Served proves the external-calendar reconciliation
// contract is reachable from the application boundary used by shipped
// commands and remains fail-closed for an unbound expectation.
func TestTodo_APPT_006_Served(t *testing.T) {
	surface := NewServedAppointmentEventsSurface()
	if surface.NewReconciler == nil || surface.ReconcileCalendar == nil {
		t.Fatal("served appointment reconciliation surface is incomplete")
	}

	reconciler := surface.NewReconciler()
	_, err := surface.ReconcileCalendar(reconciler, appointment.ExpectedAppointment{}, nil, appointment.ProviderObservation{}, time.Date(2026, 9, 29, 13, 0, 0, 0, time.UTC))
	if !errors.Is(err, appointment.ErrReconcileRejected) {
		t.Fatalf("served reconciliation error = %v, want ErrReconcileRejected", err)
	}
	var rejection *appointment.Rejection
	if !errors.As(err, &rejection) || rejection.Code != "APPT_006_REJECTED" || rejection.Field != "reservation" {
		t.Fatalf("served reconciliation rejection = %v, want APPT_006_REJECTED reservation", err)
	}
	if repairs := reconciler.Repairs(); len(repairs) != 0 {
		t.Fatalf("served reconciliation repairs = %d, want 0 after rejection", len(repairs))
	}
}

func TestTodo_APPT_005_006_Served_NilApp(t *testing.T) {
	var app *App
	if got := app.AppointmentEvents(); got.NewAttendanceLedger != nil || got.NewReconciler != nil {
		t.Fatal("nil application exposed appointment event capabilities")
	}
}
