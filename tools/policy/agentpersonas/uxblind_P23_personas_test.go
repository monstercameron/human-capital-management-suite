package agentpersonas

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const repoRoot = "../../.."

type starterTemplate struct {
	SchemaVersion int `yaml:"schema_version"`
	Template      struct {
		ID         string `yaml:"id"`
		Version    int    `yaml:"version"`
		Semver     string `yaml:"semver"`
		Status     string `yaml:"status"`
		Provenance struct {
			SourceTodo                    string   `yaml:"source_todo"`
			PersonaTodo                   string   `yaml:"persona_todo"`
			DecisionRef                   string   `yaml:"decision_ref"`
			SkillRegistryRef              string   `yaml:"skill_registry_ref"`
			CopyOnInstall                 string   `yaml:"copy_on_install"`
			ImmutableAfterPublish         bool     `yaml:"immutable_after_publish"`
			MaterialChangeRequiresVersion []string `yaml:"material_change_requires_new_version"`
		} `yaml:"provenance"`
	} `yaml:"template"`
	Persona struct {
		ID          string `yaml:"id"`
		Version     int    `yaml:"version"`
		Handle      string `yaml:"handle"`
		DisplayName string `yaml:"display_name"`
		AvatarRef   string `yaml:"avatar_ref"`
		Purpose     string `yaml:"purpose"`
		TierCeiling string `yaml:"tier_ceiling"`
		Skills      []struct {
			ID      string `yaml:"id"`
			Version int    `yaml:"version"`
			Digest  string `yaml:"digest"`
		} `yaml:"skills"`
		Audience struct {
			Roles             []string `yaml:"roles"`
			Populations       []string `yaml:"populations"`
			OrganizationScope string   `yaml:"organization_scope"`
		} `yaml:"audience"`
		AllowedConversationKinds []string            `yaml:"allowed_conversation_kinds"`
		AllowedChannelClasses    []string            `yaml:"allowed_channel_classes"`
		TierChannelClasses       map[string][]string `yaml:"tier_channel_classes"`
		AlwaysPrivate            bool                `yaml:"always_private"`
		PublicAnswersExpected    bool                `yaml:"public_answers_expected"`
		InstructionsDigest       string              `yaml:"instructions_digest"`
		Safety                   struct {
			InvocationMode       string `yaml:"invocation_mode"`
			StandingPrivilege    string `yaml:"standing_privilege"`
			ApprovalActor        string `yaml:"approval_actor"`
			ExternalChannels     bool   `yaml:"external_channels"`
			CrossCompanyChannels bool   `yaml:"cross_company_channels"`
		} `yaml:"safety"`
		Limits struct {
			InvocationsPerHour int `yaml:"invocations_per_invoker_per_hour"`
			ConcurrentTasks    int `yaml:"concurrent_tasks_per_invoker"`
		} `yaml:"limits"`
	} `yaml:"persona"`
	EvaluationSuite struct {
		ID       string `yaml:"id"`
		Version  int    `yaml:"version"`
		Fixtures []struct {
			ID                  string   `yaml:"id"`
			Kind                string   `yaml:"kind"`
			InputText           string   `yaml:"input_text"`
			PeerText            string   `yaml:"peer_text"`
			ExpectedSkill       string   `yaml:"expected_skill"`
			ExpectedTier        string   `yaml:"expected_tier"`
			ExpectedOutcome     string   `yaml:"expected_outcome"`
			ExpectedRefusalCode string   `yaml:"expected_refusal_code"`
			ExpectedPointer     string   `yaml:"expected_pointer"`
			Audience            string   `yaml:"audience"`
			ExpectedVisibility  string   `yaml:"expected_visibility"`
			ExpectedCitations   []string `yaml:"expected_citations"`
		} `yaml:"fixtures"`
	} `yaml:"evaluation_suite"`
}

type skillExpectation struct {
	Tier        string
	DataClasses []string
}

