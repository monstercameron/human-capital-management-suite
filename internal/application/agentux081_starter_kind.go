package application

import (
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
)

// agentUX081SupportDeskStarterID is the Support Desk template. Its starter
// needs a skill catalog to construct, so the kind resolver names it here;
// TestTodo_AGENTUX_081_StarterKindsDeclared keeps this in sync with the
// constructed starter.
const agentUX081SupportDeskStarterID = "hcmnext.persona_template.support_desk"

// agentUX081StarterKind resolves a template ID to the kind its starter
// declares, across the platform, ambient and demo catalogs. Unknown IDs
// resolve to the unset kind, which answers questions.
func agentUX081StarterKind(id string) agenttemplate.PersonaStarterKind {
	if starter, ok := agenttemplate.PersonaStarterFor(id, 1); ok {
		return starter.Kind
	}
	for _, starter := range AgentUXAmbientStarters() {
		if starter.ID == id {
			return starter.Kind
		}
	}
	if birthday := AgentUXDemoBirthdayStarter(); birthday.ID == id {
		return birthday.Kind
	}
	if id == agentUX081SupportDeskStarterID {
		return agenttemplate.PersonaStarterKindInbox
	}
	return ""
}

// agentUX081PersonaAnswersQuestions reports whether Ask, the mention list and
// /ask may offer this persona. The decision comes from the kind its template
// declares: a tenant-owned copy keeps its starter's template pin, so it keeps
// its starter's kind. A persona without a template, or from an unknown
// starter, stays offered.
func agentUX081PersonaAnswersQuestions(profile agentpersona.PersonaProfile) bool {
	if profile.Template == nil {
		return true
	}
	return agentUX081StarterKind(profile.Template.ID).AnswersQuestions()
}
