package agenttemplate

import "github.com/monstercameron/human-capital-management-suite/internal/agentskills"

// newAssistantStarter is the general-purpose, document-grounded starter used
// by Chat and tenant-owned drafts. It intentionally pins the same two T0
// skills as Policy Helper without changing Policy Helper's reviewed bytes.
func newAssistantStarter() PersonaStarter {
	starter := starterBase(
		"hcmnext.persona_template.assistant",
		"assistant",
		"Assistant",
		"Answers everyday questions and writes announcements from the documents you give it.",
		"AGENTUX-049.assistant",
	)
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