var registrySkills = map[string]skillExpectation{
	"hcmnext.skill.knowledge_search_with_citations": {Tier: "T0", DataClasses: []string{"POLICY_DOCUMENT"}},
	"persona.chat_reply":                            {Tier: "T0"},
	"hcmnext.skill.onboarding_checklist_read":       {Tier: "T0", DataClasses: []string{"ONBOARDING"}},
	"hcmnext.skill.onboarding_task_read":            {Tier: "T0", DataClasses: []string{"ONBOARDING"}},
	"hcmnext.skill.welcome_note_draft":              {Tier: "T1", DataClasses: []string{"ONBOARDING"}},
	"hcmnext.skill.onboarding_channel_post":         {Tier: "T2", DataClasses: []string{"ONBOARDING"}},
	"hcmnext.skill.schedule_read":                   {Tier: "T0", DataClasses: []string{"SCHEDULE"}},
	"hcmnext.skill.schedule_swap_draft":             {Tier: "T1", DataClasses: []string{"SCHEDULE"}},
	"hcmnext.skill.shift_change_submit":             {Tier: "T3", DataClasses: []string{"SCHEDULE"}},
	"hcmnext.skill.comp_band_read":                  {Tier: "T0", DataClasses: []string{"COMPENSATION"}},
	"hcmnext.skill.compa_ratio_read":                {Tier: "T0", DataClasses: []string{"COMPENSATION"}},
	"hcmnext.skill.comp_scenario_draft":             {Tier: "T1", DataClasses: []string{"COMPENSATION"}},
}

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var skillDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func loadTemplates(t *testing.T) map[string]struct {
	Name   string
	Bytes  []byte
	Record starterTemplate
} {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(repoRoot, "definitions", "agents", "personas", "uxblind_P23_*.yaml"))
	if err != nil {
		t.Fatalf("glob persona templates: %v", err)
	}
	sort.Strings(paths)
	if len(paths) != 4 {
		t.Fatalf("starter template count = %d, want 4", len(paths))
	}
	loaded := make(map[string]struct {
		Name   string
		Bytes  []byte
		Record starterTemplate
	}, len(paths))
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var record starterTemplate
		decoder := yaml.NewDecoder(strings.NewReader(string(b)))
		decoder.KnownFields(true)
		if err := decoder.Decode(&record); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		loaded[record.Template.ID] = struct {
			Name   string
			Bytes  []byte
			Record starterTemplate
		}{Name: filepath.Base(path), Bytes: b, Record: record}
	}
	return loaded
}

