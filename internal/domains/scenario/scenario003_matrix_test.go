package scenario

import (
	"testing"
)

func impactRequest(t *testing.T, baseline, target string, rate *RateSource) ImpactRequest {
	t.Helper()
	return ImpactRequest{
		HeadcountBaseline: impactDecimal(t, baseline),
		HeadcountTarget:   impactDecimal(t, target),
		Rate:              rate,
		PopulationRef:     "snapshot:2026-01",
		Horizon:           scenarioHorizon(t),
		AssumptionKeys:    []string{"headcount.target", "rates.burdened"},
	}
}

// TestTodo_SCENARIO_003_Property: ranges stay ordered even for
// headcount reductions, and identical inputs replay identically.
func TestTodo_SCENARIO_003_Property(t *testing.T) {
	ranged := &RateSource{
		Ref:      "rates:burdened-2026",
		Low:      impactDecimal(t, "80000"),
		High:     impactDecimal(t, "120000"),
		HasRange: true,
	}
	growth := mustImpact(t, impactRequest(t, "10", "14", ranged))
	if !growth.CostKnown {
		t.Fatal("ranged cost is not known")
	}
	if growth.CostLow.String() != "320000" || growth.CostHigh.String() != "480000" {
		t.Fatalf("range = %v/%v", growth.CostLow, growth.CostHigh)
	}
	// A reduction prices negative but the range stays ordered.
	shrink := mustImpact(t, impactRequest(t, "14", "10", ranged))
	if shrink.HeadcountDelta.String() != "-4" {
		t.Fatalf("delta = %v", shrink.HeadcountDelta)
	}
	if shrink.CostLow.Cmp(shrink.CostHigh) > 0 {
		t.Fatalf("unordered range = %v/%v", shrink.CostLow, shrink.CostHigh)
	}
	if shrink.CostLow.String() != "-480000" || shrink.CostHigh.String() != "-320000" {
		t.Fatalf("reduction range = %v/%v", shrink.CostLow, shrink.CostHigh)
	}
	replay := mustImpact(t, impactRequest(t, "10", "14", ranged))
	if replay.CanonicalDigest != growth.CanonicalDigest {
		t.Fatal("identical impact inputs replayed to different digests")
	}
	// Malformed rate sources never price: exact and range together.
	both := *ranged
	both.HasExact = true
	both.Exact = impactDecimal(t, "100000")
	if _, err := CalculateImpact(impactRequest(t, "10", "14", &both)); err == nil {
		t.Fatal("dual exact/range rate source priced")
	}
	// Malformed requests never price: no population, no assumptions.
	bare := impactRequest(t, "10", "14", ranged)
	bare.PopulationRef = ""
	if _, err := CalculateImpact(bare); err == nil {
		t.Fatal("population-less impact priced")
	}
	bare = impactRequest(t, "10", "14", ranged)
	bare.AssumptionKeys = nil
	if _, err := CalculateImpact(bare); err == nil {
		t.Fatal("assumption-less impact priced")
	}
}
