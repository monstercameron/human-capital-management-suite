package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	// Purposes lists the discovery purposes pinned skills are unioned over.
	// Empty means the chat purpose alone, the behavior before multi-skill
	// agents.
	Purposes []string
}

var _ agentPersonaCatalog = (*AgentUserCatalog)(nil)

// List returns the signed-in user's skill-authorized personas of a kind that
// answers questions: what Ask, the mention list and /ask may offer. It fails
// closed on invalid profiles or discovery errors.
func (c *AgentUserCatalog) List(ctx context.Context, principal *trust.Principal) ([]productui.AgentSummary, error) {
	answering, _, err := c.ListBoth(ctx, principal)
	return answering, err
}

// ListPage returns the signed-in user's skill-authorized personas of every
// kind: the Agents page roster with each agent's skills. Answering is
// decided separately by List.
func (c *AgentUserCatalog) ListPage(ctx context.Context, principal *trust.Principal) ([]productui.AgentSummary, error) {
	_, roster, err := c.ListBoth(ctx, principal)
	return roster, err
}

// ListBoth lists the installed personas once and projects both the answering
// subset and the full page roster, so one snapshot costs one persona read.
func (c *AgentUserCatalog) ListBoth(ctx context.Context, principal *trust.Principal) (answering []productui.AgentSummary, roster []productui.AgentSummary, err error) {
	if c == nil || c.Personas == nil || c.Skills == nil || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman {
		return nil, nil, errAgentUserCatalog
	}
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.Subject() != principal.Subject() || verified.Tenant() != principal.Tenant() {
		return nil, nil, errAgentUserCatalog
	}
	personas, err := c.Personas.ListAvailable(ctx, principal)
	if err != nil {
		return nil, nil, fmt.Errorf("list installed personas: %w", err)
	}
	answering = make([]productui.AgentSummary, 0, len(personas))
	roster = make([]productui.AgentSummary, 0, len(personas))
	seen := make(map[string]struct{}, len(personas))
	for _, persona := range personas {
		profile := persona.Profile
		if persona.Verify() != nil || profile.PersonaID == "" || profile.Version == 0 {
			slog.WarnContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "agent_catalog", "item_type", "persona", "item_id", profile.PersonaID, "reason", "invalid_profile")
			continue
		}
		// AGENTUX-081: "Who should answer?" offers only agents of a kind that
		// answers questions, decided from the agent's declared starter kind.
		// The page roster lists every kind.
		answers := agentUX081PersonaAnswersQuestions(profile)
		if !answers {
			slog.WarnContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "agent_catalog", "item_type", "persona", "item_id", profile.PersonaID, "reason", "kind_not_answering")
		}
		if _, ok := seen[profile.PersonaID]; ok {
			slog.WarnContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "agent_catalog", "item_type", "persona", "item_id", profile.PersonaID, "reason", "duplicate_persona")
			continue
		}
		seen[profile.PersonaID] = struct{}{}
		discovered, err := discoverAgentSkillsForPurposes(ctx, c.Skills, principal, c.Purposes)
		if err != nil {
			slog.WarnContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "agent_catalog", "item_type", "persona", "item_id", profile.PersonaID, "reason", "skill_discovery_unavailable")
			continue
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
		summary := productui.AgentSummary{ID: profile.PersonaID, Name: profile.DisplayName, Description: profile.Purpose, Status: "Ready", Skills: skillNames}
		if answers {
			answering = append(answering, summary)
		}
		roster = append(roster, summary)
	}
	return answering, roster, nil
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
