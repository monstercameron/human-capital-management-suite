package garnishment

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var garn007Now = time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)

func garn007Cell(period, amount string, unknown bool) PeriodComparison {
	dec := values.MustDecimal(amount, 2, values.RoundingHalfUp)
	return PeriodComparison{
		WorkerRef: "worker-1", OrderRef: "order:creditor-1", Period: period,
		BalanceWithheld: dec, PayrollDeducted: dec, Remitted: dec,
		RecipientObserved: dec, RecipientUnknown: unknown,
	}
}

// TestTodo_GARN_007 is the PRIMARY contract: order balance, payroll
// deductions, remittance and recipient observation are compared per
// worker/order/period, and discrepancies create a protected repair case.
func TestTodo_GARN_007(t *testing.T) {
	got, err := ReconcileWithholding("acme", []PeriodComparison{garn007Cell("2026-03", "386.75", false)}, garn007Now)
	if err != nil {
		t.Fatalf("ReconcileWithholding: %v", err)
	}
	if len(got.Cells) != 1 || got.Cells[0].Cell != CellMatch || got.Repair != nil {
		t.Fatalf("agreeing totals must MATCH with no repair: %+v", got)
	}
	if got.Digest == "" {
		t.Fatalf("result must seal a digest")
	}

	t.Run("remittance shortfall opens a repair", func(t *testing.T) {
		cell := garn007Cell("2026-03", "386.75", false)
		cell.Remitted = values.MustDecimal("300.00", 2, values.RoundingHalfUp)
		cell.RecipientObserved = values.MustDecimal("300.00", 2, values.RoundingHalfUp)
		got, err := ReconcileWithholding("acme", []PeriodComparison{cell}, garn007Now)
		if err != nil {
			t.Fatal(err)
		}
		if got.Cells[0].Cell != CellShort || got.Repair == nil {
			t.Fatalf("shortfall must be SHORT with repair: %+v", got)
		}
		if got.Cells[0].Delta.String() != "86.75" {
			t.Fatalf("delta must be 86.75, got %s", got.Cells[0].Delta.String())
		}
		if len(got.Repair.Cells) != 1 || got.Repair.Scope != "acme" {
			t.Fatalf("repair must scope the open cell: %+v", got.Repair)
		}
	})

	t.Run("unknown recipient stays unknown, never matched", func(t *testing.T) {
		got, err := ReconcileWithholding("acme", []PeriodComparison{garn007Cell("2026-03", "386.75", true)}, garn007Now)
		if err != nil {
			t.Fatal(err)
		}
		if got.Cells[0].Cell != CellUnknown || got.Repair == nil {
			t.Fatalf("unknown recipient must be UNKNOWN with repair: %+v", got)
		}
	})

	t.Run("duplicate cells are refused", func(t *testing.T) {
		_, err := ReconcileWithholding("acme", []PeriodComparison{
			garn007Cell("2026-03", "386.75", false),
			garn007Cell("2026-03", "386.75", false),
		}, garn007Now)
		if !errors.Is(err, ErrGarnReconRejected) {
			t.Fatalf("duplicate cells must be GARN_007_REJECTED, got %v", err)
		}
	})
}

func TestTodo_GARN_007_Property(t *testing.T) {
	cells := []PeriodComparison{garn007Cell("2026-02", "100.00", false), garn007Cell("2026-03", "386.75", false)}
	a, err := ReconcileWithholding("acme", cells, garn007Now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ReconcileWithholding("acme", []PeriodComparison{cells[1], cells[0]}, garn007Now)
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest {
		t.Fatalf("cell order must not change the digest")
	}
	// Every open cell appears in the repair, every match stays out.
	mixed := []PeriodComparison{garn007Cell("2026-02", "100.00", false)}
	short := garn007Cell("2026-03", "386.75", false)
	short.PayrollDeducted = values.MustDecimal("300.00", 2, values.RoundingHalfUp)
	mixed = append(mixed, short)
	got, err := ReconcileWithholding("acme", mixed, garn007Now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Repair == nil || len(got.Repair.Cells) != 1 || got.Repair.Cells[0].Period != "2026-03" {
		t.Fatalf("repair must hold exactly the open cell: %+v", got.Repair)
	}
}

func TestTodo_GARN_007_Race(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := ReconcileWithholding("acme", []PeriodComparison{garn007Cell("2026-03", "386.75", false)}, garn007Now)
			if err != nil {
				t.Error(err)
				return
			}
			if got.Cells[0].Cell != CellMatch {
				t.Errorf("concurrent reconcile diverged: %+v", got)
			}
		}()
	}
	wg.Wait()
}
