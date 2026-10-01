package application

import "github.com/monstercameron/human-capital-management-suite/internal/domains/attendance"

// ServedAttendanceSurface exposes the attendance resolution recalculation
// contract through the application boundary. The attendance package remains
// the semantic owner; this surface only makes the capability reachable from a
// composed hcmnext process.
type ServedAttendanceSurface struct {
	RecalculateDownstream func(string, attendance.Result, attendance.Result) (attendance.Recalculation, error)
	VerifyReconciliation  func(attendance.Recalculation) error
	NewLedger             func() *attendance.RecalcLedger
}

// NewServedAttendanceSurface returns the attendance capabilities available
// through the served application boundary.
func NewServedAttendanceSurface() ServedAttendanceSurface {
	return ServedAttendanceSurface{
		RecalculateDownstream: attendance.RecalculateDownstream,
		VerifyReconciliation:  attendance.VerifyReconciliation,
		NewLedger:             attendance.NewRecalcLedger,
	}
}

// Attendance returns the attendance capabilities exposed by a composed
// application. A nil application has no served capabilities.
func (a *App) Attendance() ServedAttendanceSurface {
	if a == nil {
		return ServedAttendanceSurface{}
	}
	return NewServedAttendanceSurface()
}
