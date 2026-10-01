package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

var errAgentDiscoveryContext = errors.New("application: agent discovery context unavailable")

// AgentRoleSource reads the current effective roles for a verified user.
// Implementations must resolve durable directory state for every call.
type AgentRoleSource interface {
	ResolveRoles(context.Context, *trust.Principal) ([]string, error)
}

// AgentPopulationSource reads the current population assigned to a verified
// user. An empty population is an unresolved authority and is rejected.
type AgentPopulationSource interface {
	ResolvePopulation(context.Context, *trust.Principal) (string, error)
}

// AgentOrganizationSource reads the current organization scopes for a
// verified user. Empty scopes are valid and mean no organization grant.
type AgentOrganizationSource interface {
	ResolveOrganizationScopes(context.Context, *trust.Principal, []string) ([]string, error)
}

// AgentSubjectSource resolves exact, current record subjects for a purpose.
// It must apply tenant, relationship and organization policy in its owner.
type AgentSubjectSource interface {
	ResolveSubjects(context.Context, *trust.Principal, string, []string, []string) ([]agentgate.Subject, error)
}

// AgentFieldSource resolves the field mask allowed for the user and purpose.
// The source must derive the mask from server policy, never request fields.
type AgentFieldSource interface {
	ResolveFields(context.Context, *trust.Principal, string, []string, []string, []agentgate.Subject) ([]authz.FieldID, error)
}

// ServerAgentDiscoveryContext composes current directory and policy sources
// for AGENT2-005 discovery. It carries no authority itself: every returned
// fact is read from a source using the verified principal from ctx.
type ServerAgentDiscoveryContext struct {
	Roles         AgentRoleSource
	Population    AgentPopulationSource
	Organizations AgentOrganizationSource
	Subjects      AgentSubjectSource
	Fields        AgentFieldSource
}

var _ AgentDiscoveryContext = (*ServerAgentDiscoveryContext)(nil)

// Resolve returns current user authority, exact subjects and field scope. The
// principal argument is accepted for the existing catalog seam, but authority
// comes only from the principal attached to ctx and must identify the same
// tenant and subject.
func (c *ServerAgentDiscoveryContext) Resolve(ctx context.Context, principal *trust.Principal, purpose string) (agentgate.UserContext, []agentgate.Subject, []authz.FieldID, error) {
	if c == nil || c.Roles == nil || c.Population == nil || c.Organizations == nil || c.Subjects == nil || c.Fields == nil || principal == nil || strings.TrimSpace(purpose) == "" {
		return agentgate.UserContext{}, nil, nil, errAgentDiscoveryContext
	}
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.Subject() != principal.Subject() || verified.Tenant() != principal.Tenant() || verified.SubjectKind() != trust.SubjectKindHuman {
		return agentgate.UserContext{}, nil, nil, errAgentDiscoveryContext
	}
	roles, err := c.Roles.ResolveRoles(ctx, verified)
	if err != nil {
		return agentgate.UserContext{}, nil, nil, fmt.Errorf("resolve current roles: %w", err)
	}
	population, err := c.Population.ResolvePopulation(ctx, verified)
	if err != nil {
		return agentgate.UserContext{}, nil, nil, fmt.Errorf("resolve current population: %w", err)
	}
	population = strings.TrimSpace(population)
	if population == "" {
		return agentgate.UserContext{}, nil, nil, fmt.Errorf("%w: current population is missing", errAgentDiscoveryContext)
	}
	orgs, err := c.Organizations.ResolveOrganizationScopes(ctx, verified, roles)
	if err != nil {
		return agentgate.UserContext{}, nil, nil, fmt.Errorf("resolve current organization scopes: %w", err)
	}
	subjects, err := c.Subjects.ResolveSubjects(ctx, verified, purpose, roles, orgs)
	if err != nil {
		return agentgate.UserContext{}, nil, nil, fmt.Errorf("resolve current subjects: %w", err)
	}
	fields, err := c.Fields.ResolveFields(ctx, verified, purpose, roles, orgs, subjects)
	if err != nil {
		return agentgate.UserContext{}, nil, nil, fmt.Errorf("resolve current field scope: %w", err)
	}
	user := agentgate.UserContext{Principal: verified, Population: population, Roles: slices.Clone(roles), OrganizationScopes: slices.Clone(orgs)}
	return user, slices.Clone(subjects), slices.Clone(fields), nil
}
