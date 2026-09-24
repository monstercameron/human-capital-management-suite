package forms

import "fmt"

// Field ids the Promotion request form actually asks a human to fill in
// (internal/experience/workspacecontract.RequestField.ID values). currentJobTitle is
// excluded: the form displays it (FieldKindReadOnly) but never collects it,
// so it is not part of what a submission carries.
const (
	fieldProposedJobTitle     = "proposedJobTitle"
	fieldProposedGrade        = "proposedGrade"
	fieldProposedCompensation = "proposedCompensation"
	fieldEffectiveDate        = "effectiveDate"
	fieldBusinessReason       = "businessReason"
)

// FromFormSubmission builds the [IntentInstance] the accessible rendered-form
// route produces once a human's answers are submitted. answers is exactly
// what an HTML form POST or a captured DOM submit event hands a server,
// keyed by the same field id both renderers (tools/uxqual/render/ssr,
// tools/uxqual/render/gwc) emit as each control's id/name attribute -- see
// [ExtractFormAnswers] for pulling answers out of a real rendered document.
func FromFormSubmission(workerID string, answers map[string]string) (IntentInstance, error) {
	in := PromotionRequestInputs{
		WorkerID:             workerID,
		ProposedJobTitle:     answers[fieldProposedJobTitle],
		ProposedGrade:        answers[fieldProposedGrade],
		ProposedCompensation: answers[fieldProposedCompensation],
		EffectiveDate:        answers[fieldEffectiveDate],
		BusinessReason:       answers[fieldBusinessReason],
	}
	instance, err := newIntentInstance(in)
	if err != nil {
		return IntentInstance{}, fmt.Errorf("forms: form submission route: %w", err)
	}
	return instance, nil
}
