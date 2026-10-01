package app

import (
	"context"
	"strconv"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// uxblindYPayRule is the server projection of one published ladder edge. The
// same edge is published to WorkforceOptions, where the journey client uses
// it for helper text and validation.
type uxblindYPayRule struct {
	minimumIncrease values.Percentage
	maximumIncrease values.Percentage
	targetBasis     rewards.PayBasis
	annualization   int32
}

func (e *journeyEngine) uxblindYPayRule(ctx context.Context, tenant values.TenantId, payload *structValue) *uxblindYPayRule {
	if e == nil || e.ladder == nil || payload == nil {
		return nil
	}
	current, err := fieldsOf(payload, "current_placement")
	if err != nil {
		return nil
	}
	target, err := targetPlacement(payload)
	if err != nil {
		return nil
	}
	ladder, err := e.ladderEdges(ctx, tenant)
	if err != nil {
		return nil
	}
	paths, err := publishedPromotionPathsFrom(ladder)
	if err != nil {
		return nil
	}
	for _, path := range paths {
		if !path.matches(optionalStr(current, "job_code"), optionalStr(current, "grade"), target.JobCode, target.Grade) {
			continue
		}
		rule, ruleErr := uxblindYPayRuleFromOption(path.Option)
		if ruleErr != nil {
			return nil
		}
		return rule
	}
	return nil
}

func uxblindYPayRuleFromOption(option workspace.PromotionPathOption) (*uxblindYPayRule, error) {
	minimum, err := values.NewPercentage(option.MinimumBaseIncrease, 4, values.RoundingExactRequired)
	if err != nil {
		return nil, err
	}
	maximum, err := values.NewPercentage(option.MaximumBaseIncrease, 4, values.RoundingExactRequired)
	if err != nil {
		return nil, err
	}
	basis, err := uxblindYPayBasis(option.TargetPayBasis)
	if err != nil {
		return nil, err
	}
	return &uxblindYPayRule{minimumIncrease: minimum, maximumIncrease: maximum, targetBasis: basis, annualization: option.AnnualizationHours}, nil
}

func uxblindYPayBasis(text string) (rewards.PayBasis, error) {
	if text == "HOURLY_RATE" {
		text = "HOURLY"
	}
	return payBasis(text)
}

// applyUXBlindYPayRule replaces only the entry bounds on the review guardrail.
// BandPosition remains the catalog band's position; the displayed money bounds
// and largest permitted increase are the published promotion rule that the
// form states and validates.
func applyUXBlindYPayRule(g promotion.CompensationGuardrail, rule *uxblindYPayRule, current rewards.CompensationSnapshot) promotion.CompensationGuardrail {
	if !g.Available() || rule == nil {
		return g
	}
	base, ok := current.Base.Get()
	if !ok {
		return g
	}
	comparable, err := uxblindYCurrentInTargetBasis(base, current.PayBasis, rule)
	if err != nil {
		return g
	}
	minimum, maximum, err := promotion.BasePayBounds(comparable, rule.minimumIncrease, rule.maximumIncrease)
	if err != nil {
		return g
	}
	g.CurrentAnnualized = comparable
	g.MinimumAnnualized, g.MaximumAnnualized = minimum, maximum
	g.PermittedIncreasePercent = rule.maximumIncrease
	return g
}

func uxblindYCurrentInTargetBasis(current values.Money, basis rewards.PayBasis, rule *uxblindYPayRule) (values.Money, error) {
	if rule == nil || rule.annualization <= 0 || (rule.targetBasis != rewards.PayBasisHourly && basis != rewards.PayBasisHourly) || (rule.targetBasis == rewards.PayBasisHourly && basis == rewards.PayBasisHourly) {
		return current, nil
	}
	factor, err := values.NewDecimal(strconv.Itoa(int(rule.annualization)), 0, values.RoundingExactRequired)
	if err != nil {
		return values.Money{}, err
	}
	if rule.targetBasis == rewards.PayBasisHourly {
		amount, err := current.Amount().Div(factor, 2, values.RoundingHalfEven)
		if err != nil {
			return values.Money{}, err
		}
		return values.NewMoney(amount.String(), current.Currency(), 2, values.RoundingExactRequired)
	}
	return current.MulDecimal(factor, 2, values.RoundingExactRequired)
}

func (e *journeyEngine) journeyCompensationGuardrailForPayRule(ctx context.Context, bands rewards.PayBandCatalog, principal *trust.Principal, purpose string, tenant values.TenantId, subject values.EntityRef, evaluatedAt values.Instant, relationships []authz.RelationshipFact, payload *structValue, rule *uxblindYPayRule) promotion.CompensationGuardrail {
	g := journeyCompensationGuardrail(ctx, bands, principal, purpose, tenant, subject, evaluatedAt, relationships, payload)
	if rule == nil {
		return g
	}
	current, err := compensationSnapshot(payload, "current")
	if err != nil {
		return g
	}
	return applyUXBlindYPayRule(g, rule, current)
}