func canonicalDigest(t *testing.T, b []byte) string {
	t.Helper()
	var value any
	if err := yaml.Unmarshal(b, &value); err != nil {
		t.Fatalf("canonical YAML parse: %v", err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("canonical JSON: %v", err)
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func has(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func tierRank(tier string) int {
	for i, candidate := range []string{"T0", "T1", "T2", "T3", "T4"} {
		if tier == candidate {
			return i
		}
	}
	return -1
}

func TestTodo_AGENTP_023(t *testing.T) {
	templates := loadTemplates(t)
	want := map[string]struct {
		personaID string
		ceiling   string
		roles     []string
	}{
		"hcmnext.persona_template.policy_helper":          {"hcmnext.persona.policy_helper", "T0", []string{"ALL_MEMBERS"}},
		"hcmnext.persona_template.onboarding_coordinator": {"hcmnext.persona.onboarding_coordinator", "T2", []string{"HR", "MANAGER"}},
		"hcmnext.persona_template.schedule_fixer":         {"hcmnext.persona.schedule_fixer", "T3", []string{"CREW_MEMBER", "SCHEDULER"}},
		"hcmnext.persona_template.comp_analyst":           {"hcmnext.persona.comp_analyst", "T1", []string{"MANAGER", "HR"}},
	}
	if len(templates) != len(want) {
		t.Fatalf("loaded template ids = %d, want %d", len(templates), len(want))
	}
	for id, expected := range want {
		loaded, ok := templates[id]
		if !ok {
			t.Fatalf("missing starter template %s", id)
		}
		record := loaded.Record
		if record.SchemaVersion != 1 || record.Template.Version != 1 || record.Template.Semver != "1.0.0" || record.Template.Status != "REVIEWED" {
			t.Errorf("%s is not a reviewed v1 template: %+v", id, record.Template)
		}
		provenance := record.Template.Provenance
		if provenance.SourceTodo != "AGENT-013" || provenance.PersonaTodo != "AGENTP-023" || provenance.DecisionRef != "AGENTP-001" || provenance.SkillRegistryRef != "AGENT2-004" || provenance.CopyOnInstall != "TENANT_OWNED_DRAFT" || !provenance.ImmutableAfterPublish {
			t.Errorf("%s provenance = %+v", id, provenance)
		}
		if record.Persona.ID != expected.personaID || record.Persona.Version != 1 || record.Persona.Handle == "" || record.Persona.DisplayName == "" || record.Persona.Purpose == "" || record.Persona.TierCeiling != expected.ceiling {
			t.Errorf("%s profile identity/ceiling invalid: %+v", id, record.Persona)
		}
		if !equalStrings(record.Persona.Audience.Roles, expected.roles) {
			t.Errorf("%s audience roles = %v, want %v", id, record.Persona.Audience.Roles, expected.roles)
		}
		if len(record.Persona.Skills) == 0 || record.Persona.InstructionsDigest == "" || record.Persona.Safety.InvocationMode != "ON_BEHALF_OF" || record.Persona.Safety.StandingPrivilege != "NONE" || record.Persona.Safety.ApprovalActor != "INVOKER_ONLY" {
			t.Errorf("%s missing governed profile fields", id)
		}
		if !digestPattern.MatchString(record.Persona.InstructionsDigest) || record.Persona.Limits.InvocationsPerHour != 30 || record.Persona.Limits.ConcurrentTasks != 3 {
			t.Errorf("%s instructions or limits are not pinned: %+v", id, record.Persona.Limits)
		}
		for _, skill := range record.Persona.Skills {
			if skill.Version != 1 || !skillDigestPattern.MatchString(skill.Digest) {
				t.Errorf("%s skill pin is not exact: %+v", id, skill)
			}
		}
		if record.EvaluationSuite.ID == "" || record.EvaluationSuite.Version != 1 || len(record.EvaluationSuite.Fixtures) < 5 {
			t.Errorf("%s missing AGENTP-021 suite: %+v", id, record.EvaluationSuite)
		}
		fixtureKinds := map[string]bool{}
		for _, fixture := range record.EvaluationSuite.Fixtures {
			if fixture.ID == "" || fixture.InputText == "" || fixture.Audience == "" || fixture.ExpectedOutcome == "" || fixtureKinds[fixture.Kind] {
				t.Errorf("%s has incomplete or duplicate evaluation fixture: %+v", id, fixture)
			}
			if fixture.Kind == "OUT_OF_SCOPE" && (fixture.ExpectedRefusalCode == "" || fixture.ExpectedPointer == "" || fixture.ExpectedSkill != "") {
				t.Errorf("%s fixture %s lacks an explicit typed refusal and pointer", id, fixture.ID)
			}
			if fixture.Kind == "PEER_INJECTION" && fixture.PeerText == "" {
				t.Errorf("%s fixture %s has no peer injection input", id, fixture.ID)
			}
			if fixture.Kind == "AUDIENCE" && fixture.ExpectedVisibility == "" {
				t.Errorf("%s fixture %s has no visibility assertion", id, fixture.ID)
			}
			if fixture.ExpectedVisibility == "PUBLIC" && len(fixture.ExpectedCitations) == 0 {
				t.Errorf("%s fixture %s expects public output without a citation contract", id, fixture.ID)
			}
			fixtureKinds[fixture.Kind] = true
			if fixture.ExpectedSkill != "" {
				found := false
				for _, skill := range record.Persona.Skills {
					if skill.ID == fixture.ExpectedSkill {
						found = true
					}
				}
				if !found {
					t.Errorf("%s fixture %s names unpinned skill %s", id, fixture.ID, fixture.ExpectedSkill)
				}
				if expected, ok := registrySkills[fixture.ExpectedSkill]; ok && fixture.ExpectedTier != expected.Tier {
					t.Errorf("%s fixture %s tier = %s, want registry tier %s", id, fixture.ID, fixture.ExpectedTier, expected.Tier)
				}
			}
		}
		for _, kind := range []string{"IN_SCOPE", "OUT_OF_SCOPE", "AUTHORITY", "AUDIENCE", "PEER_INJECTION"} {
			if !fixtureKinds[kind] {
				t.Errorf("%s AGENTP-021 suite lacks %s fixture", id, kind)
			}
		}
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestTodo_AGENTP_023_Golden(t *testing.T) {
	templates := loadTemplates(t)
	b, err := os.ReadFile(filepath.Join("testdata", "uxblind_P23_personas.golden.json"))
	if err != nil {
		t.Fatalf("read starter persona golden: %v", err)
	}
	var golden struct {
		Templates []struct {
			ID        string   `json:"id"`
			File      string   `json:"file"`
			Version   int      `json:"version"`
			Digest    string   `json:"canonical_digest"`
			SkillPins []string `json:"skill_pins"`
		} `json:"templates"`
	}
	if err := json.Unmarshal(b, &golden); err != nil {
		t.Fatalf("parse starter persona golden: %v", err)
	}
	if len(golden.Templates) != 4 {
		t.Fatalf("golden template count = %d, want 4", len(golden.Templates))
	}
	for _, expected := range golden.Templates {
		loaded, ok := templates[expected.ID]
		if !ok {
			t.Fatalf("golden references missing template %s", expected.ID)
		}
		if loaded.Name != expected.File || loaded.Record.Template.Version != expected.Version {
			t.Errorf("%s identity changed: file=%s version=%d", expected.ID, loaded.Name, loaded.Record.Template.Version)
		}
		if got := canonicalDigest(t, loaded.Bytes); got != expected.Digest {
			t.Errorf("%s canonical digest = %s, want %s", expected.ID, got, expected.Digest)
		}
		pins := make([]string, 0, len(loaded.Record.Persona.Skills))
		for _, skill := range loaded.Record.Persona.Skills {
			pins = append(pins, fmt.Sprintf("%s/v%d:%s", skill.ID, skill.Version, skill.Digest))
		}
		if !equalStrings(pins, expected.SkillPins) {
			t.Errorf("%s skill pins = %v, want %v", expected.ID, pins, expected.SkillPins)
		}
	}
}

func TestTodo_AGENTP_023_Security(t *testing.T) {
	templates := loadTemplates(t)
	for id, loaded := range templates {
		record := loaded.Record
		if record.Persona.Safety.ExternalChannels || record.Persona.Safety.CrossCompanyChannels {
			t.Errorf("%s permits external or cross-company channels", id)
		}
		ceiling := tierRank(record.Persona.TierCeiling)
		if ceiling < 0 || ceiling > tierRank("T3") {
			t.Errorf("%s has unsafe tier ceiling %q", id, record.Persona.TierCeiling)
		}
		for _, skill := range record.Persona.Skills {
			expected, ok := registrySkills[skill.ID]
			if !ok {
				t.Errorf("%s references unregistered starter skill %q", id, skill.ID)
				continue
			}
			if len(expected.DataClasses) == 0 && skill.ID != "persona.chat_reply" {
				t.Errorf("%s skill %s has no derived data classes", id, skill.ID)
			}
			if skill.ID == "persona.chat_reply" && (expected.Tier != "T0" || len(expected.DataClasses) != 0) {
				t.Errorf("private output-only reply skill acquired data-class authority: %+v", expected)
			}
			if tierRank(expected.Tier) > ceiling {
				t.Errorf("%s skill %s tier %s exceeds ceiling %s", id, skill.ID, expected.Tier, record.Persona.TierCeiling)
			}
		}
		for tier, channels := range record.Persona.TierChannelClasses {
			if tierRank(tier) < 0 || tierRank(tier) > ceiling {
				t.Errorf("%s declares unusable tier channel rule %s", id, tier)
			}
			for _, channel := range channels {
				if strings.Contains(channel, "EXTERNAL") || strings.Contains(channel, "CROSS_COMPANY") || channel == "ALL_COMPANY" {
					t.Errorf("%s tier %s permits unsafe channel %s", id, tier, channel)
				}
			}
		}
		for _, channel := range record.Persona.AllowedChannelClasses {
			if strings.Contains(channel, "EXTERNAL") || strings.Contains(channel, "CROSS_COMPANY") || channel == "ALL_COMPANY" {
				t.Errorf("%s permits unsafe channel %s", id, channel)
			}
		}
		if record.Persona.Safety.InvocationMode != "ON_BEHALF_OF" || record.Persona.Safety.StandingPrivilege != "NONE" || record.Persona.Safety.ApprovalActor != "INVOKER_ONLY" {
			t.Errorf("%s escapes signed-in-user authority", id)
		}
		if id == "hcmnext.persona_template.comp_analyst" {
			if !record.Persona.AlwaysPrivate || record.Persona.PublicAnswersExpected || !equalStrings(record.Persona.AllowedChannelClasses, []string{"PRIVATE", "MANAGER", "ONE_TO_ONE_DM"}) {
				t.Errorf("Comp Analyst privacy defaults widened: %+v", record.Persona)
			}
			if has(record.Persona.AllowedChannelClasses, "PUBLIC") || has(record.Persona.AllowedChannelClasses, "ANY_INTERNAL") || has(record.Persona.AllowedChannelClasses, "ALL_COMPANY") {
				t.Fatal("Comp Analyst can reach all-company/public channels")
			}
			if has(record.Persona.TierChannelClasses["T3"], "ONE_TO_ONE_DM") {
				t.Fatal("Comp Analyst unexpectedly enables T3 promotion submission")
			}
		}
		if id == "hcmnext.persona_template.schedule_fixer" && !equalStrings(record.Persona.TierChannelClasses["T3"], []string{"ONE_TO_ONE_DM"}) {
			t.Errorf("Schedule Fixer T3 channels = %v, want only 1:1 DM", record.Persona.TierChannelClasses["T3"])
		}
		if id == "hcmnext.persona_template.policy_helper" && (!record.Persona.PublicAnswersExpected || !has(record.Persona.AllowedChannelClasses, "ANY_INTERNAL")) {
			t.Errorf("Policy Helper lost tenant-wide public-answer default: %+v", record.Persona)
		}
		if id == "hcmnext.persona_template.policy_helper" {
			for _, fixture := range record.EvaluationSuite.Fixtures {
				if fixture.Kind == "AUDIENCE" && (fixture.ExpectedOutcome != "complete" || fixture.ExpectedVisibility != "PUBLIC" || len(fixture.ExpectedCitations) == 0) {
					t.Errorf("Policy Helper public-safe policy case has wrong contract: %+v", fixture)
				}
			}
		}
	}
}
