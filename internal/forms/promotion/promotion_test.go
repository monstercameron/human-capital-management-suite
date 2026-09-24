package forms

import (
	"errors"
	"testing"
)

func validPromotionInputs() PromotionRequestInputs {
	return PromotionRequestInputs{
		WorkerID:             "worker-7",
		ProposedJobTitle:     "Nurse III",
		ProposedGrade:        "G7",
		ProposedCompensation: "98000.00",
		EffectiveDate:        "2026-10-01",
		BusinessReason:       "Expanded scope",
	}
}

func TestEquivalentRoutesProduceSameIntent(t *testing.T) {
	inputs := validPromotionInputs()
	capability, err := FromCapabilityCall(inputs)
	if err != nil {
		t.Fatalf("FromCapabilityCall: %v", err)
	}
	form, err := FromFormSubmission(inputs.WorkerID, map[string]string{
		"proposedJobTitle":     inputs.ProposedJobTitle,
		"proposedGrade":        inputs.ProposedGrade,
		"proposedCompensation": inputs.ProposedCompensation,
		"effectiveDate":        inputs.EffectiveDate,
		"businessReason":       inputs.BusinessReason,
	})
	if err != nil {
		t.Fatalf("FromFormSubmission: %v", err)
	}
	if capability != form {
		t.Fatalf("equivalent routes disagree:\n capability=%+v\n form=%+v", capability, form)
	}
}

func TestRoutesRejectIncompleteInputs(t *testing.T) {
	inputs := validPromotionInputs()
	inputs.WorkerID = ""
	if _, err := FromCapabilityCall(inputs); !errors.Is(err, ErrIntentInputsInvalid) {
		t.Fatalf("FromCapabilityCall error = %v, want ErrIntentInputsInvalid", err)
	}
	if _, err := FromFormSubmission("", map[string]string{"proposedJobTitle": "Nurse III"}); !errors.Is(err, ErrIntentInputsInvalid) {
		t.Fatalf("FromFormSubmission error = %v, want ErrIntentInputsInvalid", err)
	}
}
