package agenttemplate

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
)

func TestTodo_AGENTP_023(t *testing.T) {
	starters := PersonaStarters()
	if len(starters) != 4 {
		t.Fatalf("starter count = %d, want 4", len(starters))
	}
	for _, starter := range starters {
		if starter.Version != 1 || starter.Status != "DRAFT" || !starter.OwnerNeeded || !starter.OwnerRequired {
			t.Fatalf("starter lifecycle = %+v", starter)
		}
		if starter.Published || starter.AutoInstall || len(starter.DefaultGrants) != 0 {
			t.Fatalf("starter has an unsafe default lifecycle or grant: %+v", starter)
		}
		if starter.Provenance.PersonaTodo != "AGENTP-023" || starter.Provenance.DecisionRef != "AGENTP-001" || starter.Provenance.SkillRegistryRef != "AGENT2-004" || starter.Provenance.CopyOnInstall != "TENANT_OWNED_DRAFT" {
			t.Fatalf("starter provenance = %+v", starter.Provenance)
		}
		if starter.EvaluationSuite == "" || starter.Provenance.EvaluationSuite != starter.EvaluationSuite {
			t.Fatalf("starter evaluation provenance = %+v", starter)
		}
		for _, pin := range starter.SkillPins {
			if pin.ID == "" || pin.Version != 1 || len(pin.Digest) != 64 {
				t.Fatalf("un-pinned skill in %s: %+v", starter.ID, pin)
			}
			for _, char := range pin.Digest {
				if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
					t.Fatalf("skill pin digest is not the agentskills registry's bare hex form: %+v", pin)
				}
			}
		}
	}
}

func TestTodo_AGENTP_023_Golden(t *testing.T) {
	cases := map[string]struct {
		id       string
		tier     string
		skills   int
		channels []string
		private  bool
		public   bool
	}{
		"onboarding": {"hcmnext.persona_template.onboarding_coordinator", "T2", 4, []string{"ONBOARDING", "PRIVATE", "ONE_TO_ONE_DM"}, false, false},
		"comp":       {"hcmnext.persona_template.comp_analyst", "T1", 3, []string{"PRIVATE", "MANAGER", "ONE_TO_ONE_DM"}, true, false},
		"policy":     {"hcmnext.persona_template.policy_helper", "T0", 2, []string{"ANY_INTERNAL"}, false, true},
		"schedule":   {"hcmnext.persona_template.schedule_fixer", "T3", 3, []string{"CREW", "SCHEDULER", "ONE_TO_ONE_DM"}, false, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			starter, ok := PersonaStarterFor(tc.id, 1)
			if !ok {
				t.Fatalf("starter %q missing", tc.id)
			}
			if starter.TierCeiling != tc.tier || len(starter.SkillPins) != tc.skills || starter.AlwaysPrivate != tc.private || starter.PublicAnswersExpected != tc.public {
				t.Fatalf("starter shape = %+v", starter)
			}
			if len(starter.AllowedChannelClasses) != len(tc.channels) {
				t.Fatalf("channels = %v, want %v", starter.AllowedChannelClasses, tc.channels)
			}
			for i, channel := range tc.channels {
				if starter.AllowedChannelClasses[i] != channel {
					t.Fatalf("channel %d = %q, want %q", i, starter.AllowedChannelClasses[i], channel)
				}
			}
		})
	}
}

func TestTodo_AGENTP_023_PolicyHelperPinsPrivateReplySkill(t *testing.T) {
	starter, ok := PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	if !ok {
		t.Fatal("Policy Helper starter missing")
	}
	want := agentskills.SkillPin{ID: "persona.chat_reply", Version: 1, Digest: "1f7e3a8ac15c6530bdfecab8a76c3e68208aefbeba89f9bc2426cbb3b2347790"}
	found := false
	for _, pin := range starter.SkillPins {
		if pin.ID == want.ID {
			if pin != want {
				t.Fatalf("Policy Helper private reply pin = %+v, want registry pin %+v", pin, want)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("Policy Helper starter does not pin the private reply skill")
	}
	if starter.Published || starter.AutoInstall || len(starter.DefaultGrants) != 0 {
		t.Fatalf("reply pin issued or implied a grant: %+v", starter)
	}
}

func TestTodo_AGENTP_023_Security(t *testing.T) {
	first := PersonaStarters()
	first[0].SkillPins[0].Digest = "sha256:forged"
	first[0].AllowedChannelClasses[0] = "EXTERNAL"
	second := PersonaStarters()
	if second[0].SkillPins[0].Digest == "sha256:forged" || second[0].AllowedChannelClasses[0] == "EXTERNAL" {
		t.Fatal("starter catalog leaked mutable slices")
	}
	if starter, ok := PersonaStarterFor("hcmnext.persona_template.comp_analyst", 2); ok || starter.ID != "" {
		t.Fatalf("unknown version resolved: %+v, %t", starter, ok)
	}
}

func TestTodo_AGENTP_023_GoldenStarterProvenance(t *testing.T) {
	want := map[string]string{
		"hcmnext.persona_template.onboarding_coordinator": "sha256:92480d1d90a0d56d05776f86e6f3ff44e910d8b14fa5c78ea40e8f98fad35d42",
		"hcmnext.persona_template.comp_analyst":           "sha256:126e2a650b2a1db080937f2e34d7df21dc66ba96a203add723f0f53bd8ca376f",
		"hcmnext.persona_template.policy_helper":          "sha256:91bfb2b82f409cabc414e19de6901f14c5a240cf03ed6548884f483e91e117de",
		"hcmnext.persona_template.schedule_fixer":         "sha256:5cccea56cdad94364670dfb27dd529d0bfd6c0a04b91c94d71f2fc9826ce84e8",
	}
	for _, starter := range PersonaStarters() {
		pin := PersonaStarterPin(starter)
		if pin.ID != starter.ID || pin.Version != starter.Version || pin.TemplateSchemaVersion != 1 || pin.Digest != want[starter.ID] {
			t.Fatalf("starter pin drift: %+v", pin)
		}
		starter.AlwaysPrivate = !starter.AlwaysPrivate
		if PersonaStarterPin(starter).Digest == pin.Digest {
			t.Fatal("starter privacy edit retained provenance digest")
		}
	}
}
