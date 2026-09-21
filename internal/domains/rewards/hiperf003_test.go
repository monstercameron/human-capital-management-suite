package rewards_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
)

// marketInput anchors the canonical simulation to a market position: the
// anchor's 25th percentile sits above the OPS-HRBP3 band floor (92000.00
// USD), so the raise floor must be the market leg, not the band floor. The
// anchor carries no as-of date, proving the simulation defaults it to the
// effective date ("today").
func marketInput(t *testing.T) rewards.SimulateCompensationInput {
	t.Helper()
	in := baseInput(t)
	in.Market = &rewards.MarketAnchorInput{
		Anchor: rewards.MarketAnchor{
			P25: money(t, "95000.00", "USD"),
			P50: money(t, "102000.00", "USD"),
			P75: money(t, "110000.00", "USD"),
		},
		Currency:      "USD",
		SourceVersion: "rewards.market.stub/2026.1",
	}
	return in
}

// TestTodo_HIPERF_003 proves the market-informed raise floor: with an
// anchor present the floor is the max of the band floor and the market
// anchor, the anchor is recorded as evidence, and the as-of date defaults
// to the simulation's effective date.
func TestTodo_HIPERF_003(t *testing.T) {
	ctx := context.Background()

	result, err := rewards.SimulateCompensation(ctx, catalog(t), marketInput(t))
	if err != nil {
		t.Fatalf("SimulateCompensation with market anchor: %v", err)
	}
	if result.Market == nil {
		t.Fatal("result carries no market section; the anchor was dropped")
	}
	if got := result.Market.RaiseFloor.Amount().String(); got != "95000.00" {
		t.Fatalf("raise floor = %s, want 95000.00 (max of band floor 92000.00 and market p25)", got)
	}
	if !result.Market.BandFloorKnown {
		t.Fatal("market section does not say the band floor participated")
	}
	if got := result.Market.BandFloor.Amount().String(); got != "92000.00" {
		t.Fatalf("band floor = %s, want the OPS-HRBP3 minimum 92000.00", got)
	}
	if result.Market.SourceVersion != "rewards.market.stub/2026.1" {
		t.Fatalf("market source version = %q, want the stub version", result.Market.SourceVersion)
	}
	if result.Market.Anchor.AsOf != date(t, "2026-06-01") {
		t.Fatalf("market as-of = %s, want the effective date default", result.Market.Anchor.AsOf)
	}
	if result.Market.Anchor.P50.Amount().String() != "102000.00" {
		t.Fatalf("recorded anchor p50 = %s, want the queried anchor", result.Market.Anchor.P50.Amount())
	}
	foundControl := false
	for _, c := range result.Receipt.Controls {
		if c.Name == "market_rate_source" && c.Version == "rewards.market.stub/2026.1" {
			foundControl = true
		}
	}
	if !foundControl {
		t.Fatalf("receipt controls = %+v, want the market_rate_source control citing the anchor", result.Receipt.Controls)
	}
	foundAssumption := false
	for _, a := range result.Assumptions {
		if a.Key == "market.raise_floor" && strings.Contains(a.Value, "95000.00") {
			foundAssumption = true
		}
	}
	if !foundAssumption {
		t.Fatalf("assumptions = %+v, want the market.raise_floor evidence entry", result.Assumptions)
	}
}

// TestTodo_HIPERF_003_BandFloorWins proves the max runs both ways: an
// anchor below the band floor leaves the band floor as the raise floor.
func TestTodo_HIPERF_003_BandFloorWins(t *testing.T) {
	in := marketInput(t)
	in.Market.Anchor = rewards.MarketAnchor{
		P25:  money(t, "88000.00", "USD"),
		P50:  money(t, "94000.00", "USD"),
		P75:  money(t, "101000.00", "USD"),
		AsOf: date(t, "2026-06-01"),
	}
	result, err := rewards.SimulateCompensation(context.Background(), catalog(t), in)
	if err != nil {
		t.Fatalf("SimulateCompensation with below-band anchor: %v", err)
	}
	if result.Market == nil {
		t.Fatal("result carries no market section")
	}
	if got := result.Market.RaiseFloor.Amount().String(); got != "92000.00" {
		t.Fatalf("raise floor = %s, want the band floor 92000.00 over market p25 88000.00", got)
	}
}

