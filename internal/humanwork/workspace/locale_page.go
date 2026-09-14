package workspace

import (
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/contract"
)

// localizeSourceRecord transforms only presentation strings before the
// contract is frozen. The query remains canonical, and this function is
// intentionally called after the read/simulation chain, so locale cannot
// become a selector for a governed answer.
func localizeSourceRecord(source contract.SourceRecord, q Query, locale LocaleContext, fields contract.FieldVisibility, actions contract.ActionVisibility) (contract.SourceRecord, []TranslationDiagnostic) {
	var diagnostics []TranslationDiagnostic
	translate := func(key, english string, report bool) string {
		value, diagnostic := Translate(locale, key, english)
		if diagnostic != nil && report {
			diagnostics = append(diagnostics, *diagnostic)
		}
		return value
	}

	name := source.WorkerName
	if name == "" {
		name = q.WorkerRef
	}
	source.Title = translate("title.promotion", "Promotion", true) + ": " + name

	fieldKeys := map[string]string{
		FieldWorkerName:           "field.worker",
		FieldCurrentJobTitle:      "field.current_job",
		FieldCurrentGrade:         "field.current_grade",
		FieldCurrentBasePay:       "field.current_base",
		FieldProposedJobTitle:     "field.proposed_job",
		FieldProposedGrade:        "field.proposed_grade",
		FieldTargetPosition:       "field.target_position",
		FieldTargetOrgUnit:        "field.target_org_unit",
		FieldProposedComp:         "field.proposed_base",
		FieldEffectiveDate:        "field.effective_date",
		FieldBusinessReason:       "field.business_reason",
		FieldBandPosition:         "field.band_position",
		FieldAnnualizedIncrease:   "field.annualized_increase",
		FieldCompensationWithheld: "field.compensation_disclosure",
	}
	for i := range source.AllFields {
		field := &source.AllFields[i]
		if key, ok := fieldKeys[field.ID]; ok {
			// A masked field may be localized internally, but a diagnostic for
			// it must not be rendered: reporting an unavailable label would be
			// a disclosure path around the contract projection.
			field.Label = translate(key, field.Label, fields[field.ID])
		}
		switch field.ID {
		case FieldCurrentBasePay, FieldAnnualizedIncrease:
			field.Value = localizeMoneyPrefix(locale, field.Value)
		case FieldProposedComp:
			// The editable money field receives a localized display value. The
			// handler parses it back to q's canonical amount before the existing
			// FORM-004 intent identity is checked.
			if value, err := FormatCurrency(locale, q.ProposedBase, q.Currency); err == nil {
				field.Value = value
			}
		}
	}

	actionKeys := map[string]string{
		ActionRunSimulation:     "action.run_simulation",
		ActionSubmitForApproval: "action.submit_for_approval",
		ActionForceExecute:      "action.force_execute",
	}
	for i := range source.AllActions {
		action := &source.AllActions[i]
		if key, ok := actionKeys[action.ID]; ok {
			action.Label = translate(key, action.Label, actions[action.ID])
		}
	}
	return source, diagnostics
}

// localizeMoneyPrefix replaces a leading "<canonical amount> <ISO code>"
// fragment. It leaves prose and non-money values untouched rather than
// guessing their semantics.
func localizeMoneyPrefix(locale LocaleContext, value string) string {
	parts := strings.SplitN(value, " ", 3)
	if len(parts) < 2 {
		return value
	}
	formatted, err := FormatCurrency(locale, parts[0], parts[1])
	if err != nil {
		return value
	}
	if len(parts) == 2 {
		return formatted
	}
	return formatted + " " + parts[2]
}

// CanonicalizePresentationAnswers converts the two localized editable values
// back to the canonical corpus form before they reach Request.Typed or the
// FORM-004 intent proof. It accepts canonical wire values too, which keeps
// API-like callers and native date controls interoperable without guessing at
// a locale's punctuation.
func CanonicalizePresentationAnswers(locale LocaleContext, currency string, answers map[string]string) (map[string]string, error) {
	canonical := make(map[string]string, len(answers))
	maps.Copy(canonical, answers)
	if value, ok := canonical[FieldProposedComp]; ok && value != "" {
		amount, err := canonicalAmountOrLocalized(locale, value, currency)
		if err != nil {
			return nil, fmt.Errorf("%w: proposed compensation: %v", ErrLocaleValue, err)
		}
		canonical[FieldProposedComp] = amount
	}
	if value, ok := canonical[FieldEffectiveDate]; ok && value != "" {
		date, err := canonicalDateOrLocalized(locale, value)
		if err != nil {
			return nil, fmt.Errorf("%w: effective date: %v", ErrLocaleValue, err)
		}
		canonical[FieldEffectiveDate] = date
	}
	return canonical, nil
}

func canonicalAmountOrLocalized(locale LocaleContext, value, currency string) (string, error) {
	if _, _, _, err := canonicalNumber(value); err == nil {
		_, whole, fraction, _ := canonicalNumber(value)
		negative, _, _, _ := canonicalNumber(value)
		return sign(negative) + whole + decimalSuffix(fraction, "."), nil
	}
	return ParseCurrency(locale, value, currency)
}

func canonicalDateOrLocalized(locale LocaleContext, value string) (string, error) {
	if parsed, err := time.Parse("2006-01-02", value); err == nil && parsed.Format("2006-01-02") == value {
		return value, nil
	}
	return ParseDate(locale, value)
}
