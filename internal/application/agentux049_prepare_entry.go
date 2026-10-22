package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
)

// prepareEntry prepares the agent one entry of the starter list describes: a
// seeded one (Policy Helper, Assistant) through its seed, a specified one
// (Birthday Buddy, Task Catcher, Reminder, Support Desk) through its spec. It
// returns the agent's direct conversation, which only the seeded ones have.
// An entry with neither is skipped, and the receipt says why.
func (p localAgentDemoPreparation) prepareEntry(adminCtx context.Context, entry localAgentDemoStarterEntry, policyProfile agentpersona.PersonaProfile) (localAgentDemoPreparedAgent, string, bool, error) {
	switch {
	case entry.seed != nil:
		prepared, direct, err := p.prepare(adminCtx, *entry.seed, policyProfile)
		return prepared, direct, true, err
	case entry.spec != nil:
		spec, err := entry.spec()
		if err != nil {
			return localAgentDemoPreparedAgent{}, "", true, err
		}
		prepared, direct, err := p.prepareSpec(adminCtx, spec, policyProfile)
		return prepared, direct, true, err
	}
	return localAgentDemoPreparedAgent{}, "", false, nil
}
