package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionhiperf"
)

// stubRatings is a CalibratedRatingLookup over a fixed subject table.
type stubRatings struct {
	ratings map[string]performance.FinalCalibratedRating
	err     error
}

func (s stubRatings) LookupCalibratedRating(_ context.Context, _ values.TenantId, subject string) (performance.FinalCalibratedRating, error) {
	if s.err != nil {
		return performance.FinalCalibratedRating{}, s.err
	}
	rating, ok := s.ratings[subject]
	if !ok {
		return performance.FinalCalibratedRating{}, ErrNoCalibratedRating
	}
	return rating, nil
}

// marketInputFor carries a fetched anchor into the variant simulation.
func marketInputFor(eval rewards.MarketRateEvaluation) *rewards.MarketAnchorInput {
	return &rewards.MarketAnchorInput{
		Anchor: eval.Anchor, Currency: eval.Query.Currency, SourceVersion: eval.SourceVersion,
	}
}

// TestTodo_HIPERF_005 is the PRIMARY test for the owned half of the variant
// binding: the step services fetch the market anchor through the stub
// source, simulate with it so the anchor lands in the raise evidence, fail
// the fetch closed with no source, and pin the variant digest only for a
// top-band subject under the execute plan.
func TestTodo_HIPERF_005(t *testing.T) {
	ctx := context.Background()
	h := newStepHarness(t, nil)
	h.services.SetMarketRateSource(fixtures.NewMemoryMarketRateCatalog())

	eval, fetchAnswer, err := h.services.FetchMarketRate(ctx, h.call)
	if err != nil {
		t.Fatalf("FetchMarketRate: %v", err)
	}
	if got := eval.Anchor.P25.Amount().String(); got != "104000.00" {
		t.Fatalf("fetched p25 = %s, want the OPS-HRBP3 stub leg 104000.00", got)
	}
	if fetchAnswer.Digest == "" || fetchAnswer.Digest != eval.ResultDigest {
		t.Fatalf("fetch answer digest = %q, want the market evaluation %q", fetchAnswer.Digest, eval.ResultDigest)
	}

	result, simAnswer, err := h.services.SimulateCompensationWithMarket(ctx, h.call, marketInputFor(eval))
	if err != nil {
		t.Fatalf("SimulateCompensationWithMarket: %v", err)
	}
	if result.Market == nil {
		t.Fatal("market simulation carries no market section: the anchor was dropped")
	}
	if got := result.Market.RaiseFloor.Amount().String(); got != "104000.00" {
		t.Fatalf("raise floor = %s, want max(92000.00 band, 104000.00 market)", got)
	}
	if simAnswer.Digest == "" || simAnswer.Digest != result.ResultDigest {
		t.Fatalf("simulate answer digest = %q, want the result %q", simAnswer.Digest, result.ResultDigest)
	}
	if len(simAnswer.EvidenceIDs) == 0 {
		t.Fatal("market simulation recorded no gateway evidence")
	}
	plain, _, err := h.services.SimulateCompensationWithMarket(ctx, h.call, nil)
	if err != nil {
		t.Fatalf("anchor-less variant simulation: %v", err)
	}
	if plain.Market != nil || plain.ResultDigest == result.ResultDigest {
		t.Fatal("an anchor-less variant simulation is not the execute behavior")
	}

	unbound := newStepHarness(t, nil)
	if _, _, err := unbound.services.FetchMarketRate(ctx, unbound.call); err == nil {
		t.Fatal("fetch with no market source must fail closed, never fabricate an anchor")
	}

	top := hiperfCalibratedRating(t, "5", "5")
	worker, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("worker ref: %v", err)
	}
	rated := func(plan string, lookup CalibratedRatingLookup) *IntentService {
		return &IntentService{
			executionPlan: plan, performanceRatings: lookup,
			highPerformerVariantDigest: "sha256:variant",
		}
	}
	svc := rated(PromotionPlanExecute, stubRatings{ratings: map[string]performance.FinalCalibratedRating{worker.Id: top}})
	if pin := svc.highPerformerPin(ctx, fixtures.Tenant, worker.Id); pin != "sha256:variant" {
		t.Fatalf("top-band pin = %q, want the variant digest", pin)
	}
	mid := hiperfCalibratedRating(t, "3", "3")
	svc = rated(PromotionPlanExecute, stubRatings{ratings: map[string]performance.FinalCalibratedRating{worker.Id: mid}})
	if pin := svc.highPerformerPin(ctx, fixtures.Tenant, worker.Id); pin != "" {
		t.Fatalf("mid-rated pin = %q, want no pin", pin)
	}
	svc = rated(PromotionPlanExecute, stubRatings{})
	if pin := svc.highPerformerPin(ctx, fixtures.Tenant, worker.Id); pin != "" {
		t.Fatalf("unrated pin = %q, want no pin", pin)
	}
	svc = rated(PromotionPlanPrototype, stubRatings{ratings: map[string]performance.FinalCalibratedRating{worker.Id: top}})
	if pin := svc.highPerformerPin(ctx, fixtures.Tenant, worker.Id); pin != "" {
		t.Fatalf("prototype pin = %q: prototype mode never routes to the variant", pin)
	}
	svc = rated(PromotionPlanExecute, stubRatings{err: errors.New("ratings unavailable")})
	if pin := svc.highPerformerPin(ctx, fixtures.Tenant, worker.Id); pin != "" {
		t.Fatalf("fault pin = %q: a ratings outage resolves the execute plan", pin)
	}
	if pin := (*IntentService)(nil).highPerformerPin(ctx, fixtures.Tenant, worker.Id); pin != "" {
		t.Fatalf("nil service pin = %q, want no pin", pin)
	}

	subjects := []intent.SubjectReference{
		{Kind: "POSITION", SubjectID: "position-1", AuthorityDomain: "POSITION"},
		{Kind: "EMPLOYMENT", SubjectID: worker.Id, AuthorityDomain: "PEOPLE"},
	}
	if got := employmentSubject(subjects); got != worker.Id {
		t.Fatalf("employment subject = %q, want the PEOPLE reference %q", got, worker.Id)
	}
	if got := employmentSubject([]intent.SubjectReference{{Kind: "POSITION", SubjectID: "position-1", AuthorityDomain: "POSITION"}}); got != "" {
		t.Fatalf("employment subject = %q, want empty with no worker", got)
	}
}

