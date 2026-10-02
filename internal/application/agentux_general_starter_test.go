package application

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
)

func TestAgentUXGeneral_AssistantInstructions(t *testing.T) {
	starter, ok := agenttemplate.PersonaStarterFor(localAgentDemoAssistantStarterID, 1)
	if !ok {
		t.Fatal("Assistant starter is missing")
	}
	instructions := personaStarterInstructions(starter)
	for _, required := range []string{"plain, short", "Cite every document", "do not answer", "Never invent policy"} {
		if !strings.Contains(instructions, required) {
			t.Fatalf("Assistant instructions %q do not contain %q", instructions, required)
		}
	}
	policy, _ := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	if got := personaStarterInstructions(policy); got != strings.TrimSpace(policy.Purpose)+" Follow only the pinned skills and the tenant's current authorization. Cite the records used, refuse actions outside the starter's tier ceiling, and never expose private information to a broader audience." {
		t.Fatalf("Policy Helper instructions changed: %q", got)
	}
	budget := agentmanifest.Budget{MaxCostMicros: 100, MaxInputTokens: 4000, MaxOutputTokens: 1000, MaxConcurrentRuns: 1}
	if got := agentUXGeneralStarterBudget(policy, budget); got != budget {
		t.Fatalf("Policy Helper budget changed: %+v", got)
	}
	if got := agentUXGeneralStarterBudget(starter, budget); got.MaxCostMicros != uint64(LocalPersonaOpenAIMaxCostMicros) || got.MaxInputTokens != budget.MaxInputTokens || got.MaxOutputTokens != budget.MaxOutputTokens || got.MaxConcurrentRuns != budget.MaxConcurrentRuns {
		t.Fatalf("Assistant budget=%+v", got)
	}
}
