// Package productui resolves the proposal confirmation
// review. The review composes the chain's governed pieces
// — the values comparison, the collection guide, and the
// missing-fact summary — into the single confirmation a
// submitter reads. Ready means every fact present and the
// change shown; it never authorizes submission, which
// stays server authority. Inputs are never mutated.
package productui

import "strings"

// ProposalConfirmation is the resolved confirmation
// review for one proposal's supplied facts: the values
// change, the carried facts, the collection guide, the
// missing-fact summary, and whether the proposal is ready
// to confirm.
type ProposalConfirmation struct {
	Change        FieldComparison
	WorkerRef     string
	EffectiveDate string
	Effective     EffectiveDateExplanation
	Guide         ProposalGuide
	Missing       ValidationSummaryModel
	// Ready reports the proposal reviewable. It never
	// authorizes submission.
	Ready bool
}

// EffectiveDateExplanation keeps the date visible in a compact confirmation
// while explaining its boundary without claiming that approval has executed
// the change. The date is display data supplied by the caller; this helper
// performs no calendar or policy evaluation.
type EffectiveDateExplanation struct {
	Date        string
	Label       string
	Explanation string
	Present     bool
}

// ExplainEffectiveDate resolves reviewed copy for a proposal date. A blank
// date remains actionable and is never described as immediate or scheduled.
func ExplainEffectiveDate(locale LocaleContext, date string) EffectiveDateExplanation {
	date = strings.TrimSpace(date)
	result := EffectiveDateExplanation{Date: date, Label: locale.Text("work.effective_date"), Present: date != ""}
	if date == "" {
		result.Explanation = locale.Text("work.proposal_need_date")
		return result
	}
	result.Explanation = locale.Text("history.effective", map[string]string{"value": date})
	return result
}

// ResolveProposalConfirmation resolves the confirmation
// review for one proposal's supplied facts.
func ResolveProposalConfirmation(locale LocaleContext, input ProposalCollection) ProposalConfirmation {
	guide := ResolveProposalGuide(locale, input)
	var issues []ValidationIssue
	for _, step := range guide.Steps {
		if !step.Complete {
			issues = append(issues, ValidationIssue{Message: step.Title + ": " + step.Detail})
		}
	}
	return ProposalConfirmation{
		Change:        CompareFieldValue(locale, locale.Text("work.proposal_step_values"), input.Current, input.Proposed),
		WorkerRef:     input.WorkerRef,
		EffectiveDate: input.EffectiveDate,
		Effective:     ExplainEffectiveDate(locale, input.EffectiveDate),
		Guide:         guide,
		Missing:       ResolveValidationSummary(locale, ValidationState{Issues: issues}, nil),
		Ready:         guide.Complete,
	}
}
