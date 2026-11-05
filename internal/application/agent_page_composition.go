package application

import (
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// AgentPageCompositionInput contains the already composed, tenant-aware read
// ports needed by the Agents page. PersonaBackend and Audience must be the
// isolated persona store and trusted audience resolver respectively; Skills
// must resolve the caller's current skill grants.
type AgentPageCompositionInput struct {
	Runners        agentclient.Runners
	Settings       agentclient.SettingReader
	Controller     agentclient.Controller
	PersonaBackend AvailablePersonaBackend
	Audience       CurrentPersonaAudienceResolver
	Skills         AgentSkillDiscoverer
	// SkillPurposes lists the discovery purposes pinned skills are unioned
	// over. Empty means the chat purpose alone.
	SkillPurposes []string
	// Published, when set, also lists valid but unplaced published
	// personas the installation tuples cannot reach.
	Published AvailablePersonaPublishedBackend
}

// ComposeAgentPageClient builds the production Agents page client. It fails
// closed by returning nil when any authority source is absent. The returned
// client derives the tenant and subject from each request, verifies the
// request trust context, and reads the persona catalog only after trusted
// audience and skill checks succeed.
func ComposeAgentPageClient(in AgentPageCompositionInput) productui.AgentClient {
	if in.Runners == nil || in.PersonaBackend == nil || in.Audience == nil || in.Skills == nil {
		return nil
	}
	personas := &TenantAvailablePersonaReader{Backend: in.PersonaBackend, Audience: in.Audience, Skills: in.Skills, Purposes: in.SkillPurposes, Published: in.Published}
	catalog := &AgentUserCatalog{Personas: personas, Skills: in.Skills, Purposes: in.SkillPurposes}
	return agentclient.NewWithControlsAndCatalog(in.Runners, in.Settings, in.Controller, catalog)
}
