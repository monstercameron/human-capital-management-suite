package application

import (
	"encoding/json"

	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentmodelpolicystore"
)

const AgentUXDemoSupportPlanSchema = "support.ticket_plan.v1"

// These records are appended alongside existing contracts by the local
// policy publisher. Creating bytes does not publish or activate a route.
func AgentUXDemoPolicyRecords() []agentmodelpolicystore.Record {
	var records []agentmodelpolicystore.Record
	for _, suite := range []agenteval.PersonaSuite{agenteval.BirthdayBuddySuite(personaChatReplySkillID), agenteval.SupportDeskSuite(agentskills.SupportCreateTicket, agentskills.SupportAlertChannel)} {
		raw, _ := json.Marshal(suite)
		records = append(records, agentmodelpolicystore.Record{Kind: agentmodelpolicystore.EvaluationSuite, Reference: agentmanifest.Reference{ID: suite.ID, Version: 1, SchemaVersion: 1, Digest: agenteval.PersonaSuiteDigest(suite)}, Content: raw})
	}
	raw := agentskills.SupportEffectSkills()[0].Skill.InputSchema
	return append(records, agentmodelpolicystore.Record{Kind: agentmodelpolicystore.OutputSchema, Reference: agentmanifest.Reference{ID: AgentUXDemoSupportPlanSchema, Version: 1, SchemaVersion: 1, Digest: personaRunBytesDigest(raw)}, Content: append([]byte(nil), raw...)})
}
