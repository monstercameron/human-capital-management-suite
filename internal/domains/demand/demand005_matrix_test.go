package demand

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func demandQuantity(t *testing.T, amount string) values.Quantity {
	t.Helper()
	quantity, err := values.NewQuantity(amount, "FTE", 0, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return quantity
}

// TestTodo_DEMAND_005_Property: directions, magnitudes and every
// mismatch edge resolve deterministically.
func TestTodo_DEMAND_005_Property(t *testing.T) {
	signal := validSignal(t)
	cases := []struct {
		name      string
		actual    string
		direction ForecastDirection
		delta     string
	}{
		{"over", "3", ForecastOver, "1 FTE"},
		{"under", "1", ForecastUnder, "1 FTE"},
		{"exact", "2", ForecastExact, "0 FTE"},
	}
	for _, tc := range cases {
		actual := validActual(t, true)
		actual.Quantity = demandQuantity(t, tc.actual)
		reconciliation, err := ReconcileForecast(signal, actual)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if reconciliation.Direction != tc.direction || reconciliation.Delta.String() != tc.delta {
			t.Fatalf("%s = %v %v", tc.name, reconciliation.Direction, reconciliation.Delta)
		}
		again, err := ReconcileForecast(signal, actual)
		if err != nil {
			t.Fatal(err)
		}
		if again.CanonicalDigest != reconciliation.CanonicalDigest {
			t.Fatalf("%s replayed to a different digest", tc.name)
		}
	}
	mismatches := map[string]func(*DemandActual){
		"scope":  func(a *DemandActual) { a.Location = "loc-bos" },
		"target": func(a *DemandActual) { a.Skill = "surgery" },
		"unit": func(a *DemandActual) {
			head, err := values.NewQuantity("2", "HEAD", 0, values.RoundingHalfEven)
			if err != nil {
				t.Fatal(err)
			}
			a.Quantity = head
			a.Unit = "HEAD"
		},
		"interval": func(a *DemandActual) {
			start, err := values.NewLocalDate(2026, 5, 1)
			if err != nil {
				t.Fatal(err)
			}
			end, err := values.NewLocalDate(2026, 5, 2)
			if err != nil {
				t.Fatal(err)
			}
			period, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "us-federal", Version: "2026"})
			if err != nil {
				t.Fatal(err)
			}
			a.Period = period
		},
	}
	for name, mutate := range mismatches {
		actual := validActual(t, true)
		mutate(&actual)
		if _, err := ReconcileForecast(signal, actual); err == nil {
			t.Fatalf("%s mismatch closed", name)
		}
	}
}
