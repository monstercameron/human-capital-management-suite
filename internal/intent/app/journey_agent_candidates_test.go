package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestIssue93_AgentRunHoldsFailClosedWithoutDurableStore proves an agent-run
// promotion cannot reach execution when its budget and seat holds have nowhere
// to be recorded. Without the durable database the candidate writer used to
// return nil, so the hold was skipped and execution continued.
func TestIssue93_AgentRunHoldsFailClosedWithoutDurableStore(t *testing.T) {
	ctx := trust.WithPrincipal(context.Background(), submitPrincipal(t))
	principal, _ := trust.FromContext(ctx)
	origin := agentRunOriginPrefix + "{\"run_id\":\"run-93\"}"

	cases := map[string]*journeyEngine{
		"no durable database and no service":     {},
		"durable database absent with a service": {svc: &IntentService{}},
	}
	for name, engine := range cases {
		err := engine.recordAgentProposalCandidates(ctx, principal, "intent-93", origin, simulationResult{})
		if !errors.Is(err, ErrAgentProposalCandidatesUnavailable) {
			t.Fatalf("%s: agent-run holds = %v, want ErrAgentProposalCandidatesUnavailable", name, err)
		}
	}
}

// TestIssue93_NonAgentOriginIsUnchangedWithoutDurableStore pins that the
// refusal is scoped to agent-run origins: a journey-form proposal with no
// durable store keeps the existing behaviour and is not refused here.
func TestIssue93_NonAgentOriginIsUnchangedWithoutDurableStore(t *testing.T) {
	ctx := trust.WithPrincipal(context.Background(), submitPrincipal(t))
	principal, _ := trust.FromContext(ctx)
	engine := &journeyEngine{}
	if err := engine.recordAgentProposalCandidates(ctx, principal, "intent-93", "", simulationResult{}); err != nil {
		t.Fatalf("journey-form origin refused: %v", err)
	}
	if err := engine.recordAgentProposalCandidates(ctx, principal, "intent-93", "journey:form", simulationResult{}); err != nil {
		t.Fatalf("non-agent origin refused: %v", err)
	}
}
