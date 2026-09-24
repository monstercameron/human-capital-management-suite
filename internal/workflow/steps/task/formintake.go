package task

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/forms/continuity"
	"github.com/monstercameron/human-capital-management-suite/internal/forms/drafts"
)

// FormIntake is the served task/form intake boundary for interrupted form
// sessions (FORM-005) and accommodation/alternate-channel completion
// (FORM-006). It binds one encrypted draft store to the governed TASK
// completion path: drafts keep an interrupted fill-out resumable, and an
// established continuity record carries the alternate route as evidence into
// the submission spec [Submit] consumes. Neither record decides anything: a
// draft is never a submission and the alternate route never moves decision
// authority away from the respondent.
type FormIntake struct {
	Drafts drafts.Repository
}

// NewFormIntake builds the intake over store. A nil store refuses every
// draft operation; continuity establishment is stateless and stays usable.
func NewFormIntake(store drafts.Repository) FormIntake { return FormIntake{Drafts: store} }

func (f FormIntake) checkStore() error {
	if f.Drafts == nil {
		return fmt.Errorf("%w: no draft store supplied", ErrInvalidSubmission)
	}
	return nil
}

// SaveDraft persists one encrypted revision of an interrupted form session.
func (f FormIntake) SaveDraft(req drafts.SaveRequest) (drafts.Draft, error) {
	if err := f.checkStore(); err != nil {
		return drafts.Draft{}, err
	}
	return f.Drafts.Save(req)
}

// ResumeDraft reopens an interrupted session's answers for its owner.
func (f FormIntake) ResumeDraft(req drafts.ResumeRequest) (drafts.ResumeResult, error) {
	if err := f.checkStore(); err != nil {
		return drafts.ResumeResult{}, err
	}
	return f.Drafts.Resume(req)
}

// SubmitDraft validates the resumed answers and mints the immutable draft
// submission. Saving and resuming stay effect-free; only Submit produces a
// submission, and it carries zero workflow effects.
func (f FormIntake) SubmitDraft(req drafts.ResumeRequest, validate drafts.ValidateFunc) (drafts.SubmitResult, error) {
	if err := f.checkStore(); err != nil {
		return drafts.SubmitResult{}, err
	}
	return f.Drafts.Submit(req, validate)
}

// EstablishAlternateRoute records an accommodation/alternate-channel route,
// keeping the canonical deadline and the respondent's authority. An assistant
// is recorded as evidence only.
func (f FormIntake) EstablishAlternateRoute(req continuity.Request) (continuity.Record, error) {
	return continuity.Establish(req)
}

// RouteAlternateChannel is the channel-adapter spelling of
// [FormIntake.EstablishAlternateRoute] with the same contract.
func (f FormIntake) RouteAlternateChannel(req continuity.Request) (continuity.Record, error) {
	return continuity.Route(req)
}

// AlternateSubmissionSpec maps a resumed draft and an established continuity
// record into the evidence-bearing [SubmissionSpec] a governed [Submit]
// consumes. Draft answers enter only as their sealed digest, and continuity
// evidence enters only as reference strings: both records stay evidence, not
// workflow input.
//
// The completer must be the respondent on both records. The assistant
// recorded on the continuity record can never satisfy this check, so the
// assisted route cannot gain decision authority. The answers must still match
// the sealed draft digest, and the draft must belong to the compiled node's
// form, so neither substituted content nor a foreign draft can ride an
// alternate route into a submission.
func AlternateSubmissionSpec(base SubmissionSpec, draft drafts.Draft, answers []byte, rec continuity.Record) (SubmissionSpec, error) {
	if rec.Outcome != continuity.OutcomeSafe {
		return SubmissionSpec{}, fmt.Errorf("%w: alternate route is not safe to continue", ErrInvalidSubmission)
	}
	if base.CompletedBy != draft.PrincipalID || base.CompletedBy != rec.RespondentID {
		return SubmissionSpec{}, fmt.Errorf("%w: completer %q does not hold the respondent's decision authority", ErrInvalidSubmission, base.CompletedBy)
	}
	if sum := sha256.Sum256(answers); sum != draft.AnswerDigest {
		return SubmissionSpec{}, fmt.Errorf("%w: answers do not match the sealed draft", ErrBindingMismatch)
	}
	if draft.FormID != base.FormDefinition.Ref {
		return SubmissionSpec{}, fmt.Errorf("%w: draft for form %q cannot back node form %q", ErrBindingMismatch, draft.FormID, base.FormDefinition.Ref)
	}
	accommodation := rec.AccommodationRef
	if accommodation == "" {
		accommodation = rec.TranscriptionRef
	}
	if accommodation == "" {
		return SubmissionSpec{}, fmt.Errorf("%w: alternate route carries no accommodation evidence", ErrInvalidSubmission)
	}
	out := base
	out.CanonicalPayloadDigest = "sha256:" + hex.EncodeToString(draft.AnswerDigest[:])
	out.ValidationEvidenceRef = rec.EvidenceDigest
	out.AccommodationEvidenceRef = accommodation
	return out, nil
}