// TestTodo_HIPERF_003_NoBand proves a market anchor without a band still
// floors the raise: with no band floor to compare, the floor is the anchor.
func TestTodo_HIPERF_003_NoBand(t *testing.T) {
	in := marketInput(t)
	in.Band = nil
	result, err := rewards.SimulateCompensation(context.Background(), nil, in)
	if err != nil {
		t.Fatalf("SimulateCompensation with market and no band: %v", err)
	}
	if result.Market == nil {
		t.Fatal("result carries no market section")
	}
	if got := result.Market.RaiseFloor.Amount().String(); got != "95000.00" {
		t.Fatalf("raise floor = %s, want the anchor p25 95000.00 with no band", got)
	}
	if result.Market.BandFloorKnown {
		t.Fatal("market section claims a band floor participated with no band requested")
	}
}

// TestTodo_HIPERF_003_Refusals proves a malformed anchor is refused, never
// silently unfloored: currency mismatch, missing source version and an
// unordered anchor all fail the simulation.
func TestTodo_HIPERF_003_Refusals(t *testing.T) {
	ctx := context.Background()
	in := marketInput(t)
	in.Market.Currency = "EUR"
	if _, err := rewards.SimulateCompensation(ctx, catalog(t), in); !errors.Is(err, rewards.ErrCurrencyMismatch) {
		t.Fatalf("currency-mismatched anchor error = %v, want ErrCurrencyMismatch", err)
	}
	in = marketInput(t)
	in.Market.SourceVersion = ""
	if _, err := rewards.SimulateCompensation(ctx, catalog(t), in); !errors.Is(err, rewards.ErrSimulationInputInvalid) {
		t.Fatalf("unpinned anchor error = %v, want ErrSimulationInputInvalid", err)
	}
	in = marketInput(t)
	in.Market.Anchor = rewards.MarketAnchor{
		P25:  money(t, "110000.00", "USD"),
		P50:  money(t, "102000.00", "USD"),
		P75:  money(t, "95000.00", "USD"),
		AsOf: date(t, "2026-06-01"),
	}
	if _, err := rewards.SimulateCompensation(ctx, catalog(t), in); !errors.Is(err, rewards.ErrMarketAnchorInvalid) {
		t.Fatalf("unordered anchor error = %v, want ErrMarketAnchorInvalid", err)
	}
}

// TestTodo_HIPERF_003_Golden proves an absent anchor leaves every existing
// golden byte-identical: the canonical simulation digests exactly as it did
// before the market field existed, and the result carries no market section.
func TestTodo_HIPERF_003_Golden(t *testing.T) {
	in := baseInput(t)
	if in.Market != nil {
		t.Fatal("canonical input carries a market anchor; the absent-anchor golden proves nothing")
	}
	result, err := rewards.SimulateCompensation(context.Background(), catalog(t), in)
	if err != nil {
		t.Fatalf("SimulateCompensation: %v", err)
	}
	const wantInputs = "sha256:47b8a78fc154209ac54e90c2c5002dc1a44ad667ea8b27bef6e5caf28d4f6704"
	const wantResult = "sha256:9638a81fda9e859cda344d279ca29efcd5b4b0b646f3b55db017d19d2728b233"
	if result.InputsDigest != wantInputs {
		t.Fatalf("inputs digest = %q, want the pre-market %q", result.InputsDigest, wantInputs)
	}
	if result.ResultDigest != wantResult {
		t.Fatalf("result digest = %q, want the pre-market %q", result.ResultDigest, wantResult)
	}
	if result.Market != nil {
		t.Fatalf("absent-anchor result carries a market section: %+v", result.Market)
	}
	for _, c := range result.Receipt.Controls {
		if c.Name == "market_rate_source" {
			t.Fatalf("absent-anchor receipt cites a market source: %+v", result.Receipt.Controls)
		}
	}
}
