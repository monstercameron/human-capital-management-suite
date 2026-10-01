package application

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
)

func TestTodo_AGENTP_023_StarterSkillCatalog(t *testing.T) {
	specs := PersonaStarterSkillCatalog()
	if len(specs) != 10 {
		t.Fatalf("starter skill count = %d, want 10", len(specs))
	}
	want := map[string]struct {
		tier agentskills.SideEffectTier
		data string
		dep  string
		op   string
	}{
		"hcmnext.skill.onboarding_checklist_read": {agentskills.TierRead, "ONBOARDING", "internal/domains/workerlifecycle", "hcmnext.persona.onboarding_checklist"},
		"hcmnext.skill.onboarding_task_read":      {agentskills.TierRead, "ONBOARDING", "internal/domains/workerlifecycle", "hcmnext.persona.onboarding_tasks"},
		"hcmnext.skill.welcome_note_draft":        {agentskills.TierPrivateDraft, "ONBOARDING", "internal/domains/workerlifecycle", "hcmnext.persona.welcome_note_draft"},
		"hcmnext.skill.onboarding_channel_post":   {agentskills.TierCommunicate, "ONBOARDING", "internal/collaboration/chat", "hcmnext.persona.onboarding_channel_post"},
		"hcmnext.skill.comp_band_read":            {agentskills.TierRead, "COMPENSATION", "internal/data/bandfacts", "hcmnext.persona.comp_band_read"},
		"hcmnext.skill.compa_ratio_read":          {agentskills.TierRead, "COMPENSATION", "internal/domains/rewards", "hcmnext.persona.compa_ratio_read"},
		"hcmnext.skill.comp_scenario_draft":       {agentskills.TierPrivateDraft, "COMPENSATION", "internal/domains/rewards", "hcmnext.persona.comp_scenario_draft"},
		"hcmnext.skill.schedule_read":             {agentskills.TierRead, "SCHEDULE", "internal/domains/availability", "hcmnext.persona.schedule_read"},
		"hcmnext.skill.schedule_swap_draft":       {agentskills.TierPrivateDraft, "SCHEDULE", "internal/application", "hcmnext.persona.schedule_swap_draft"},
		"hcmnext.skill.shift_change_submit":       {agentskills.TierSubmitGoverned, "SCHEDULE", "internal/application", "hcmnext.persona.shift_change_submit"},
	}
	for _, spec := range specs {
		got, ok := want[spec.Definition.ID]
		if !ok {
			t.Fatalf("unexpected starter skill %q", spec.Definition.ID)
		}
		if spec.Dependency != got.dep || spec.Definition.SideEffectTier != got.tier || len(spec.Definition.DataClassesRead) != 1 || spec.Definition.DataClassesRead[0] != got.data {
			t.Fatalf("spec %q = %+v", spec.Definition.ID, spec)
		}
		if len(spec.Definition.Operations) != 1 || spec.Definition.Operations[0].Kind != agentskills.OperationCapability || spec.Definition.Operations[0].Capability.ID != got.op {
			t.Fatalf("operation %q = %+v", spec.Definition.ID, spec.Definition.Operations)
		}
	}
}

func TestTodo_AGENTP_023_StarterSkillCatalogUnavailable(t *testing.T) {
	for _, spec := range PersonaStarterSkillCatalog() {
		err := PersonaStarterSkillUnavailableError(spec)
		if spec.Status == PersonaStarterSkillAvailable {
			if err != nil {
				t.Fatalf("%s available error = %v", spec.Definition.ID, err)
			}
			continue
		}
		if !errors.Is(err, ErrPersonaStarterSkillUnavailable) {
			t.Fatalf("%s error = %v, want unavailable sentinel", spec.Definition.ID, err)
		}
		if spec.Dependency == "" {
			t.Fatalf("%s has no dependency in unavailable error", spec.Definition.ID)
		}
	}
}

func TestTodo_AGENTP_023_StarterSkillDefinitionsAreIndependent(t *testing.T) {
	first := PersonaStarterSkillDefinitions()
	first[0].InputSchema[0] = '['
	first[0].Operations[0].Operation = "forged"
	second := PersonaStarterSkillDefinitions()
	if second[0].InputSchema[0] == '[' || second[0].Operations[0].Operation == "forged" {
		t.Fatal("starter skill catalog exposed mutable definition state")
	}
}
