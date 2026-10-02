package application

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
)

func TestAgentUXDemo_StarterManifest_Security(t *testing.T) {
	_, fixture, _, _ := agentuxDemoSupportSetup(t)
	for _, starter := range []agenttemplate.PersonaStarter{AgentUXDemoBirthdayStarter(), agentuxDemoStarterBase("support_desk", "support-desk", "Support Desk", "Turns customer emails into support tickets and tells the incident channel.", "AGENTUX-054.support-desk", "AGENTUX-054")} {
		if starter.Handle == "support-desk" {
			starter.TierCeiling = "T3"
			starter.SkillPins = append(starter.SkillPins, fixture.grant.TicketPin, fixture.grant.AlertPin)
		}
		manifest, err := AgentUXDemoManifest(starter)
		if err != nil || manifest.AutonomyCeiling != "SPONSORED" || len(manifest.ToolCeiling) != len(starter.SkillPins) || len(manifest.EvaluationRefs) != 1 || manifest.EvaluationRefs[0].ID != starter.EvaluationSuite {
			t.Fatalf("manifest pins: %+v %v", manifest, err)
		}
		instructions, ok := AgentUXDemoStarterInstructions(starter)
		if !ok || manifest.InstructionsDigest != personaInstructionDigest(instructions) {
			t.Fatal("model instructions differ from pinned bytes")
		}
		if starter.Handle == "support-desk" && manifest.OutputSchema.ID != AgentUXDemoSupportPlanSchema {
			t.Fatalf("support planner has a chat output schema: %+v", manifest.OutputSchema)
		}
		bad := starter
		bad.TierCeiling = "T1"
		if _, err := AgentUXDemoManifest(bad); !errors.Is(err, ErrPersonaDraftInvalid) {
			t.Fatalf("changed tier accepted: %v", err)
		}
		starter.SkillPins = nil
		if _, err := AgentUXDemoManifest(starter); !errors.Is(err, ErrPersonaDraftInvalid) {
			t.Fatalf("missing effects accepted: %v", err)
		}
	}
	if _, err := AgentUXDemoManifest(agenttemplate.PersonaStarter{}); !errors.Is(err, ErrPersonaDraftInvalid) {
		t.Fatalf("unknown starter accepted: %v", err)
	}
}
