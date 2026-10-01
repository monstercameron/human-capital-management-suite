package journeyclient

import (
	"strings"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// payRuleProjectionFor is the one client projection of a published ladder
// edge. Its percentage edges travel with the exact money bounds so helper text
// and validation cannot accidentally read different rule values.
func payRuleProjectionFor(worker *journeyv1.Worker, options *journeyv1.WorkforceOptions, path *journeyv1.PromotionPathOption) *proposalPayRange {
	if path == nil {
		return nil
	}
	minimum, err := values.NewPercentage(path.GetMinimumBaseIncrease(), 4, values.RoundingExactRequired)
	if err != nil {
		return nil
	}
	maximum, err := values.NewPercentage(path.GetMaximumBaseIncrease(), 4, values.RoundingExactRequired)
	if err != nil {
		return nil
	}
	rule := &proposalPayRange{minimumIncrease: minimum, maximumIncrease: maximum}
	if worker == nil || strings.TrimSpace(worker.GetBasePay()) == "" {
		return rule
	}
	current, err := values.NewMoney(worker.GetBasePay(), workerCurrency(worker, options), 2, values.RoundingExactRequired)
	if err != nil {
		return nil
	}
	if current, err = currentInTargetBasis(current, worker, path); err != nil {
		return nil
	}
	rule.minimum, rule.maximum, err = promotion.BasePayBounds(current, minimum, maximum)
	if err != nil {
		return nil
	}
	return rule
}

func proposalPayRuleHelpLocale(rule *proposalPayRange, path *journeyv1.PromotionPathOption, copy productui.LocaleContext) string {
	help := copy.Text("journey.form_base_rule_unavailable")
	if rule != nil && rule.minimumIncrease.Validate() == nil && rule.maximumIncrease.Validate() == nil {
		help = copy.Text("journey.form_base_rule", map[string]string{
			"minimum": fractionPercentLabelLocale(copy, rule.minimumIncrease.Fraction().String()),
			"maximum": fractionPercentLabelLocale(copy, rule.maximumIncrease.Fraction().String()),
		})
	}
	if path != nil && len(path.GetBenefitRuleRefs()) > 0 {
		help += " " + copy.Text("journey.form_benefit_rule")
	}
	return help
}

func (r *proposalPayRange) hasBounds() bool {
	return r != nil && r.minimum.Validate() == nil && r.maximum.Validate() == nil
}
