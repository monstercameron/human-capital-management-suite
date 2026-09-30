package application

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
)

// PersonaPickerCompositionInput contains the trusted production sources used
// to expose personas in chat's mention picker. Personas must already enforce
// verified-human, audience, installation, publication, and skill eligibility;
// Store supplies the durable canonical Agent.ID namespace and exact lookup.
type PersonaPickerCompositionInput struct {
	Personas AvailablePersonaReader
	Store    *agentpersonastore.Store
	Now      func() time.Time
}

// ComposePersonaPickerComposition builds the chat composition for persona
// references. It has no fallback source: an incomplete production wiring is
// rejected so chat cannot derive persona identities from labels or claims.
func ComposePersonaPickerComposition(in PersonaPickerCompositionInput) (ChatComposition, error) {
	if in.Personas == nil {
		return ChatComposition{}, fmt.Errorf("application: persona picker requires an available-persona reader")
	}
	if in.Store == nil {
		return ChatComposition{}, fmt.Errorf("application: persona picker requires the production persona store")
	}
	identities, err := newProductionPersonaChatIdentityDirectory(in.Store)
	if err != nil {
		return ChatComposition{}, fmt.Errorf("application: compose persona identity directory: %w", err)
	}
	lookup, err := newProductionPersonaReferenceLookup(identities, in.Store)
	if err != nil {
		return ChatComposition{}, fmt.Errorf("application: compose persona reference lookup: %w", err)
	}
	source, err := newPersonaChatReferenceSource(in.Personas, identities, lookup, in.Now)
	if err != nil {
		return ChatComposition{}, fmt.Errorf("application: compose persona reference source: %w", err)
	}
	return ChatComposition{PersonaReferences: source}, nil
}
