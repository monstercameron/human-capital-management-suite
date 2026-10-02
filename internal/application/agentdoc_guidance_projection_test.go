package application

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestAgentDocGuidance_ProjectionCarriesBuiltInAndAdministratorTextSeparately(t *testing.T) {
	persona := productui.PersonaAdminPersona{Instructions: "Built-in manifest text"}
	applyAgentDocGuidanceProjection(&persona, agentpersona.PersonaProfile{Instructions: "Built-in manifest text", Guidance: "Workspace guidance"})
	if persona.Instructions != "Built-in manifest text" || persona.Guidance != "Workspace guidance" {
		t.Fatalf("persona projection = %+v", persona)
	}
	starter := productui.PersonaAdminStarter{}
	applyAgentDocStarterInstructions(&starter, "Built-in starter text")
	if starter.Instructions != "Built-in starter text" {
		t.Fatalf("starter projection = %+v", starter)
	}
}
