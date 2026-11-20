package application

import (
	"github.com/monstercameron/human-capital-management-suite/internal/agentcost"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// PersonaAllowanceReader says how much of an agent's daily allowance is left in
// one conversation (#3807), as a whole percent. It is false when the agent has
// no limit or the limits could not be read.
type PersonaAllowanceReader interface {
	AllowanceRemaining(tenant, personaID, conversationID string) (int, bool)
}

// AllowanceRemaining reads the cost gate the worker is held to. Without a gate
// there is no allowance to report.
func (w *personaServeWiring) AllowanceRemaining(tenant, personaID, conversationID string) (int, bool) {
	if w == nil {
		return 0, false
	}
	gate := w.allowance.Load()
	if gate == nil {
		return 0, false
	}
	return gate.Meter.Gate.Remaining(agentcost.Subject{TenantID: tenant, AgentID: personaID, ConversationID: conversationID})
}

// BindAllowance gives the surface the gate the persona runs are held to, once
// the agent services are composed.
func (w *personaServeWiring) BindAllowance(gate *PersonaRunCostGate) {
	if w != nil && gate != nil && gate.Meter.Gate != nil {
		w.allowance.Store(gate)
	}
}

// projectPersonaCost fills the invoker's own cost lines on their row: the model
// calls the run sealed and what is left of the agent's allowance. A run whose
// sealed lines do not rebuild cleanly shows no figure rather than a wrong one.
func (s *PersonaChatSurface) projectPersonaCost(out *personachat.Invocation, run runstate.Run, tenant, personaID, conversationID string) {
	if lines, err := agentcost.Agentcost001LinesFromRunRecords([]runstate.Run{run}); err == nil {
		out.UsedUnits = int64(len(lines))
	}
	if s.Allowance == nil || personaID == "" {
		return
	}
	if pct, known := s.Allowance.AllowanceRemaining(tenant, personaID, conversationID); known {
		pct = max(0, min(100, pct))
		out.AllowanceRemainingPct = &pct
	}
}
