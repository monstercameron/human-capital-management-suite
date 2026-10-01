package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

var errAgentDiscoveryContextSource = errors.New("application: production agent discovery context unavailable")

// AgentDiscoveryContextSource composes the trusted current-directory
// adapters into the discovery context used by the skill gate. It retains no
// request authority: every dimension is read again using the principal in
// the verified request context.
type AgentDiscoveryContextSource struct {
	roles         *TenantAgentRoleSource
	population    *TenantAgentPopulationSource
	organizations *TenantAgentOrganizationSource
	subjects      *TenantAgentSubjectSource
	fields        *TenantAgentFieldSource
}

var _ AgentDiscoveryContext = (*AgentDiscoveryContextSource)(nil)

// NewAgentDiscoveryContextSource constructs the production discovery source
// over current tenant directory and field-policy readers.
func NewAgentDiscoveryContextSource(
	roles AgentRoleDirectory,
	population AgentPopulationDirectory,
	organizations AgentOrganizationDirectory,
	subjects AgentSubjectDirectory,
	fields AgentFieldPolicy,
) (*AgentDiscoveryContextSource, error) {
	roleSource, err := NewTenantAgentRoleSource(roles)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errAgentDiscoveryContextSource, err)
	}
	populationSource, err := NewTenantAgentPopulationSource(population)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errAgentDiscoveryContextSource, err)
	}
	organizationSource, err := NewTenantAgentOrganizationSource(organizations)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errAgentDiscoveryContextSource, err)
	}
	subjectSource, err := NewTenantAgentSubjectSource(subjects)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errAgentDiscoveryContextSource, err)
	}
	fieldSource, err := NewTenantAgentFieldSource(fields)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errAgentDiscoveryContextSource, err)
	}
	return &AgentDiscoveryContextSource{
		roles: roleSource, population: populationSource, organizations: organizationSource,
		subjects: subjectSource, fields: fieldSource,
	}, nil
}

// Resolve returns current authority and exact disclosure scope for purpose.
// The principal argument is retained for AgentDiscoveryContext compatibility;
// trust.FromContext is the authority source and must match it, including the
// active session.
func (s *AgentDiscoveryContextSource) Resolve(ctx context.Context, principal *trust.Principal, purpose string) (agentgate.UserContext, []agentgate.Subject, []authz.FieldID, error) {
	if s == nil || s.roles == nil || s.population == nil || s.organizations == nil || s.subjects == nil || s.fields == nil || principal == nil || strings.TrimSpace(purpose) == "" {
		return agentgate.UserContext{}, nil, nil, errAgentDiscoveryContextSource
	}
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.SubjectKind() != trust.SubjectKindHuman ||
		verified.Tenant() != principal.Tenant() || verified.Subject() != principal.Subject() ||
		verified.SessionRef() != principal.SessionRef() || !verified.AuthorizesPurpose(purpose) {
		return agentgate.UserContext{}, nil, nil, errAgentDiscoveryContextSource
	}
	roles, err := s.roles.ResolveRoles(ctx, verified)
	if err != nil {
		return agentgate.UserContext{}, nil, nil, fmt.Errorf("%w: roles: %w", errAgentDiscoveryContextSource, err)
	}
	population, err := s.population.ResolvePopulation(ctx, verified)
	if err != nil {
		return agentgate.UserContext{}, nil, nil, fmt.Errorf("%w: population: %w", errAgentDiscoveryContextSource, err)
	}
	organizations, err := s.organizations.ResolveOrganizationScopes(ctx, verified, roles)
	if err != nil {
		return agentgate.UserContext{}, nil, nil, fmt.Errorf("%w: organizations: %w", errAgentDiscoveryContextSource, err)
	}
	exactSubjects, err := s.subjects.ResolveSubjects(ctx, verified, purpose, roles, organizations)
	if err != nil {
		return agentgate.UserContext{}, nil, nil, fmt.Errorf("%w: subjects: %w", errAgentDiscoveryContextSource, err)
	}
	fieldScope, err := s.fields.ResolveFields(ctx, verified, purpose, roles, organizations, exactSubjects)
	if err != nil {
		return agentgate.UserContext{}, nil, nil, fmt.Errorf("%w: fields: %w", errAgentDiscoveryContextSource, err)
	}
	return agentgate.UserContext{Principal: verified, Population: population, Roles: roles, OrganizationScopes: organizations}, exactSubjects, fieldScope, nil
}
