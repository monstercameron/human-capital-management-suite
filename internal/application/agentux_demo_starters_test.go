package application

import (
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentmodelpolicystore"
)

func TestAgentUXDemo_Starters(t *testing.T) {
	_, fixture, _, _ := agentuxDemoSupportSetup(t)
	catalog := agentskills.NewRegistry(agentuxDemoSupportCapabilities{})
	for _, def := range agentskills.SupportEffectSkills() {
		if err := catalog.Publish(def.Skill); err != nil {
			t.Fatal(err)
		}
	}
	birthday := AgentUXDemoBirthdayStarter()
	support, err := AgentUXDemoSupportStarter(catalog)
	if err != nil {
		t.Fatal(err)
	}
	b, s := agentuxDemoTestTemplate(t, birthday), agentuxDemoTestTemplate(t, support)
	for _, starter := range []agenttemplate.PersonaStarter{b, s} {
		if starter.Published || starter.AutoInstall || !starter.OwnerRequired || starter.Version != 1 || !starter.Provenance.ImmutableAfterPublish {
			t.Fatalf("starter bypassed governance: %+v", starter)
		}
	}
	if b.Handle != "birthday-buddy" || b.DisplayName != "Birthday Buddy" || b.Purpose != "Wishes people a happy birthday in the channel on the day." || b.TierCeiling != "T0" || len(b.SkillPins) != 1 || b.SkillPins[0].ID != personaChatReplySkillID {
		t.Fatalf("birthday starter: %+v", birthday)
	}
	if s.Handle != "support-desk" || s.DisplayName != "Support Desk" || s.Purpose != "Turns customer emails into support tickets and tells the incident channel." || s.TierCeiling != "T3" || len(s.SkillPins) != 2 || s.SkillPins[0] != fixture.grant.TicketPin || s.SkillPins[1] != fixture.grant.AlertPin {
		t.Fatalf("support starter: %+v", support)
	}
	records := AgentUXDemoPolicyRecords()
	if len(records) != 3 {
		t.Fatal("missing immutable contracts")
	}
	for _, record := range records {
		if record.Reference.Version != 1 || record.Reference.SchemaVersion != 1 {
			t.Fatal("unstable contract version")
		}
		if record.Kind == agentmodelpolicystore.EvaluationSuite {
			var suite agenteval.PersonaSuite
			if json.Unmarshal(record.Content, &suite) != nil || agenteval.PersonaSuiteDigest(suite) != record.Reference.Digest {
				t.Fatal("suite bytes differ from pin")
			}
		} else if personaRunBytesDigest(record.Content) != record.Reference.Digest {
			t.Fatal("schema bytes differ from pin")
		}
	}
	records[0].Content[0] = 'x'
	if AgentUXDemoPolicyRecords()[0].Content[0] == 'x' {
		t.Fatal("caller could mutate shipped contract")
	}
	b.SkillPins[0].ID = "tampered"
	if agentuxDemoTestTemplate(t, AgentUXDemoBirthdayStarter()).SkillPins[0].ID != personaChatReplySkillID {
		t.Fatal("starter slices alias")
	}
	if _, err := AgentUXDemoSupportStarter(nil); err == nil {
		t.Fatal("unbound effects pinned")
	}
}

// The overlapping preparation writer is choosing the API wrapper; test the
// stable starter payload without depending on that unfinished decision.
func agentuxDemoTestTemplate(t *testing.T, value any) agenttemplate.PersonaStarter {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var wrapper struct{ Template *agenttemplate.PersonaStarter }
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		t.Fatal(err)
	}
	if wrapper.Template != nil {
		return *wrapper.Template
	}
	var starter agenttemplate.PersonaStarter
	if err := json.Unmarshal(raw, &starter); err != nil {
		t.Fatal(err)
	}
	return starter
}
