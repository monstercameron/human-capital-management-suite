package application

import (
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
)

const (
	personaAdminCodeForbidden             = "forbidden"
	personaAdminCodeInvalid               = "invalid"
	personaAdminCodeConflict              = "conflict"
	personaAdminCodeUnavailable           = "unavailable"
	personaAdminCodeEvaluationUnavailable = "evaluation_unavailable"
	personaAdminCodeDocumentUnreadable    = "document_unreadable"
)

// PersonaAdminCommandError exposes a stable, non-sensitive code to the
// product UI while retaining an internal cause for application diagnostics.
type PersonaAdminCommandError struct {
	code  string
	cause error
}

// Error returns a generic message that never includes backend details.
func (e *PersonaAdminCommandError) Error() string {
	if e == nil {
		return "application: persona administration command unavailable"
	}
	return "application: persona administration command " + e.code
}

// Unwrap retains sentinel matching for application callers.
func (e *PersonaAdminCommandError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// PersonaAdminCommandCode returns the stable product UI outcome code.
func (e *PersonaAdminCommandError) PersonaAdminCommandCode() string {
	if e == nil || e.code == "" {
		return personaAdminCodeUnavailable
	}
	return e.code
}

func personaAdminCommandError(code string, cause error) error {
	return &PersonaAdminCommandError{code: code, cause: cause}
}

func classifyPersonaAdminCommandError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, agentpersonastore.ErrConflict):
		return personaAdminCommandError(personaAdminCodeConflict, err)
	case errors.Is(err, ErrPersonaDocumentUnreadable):
		return personaAdminCommandError(personaAdminCodeDocumentUnreadable, err)
	case errors.Is(err, ErrPersonaAdminEvaluationUnavailable):
		return personaAdminCommandError(personaAdminCodeEvaluationUnavailable, err)
	case errors.Is(err, ErrPersonaDraftInvalid), errors.Is(err, ErrPersonaReviewInvalid), errors.Is(err, agentpersonastore.ErrInvalid):
		return personaAdminCommandError(personaAdminCodeInvalid, err)
	case errors.Is(err, ErrPersonaDraftDenied), errors.Is(err, ErrPersonaReviewDenied):
		return personaAdminCommandError(personaAdminCodeForbidden, err)
	default:
		return personaAdminCommandError(personaAdminCodeUnavailable, err)
	}
}
