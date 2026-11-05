package agentgate

import (
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/agentcapability"
)

// Policy adapts published agent bindings to the compiler side-effecting agent gate (WF-CTRL-005-F1). It is the production caller the gate was missing: the compiler computes gate-on-path from the definition graph and this policy answers with agentcapability.CompileCheck.
//
// Agents are keyed by their registry identity (agentcapability.CapabilityID plus version). Capabilities the policy was not constructed with are not agent-backed, so they clear the gate.
//
// The adapter lives in its own package rather than beside the driver: agentcapability reaches the execute package through capabilityrunner, so an adapter inside execute would close an import cycle.
type Policy struct {
	agents map[policyKey]agentcapability.PublishedAgent
}

type policyKey struct {
	id      string
	version uint32
}

// NewPolicy publishes agents for gate checks. A nil policy skips every check, so compositions without agent-backed capabilities pass nil instead of an empty policy.
func NewPolicy(agents ...agentcapability.PublishedAgent) *Policy {
	p := &Policy{agents: make(map[policyKey]agentcapability.PublishedAgent, len(agents))}
	for _, a := range agents {
		p.agents[policyKey{id: agentcapability.CapabilityID(a.AgentID), version: a.Version}] = a
	}
	return p
}

// CheckAgentGate implements workflow.AgentGatePolicy.
func (p *Policy) CheckAgentGate(id string, version uint32, pathPassesGate bool) error {
	if p == nil {
		return nil
	}
	a, ok := p.agents[policyKey{id: id, version: version}]
	if !ok {
		return nil
	}
	return agentcapability.CompileCheck(a, pathPassesGate)
}

var _ workflow.AgentGatePolicy = (*Policy)(nil)
