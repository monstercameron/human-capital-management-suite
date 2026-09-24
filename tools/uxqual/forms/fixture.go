package forms

import (
	"github.com/monstercameron/human-capital-management-suite/internal/experience/workspacecontract"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/testdata"
)

// RequiredFieldIDs are the tools/uxqual/testdata.PromotionFixture's
// currently-visible RequestField ids that carry Validation.Required. Both
// FORM-004 tests and any caller scoring a rendered document against
// [CheckRequiredFieldSemantics] use this list rather than re-deriving it, so
// a future fixture change that adds or removes a required field is caught
// by this list going stale instead of silently under-checking.
var RequiredFieldIDs = []string{"proposedJobTitle", "proposedGrade", "proposedCompensation", "effectiveDate", "businessReason"}

// erroredFieldID is the one fixture field FixtureWithValidationError puts a
// validation error message on.
const erroredFieldID = "proposedCompensation"

// FixtureWithValidationError returns tools/uxqual/testdata.PromotionFixture
// with erroredFieldID's Validation.Message set, so
// [CheckErrorAssociation] has a real failing-validation field to score. The
// frozen UX-QUAL-001 fixture (tools/uxqual/testdata) has no error-state
// variant of its own -- it only ever needed a clean render -- so this lane
// builds its own copy here rather than editing that package.
func FixtureWithValidationError() contract.WorkspaceContract {
	c := testdata.PromotionFixture()
	fields := make([]contract.RequestField, len(c.Request.Fields))
	copy(fields, c.Request.Fields)
	for i, f := range fields {
		if f.ID == erroredFieldID {
			f.Validation.Message = "Enter an amount within the approved compensation band."
			fields[i] = f
		}
	}
	c.Request.Fields = fields
	return c
}

// ErroredFieldIDs is the field id list [FixtureWithValidationError] sets a
// validation message on.
var ErroredFieldIDs = []string{erroredFieldID}

// fieldValue returns the fixture field's already-formatted display value
// for id, or "" if no such field exists. Tests use it to build a
// [PromotionRequestInputs] that matches the fixture the rendered documents
// were built from, without hand-duplicating the fixture's literal values.
func fieldValue(c contract.WorkspaceContract, id string) string {
	for _, f := range c.Request.Fields {
		if f.ID == id {
			return f.Value
		}
	}
	return ""
}
