package scenario

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func impactDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	return values.MustDecimal(text, 0, values.RoundingHalfEven)
}

func mustImpact(t *testing.T, request ImpactRequest) WorkforceImpact {
	t.Helper()
	impact, err := CalculateImpact(request)
	if err != nil {
		t.Fatal(err)
	}
	return impact
}

// TestTodo_SCENARIO_003: workforce, headcount and cost impact binds
// its formula, rates, population, horizon and assumptions — and a
// missing rate source yields an explicit unknown, never a fabricated
// cost.
func TestTodo_SCENARIO_003(t *testing.T) {
	exact := impactDecimal(t, "100000")
	impact := mustImpact(t, ImpactRequest{
		HeadcountBaseline: impactDecimal(t, "10"),
		HeadcountTarget:   impactDecimal(t, "14"),
		Rate:              &RateSource{Ref: "rates:burdened-2026", Exact: exact, HasExact: true},
		PopulationRef:     "snapshot:2026-01",
		Horizon:           scenarioHorizon(t),
		AssumptionKeys:    []string{"headcount.target"},
	})
	if impact.HeadcountDelta.String() != "4" {
		t.Fatalf("headcount delta = %v", impact.HeadcountDelta)
	}
	if !impact.CostKnown || impact.CostLow.String() != "400000" || impact.CostHigh.String() != "400000" {
		t.Fatalf("exact cost = %v/%v known=%v", impact.CostLow, impact.CostHigh, impact.CostKnown)
	}
	if impact.Formula == "" || impact.RateRef != "rates:burdened-2026" ||
		impact.PopulationRef != "snapshot:2026-01" || impact.Horizon == "" ||
		len(impact.AssumptionKeys) != 1 || impact.CanonicalDigest == "" {
		t.Fatalf("impact is not fully bound: %+v", impact)
	}
	// Seeded defect: a missing rate source must produce an explicit
	// unknown rather than a fabricated zero.
	unknown := mustImpact(t, ImpactRequest{
		HeadcountBaseline: impactDecimal(t, "10"),
		HeadcountTarget:   impactDecimal(t, "14"),
		PopulationRef:     "snapshot:2026-01",
		Horizon:           scenarioHorizon(t),
		AssumptionKeys:    []string{"headcount.target"},
	})
	if unknown.CostKnown {
		t.Fatal("missing rate source produced a fabricated cost")
	}
	if unknown.CostUnknownReason == "" {
		t.Fatal("unknown cost carries no reason")
	}
	if err := impact.Validate(); err != nil {
		t.Fatal(err)
	}
}
