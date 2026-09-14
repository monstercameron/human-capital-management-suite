package demand

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func validActual(t *testing.T, complete bool) DemandActual {
	t.Helper()
	signal := validSignal(t)
	return DemandActual{
		ActualRef: "actual-1",
		Location:  signal.Location,
		Skill:     signal.Skill,
		Period:    signal.Work,
		Quantity:  signal.Quantity,
		Unit:      signal.Unit,
		Source:    "timekeeping",
		SourceRef: "timekeeping:2026-04",
		Complete:  complete,
	}
}

// TestTodo_DEMAND_005: forecast reconciles against complete actuals
// for the exact interval — stale or partial actuals never close.
func TestTodo_DEMAND_005(t *testing.T) {
	signal := validSignal(t)
	actual := validActual(t, true)
	under, err := values.NewQuantity("1", "FTE", 0, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	actual.Quantity = under
	reconciliation, err := ReconcileForecast(signal, actual)
	if err != nil {
		t.Fatal(err)
	}
	if reconciliation.Delta.String() != "1 FTE" || reconciliation.Direction != ForecastUnder {
		t.Fatalf("reconciliation=%+v", reconciliation)
	}
	if reconciliation.Interval == "" || reconciliation.ModelVersion != signal.Version ||
		reconciliation.ForecastSource == "" || reconciliation.ActualSource == "" ||
		reconciliation.CanonicalDigest == "" {
		t.Fatalf("reconciliation is not fully bound: %+v", reconciliation)
	}
	// Seeded defect: partial actuals must refuse to close the
	// comparison instead of reconciling against incomplete truth.
	if _, err := ReconcileForecast(signal, validActual(t, false)); err == nil {
		t.Fatal("partial actuals closed the comparison")
	}
	if err := reconciliation.Validate(); err != nil {
		t.Fatal(err)
	}
}
