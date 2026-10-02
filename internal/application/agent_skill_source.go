package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errAgentSkillSource = errors.New("application: agent skill source unavailable")

// AgentSkillSource wires the immutable skill registry and the current grant
// provider to the AGENT2-005 application seam. Roles, organization scopes,
// subjects and fields are obtained from Current; none can be supplied by a
// caller of Discover.
type AgentSkillSource struct {
	skills  agentgate.SkillCatalog
	grants  agentgate.GrantProvider
	gate    *agentgate.Gate
	current AgentDiscoveryContext
}

// NewAgentSkillSource builds a trusted source over the existing skill and
// grant registries. Current must resolve authority and disclosure scope from
// the verified principal on every request.
func NewAgentSkillSource(skills agentgate.SkillCatalog, grants agentgate.GrantProvider, current AgentDiscoveryContext) (*AgentSkillSource, error) {
	if skills == nil || grants == nil || current == nil {
		return nil, fmt.Errorf("%w: skill registry, grant source and discovery context are required", errAgentSkillSource)
	}
	gate, err := agentgate.New(agentgate.Config{Skills: skills, Grants: grants})
	if err != nil {
		return nil, fmt.Errorf("%w: build policy gate: %v", errAgentSkillSource, err)
	}
	return &AgentSkillSource{skills: skills, grants: grants, gate: gate, current: current}, nil
}

// Discover resolves current authority and returns the exact active skill
// records allowed for the purpose. The verified principal in ctx is the only
// identity used for tenant and role selection.
func (s *AgentSkillSource) Discover(ctx context.Context, principal *trust.Principal, purpose string) ([]agentskills.SkillRecord, error) {
	if s == nil || s.gate == nil || s.current == nil || principal == nil || strings.TrimSpace(purpose) == "" {
		return nil, errAgentSkillSource
	}
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.SubjectKind() != trust.SubjectKindHuman || verified.Subject() != principal.Subject() || verified.Tenant() != principal.Tenant() {
		return nil, errAgentSkillSource
	}
	user, subjects, fields, err := s.current.Resolve(ctx, principal, purpose)
	if err != nil {
		return nil, fmt.Errorf("resolve current agent discovery context: %w", err)
	}
	if user.Principal == nil || user.Principal.Subject() != principal.Subject() || user.Principal.Tenant() != principal.Tenant() {
		return nil, errAgentSkillSource
	}
	if purpose == personaChatReplyPurpose {
		// A mention's subject is the conversation it happens in, which is not
		// known when the agent is only being offered. Offer the skills the
		// user is granted; the chat-scoped projection authorizes each use
		// with its exact conversation, post and documents.
		return s.gate.DiscoverGranted(ctx, agentgate.DiscoveryRequest{User: user, Purpose: purpose})
	}
	return s.gate.Discover(ctx, agentgate.DiscoveryRequest{User: user, Purpose: purpose, Subjects: subjects, Fields: fields})
}

// ResolvePin resolves a persona's immutable skill pin from the same trusted
// registry used for discovery. Registry status and digest are checked again.
func (s *AgentSkillSource) ResolvePin(pin agentskills.SkillPin) (agentskills.SkillRecord, error) {
	if s == nil || s.skills == nil {
		return agentskills.SkillRecord{}, errAgentSkillSource
	}
	return s.skills.ResolvePin(pin)
}

var _ AgentSkillDiscoverer = (*AgentSkillSource)(nil)
var _ agentpersona.SkillResolver = (*AgentSkillSource)(nil)
