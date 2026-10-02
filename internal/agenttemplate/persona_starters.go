package agenttemplate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
)

// PersonaStarterPin seals the complete reviewed starter shape, including its
// exact skill pins and placement defaults. Copying a starter never follows a
// mutable platform version pointer.
func PersonaStarterPin(starter PersonaStarter) Pin {
	encoded, _ := json.Marshal(starter)
	digest := sha256.Sum256(append([]byte("hcm-next-persona-starter/v1\x00"), encoded...))
	return Pin{ID: starter.ID, Version: starter.Version, TemplateSchemaVersion: 1, Digest: "sha256:" + hex.EncodeToString(digest[:])}
}

// PersonaStarter is a versioned platform persona template. It describes the
// reviewed shape an owner may start from; it is not a published persona and
// carries no tenant grants.
type PersonaStarter struct {
	ID                    string
	Version               uint32
	Handle                string
	DisplayName           string
	Purpose               string
	Status                string
	OwnerNeeded           bool
	OwnerRequired         bool
	Published             bool
	AutoInstall           bool
	DefaultGrants         []string
	SkillPins             []agentskills.SkillPin
	TierCeiling           string
	AudienceRoles         []string
	AudiencePopulations   []string
	AllowedChannelClasses []string
	AlwaysPrivate         bool
	PublicAnswersExpected bool
	EvaluationSuite       string
	Provenance            PersonaStarterProvenance
}

// PersonaStarterProvenance identifies the contracts and exact skill versions
// from which a starter was assembled.
type PersonaStarterProvenance struct {
	SourceTodo            string
	PersonaTodo           string
	DecisionRef           string
	SkillRegistryRef      string
	EvaluationSuite       string
	CopyOnInstall         string
	ImmutableAfterPublish bool
}

const (
	personaStarterDraft = "DRAFT"
	personaStarterTodo  = "AGENTP-023"
	personaStarterEval  = "AGENTP-021"
)

// PersonaStarters returns the platform persona starters. Each call
// returns independent slices so callers cannot mutate the platform catalog.
func PersonaStarters() []PersonaStarter {
	return []PersonaStarter{
		newOnboardingCoordinatorStarter(),
		newCompAnalystStarter(),
		newPolicyHelperStarter(),
		newScheduleFixerStarter(),
		newAssistantStarter(),
	}
}

// PlatformPersonaStarters is the explicit platform-catalog name for
// PersonaStarters.
func PlatformPersonaStarters() []PersonaStarter { return PersonaStarters() }

// PersonaStarterFor returns a copy of one exact starter version.
func PersonaStarterFor(id string, version uint32) (PersonaStarter, bool) {
	for _, starter := range PersonaStarters() {
		if starter.ID == id && starter.Version == version {
			return starter, true
		}
	}
	return PersonaStarter{}, false
}

func starterBase(id, handle, displayName, purpose, suite string) PersonaStarter {
	return PersonaStarter{
		ID: id, Version: 1, Handle: handle, DisplayName: displayName,
		Purpose: purpose, Status: personaStarterDraft, OwnerNeeded: true,
		OwnerRequired: true, Published: false, AutoInstall: false,
		DefaultGrants: []string{}, EvaluationSuite: suite,
		Provenance: PersonaStarterProvenance{
			SourceTodo: "AGENT-013", PersonaTodo: personaStarterTodo,
			DecisionRef: "AGENTP-001", SkillRegistryRef: "AGENT2-004",
			EvaluationSuite: suite, CopyOnInstall: "TENANT_OWNED_DRAFT",
			ImmutableAfterPublish: true,
		},
	}
}

func newOnboardingCoordinatorStarter() PersonaStarter {
	starter := starterBase("hcmnext.persona_template.onboarding_coordinator", "onboarding-coordinator", "Onboarding Coordinator", "Keep a new hire's onboarding checklist moving and prepare a welcoming first note.", personaStarterEval+".onboarding_coordinator")
	starter.TierCeiling = "T2"
	starter.SkillPins = []agentskills.SkillPin{
		{ID: "hcmnext.skill.onboarding_checklist_read", Version: 1, Digest: "0d5d5065e5188888c3bbf49bdba4e05668fa22373f3522b3b0e146b74e2595c5"},
		{ID: "hcmnext.skill.onboarding_task_read", Version: 1, Digest: "7dbcc454c8551b17d75c1a4a59219327a926cd0c4a74a2ff39fdc18328751c3d"},
		{ID: "hcmnext.skill.welcome_note_draft", Version: 1, Digest: "2525e335ad572717a240a813dffad84e3ab6837343ba3a6702085a946c9ab47f"},
		{ID: "hcmnext.skill.onboarding_channel_post", Version: 1, Digest: "776f8fae61766c1602c37430663d63f95328096b4cf6d4fcec78befa4cebc4f0"},
	}
	starter.AudienceRoles = []string{"HR", "MANAGER"}
	starter.AudiencePopulations = []string{"NEW_HIRES"}
	starter.AllowedChannelClasses = []string{"ONBOARDING", "PRIVATE", "ONE_TO_ONE_DM"}
	return starter
}

