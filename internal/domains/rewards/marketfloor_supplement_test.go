package rewards_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
)

// TestMarketAnchorExplicitAsOfSurvivesSimulation proves an anchor that names
// its own business date keeps it: only an empty as-of defaults to the
// simulation's effective date, and the date participates in the digest.
func TestMarketAnchorExplicitAsOfSurvivesSimulation(t *testing.T) {
	ctx := context.Background()
	in := marketInput(t)
	in.Market.Anchor.AsOf = date(t, "2026-05-01")
	explicit, err := rewards.SimulateCompensation(ctx, catalog(t), in)
	if err != nil {
		t.Fatalf("SimulateCompensation: %v", err)
	}
	if explicit.Market == nil {
		t.Fatal("result carries no market section")
	}
	if got := explicit.Market.Anchor.AsOf.String(); got != "2026-05-01" {
		t.Fatalf("anchor as-of = %s, want the kept 2026-05-01", got)
	}
	if got := explicit.Market.RaiseFloor.Amount().String(); got != "95000.00" {
		t.Fatalf("raise floor = %s, want 95000.00", got)
	}
	defaulted, err := rewards.SimulateCompensation(ctx, catalog(t), marketInput(t))
	if err != nil {
		t.Fatalf("SimulateCompensation with defaulted as-of: %v", err)
	}
	if explicit.ResultDigest == defaulted.ResultDigest || explicit.InputsDigest == defaulted.InputsDigest {
		t.Fatal("an explicit as-of digests exactly like a defaulted one: the date is not covered")
	}
}

// TestTodo_HIPERF_003_MarketGolden pins the market-informed bytes of the
// canonical anchored simulation, complementing the absent-anchor golden:
// the anchor's presence is a versioned contract, not free-form metadata.
func TestTodo_HIPERF_003_MarketGolden(t *testing.T) {
	result, err := rewards.SimulateCompensation(context.Background(), catalog(t), marketInput(t))
	if err != nil {
		t.Fatalf("SimulateCompensation: %v", err)
	}
	if result.InputsDigest != hiperf003MarketInputsDigest {
		t.Fatalf("market inputs digest = %q, want the pinned %q", result.InputsDigest, hiperf003MarketInputsDigest)
	}
	if result.ResultDigest != hiperf003MarketResultDigest {
		t.Fatalf("market result digest = %q, want the pinned %q", result.ResultDigest, hiperf003MarketResultDigest)
	}
	if result.Market.Canonical() == nil {
		t.Fatal("market section has no canonical encoding")
	}
}

const (
	hiperf003MarketInputsDigest = "sha256:e7a18f639e0e8d6818e491ab97496c5349d9e0e76c225dca3bf2f945b9d78b49"
	hiperf003MarketResultDigest = "sha256:6676c40ba5bff1b9527d822ada90836c4d606b6520fd6f50d35c1ea70013a756"
)
