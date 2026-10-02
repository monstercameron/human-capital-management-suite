package agentskills

import (
	"errors"
	"testing"
)

func TestAgentUXDemo_SupportSkills_Security(t *testing.T) {
	for _, def := range SupportEffectSkills() {
		if err := def.Validate(); err != nil {
			t.Fatal(err)
		}
		if def.DailyLimit != 25 || def.Skill.SideEffectTier != TierT3 || len(def.Skill.Operations) != 1 || def.Skill.Operations[0].Capability.ID != def.Skill.ID {
			t.Fatalf("effect manifest: %+v", def)
		}
		bad := def
		bad.Skill.SideEffectTier = TierT1
		if !errors.Is(bad.Validate(), ErrInvalidDefinition) {
			t.Fatal("private draft tier accepted for mutation")
		}
		bad = def
		bad.DailyLimit++
		if !errors.Is(bad.Validate(), ErrInvalidDefinition) {
			t.Fatal("unsealed budget accepted")
		}
		bad = def
		bad.IdempotencyKey = "model-selected"
		if !errors.Is(bad.Validate(), ErrInvalidDefinition) {
			t.Fatal("unsealed effect key accepted")
		}
	}
}
