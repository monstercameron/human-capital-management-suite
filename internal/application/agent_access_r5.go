package application

import "github.com/monstercameron/human-capital-management-suite/internal/data/agentcoststore"

// ownedAgentSet answers IsOwner for the agents a person was just listed as the
// owner of, without a query for each. Anything it is not asked about goes to the
// wrapped Owners.
type ownedAgentSet struct {
	AgentCostOwners
	owned []agentcoststore.OwnedAgent
}

func (s ownedAgentSet) IsOwner(tenant, agentID, actor string) bool {
	// The list is the actor's own: it came from OwnedAgents(tenant, actor).
	for _, agent := range s.owned {
		if agent.ID == agentID {
			return true
		}
	}
	return false
}
