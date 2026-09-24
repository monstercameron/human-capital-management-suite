package forms

import "fmt"

// EquivalentRoute is FORM-004's machine-readable description of how the
// same Promotion request can be completed without the accessible form: a
// governed capability call carrying the identical typed inputs.
//
// planning/specs/human-work-forms-and-rules.md's other named alternative --
// a human-work task backed by a form -- is not built here: Phase 1 packages
// Promotion approval as a WorkItem with a typed reason field, not a
// form-backed task (see FORM-001's RETIRED disposition in
// planning/todos.md, 2026-09-02), so a capability-call description is the
// one equivalent route this lane can prove end-to-end without importing
// another lane's WorkItem package.
type EquivalentRoute struct {
	// Kind names which of FORM-004's two named alternatives this is:
	// "CAPABILITY_CALL" (a governed capability call with the same typed
	// inputs) or "HUMAN_WORK_TASK" (a human-work task with the same typed
	// inputs). This package only builds the former; see the type doc.
	Kind              string
	CapabilityID      string
	CapabilityVersion string
	InputSchemaRef    string
	Description       string
}

// PromotionEquivalentRoute returns the equivalent route for the Promotion
// request tools/uxqual/testdata.PromotionFixture renders.
// CapabilityID/CapabilityVersion match that fixture's
// contract.Provenance.CapabilityID/CapabilityVersion exactly, so the route
// description names the same governed operation the rendered workspace says
// produced the contract in the first place.
func PromotionEquivalentRoute() EquivalentRoute {
	const capabilityID, capabilityVersion = "people.promote", "v1"
	return EquivalentRoute{
		Kind:              "CAPABILITY_CALL",
		CapabilityID:      capabilityID,
		CapabilityVersion: capabilityVersion,
		InputSchemaRef:    "tools/uxqual/forms.PromotionRequestInputs/v1",
		Description: fmt.Sprintf(
			"Call the %s %s capability directly with the same typed "+
				"PromotionRequestInputs (worker id, proposed job title, "+
				"proposed grade, proposed compensation, effective date, business reason) a "+
				"caller who cannot use the rendered form -- an API "+
				"integration, a bulk loader, or an operator using a governed "+
				"CLI -- would supply instead of filling in the workspace "+
				"request form. See FromCapabilityCall.",
			capabilityID, capabilityVersion),
	}
}

// FromCapabilityCall builds the [IntentInstance] the direct governed
// capability-call route produces for in, independent of any rendered form.
func FromCapabilityCall(in PromotionRequestInputs) (IntentInstance, error) {
	instance, err := newIntentInstance(in)
	if err != nil {
		return IntentInstance{}, fmt.Errorf("forms: capability-call route: %w", err)
	}
	return instance, nil
}
