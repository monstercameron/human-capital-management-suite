package application

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
)

func validatePersonaInstructionDocumentTokens(profile agentpersona.PersonaProfile) error {
	if err := agentdocref.ValidateGuidance(profile.Guidance, profile.DocumentReferences); err != nil {
		return fmt.Errorf("%w: %w", ErrPersonaDraftInvalid, err)
	}
	return nil
}

func renderPersonaGuidanceDocumentTokens(profile agentpersona.PersonaProfile, documents []agentdocref.ResolvedDocument) string {
	titles := make([]agentdocref.InstructionDocumentTitle, 0, len(documents))
	for _, document := range documents {
		titles = append(titles, agentdocref.InstructionDocumentTitle{DocumentID: document.Reference.DocumentID, Title: document.Title, Readable: true})
	}
	return agentdocref.RenderInstructionDocumentTokens(profile.Guidance, profile.DocumentReferences, titles, "a document you cannot read")
}
