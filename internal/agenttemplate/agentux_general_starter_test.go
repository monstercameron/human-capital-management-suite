package agenttemplate

import "testing"

func TestAgentUXGeneral_AssistantStarter(t *testing.T) {
	starter, ok := PersonaStarterFor("hcmnext.persona_template.assistant", 1)
	if !ok {
		t.Fatal("Assistant starter is missing")
	}
	policy, ok := PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	if !ok {
		t.Fatal("Policy Helper starter is missing")
	}
	if starter.Handle != "assistant" || starter.DisplayName != "Assistant" || starter.Purpose != "Answers everyday questions and writes announcements from the documents you give it." || starter.EvaluationSuite != "AGENTUX-049.assistant" {
		t.Fatalf("Assistant starter = %+v", starter)
	}
	if len(starter.SkillPins) != 2 || len(policy.SkillPins) != 2 || starter.SkillPins[0] != policy.SkillPins[0] || starter.SkillPins[1] != policy.SkillPins[1] {
		t.Fatalf("Assistant pins = %+v, Policy Helper pins = %+v", starter.SkillPins, policy.SkillPins)
	}
}

func TestAgentUXGeneral_AssistantStarter_Security(t *testing.T) {
	starter, _ := PersonaStarterFor("hcmnext.persona_template.assistant", 1)
	if starter.TierCeiling != "T0" || starter.AlwaysPrivate || !starter.PublicAnswersExpected || starter.Published || starter.AutoInstall || len(starter.DefaultGrants) != 0 {
		t.Fatalf("Assistant starter has unsafe authority defaults: %+v", starter)
	}
}
