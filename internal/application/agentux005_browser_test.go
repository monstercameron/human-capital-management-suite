package application

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_AGENTUX_005_Browser pins the page-facing contract of the prepared
// demo cell at component level. The preparation publishes and places Policy
// Helper (proven by PRIMARY, INTEGRATION and RECOVERY); this test proves the
// same availability reader the served cell composes lists a prepared agent
// by name in the Agents page choices and in the mention lookup of each
// conversation where it is placed. A live model answer is out of reach of
// unit CI; the delivered-answer half of GREEN is covered by the served
// review cell, not asserted here.
func TestTodo_AGENTUX_005_Browser(t *testing.T) {
	starter, ok := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	if !ok || starter.DisplayName != "Policy Helper" || starter.Handle != "policy-helper" {
		t.Fatalf("prepared demo starter=%+v ok=%t", starter, ok)
	}
	f := newAgentReachFixture(t, true)
	ctx, principal := agentp026Context(t, agentReachTenant, "user-a", trust.SubjectKindHuman)

	agents, err := f.catalog.List(ctx, principal)
	if err != nil || len(agents) != 1 || agents[0].Name == "" || agents[0].Status != "Ready" {
		t.Fatalf("Agents page choices=%+v err=%v", agents, err)
	}
	for _, conversation := range []string{"general", "dm"} {
		got := agentReachMentions(t, f, ctx, "user-a", conversation)
		if len(got) != 1 || got[0].Kind != chat.AgentMention || got[0].Display == "" || !got[0].Eligible {
			t.Fatalf("mention menu in %s=%+v", conversation, got)
		}
		if got[0].Display != agents[0].Name {
			t.Fatalf("mention display %q does not match Agents page choice %q", got[0].Display, agents[0].Name)
		}
	}
}
