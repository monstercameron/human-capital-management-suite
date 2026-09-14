package journeyclient

import (
	"strings"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
	"google.golang.org/grpc/status"
)

// proposalRefusalPresentation is the one refusal projection consumed by the
// live client and by the renderer's SSR-capable Page contract. Neither path
// interprets human-readable transport descriptions.
type proposalRefusalPresentation struct {
	notice      *journey.Notice
	fields      map[string]string
	corrections map[string]proposalCorrection
}

func mapProposalRefusal(err error, copy productui.LocaleContext) proposalRefusalPresentation {
	corrections := proposalCorrections(err)
	return proposalRefusalPresentation{
		notice:      noticeFromError(err, copy),
		fields:      localizeProposalCorrections(corrections, copy),
		corrections: corrections,
	}
}

// proposalCorrection retains only an allowlisted catalog key and exact
// authorized money. Raw transport descriptions and diagnostic identifiers
// never survive in the client state after a refusal has been projected.
type proposalCorrection struct {
	key      string
	payRange *proposalPayRange
}

func proposalCorrections(err error) map[string]proposalCorrection {
	out := map[string]proposalCorrection{}
	for _, raw := range status.Convert(err).Details() {
		detail, ok := raw.(*commonv1.ErrorDetail)
		if !ok {
			continue
		}
		for _, violation := range detail.GetFieldViolations() {
			fieldID, key := proposalFieldErrorKey(violation.GetFieldPath())
			if fieldID == "" {
				continue
			}
			correction := proposalCorrection{key: key}
			if fieldID == FieldBase {
				switch violation.GetRuleRef() {
				case "promotion.base_pay.not_exact":
					correction.key = "journey.field_base_exact_error"
				case "promotion.ladder.base_increase_out_of_range":
					if payRange := typedProposalPayRange(violation.GetPermittedMoneyRange()); payRange != nil {
						correction.key = "journey.field_base_range_error"
						correction.payRange = payRange
					}
				}
			}
			// A generic field refusal may precede the typed pay rule in a
			// status. Prefer the correction with actionable exact bounds,
			// independently of violation order, while retaining the first
			// equally specific correction for a field.
			if previous := out[fieldID]; proposalCorrectionRank(correction) > proposalCorrectionRank(previous) {
				out[fieldID] = correction
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func proposalCorrectionRank(correction proposalCorrection) int {
	if correction.payRange != nil {
		return 3
	}
	if correction.key == "journey.field_base_exact_error" {
		return 2
	}
	if correction.key != "" {
		return 1
	}
	return 0
}

// typedProposalPayRange accepts only exact, coherent bounds supplied by the
// server's typed refusal. A cached worker/path projection is useful form help,
// but must never be presented as the current server correction after rejection.
func typedProposalPayRange(raw *commonv1.MoneyRange) *proposalPayRange {
	if raw == nil {
		return nil
	}
	minimum, err := values.NewMoney(raw.GetMinimum(), raw.GetCurrency(), 2, values.RoundingExactRequired)
	if err != nil {
		return nil
	}
	maximum, err := values.NewMoney(raw.GetMaximum(), raw.GetCurrency(), 2, values.RoundingExactRequired)
	if err != nil || minimum.Amount().Sign() <= 0 || minimum.Amount().Cmp(maximum.Amount()) > 0 {
		return nil
	}
	return &proposalPayRange{minimum: minimum, maximum: maximum}
}

func localizeProposalCorrections(corrections map[string]proposalCorrection, copy productui.LocaleContext) map[string]string {
	if len(corrections) == 0 {
		return nil
	}
	out := make(map[string]string, len(corrections))
	for fieldID, correction := range corrections {
		if correction.payRange != nil {
			out[fieldID] = copy.Text(correction.key, correction.payRange.localizedBounds(copy))
		} else {
			out[fieldID] = copy.Text(correction.key)
		}
	}
	return out
}

// supportReference accepts only the request/correlation token shapes the
// server issues. A diagnostic detail is not permission to render arbitrary
// provider text inside the support disclosure.
func supportReference(err error) string {
	if err == nil {
		return ""
	}
	for _, raw := range status.Convert(err).Details() {
		detail, ok := raw.(*commonv1.ErrorDetail)
		if !ok {
			continue
		}
		id := detail.GetCorrelationId()
		if validSupportReference(id) {
			return id
		}
	}
	return ""
}

func validSupportReference(id string) bool {
	var suffix string
	switch {
	case strings.HasPrefix(id, "req:"):
		suffix = strings.TrimPrefix(id, "req:")
		if len(suffix) < 16 || len(suffix) > 64 {
			return false
		}
		for _, c := range suffix {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				return false
			}
		}
		return true
	case strings.HasPrefix(id, "cor_"):
		suffix = strings.TrimPrefix(id, "cor_")
		if len(suffix) < 12 || len(suffix) > 64 {
			return false
		}
		for _, c := range suffix {
			if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z')) {
				return false
			}
		}
		return true
	}
	return false
}

// proposalPayRange projects the server-supplied worker baseline and published
// ladder percentages through the same exact-cent predicate used by admission.
// It is guidance, never a substitute for the server's current-fact check.
type proposalPayRange struct {
	minimum values.Money
	maximum values.Money
}

func proposalPayRangeFor(worker *journeyv1.Worker, options *journeyv1.WorkforceOptions, path *journeyv1.PromotionPathOption) *proposalPayRange {
	if worker == nil || path == nil || strings.TrimSpace(worker.GetBasePay()) == "" {
		return nil
	}
	currency := workerCurrency(worker, options)
	current, err := values.NewMoney(worker.GetBasePay(), currency, 2, values.RoundingExactRequired)
	if err != nil {
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
	lower, upper, err := promotion.BasePayBounds(current, minimum, maximum)
	if err != nil {
		return nil
	}
	return &proposalPayRange{minimum: lower, maximum: upper}
}

func (r *proposalPayRange) localizedBounds(copy productui.LocaleContext) map[string]string {
	if r == nil {
		return nil
	}
	return map[string]string{
		"minimum": copy.FormatMoney(r.minimum.Amount().String(), r.minimum.Currency(), 2),
		"maximum": copy.FormatMoney(r.maximum.Amount().String(), r.maximum.Currency(), 2),
	}
}
