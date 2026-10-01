package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
)

// ProjectStoreAgentProposalStore binds the skill's proposal port to the
// project owner's append-only activity stream. It is the production store;
// the in-memory implementation is intended only for isolated unit tests.
type ProjectStoreAgentProposalStore struct {
	Store *projectstore.Store
}

func (s ProjectStoreAgentProposalStore) Save(ctx context.Context, p project.AgentTaskProposal) error {
	if s.Store == nil {
		return ErrAgentProjectProposal
	}
	return s.Store.SaveAgentTaskProposal(ctx, p)
}
func (s ProjectStoreAgentProposalStore) Get(ctx context.Context, tenant, projectID, id string) (project.AgentTaskProposal, error) {
	if s.Store == nil {
		return project.AgentTaskProposal{}, ErrAgentProjectProposal
	}
	return s.Store.GetAgentTaskProposal(ctx, tenant, projectID, id)
}
func (s ProjectStoreAgentProposalStore) Update(ctx context.Context, p project.AgentTaskProposal) error {
	if s.Store == nil {
		return ErrAgentProjectProposal
	}
	return s.Store.UpdateAgentTaskProposal(ctx, p)
}
