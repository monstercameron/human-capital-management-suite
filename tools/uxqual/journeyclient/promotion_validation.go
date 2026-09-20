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

// proposalCorrections decodes the typed wire violations and hands them to
// productui's refusal mapper (REV-091-01). This file owns only the wire
// decoding and the form's element ids; which message a refusal gets, and
// which of several refusals on one field wins, is decided in one place.
func proposalCorrections(err error) map[string]proposalCorrection {
	var violations []productui.PromotionRefusalViolation
	for _, raw := range status.Convert(err).Details() {
		detail, ok := raw.(*commonv1.ErrorDetail)
		if !ok {
			continue
		}
		for _, violation := range detail.GetFieldViolations() {
			typed := productui.PromotionRefusalViolation{FieldPath: violation.GetFieldPath(), RuleRef: violation.GetRuleRef()}
			if permitted := violation.GetPermittedMoneyRange(); permitted != nil {
				typed.Minimum, typed.Maximum, typed.Currency = permitted.GetMinimum(), permitted.GetMaximum(), permitted.GetCurrency()
			}
			violations = append(violations, typed)
		}
	}
	mapped := productui.MapPromotionWireRefusal(violations)
	if len(mapped) == 0 {
		return nil
	}
	out := make(map[string]proposalCorrection, len(mapped))
	for field, correction := range mapped {
		fieldID := proposalFormFieldID(field)
		if fieldID == "" {
			continue
		}
		mappedCorrection := proposalCorrection{key: correction.Key}
		if correction.PayRange != nil {
			mappedCorrection.payRange = &proposalPayRange{minimum: correction.PayRange.Minimum, maximum: correction.PayRange.Maximum}
		}
		out[fieldID] = mappedCorrection
	}
	return out
}

// proposalFormFieldID is this client's element id for a canonical proposal
// form control.
func proposalFormFieldID(field productui.PromotionFormField) string {
	switch field {
	case productui.PromotionFieldWorker:
		return FieldWorker
	case productui.PromotionFieldJobCode:
		return FieldJobCode
	case productui.PromotionFieldGrade:
		return FieldGrade
	case productui.PromotionFieldPosition:
		return FieldPosition
	case productui.PromotionFieldBase:
		return FieldBase
	case productui.PromotionFieldEffective:
		return FieldEffective
	case productui.PromotionFieldReason:
		return FieldReason
	default:
		return ""
	}
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

// refusalReasonRef returns the owned reason reference the server attached to
// a refusal, or "". It is read only to select copy from the closed table in
// [reasonCopyKey]; the string itself is never rendered.
func refusalReasonRef(err error) string {
	if err == nil {
		return ""
	}
	for _, raw := range status.Convert(err).Details() {
		detail, ok := raw.(*commonv1.ErrorDetail)
		if !ok {
			continue
		}
		if reason := detail.GetReasonRef(); reason != "" {
			return reason
		}
	}
	return ""
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
