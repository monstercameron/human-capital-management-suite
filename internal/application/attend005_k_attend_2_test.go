package application

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/attendance"
)

// TestTodo_ATTEND_005_Served proves the ATTEND-005 domain contract is
// reachable from the application boundary used by shipped commands.
func TestTodo_ATTEND_005_Served(t *testing.T) {
	surface := NewServedAttendanceSurface()
	if surface.RecalculateDownstream == nil || surface.VerifyReconciliation == nil || surface.NewLedger == nil {
		t.Fatal("served attendance surface is incomplete")
	}
	if _, err := surface.RecalculateDownstream("", attendance.Result{}, attendance.Result{}); !errors.Is(err, attendance.ErrRecalculationRejected) {
		t.Fatalf("served recalculation error=%v, want ATTEND_005_REJECTED", err)
	}
	if err := surface.VerifyReconciliation(attendance.Recalculation{}); !errors.Is(err, attendance.ErrRecalculationRejected) {
		t.Fatalf("served reconciliation error=%v, want ATTEND_005_REJECTED", err)
	}
	ledger := surface.NewLedger()
	if ledger == nil {
		t.Fatal("served attendance surface returned a nil ledger")
	}
}
