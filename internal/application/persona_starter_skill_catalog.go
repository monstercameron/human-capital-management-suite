package application

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
)

// PersonaStarterSkillStatus describes whether the application has a real
// domain adapter for a starter operation. Keeping this fact beside the
// operation prevents an unavailable dependency from being mistaken for a
// successful read or write.
type PersonaStarterSkillStatus string

const (
	PersonaStarterSkillUnavailable PersonaStarterSkillStatus = "UNAVAILABLE"
	PersonaStarterSkillAvailable   PersonaStarterSkillStatus = "AVAILABLE"
)

// PersonaStarterSkillSpec is the application-owned contract for one of the
// skills referenced by a platform starter. Definition is the immutable
// agentskills registry input; Dependency identifies the real owning API that
// must be composed before invocation is possible.
type PersonaStarterSkillSpec struct {
	Definition agentskills.SkillDefinition
	Dependency string
	Status     PersonaStarterSkillStatus
}

const (
	personaOnboardingAPI   = "internal/domains/workerlifecycle"
	personaCompensationAPI = "internal/domains/rewards"
	personaScheduleAPI     = "internal/application"
)

var starterObjectSchema = json.RawMessage(`{"type":"object","additionalProperties":false}`)

// PersonaStarterSkillCatalog returns the reviewed starter skill definitions in
// stable order. The catalog is fail-closed: a skill becomes invocable only
// after a binder registers a real domain handler with the capability registry.
func PersonaStarterSkillCatalog() []PersonaStarterSkillSpec {
	specs := []PersonaStarterSkillSpec{
		starterSkill("hcmnext.skill.onboarding_checklist_read", "Read the authorized onboarding checklist for a new hire.", agentskills.TierRead, "ONBOARDING", "ResolveOnboardingReadiness", personaOnboardingAPI, "onboarding_coordinator", PersonaStarterSkillUnavailable),
		starterSkill("hcmnext.skill.onboarding_task_read", "Read authorized onboarding tasks and their completion state.", agentskills.TierRead, "ONBOARDING", "NativePersonaOnboardingPort.TaskStates", personaOnboardingAPI, "onboarding_coordinator", PersonaStarterSkillUnavailable),
		starterSkill("hcmnext.skill.welcome_note_draft", "Draft a private welcome note from authorized onboarding facts.", agentskills.TierPrivateDraft, "ONBOARDING", "NativePersonaOnboardingPort.DraftWelcome", personaOnboardingAPI, "onboarding_coordinator", PersonaStarterSkillUnavailable),
		starterSkill("hcmnext.skill.onboarding_channel_post", "Prepare a governed post for the new hire's onboarding channel.", agentskills.TierCommunicate, "ONBOARDING", "NativePersonaOnboardingPort.PrepareWelcomePost", "internal/collaboration/chat", "onboarding_coordinator", PersonaStarterSkillUnavailable),
		starterSkill("hcmnext.skill.comp_band_read", "Read an authorized compensation band.", agentskills.TierRead, "COMPENSATION", "Catalog.LookupBand", "internal/data/bandfacts", "comp_analyst", PersonaStarterSkillUnavailable),
		starterSkill("hcmnext.skill.compa_ratio_read", "Read an authorized worker compa-ratio.", agentskills.TierRead, "COMPENSATION", "EvaluatePayBandPosition", personaCompensationAPI, "comp_analyst", PersonaStarterSkillUnavailable),
		starterSkill("hcmnext.skill.comp_scenario_draft", "Draft a private compensation scenario from authorized facts.", agentskills.TierPrivateDraft, "COMPENSATION", "SimulateCompensation", personaCompensationAPI, "comp_analyst", PersonaStarterSkillUnavailable),
		starterSkill("hcmnext.skill.schedule_read", "Read an authorized published work schedule.", agentskills.TierRead, "SCHEDULE", "VersionedWorkSchedule", "internal/domains/availability", "schedule_fixer", PersonaStarterSkillUnavailable),
		starterSkill("hcmnext.skill.schedule_swap_draft", "Draft a private shift swap against the published schedule.", agentskills.TierPrivateDraft, "SCHEDULE", "PersonaScheduleNativeReader.DraftSwap", personaScheduleAPI, "schedule_fixer", PersonaStarterSkillUnavailable),
		starterSkill("hcmnext.skill.shift_change_submit", "Submit an approved shift change through the governed schedule API.", agentskills.TierSubmitGoverned, "SCHEDULE", "ShiftSelfServiceActor.AcceptTrade", personaScheduleAPI, "schedule_fixer", PersonaStarterSkillUnavailable),
	}
	// Keep one immutable definition source: the binder factories below are the
	// exact operation-capability manifests that can actually be published.
	defs := map[string]agentskills.SkillDefinition{
		"hcmnext.skill.onboarding_checklist_read": personaStarterOperationSkill("hcmnext.skill.onboarding_checklist_read", "Read the authorized onboarding checklist.", "hcmnext.persona.onboarding_checklist", agentskills.TierT0, "ONBOARDING"),
		"hcmnext.skill.onboarding_task_read":      personaStarterOperationSkill("hcmnext.skill.onboarding_task_read", "Read authorized onboarding tasks.", "hcmnext.persona.onboarding_tasks", agentskills.TierT0, "ONBOARDING"),
		"hcmnext.skill.welcome_note_draft":        personaStarterOperationSkill("hcmnext.skill.welcome_note_draft", "Draft a private welcome note.", "hcmnext.persona.welcome_note_draft", agentskills.TierT1, "ONBOARDING"),
		"hcmnext.skill.onboarding_channel_post":   personaStarterOperationSkill("hcmnext.skill.onboarding_channel_post", "Prepare an approved onboarding welcome for governed delivery.", "hcmnext.persona.onboarding_channel_post", agentskills.TierT2, "ONBOARDING"),
		"hcmnext.skill.comp_band_read":            personaCompBandSkill(),
		"hcmnext.skill.compa_ratio_read":          personaCompaSkill(),
		"hcmnext.skill.comp_scenario_draft":       personaCompScenarioSkill(),
		"hcmnext.skill.schedule_read":             personaStarterOperationSkill("hcmnext.skill.schedule_read", "Read the authorized work schedule.", "hcmnext.persona.schedule_read", agentskills.TierT0, "SCHEDULE"),
		"hcmnext.skill.schedule_swap_draft":       personaStarterOperationSkill("hcmnext.skill.schedule_swap_draft", "Draft a private shift swap.", "hcmnext.persona.schedule_swap_draft", agentskills.TierT1, "SCHEDULE"),
		"hcmnext.skill.shift_change_submit":       personaStarterOperationSkill("hcmnext.skill.shift_change_submit", "Submit an approved shift change.", "hcmnext.persona.shift_change_submit", agentskills.TierT3, "SCHEDULE"),
	}
	for i := range specs {
		if def, ok := defs[specs[i].Definition.ID]; ok {
			specs[i].Definition = def
		}
	}
	return specs
}

