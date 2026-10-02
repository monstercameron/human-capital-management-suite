package application

import "github.com/monstercameron/human-capital-management-suite/internal/agentskills"

// personaChatDocumentScope says, from the skills an agent version is pinned to,
// which documents it reads in a conversation, so the conversation can state it
// before anyone asks (AGENTUX-064): the workspace search skill reads workspace
// documents, the conversation search skill reads the documents officially
// placed in the conversation, and an agent with neither reads none.
func personaChatDocumentScope(pins []agentskills.SkillPin) string {
	scope := "NO_DOCUMENTS"
	for _, pin := range pins {
		switch pin.ID {
		case personaWorkspaceSearchSkillID:
			return "WORKSPACE_DOCUMENTS"
		case personaPolicyHelperSkillID:
			scope = "CHANNEL_DOCUMENTS"
		}
	}
	return scope
}
