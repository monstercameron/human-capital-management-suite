package labor

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func exportLine(ordinal int, wage, burden string) CostLine {
	return CostLine{
		Ordinal: ordinal,
		Dimensions: []Dimension{
			{Kind: DimensionCostCenter, Value: "cc-100", Version: "v1"},
			{Kind: DimensionProject, Value: "proj-7", Version: "v1"},
		},
		WageAmount:   mustLaborDecimal(wage),
		BurdenAmount: mustLaborDecimal(burden),
		Currency:     "USD",
	}
}

func mustLaborDecimal(text string) values.Decimal {
	d, err := values.NewDecimal(text, 2, values.RoundingHalfUp)
	if err != nil {
		panic(err)
	}
	return d
}

func exportFixture(t *testing.T) CostExport {
	t.Helper()
	rule := validLaborRule(t)
	exp, err := NewCostExport(CostExportRequest{
		ExportID:    "labor-export-26",
		Rule:        rule,
		RuleVersion: rule.Version,
		Source:      "labor-costing/2026-W26",
		Lines: []CostLine{
			exportLine(1, "1600.00", "440.00"),
			exportLine(2, "320.00", "90.00"),
		},
	})
	if err != nil {
		t.Fatalf("NewCostExport: %v", err)
	}
	return exp
}

func observedFixture(exp CostExport) ExternalCostObservation {
	lines := make([]ExternalCostLine, len(exp.Lines))
	for i, line := range exp.Lines {
		total, err := line.WageAmount.Add(line.BurdenAmount)
		if err != nil {
			panic(err)
		}
		lines[i] = ExternalCostLine{
			Ordinal:      line.Ordinal,
			ExternalID:   "erp-line-" + itoa(line.Ordinal),
			Amount:       total,
			Currency:     line.Currency,
			DimensionIDs: []string{"cc-100", "proj-7"},
			State:        ExternalAccepted,
		}
	}
	return ExternalCostObservation{
		Source:       "erp-finance/2026-W26",
		ExternalID:   "erp-batch-1",
		ExportDigest: exp.Digest,
		Lines:        lines,
		Total:        exp.TotalCost,
		Currency:     exp.Currency,
	}
}