// PersonaStarterSkillDefinitions projects only immutable registry inputs.
func PersonaStarterSkillDefinitions() []agentskills.SkillDefinition {
	specs := PersonaStarterSkillCatalog()
	out := make([]agentskills.SkillDefinition, 0, len(specs))
	for _, spec := range specs {
		out = append(out, cloneStarterSkillDefinition(spec.Definition))
	}
	return out
}

func starterSkill(id, description string, tier agentskills.SideEffectTier, dataClass, operation, dependency, persona string, status PersonaStarterSkillStatus) PersonaStarterSkillSpec {
	return PersonaStarterSkillSpec{
		Definition: agentskills.SkillDefinition{
			ID: id, Version: 1, Owner: "people",
			Description: description, InputSchema: append(json.RawMessage(nil), starterObjectSchema...), OutputSchema: append(json.RawMessage(nil), starterObjectSchema...),
			Operations:     []agentskills.OperationRef{{Kind: agentskills.OperationConnection, ConnectionID: dependency, Operation: operation}},
			SideEffectTier: tier, RequiredPurposes: []string{"persona." + persona}, DataClassesRead: []string{dataClass},
			IdempotencyRule: "governed-operation", CostClass: "LOW", EvalRefs: []string{"AGENTP-021." + persona},
		},
		Dependency: dependency, Status: status,
	}
}

func cloneStarterSkillDefinition(def agentskills.SkillDefinition) agentskills.SkillDefinition {
	def.InputSchema = append(json.RawMessage(nil), def.InputSchema...)
	def.OutputSchema = append(json.RawMessage(nil), def.OutputSchema...)
	def.Operations = slices.Clone(def.Operations)
	def.RequiredPurposes = slices.Clone(def.RequiredPurposes)
	def.DataClassesRead = slices.Clone(def.DataClassesRead)
	def.DataClassesWritten = slices.Clone(def.DataClassesWritten)
	def.EvalRefs = slices.Clone(def.EvalRefs)
	return def
}

// PersonaStarterSkillUnavailableError gives integrations a stable, truthful
// error while retaining the dependency name for diagnostics.
func PersonaStarterSkillUnavailableError(spec PersonaStarterSkillSpec) error {
	if spec.Status == PersonaStarterSkillAvailable {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrPersonaStarterSkillUnavailable, strings.TrimSpace(spec.Dependency))
}
