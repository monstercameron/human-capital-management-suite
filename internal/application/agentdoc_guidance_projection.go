package application

import (
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func applyAgentDocGuidanceProjection(persona *productui.PersonaAdminPersona, profile agentpersona.PersonaProfile) {
	if persona == nil {
		return
	}
	persona.Guidance = profile.Guidance
}

func applyAgentDocStarterInstructions(starter *productui.PersonaAdminStarter, instructions string) {
	if starter == nil {
		return
	}
	starter.Instructions = instructions
}
