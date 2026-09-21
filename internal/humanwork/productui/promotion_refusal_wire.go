package productui

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// REV-091-01: this file extends PROMOUX-007's one refusal-to-presentation
// mapper to the shape a refusal actually arrives in on the live proposal
// form -- a transport field violation (field path, typed reason reference,
// optional exact money range) rather than an in-process
// promotion.PreflightResult. The enhanced client decodes the wire and hands
// the typed values here; it keeps no field or reason table of its own, so
// the page form and this package cannot drift apart.
//
// Like [MapPromotionRefusal], it never reads a human-readable description:
// the transport's Description text is not part of
// [PromotionRefusalViolation] at all.

// PromotionFormField is the canonical identity of one promotion proposal
// form control, independent of how a particular client names its elements.
type PromotionFormField string

// The promotion proposal form's controls.
const (
	PromotionFieldWorker    PromotionFormField = "worker"
	PromotionFieldJobCode   PromotionFormField = "job_code"
	PromotionFieldGrade     PromotionFormField = "grade"
	PromotionFieldPosition  PromotionFormField = "position"
	PromotionFieldBase      PromotionFormField = "base"
	PromotionFieldEffective PromotionFormField = "effective_date"
	PromotionFieldReason    PromotionFormField = "reason"
)

// Typed reason references the proposal write path emits for a field. They
// are the server's stable identities (internal/intent/app and
// internal/humanwork/workspace), matched exactly and never parsed.
const (
	promotionRuleBaseNotExact    = "promotion.base_pay.not_exact"
	promotionRuleBaseOutOfRange  = "promotion.ladder.base_increase_out_of_range"
	promotionPayRangeScaleDigits = 2
)

// PromotionRefusalViolation is one typed field violation from the wire.
// Minimum, Maximum and Currency are the server's exact permitted range, when
// it sent one; they are validated here before any of them can be shown.
type PromotionRefusalViolation struct {
	FieldPath string
	RuleRef   string
	Minimum   string
	Maximum   string
	Currency  string
}

// PromotionPayRange is an exact, coherent server-supplied correction.
type PromotionPayRange struct {
	Minimum values.Money
	Maximum values.Money
}

// PromotionRefusalCorrection is the presentation of one refused field: the
// control it belongs to, the catalog key of its message, and the exact range
// the message quotes when the key needs one. It holds a key rather than a
// sentence so a client can re-localize it when the reader changes language.
type PromotionRefusalCorrection struct {
	Field    PromotionFormField
	Key      string
	PayRange *PromotionPayRange
}

// PromotionRefusalField maps a wire field path onto its form control and the
// field's generic corrective key. An unknown path maps to nothing, so an
// unrecognised path never becomes an implementation-name disclosure or an
// error attached to the wrong control.
func PromotionRefusalField(path string) (PromotionFormField, string) {
	switch strings.TrimSpace(path) {
	case "subject_worker_ref", "worker_ref":
		return PromotionFieldWorker, "journey.field_worker_error"
	case "desired_job_code", "target_job_code", "job_code":
		return PromotionFieldJobCode, "journey.field_job_error"
	case "desired_grade", "target_grade", "grade":
		return PromotionFieldGrade, "journey.field_grade_error"
	case "desired_position_id", "target_position_id", "position_id":
		return PromotionFieldPosition, "journey.field_position_error"
	case "desired_base_pay", "proposed_base", "proposed_base_pay", "base_pay":
		return PromotionFieldBase, "journey.field_base_error"
	case "effective_date":
		return PromotionFieldEffective, "journey.field_effective_error"
	case "reason", "business_reason":
		return PromotionFieldReason, "journey.field_reason_error"
	default:
		return "", ""
	}
}

// MapPromotionWireRefusal maps typed wire violations onto at most one
// correction per form control. When several violations name the same
// control, the most actionable one wins independently of their order: exact
// bounds over "not exact" over the field's generic message.
func MapPromotionWireRefusal(violations []PromotionRefusalViolation) map[PromotionFormField]PromotionRefusalCorrection {
	out := map[PromotionFormField]PromotionRefusalCorrection{}
	for _, violation := range violations {
		field, key := PromotionRefusalField(violation.FieldPath)
		if field == "" {
			continue
		}
		correction := PromotionRefusalCorrection{Field: field, Key: key}
		switch {
		case field == PromotionFieldBase && violation.RuleRef == promotionRuleBaseNotExact:
			correction.Key = "journey.field_base_exact_error"
		case field == PromotionFieldBase && violation.RuleRef == promotionRuleBaseOutOfRange:
			if payRange := promotionWirePayRange(violation); payRange != nil {
				correction.Key = "journey.field_base_range_error"
				correction.PayRange = payRange
			}
		}
		if previous, seen := out[field]; !seen || promotionCorrectionRank(correction) > promotionCorrectionRank(previous) {
			out[field] = correction
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Message renders the correction in locale, quoting exact bounds only when
// the correction carries a validated server range.
func (c PromotionRefusalCorrection) Message(locale LocaleContext) string {
	if c.PayRange != nil {
		return locale.Text(c.Key, map[string]string{
			"minimum": locale.FormatMoney(c.PayRange.Minimum.Amount().String(), c.PayRange.Minimum.Currency(), promotionPayRangeScaleDigits),
			"maximum": locale.FormatMoney(c.PayRange.Maximum.Amount().String(), c.PayRange.Maximum.Currency(), promotionPayRangeScaleDigits),
		})
	}
	return locale.Text(c.Key)
}

func promotionCorrectionRank(correction PromotionRefusalCorrection) int {
	switch {
	case correction.PayRange != nil:
		return 3
	case correction.Key == "journey.field_base_exact_error":
		return 2
	case correction.Key != "":
		return 1
	}
	return 0
}

// promotionWirePayRange accepts only exact, coherent, positive bounds in one
// currency. A cached client projection is useful form help, but must never
// be presented as the server's current correction after a rejection.
func promotionWirePayRange(v PromotionRefusalViolation) *PromotionPayRange {
	if strings.TrimSpace(v.Minimum) == "" || strings.TrimSpace(v.Maximum) == "" {
		return nil
	}
	minimum, err := values.NewMoney(v.Minimum, v.Currency, promotionPayRangeScaleDigits, values.RoundingExactRequired)
	if err != nil {
		return nil
	}
	maximum, err := values.NewMoney(v.Maximum, v.Currency, promotionPayRangeScaleDigits, values.RoundingExactRequired)
	if err != nil || minimum.Amount().Sign() <= 0 || minimum.Amount().Cmp(maximum.Amount()) > 0 {
		return nil
	}
	return &PromotionPayRange{Minimum: minimum, Maximum: maximum}
}