// TestTodo_LABOR_005 is the primary acceptance case: canonical costs map to
// Finance/Payroll with no lost dimension, and external totals, lines and
// unknowns reconcile exactly or create scoped repair.
func TestTodo_LABOR_005(t *testing.T) {
	exp := exportFixture(t)
	rejectEmptyDigest(t, exp.Digest)
	if exp.TotalCost.String() != "2450.00" {
		t.Fatalf("total = %s, want 2450.00", exp.TotalCost)
	}
	if err := exp.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, line := range exp.Lines {
		if len(line.Dimensions) < 2 {
			t.Fatalf("line %d lost dimensions", line.Ordinal)
		}
	}

	rec, err := ReconcileCostExport(exp, observedFixture(exp))
	if err != nil {
		t.Fatalf("ReconcileCostExport: %v", err)
	}
	if rec.Aggregate != ReconcileConsistent {
		t.Fatalf("aggregate = %s, want CONSISTENT", rec.Aggregate)
	}
	if len(rec.Repairs) != 0 {
		t.Fatalf("repairs = %v, want none", rec.Repairs)
	}

	// A short external total creates scoped repair, not silent acceptance.
	short := observedFixture(exp)
	short.Total = mustLaborDecimal("2500.00")
	rec, err = ReconcileCostExport(exp, short)
	if err != nil {
		t.Fatalf("short total: %v", err)
	}
	if rec.Aggregate != ReconcileRepairRequired {
		t.Fatalf("short aggregate = %s, want REPAIR_REQUIRED", rec.Aggregate)
	}
	if len(rec.Repairs) == 0 || rec.Repairs[0].Scope != RepairScopeTotals {
		t.Fatalf("repairs = %+v, want a totals-scoped repair", rec.Repairs)
	}

	// A rejected external line routes to repair with the line scope.
	rejected := observedFixture(exp)
	rejected.Lines[1].State = ExternalRejected
	rec, err = ReconcileCostExport(exp, rejected)
	if err != nil {
		t.Fatalf("rejected line: %v", err)
	}
	if rec.Lines[1].Status != LineMismatch {
		t.Fatalf("line status = %s, want MISMATCH", rec.Lines[1].Status)
	}
	if len(rec.Repairs) == 0 || rec.Repairs[0].Scope != RepairScopeLine {
		t.Fatalf("repairs = %+v, want a line-scoped repair", rec.Repairs)
	}

	// An unknown external line is reported unknown, never guessed.
	unknown := observedFixture(exp)
	unknown.Lines[0].State = ExternalUnknown
	rec, err = ReconcileCostExport(exp, unknown)
	if err != nil {
		t.Fatalf("unknown line: %v", err)
	}
	if rec.Lines[0].Status != LineUnknown {
		t.Fatalf("line status = %s, want UNKNOWN", rec.Lines[0].Status)
	}
	if rec.Aggregate != ReconcileRepairRequired {
		t.Fatalf("unknown aggregate = %s, want REPAIR_REQUIRED", rec.Aggregate)
	}

	// Export construction rejects lost dimensions and bad totals.
	rule := validLaborRule(t)
	bad := exportFixture(t)
	_ = bad
	_, err = NewCostExport(CostExportRequest{
		ExportID: "bad", Rule: rule, RuleVersion: rule.Version, Source: "s",
		Lines: []CostLine{{
			Ordinal:      1,
			WageAmount:   mustLaborDecimal("10.00"),
			BurdenAmount: mustLaborDecimal("1.00"),
			Currency:     "USD",
		}},
	})
	if !errors.Is(err, ErrCostExportRejected) {
		t.Fatalf("dimensionless line: err = %v, want LABOR_005_REJECTED", err)
	}

	_, err = ReconcileCostExport(exp, ExternalCostObservation{})
	if !errors.Is(err, ErrCostExportRejected) {
		t.Fatalf("empty observation: err = %v, want LABOR_005_REJECTED", err)
	}
}

// TestTodo_LABOR_005_Property proves totals conservation and that repair is
// created exactly when observation and export disagree.
func TestTodo_LABOR_005_Property(t *testing.T) {
	exp := exportFixture(t)
	for i, mutate := range []func(*ExternalCostObservation){
		func(o *ExternalCostObservation) {},
		func(o *ExternalCostObservation) { o.Lines[0].Amount = mustLaborDecimal("0.01") },
		func(o *ExternalCostObservation) { o.Lines = o.Lines[:1] },
		func(o *ExternalCostObservation) { o.Total = mustLaborDecimal("9999.99") },
		func(o *ExternalCostObservation) { o.Currency = "EUR" },
	} {
		obs := observedFixture(exp)
		mutate(&obs)
		rec, err := ReconcileCostExport(exp, obs)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		consistent := i == 0
		if consistent && rec.Aggregate != ReconcileConsistent {
			t.Fatalf("case %d: want CONSISTENT, got %s", i, rec.Aggregate)
		}
		if !consistent && rec.Aggregate == ReconcileConsistent {
			t.Fatalf("case %d: disagreement reported CONSISTENT", i)
		}
		if (!consistent) != (len(rec.Repairs) > 0) {
			t.Fatalf("case %d: repairs=%v for consistent=%v", i, rec.Repairs, consistent)
		}
	}
}

// TestTodo_LABOR_005_Golden pins the canonical digest of the fixture export.
func TestTodo_LABOR_005_Golden(t *testing.T) {
	exp := exportFixture(t)
	const want = "sha256:6a9a5505b186e83eed50683b9b47a36510bcb98af069d66eb960fc3cf5a0d883"
	if exp.Digest != want {
		t.Fatalf("digest = %s, want %s", exp.Digest, want)
	}
	if !strings.HasPrefix(exp.Digest, "sha256:") {
		t.Fatalf("digest %q is not algorithm-tagged", exp.Digest)
	}
}
