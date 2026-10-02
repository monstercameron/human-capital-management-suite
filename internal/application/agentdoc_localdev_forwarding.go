package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// The local-dev decorator only changes snapshot metadata. It must expose
// every command extension its inner surface offers: the transport discovers
// these by interface, so a decorator that forwards only the basic command
// silently removes document references and evaluation from the served cell.

// ExecutePersonaAdminCommandWithDocumentReferences forwards a command that
// carries document references to the decorated surface.
func (f personaAdminLocalDevAvailability) ExecutePersonaAdminCommandWithDocumentReferences(ctx context.Context, request productui.PersonaAdminCommandRequest, references []agentdocref.Reference) error {
	if commands, ok := f.inner.(interface {
		ExecutePersonaAdminCommandWithDocumentReferences(context.Context, productui.PersonaAdminCommandRequest, []agentdocref.Reference) error
	}); ok {
		return commands.ExecutePersonaAdminCommandWithDocumentReferences(ctx, request, references)
	}
	return ErrPersonaAdminLifecycleUnavailable
}

// ExecutePersonaAdminCommandWithResult forwards a command that answers with
// an evaluation receipt to the decorated surface.
func (f personaAdminLocalDevAvailability) ExecutePersonaAdminCommandWithResult(ctx context.Context, request productui.PersonaAdminCommandRequest) (productui.PersonaAdminEvaluationResult, error) {
	if commands, ok := f.inner.(interface {
		ExecutePersonaAdminCommandWithResult(context.Context, productui.PersonaAdminCommandRequest) (productui.PersonaAdminEvaluationResult, error)
	}); ok {
		return commands.ExecutePersonaAdminCommandWithResult(ctx, request)
	}
	return productui.PersonaAdminEvaluationResult{}, ErrPersonaAdminLifecycleUnavailable
}
