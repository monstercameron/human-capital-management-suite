package app

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// agentRunOriginPrefix marks an intent the agent action service created
// (internal/application.AgentActionService): its origin event names the agent
// run, and the intent is made and simulated there, not proposed through the
// journey form.
const agentRunOriginPrefix = "agent-run:"

// recordAgentProposalCandidates gives an agent-created promotion what a proposed
// one has from the start: the durable candidates, and the budget and seat holds
// that are made with them. The agent path prepares and simulates the proposal
// without reserving anything, so when the foreground human submits it for
// execution the effect boundary would find no budget reservation and refuse the
// promotion as unfunded. The record is the one proposing writes (idempotent: a
// repeat finds the same rows), made after the human's approval of the exact
// proposal and before the workflow starts. Any other origin is left alone.
func (e *journeyEngine) recordAgentProposalCandidates(ctx context.Context, principal *trust.Principal, intentID, origin string, simulated simulationResult) error {
	if !strings.HasPrefix(origin, agentRunOriginPrefix) {
		return nil
	}
	inst, _, err := e.svc.loadInstance(ctx, principal.Tenant().String(), intentID)
	if err != nil {
		return journeyError(err)
	}
	return e.recordProposalCandidates(ctx, principal, inst, simulated)
}
