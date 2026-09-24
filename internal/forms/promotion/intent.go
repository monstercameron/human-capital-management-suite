package forms

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
)

// ErrIntentInputsInvalid is returned when a [PromotionRequestInputs] value is
// missing a typed input either route needs before it can produce an
// [IntentInstance].
var ErrIntentInputsInvalid = errors.New("forms: promotion request inputs fail validation")

// PromotionRequestInputs is the one typed payload both of FORM-004's
// equivalent routes consume: the accessible rendered-form route
// ([FromFormSubmission]) and the route that completes the same request
// without the form ([FromCapabilityCall]). Field names match
// internal/experience/workspacecontract's RequestField ids exactly (proposedJobTitle,
// proposedGrade, proposedCompensation, effectiveDate, businessReason), so a
// value read off the rendered form and a value passed straight to a governed
// capability call are, by construction, the same typed shape -- not two
// schemas a caller has to keep in sync by hand.
type PromotionRequestInputs struct {
	WorkerID             string
	ProposedJobTitle     string
	ProposedGrade        string
	ProposedCompensation string
	EffectiveDate        string
	BusinessReason       string
}

// Validate reports whether every typed input is present. Neither route
// builds an [IntentInstance] from an invalid value.
func (in PromotionRequestInputs) Validate() error {
	switch {
	case in.WorkerID == "":
		return fmt.Errorf("%w: no worker id", ErrIntentInputsInvalid)
	case in.ProposedJobTitle == "":
		return fmt.Errorf("%w: no proposed job title", ErrIntentInputsInvalid)
	case in.ProposedGrade == "":
		return fmt.Errorf("%w: no proposed grade", ErrIntentInputsInvalid)
	case in.ProposedCompensation == "":
		return fmt.Errorf("%w: no proposed compensation", ErrIntentInputsInvalid)
	case in.EffectiveDate == "":
		return fmt.Errorf("%w: no effective date", ErrIntentInputsInvalid)
	case in.BusinessReason == "":
		return fmt.Errorf("%w: no business reason", ErrIntentInputsInvalid)
	}
	return nil
}

// canonicalBytes encodes every field with length-prefixed labeled framing --
// the same anti-ambiguity framing internal/governance/legal and
// internal/trust/authz use for their own canonical digests -- in a fixed
// field order that does not depend on which route built the value, so two
// PromotionRequestInputs values built by different code paths from the same
// logical answers always encode identically.
func (in PromotionRequestInputs) canonicalBytes() []byte {
	var dst []byte
	appendField := func(label, value string) {
		dst = binary.BigEndian.AppendUint32(dst, uint32(len(label)))
		dst = append(dst, label...)
		dst = binary.BigEndian.AppendUint32(dst, uint32(len(value)))
		dst = append(dst, value...)
	}
	appendField("worker_id", in.WorkerID)
	appendField("proposed_job_title", in.ProposedJobTitle)
	appendField("proposed_grade", in.ProposedGrade)
	appendField("proposed_compensation", in.ProposedCompensation)
	appendField("effective_date", in.EffectiveDate)
	appendField("business_reason", in.BusinessReason)
	return dst
}

// promotionIntentType is the governed intent type both routes produce, per
// planning/specs/business-intent-and-change-request.md's naming (a
// versioned intent definition, e.g. "PromoteWorker", whose kernel family is
// ChangeRequest).
const promotionIntentType = "PromoteWorker"

// IntentInstance is the minimal governed-identity proof object both routes
// produce: which intent type, from which typed inputs, and the resulting
// canonical digest. It mirrors the CanonicalDigestReference concept
// planning/specs/business-intent-and-change-request.md names for a real
// BusinessIntent's ProposalDigest, scoped down to what this lane can prove
// without importing internal/intent/app (another lane's package): two
// IntentInstance values built from equal inputs are always byte-for-byte
// equal, and any difference in any typed input changes the digest.
type IntentInstance struct {
	IntentType string
	Inputs     PromotionRequestInputs
	Digest     string
}

// newIntentInstance validates in and computes its canonical digest. Both
// routes call this and only this, so neither can compute a digest under
// different rules than the other.
func newIntentInstance(in PromotionRequestInputs) (IntentInstance, error) {
	if err := in.Validate(); err != nil {
		return IntentInstance{}, err
	}
	sum := sha256.Sum256(in.canonicalBytes())
	return IntentInstance{
		IntentType: promotionIntentType,
		Inputs:     in,
		Digest:     hex.EncodeToString(sum[:]),
	}, nil
}
