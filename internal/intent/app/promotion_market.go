package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// ErrNoCalibratedRating is the miss a [CalibratedRatingLookup] reports for
// a subject with no finalized calibration. It is a typed miss, not a zero
// rating: an unrated subject resolves the execute plan, never the variant.
var ErrNoCalibratedRating = errors.New("app: subject has no calibrated rating")

// CalibratedRatingLookup resolves one subject's calibrated performance
// rating for high-performer routing (HIPERF-005). Production binds a reader
// over the performance domain's durable calibrations; a miss or a fault
// resolves the execute plan, never the variant.
type CalibratedRatingLookup interface {
	LookupCalibratedRating(ctx context.Context, tenant values.TenantId, subjectRef string) (performance.FinalCalibratedRating, error)
}

// SetMarketRateSource binds the market-rate source the variant's
// fetch_market_rate node reads through [PromotionStepServices.FetchMarketRate].
// Nil leaves the node failing closed: no anchor is ever fabricated.
func (p *PromotionStepServices) SetMarketRateSource(source rewards.MarketRateSource) {
	p.marketRates = source
}

// simulateWithMarket runs simulate_compensation with an optional market
// anchor. A nil anchor is the execute plan's behavior exactly; the variant
// passes the fetch's anchor so the raise floor is market-informed.
func (p *PromotionStepServices) simulateWithMarket(ctx context.Context, s *promotionSession, call PromotionStepCall, market *rewards.MarketAnchorInput) (rewards.SimulateCompensationResult, error) {
	req := s.call.Promotion
	got, err := p.bound(ctx, rewards.SimulateCompensationIntentType, s.principal, s.purpose, rewards.SimulateCompensationInput{
		Tenant: req.Tenant, Subject: req.Subject, Current: req.Current, Proposed: req.Proposed,
		Annualization: req.Annualization, EffectiveDate: req.EffectiveDate, Market: market,
	}, call, &s.answer)
	if err != nil {
		return rewards.SimulateCompensationResult{}, err
	}
	result, ok := got.(rewards.SimulateCompensationResult)
	if !ok {
		return rewards.SimulateCompensationResult{}, fmt.Errorf("app: simulate_compensation returned %T", got)
	}
	return result, nil
}

// SimulateCompensationWithMarket invokes simulate_compensation with the
// variant fetch's market anchor, so the raise floor is the max of the band
// floor and the anchor and the anchor is recorded as evidence. It is the
// served binding of the variant's simulate_compensation node; the execute
// plan keeps calling [PromotionStepServices.SimulateCompensation].
func (p *PromotionStepServices) SimulateCompensationWithMarket(ctx context.Context, call PromotionStepCall, market *rewards.MarketAnchorInput) (ret0 rewards.SimulateCompensationResult, ret1 PromotionStepAnswer, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_services.simulate_compensation_market", call.NodeID, call.IntentID)
	defer func() { observe.DoneWith(obsOp, retErr, ret1) }()
	s, err := p.open(ctx, call, rewards.SimulateCompensationIntentType)
	if err != nil {
		return rewards.SimulateCompensationResult{}, PromotionStepAnswer{}, err
	}
	result, err := p.simulateWithMarket(ctx, s, call, market)
	if err != nil {
		return rewards.SimulateCompensationResult{}, s.answer, err
	}
	s.answer.Digest = result.ResultDigest
	return result, s.answer, nil
}

// FetchMarketRate resolves the market anchor for the promotion's target
// band scope through the bound market-rate source. It is the served binding
// of the variant's fetch_market_rate node: the query carries the band's own
// job, grade, zone and currency with the as-of defaulted to today, and the
// answer digest is the anchor the variant simulation floors against.
func (p *PromotionStepServices) FetchMarketRate(ctx context.Context, call PromotionStepCall) (ret0 rewards.MarketRateEvaluation, ret1 PromotionStepAnswer, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_services.fetch_market_rate", call.NodeID, call.IntentID)
	defer func() { observe.DoneWith(obsOp, retErr, ret1) }()
	s, err := p.open(ctx, call, rewards.MarketRateIntentType)
	if err != nil {
		return rewards.MarketRateEvaluation{}, PromotionStepAnswer{}, err
	}
	source := p.marketRates
	if source == nil {
		return rewards.MarketRateEvaluation{}, s.answer, fmt.Errorf("app: fetch_market_rate: no market rate source is bound")
	}
	preflight, err := p.preflight(ctx, s, call)
	if err != nil {
		return rewards.MarketRateEvaluation{}, s.answer, err
	}
	if preflight.Band.State != rewards.BandResultEvaluated {
		return rewards.MarketRateEvaluation{}, s.answer, fmt.Errorf("app: fetch_market_rate: the promotion names no evaluable target band: %s", preflight.Band.Reason)
	}
	query := preflight.Band.Evaluation.Query
	now := p.now()
	today, err := values.NewLocalDate(now.Year(), now.Month(), now.Day())
	if err != nil {
		return rewards.MarketRateEvaluation{}, s.answer, fmt.Errorf("app: fetch_market_rate: today's date is not a business date: %v", err)
	}
	marketQuery := rewards.MarketQuery{
		Tenant: s.principal.Tenant(), JobCode: query.JobCode, Grade: query.Grade,
		PayZone: query.PayZone, Currency: query.Currency, AsOf: query.AsOf,
	}
	if !marketQuery.AsOf.IsSet() {
		marketQuery.AsOf = today
	}
	eval, err := rewards.LookupMarketRate(ctx, source, marketQuery)
	if err != nil {
		return rewards.MarketRateEvaluation{}, s.answer, err
	}
	s.answer.Digest = eval.ResultDigest
	return eval, s.answer, nil
}

// employmentSubject returns the intent's worker subject: the PEOPLE-domain
// reference the rating lookup keys on. Empty means the intent names no
// worker, and no rating is consulted.
func employmentSubject(subjects []intent.SubjectReference) string {
	for _, subject := range subjects {
		if subject.AuthorityDomain == "PEOPLE" && subject.SubjectID != "" {
			return subject.SubjectID
		}
	}
	return ""
}

// highPerformerPin resolves the compiled-plan pin a promotion start carries
// for subjectRef: the configured variant digest for a top-band subject
// under the execute plan, empty for everyone else. Empty means "no pin" —
// the resolver serves the plan's own digest. Every doubt (prototype mode,
// unconfigured variant, no lookup, unrated or lower-rated subject, lookup
// fault) resolves the execute plan, never the variant.
func (s *IntentService) highPerformerPin(ctx context.Context, tenant values.TenantId, subjectRef string) string {
	if s == nil || s.executionPlan != PromotionPlanExecute || s.highPerformerVariantDigest == "" ||
		s.performanceRatings == nil || subjectRef == "" {
		return ""
	}
	rating, err := s.performanceRatings.LookupCalibratedRating(ctx, tenant, subjectRef)
	if err != nil {
		return ""
	}
	if !PinsHighPerformerVariant(s.executionPlan, &rating) {
		return ""
	}
	return s.highPerformerVariantDigest
}
