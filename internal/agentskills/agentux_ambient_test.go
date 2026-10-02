package agentskills

import "testing"

func TestAgentUXAmbient_SkillDeclarations_Security(t *testing.T) {
	r := NewRegistry(nil)
	for _, def := range AmbientOfferSkills() {
		if def.SideEffectTier != TierT1 {
			t.Fatal("offer is not T1")
		}
		if err := r.Publish(def); err != nil {
			t.Fatal(err)
		}
		record, ok := r.Lookup(def.Key())
		if !ok || record.Definition.Operations[0].Operation == "add_task" || record.Definition.Operations[0].Operation == "set_reminder" {
			t.Fatal("message could consent to an effect")
		}
	}
}
