package application

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

// Candidate execution resolves references in the reserved synthetic tenant;
// production document identifiers therefore remain omissions there.
type personaCandidateDocumentModelWorkSource struct {
	base      *PersonaCandidateModelWorkSource
	documents agentdocref.Resolver
}

func (s *personaCandidateDocumentModelWorkSource) BuildPersonaRunModelWork(ctx context.Context, admission agentrun.Record, run runstate.Run) (PersonaRunModelWork, error) {
	work, err := s.base.BuildPersonaRunModelWork(ctx, admission, run)
	if err != nil {
		return PersonaRunModelWork{}, err
	}
	_, profile, _, err := s.base.Definitions.Resolve(ctx)
	if err != nil {
		return PersonaRunModelWork{}, err
	}
	if err := addPersonaCandidateReferenceDocuments(ctx, &work.Request, s.documents, admission, profile, s.base.Route); err != nil {
		return PersonaRunModelWork{}, fmt.Errorf("candidate reference documents: %w", err)
	}
	return work, nil
}
