package application

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

var errAgentUserCatalog = errors.New("application: agent user catalog unavailable")

// AvailablePersonaReader returns only the current published personas installed
// in a conversation that the principal may access. Implementations must apply
// conversation membership and installation lifecycle before returning profiles.
type AvailablePersonaReader interface {
	ListAvailable(context.Context, *trust.Principal) ([]agentpersona.PersonaVersion, error)
}

// AgentDiscoveryContext resolves current directory roles, population and
// organization scopes for one verified principal and persona purpose.
type AgentDiscoveryContext interface {
	Resolve(context.Context, *trust.Principal, string) (agentgate.UserContext, []agentgate.Subject, []authz.FieldID, error)
}

// AgentSkillDiscoverer applies AGENT2-005 to the current caller and purpose.
type AgentSkillDiscoverer interface {
	Discover(context.Context, *trust.Principal, string) ([]agentskills.SkillRecord, error)
}

// agentPersonaCatalog is the minimal catalog contract consumed by the
// Agents page client.
type agentPersonaCatalog interface {
	List(context.Context, *trust.Principal) ([]productui.AgentSummary, error)
}

// AgentUserCatalog implements the Agents page catalog contract. A persona is shown
// only when it is a valid sealed profile and every pinned skill is currently
// discoverable to this principal under that persona's purpose.
type AgentUserCatalog struct {
	Personas AvailablePersonaReader
	Skills   AgentSkillDiscoverer
}

var _ agentPersonaCatalog = (*AgentUserCatalog)(nil)

// List returns the signed-in user's currently installed, skill-authorized
// personas. It fails closed on invalid profiles or discovery errors.
func (c *AgentUserCatalog) List(ctx context.Context, principal *trust.Principal) ([]productui.AgentSummary, error) {
	if c == nil || c.Personas == nil || c.Skills == nil || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman {
		return nil, errAgentUserCatalog
	}
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.Subject() != principal.Subject() || verified.Tenant() != principal.Tenant() {
		return nil, errAgentUserCatalog
	}
	personas, err := c.Personas.ListAvailable(ctx, principal)
	if err != nil {
		return nil, fmt.Errorf("list installed personas: %w", err)
	}
	out := make([]productui.AgentSummary, 0, len(personas))
	seen := make(map[string]struct{}, len(personas))
	for _, persona := range personas {
		profile := persona.Profile
		if persona.Verify() != nil || profile.PersonaID == "" || profile.Version == 0 {
			return nil, errAgentUserCatalog
		}
		if _, ok := seen[profile.PersonaID]; ok {
			return nil, errAgentUserCatalog
		}
		seen[profile.PersonaID] = struct{}{}
		discovered, err := c.Skills.Discover(ctx, principal, profile.Purpose)
		if err != nil {
			return nil, fmt.Errorf("discover skills for persona %s: %w", profile.PersonaID, err)
		}
		available := make(map[agentskills.SkillKey]agentskills.SkillRecord, len(discovered))
		for _, record := range discovered {
			available[record.Definition.Key()] = record
		}
		skillNames := make([]string, 0, len(profile.SkillPins))
		allAvailable := true
		for _, pin := range profile.SkillPins {
			record, ok := available[agentskills.SkillKey{ID: pin.ID, Version: pin.Version}]
			if !ok || record.Digest != pin.Digest {
				allAvailable = false
				break
			}
			skillNames = append(skillNames, record.Definition.Description)
		}
		if !allAvailable {
			continue
		}
		slices.Sort(skillNames)
		out = append(out, productui.AgentSummary{ID: profile.PersonaID, Name: profile.DisplayName, Description: profile.Purpose, Status: "Ready", Skills: skillNames})
	}
	return out, nil
}

// GateAgentSkillDiscoverer adapts the gate-backed discovery function to the
// catalog seam while keeping directory resolution in the application layer.
type GateAgentSkillDiscoverer struct {
	Gate    *agentgate.Gate
	Context AgentDiscoveryContext
}

// Discover resolves up-to-date authority and returns only AGENT2-005 skills.
func (d GateAgentSkillDiscoverer) Discover(ctx context.Context, principal *trust.Principal, purpose string) ([]agentskills.SkillRecord, error) {
	if d.Gate == nil || d.Context == nil || principal == nil {
		return nil, errAgentUserCatalog
	}
	user, subjects, fields, err := d.Context.Resolve(ctx, principal, purpose)
	if err != nil {
		return nil, fmt.Errorf("resolve current agent discovery context: %w", err)
	}
	if user.Principal == nil || user.Principal.Subject() != principal.Subject() || user.Principal.Tenant() != principal.Tenant() {
		return nil, errAgentUserCatalog
	}
	return d.Gate.Discover(ctx, agentgate.DiscoveryRequest{User: user, Purpose: purpose, Subjects: subjects, Fields: fields})
}