// TestTodo_HIPERF_005_Integration runs a rated worker from propose through
// the market-rate node with the anchor visible in the raise evidence: the
// harness proposes, a real calibrated top-band rating resolves the real
// variant digest, the stub fetch answers the target scope, the market
// simulation floors the raise on the anchor, and the threshold inputs still
// resolve over that raise evidence.
func TestTodo_HIPERF_005_Integration(t *testing.T) {
	ctx := context.Background()
	h := newStepHarness(t, nil)
	h.services.SetMarketRateSource(fixtures.NewMemoryMarketRateCatalog())

	top := hiperfCalibratedRating(t, "5", "5")
	executePlan, err := promotionexec.Compile()
	if err != nil {
		t.Fatalf("promotionexec.Compile: %v", err)
	}
	variantPlan, err := promotionhiperf.Compile()
	if err != nil {
		t.Fatalf("promotionhiperf.Compile: %v", err)
	}
	if got := ResolvePromotionPlanDigest(PromotionPlanExecute, &top, executePlan.Digest(), variantPlan.Digest()); got != variantPlan.Digest() {
		t.Fatalf("rated worker resolves %q, want the variant %q", got, variantPlan.Digest())
	}

	eval, _, err := h.services.FetchMarketRate(ctx, h.call)
	if err != nil {
		t.Fatalf("FetchMarketRate: %v", err)
	}
	result, simAnswer, err := h.services.SimulateCompensationWithMarket(ctx, h.call, marketInputFor(eval))
	if err != nil {
		t.Fatalf("SimulateCompensationWithMarket: %v", err)
	}
	if result.Market == nil {
		t.Fatal("raise evidence carries no market section")
	}
	if got := result.Market.Anchor.P50.Amount().String(); got != "112000.00" {
		t.Fatalf("raise evidence anchor p50 = %s, want the stub 112000.00", got)
	}
	if got := result.Market.RaiseFloor.Amount().String(); got != "104000.00" {
		t.Fatalf("raise evidence floor = %s, want 104000.00", got)
	}
	foundFloor, foundControl := false, false
	for _, a := range result.Assumptions {
		if a.Key == "market.raise_floor" && strings.Contains(a.Value, "104000.00") {
			foundFloor = true
		}
	}
	for _, c := range result.Receipt.Controls {
		if c.Name == "market_rate_source" && c.Version == eval.SourceVersion {
			foundControl = true
		}
	}
	if !foundFloor || !foundControl {
		t.Fatalf("raise evidence assumptions/controls hide the anchor: %+v / %+v", result.Assumptions, result.Receipt.Controls)
	}
	inputs, thresholdAnswer, err := h.services.ThresholdInputs(ctx, h.call)
	if err != nil {
		t.Fatalf("ThresholdInputs over the market raise: %v", err)
	}
	if thresholdAnswer.Digest == "" || !inputs.BandPosition.Valid() {
		t.Fatalf("threshold inputs = %+v, %+v; want a digest and a known band", inputs, thresholdAnswer)
	}
	if simAnswer.Digest != result.ResultDigest {
		t.Fatalf("simulate answer digest = %q, want the raise evidence %q", simAnswer.Digest, result.ResultDigest)
	}
}