func newCompAnalystStarter() PersonaStarter {
	starter := starterBase("hcmnext.persona_template.comp_analyst", "comp-analyst", "Comp Analyst", "Read authorized compensation bands and compa-ratios, then draft private scenarios.", personaStarterEval+".comp_analyst")
	starter.TierCeiling = "T1"
	starter.SkillPins = []agentskills.SkillPin{
		{ID: "hcmnext.skill.comp_band_read", Version: 1, Digest: "d4b215447f7d09a1055b987a9364e4f0edb66ebd22a74c20aed884f9c3d3b1ac"},
		{ID: "hcmnext.skill.compa_ratio_read", Version: 1, Digest: "9b6380cfee621c6dafdc6356e67bb495e6026c2e5644ad07f6dd06ae195d6f29"},
		{ID: "hcmnext.skill.comp_scenario_draft", Version: 1, Digest: "5aadefb3088e4fa83ffb76d5a95afcce8013235d1311797fb5a73e093106893a"},
	}
	starter.AudienceRoles = []string{"MANAGER", "HR"}
	starter.AudiencePopulations = []string{"AUTHORIZED_COMPENSATION_VIEWERS"}
	starter.AllowedChannelClasses = []string{"PRIVATE", "MANAGER", "ONE_TO_ONE_DM"}
	starter.AlwaysPrivate = true
	return starter
}

func newPolicyHelperStarter() PersonaStarter {
	starter := starterBase("hcmnext.persona_template.policy_helper", "policy-helper", "Policy Helper", "Answer tenant-wide policy questions with cited policy documents.", personaStarterEval+".policy_helper")
	starter.TierCeiling = "T0"
	starter.SkillPins = []agentskills.SkillPin{
		{ID: "hcmnext.skill.knowledge_search_with_citations", Version: 1, Digest: "ca6172826c3f867139cd4e540767cb76683a216e176d5883ea48ce944578fc29"},
		{ID: "persona.chat_reply", Version: 1, Digest: "1f7e3a8ac15c6530bdfecab8a76c3e68208aefbeba89f9bc2426cbb3b2347790"},
	}
	starter.AudienceRoles = []string{"ALL_MEMBERS"}
	starter.AudiencePopulations = []string{"TENANT_WIDE"}
	starter.AllowedChannelClasses = []string{"ANY_INTERNAL"}
	starter.PublicAnswersExpected = true
	return starter
}

func newScheduleFixerStarter() PersonaStarter {
	starter := starterBase("hcmnext.persona_template.schedule_fixer", "schedule-fixer", "Schedule Fixer", "Find schedule conflicts, prepare swap drafts, and submit a governed shift change when approved.", personaStarterEval+".schedule_fixer")
	starter.TierCeiling = "T3"
	starter.SkillPins = []agentskills.SkillPin{
		{ID: "hcmnext.skill.schedule_read", Version: 1, Digest: "66465597f827da7ec78e2e4cc91a34e28fe88a73548efe26dcd734b5488174d5"},
		{ID: "hcmnext.skill.schedule_swap_draft", Version: 1, Digest: "9ff88a8df48da0dc363a9cfe49a6c45189959822c256887904398dc545675c6a"},
		{ID: "hcmnext.skill.shift_change_submit", Version: 1, Digest: "e4d2353f79ae78f2e906658cfa5f685f6cbdeece9037ff8dc639901e8ac2e9d0"},
	}
	starter.AudienceRoles = []string{"CREW_MEMBER", "SCHEDULER"}
	starter.AudiencePopulations = []string{"SCHEDULED_WORKFORCE"}
	starter.AllowedChannelClasses = []string{"CREW", "SCHEDULER", "ONE_TO_ONE_DM"}
	return starter
}

func clonePersonaStarter(starter PersonaStarter) PersonaStarter {
	starter.DefaultGrants = slices.Clone(starter.DefaultGrants)
	starter.SkillPins = slices.Clone(starter.SkillPins)
	starter.AudienceRoles = slices.Clone(starter.AudienceRoles)
	starter.AudiencePopulations = slices.Clone(starter.AudiencePopulations)
	starter.AllowedChannelClasses = slices.Clone(starter.AllowedChannelClasses)
	return starter
}
