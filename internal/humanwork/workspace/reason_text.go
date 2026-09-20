package workspace

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/reasontext"

// JourneyReasonNotProse is the refusal reason for a business reason written
// as a machine token (`promotion_into_senior_hrbp`) instead of a sentence an
// approver can read (REV-095-02).
const JourneyReasonNotProse = "journey.input.reason_not_prose"

// TokenShaped reports whether a free-text value is a machine token rather
// than prose. It is [reasontext.TokenShaped], kept here so write-path callers
// need only this package.
func TokenShaped(value string) bool { return reasontext.TokenShaped(value) }

// ValidateBusinessReason refuses a token-shaped business reason with a typed
// input error naming field, so the caller can mark the exact form control.
// An empty value is not judged here; required-field checks own that.
func ValidateBusinessReason(field, value string) error {
	if TokenShaped(value) {
		return &JourneyInputError{FieldPath: field, ReasonRef: JourneyReasonNotProse,
			Detail: "is written as an identifier; describe the reason in words"}
	}
	return nil
}
