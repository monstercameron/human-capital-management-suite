package agentdocref

import (
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"
)

const MaxGuidanceCharacters = 8000

var (
	ErrGuidanceTooLong     = errors.New("agent guidance exceeds 8000 characters")
	ErrGuidanceInvalidUTF8 = errors.New("agent guidance is not valid UTF-8")
	ErrGuidanceControl     = errors.New("agent guidance contains a control character other than newline or tab")
)

// GuidanceValidationError is safe to return to an administrator. It describes
// the invalid instruction text without disclosing any document metadata that
// was not already supplied by that administrator.
type GuidanceValidationError struct {
	Problem    error
	DocumentID string
}

func (e *GuidanceValidationError) Error() string {
	if e == nil {
		return "These instructions are invalid."
	}
	if errors.Is(e.Problem, ErrUnknownInstructionDocumentToken) {
		return fmt.Sprintf("These instructions mention a document that is not in the list below: %s", e.DocumentID)
	}
	return "These instructions are invalid: " + e.Problem.Error()
}

func (e *GuidanceValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Problem
}

// ValidateGuidance validates administrator-authored, authority-free guidance.
// References that are not mentioned are allowed; every parsed token must have
// a matching sealed reference.
func ValidateGuidance(text string, refs []Reference) error {
	if !utf8.ValidString(text) {
		return &GuidanceValidationError{Problem: ErrGuidanceInvalidUTF8}
	}
	if utf8.RuneCountInString(text) > MaxGuidanceCharacters {
		return &GuidanceValidationError{Problem: ErrGuidanceTooLong}
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return &GuidanceValidationError{Problem: ErrGuidanceControl}
		}
	}
	known := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		known[ref.DocumentID] = struct{}{}
	}
	for _, documentID := range InstructionDocumentTokens(text) {
		if _, ok := known[documentID]; !ok {
			return &GuidanceValidationError{Problem: ErrUnknownInstructionDocumentToken, DocumentID: documentID}
		}
	}
	return nil
}
