package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

var errAgentDirectoryFacts = errors.New("application: current agent directory facts unavailable")

// AgentRoleDirectory reads the current role assignment from the tenant's
// directory. Implementations must perform a fresh read for every call.
type AgentRoleDirectory interface {
	CurrentRoles(context.Context, values.TenantId, string) ([]string, error)
}

// AgentPopulationDirectory reads the current named population for a user.
type AgentPopulationDirectory interface {
	CurrentPopulation(context.Context, values.TenantId, string) (string, error)
}

// AgentOrganizationDirectory reads organization scope from current roles and
// directory policy. The roles argument is server-resolved and must not be
// replaced with credential or request values.
type AgentOrganizationDirectory interface {
	CurrentOrganizationScopes(context.Context, values.TenantId, string, []string) ([]string, error)
}

// AgentSubjectDirectory resolves exact subjects and their current policy
// relationships for one purpose. It must never return a population wildcard.
type AgentSubjectDirectory interface {
	CurrentSubjects(context.Context, values.TenantId, string, string, []string, []string) ([]agentgate.Subject, error)
}

// AgentFieldPolicy reads fields allowed by current server policy. The source
// owns the field set; callers cannot use it to request additional fields.
type AgentFieldPolicy interface {
	CurrentFields(context.Context, *trust.Principal, string, []string, []string, []agentgate.Subject) ([]authz.FieldID, error)
}

// TenantAgentRoleSource adapts the current tenant directory to the discovery
// source. It validates the trusted principal before every directory read.
type TenantAgentRoleSource struct{ Directory AgentRoleDirectory }

// NewTenantAgentRoleSource constructs a current role source.
func NewTenantAgentRoleSource(directory AgentRoleDirectory) (*TenantAgentRoleSource, error) {
	if directory == nil {
		return nil, fmt.Errorf("%w: role directory is required", errAgentDirectoryFacts)
	}
	return &TenantAgentRoleSource{Directory: directory}, nil
}

// ResolveRoles reads and validates the current roles for the trusted user.
func (s *TenantAgentRoleSource) ResolveRoles(ctx context.Context, principal *trust.Principal) ([]string, error) {
	verified, err := trustedAgentPrincipal(ctx, principal)
	if err != nil || s == nil || s.Directory == nil {
		return nil, errAgentDirectoryFacts
	}
	roles, err := s.Directory.CurrentRoles(ctx, verified.Tenant(), verified.Subject())
	if err != nil {
		return nil, fmt.Errorf("read current roles: %w", err)
	}
	roles = normalizeFacts(roles)
	if len(roles) == 0 {
		return nil, fmt.Errorf("%w: current roles are unavailable", errAgentDirectoryFacts)
	}
	return roles, nil
}

// TenantAgentPopulationSource adapts the current population directory.
type TenantAgentPopulationSource struct{ Directory AgentPopulationDirectory }

// NewTenantAgentPopulationSource constructs a current population source.
func NewTenantAgentPopulationSource(directory AgentPopulationDirectory) (*TenantAgentPopulationSource, error) {
	if directory == nil {
		return nil, fmt.Errorf("%w: population directory is required", errAgentDirectoryFacts)
	}
	return &TenantAgentPopulationSource{Directory: directory}, nil
}

// ResolvePopulation reads the current named population for the trusted user.
func (s *TenantAgentPopulationSource) ResolvePopulation(ctx context.Context, principal *trust.Principal) (string, error) {
	verified, err := trustedAgentPrincipal(ctx, principal)
	if err != nil || s == nil || s.Directory == nil {
		return "", errAgentDirectoryFacts
	}
	population, err := s.Directory.CurrentPopulation(ctx, verified.Tenant(), verified.Subject())
	if err != nil {
		return "", fmt.Errorf("read current population: %w", err)
	}
	population = strings.TrimSpace(population)
	if population == "" {
		return "", fmt.Errorf("%w: current population is unavailable", errAgentDirectoryFacts)
	}
	return population, nil
}

// TenantAgentOrganizationSource adapts current organization policy.
type TenantAgentOrganizationSource struct{ Directory AgentOrganizationDirectory }

// NewTenantAgentOrganizationSource constructs a current organization source.
func NewTenantAgentOrganizationSource(directory AgentOrganizationDirectory) (*TenantAgentOrganizationSource, error) {
	if directory == nil {
		return nil, fmt.Errorf("%w: organization directory is required", errAgentDirectoryFacts)
	}
	return &TenantAgentOrganizationSource{Directory: directory}, nil
}

// ResolveOrganizationScopes reads organization scope using current roles.
func (s *TenantAgentOrganizationSource) ResolveOrganizationScopes(ctx context.Context, principal *trust.Principal, roles []string) ([]string, error) {
	verified, err := trustedAgentPrincipal(ctx, principal)
	if err != nil || s == nil || s.Directory == nil || roles == nil {
		return nil, errAgentDirectoryFacts
	}
	scopes, err := s.Directory.CurrentOrganizationScopes(ctx, verified.Tenant(), verified.Subject(), slices.Clone(roles))
	if err != nil {
		return nil, fmt.Errorf("read current organization scopes: %w", err)
	}
	return normalizeFacts(scopes), nil
}

// TenantAgentSubjectSource adapts the exact subject resolver owned by the
// directory/authz layer. Every result is checked for tenant identity.
type TenantAgentSubjectSource struct{ Directory AgentSubjectDirectory }

// NewTenantAgentSubjectSource constructs an exact current subject source.
func NewTenantAgentSubjectSource(directory AgentSubjectDirectory) (*TenantAgentSubjectSource, error) {
	if directory == nil {
		return nil, fmt.Errorf("%w: subject directory is required", errAgentDirectoryFacts)
	}
	return &TenantAgentSubjectSource{Directory: directory}, nil
}

// ResolveSubjects reads exact current subjects for purpose and scope.
func (s *TenantAgentSubjectSource) ResolveSubjects(ctx context.Context, principal *trust.Principal, purpose string, roles, organizations []string) ([]agentgate.Subject, error) {
	verified, err := trustedAgentPrincipal(ctx, principal)
	if err != nil || s == nil || s.Directory == nil || strings.TrimSpace(purpose) == "" || roles == nil {
		return nil, errAgentDirectoryFacts
	}
	subjects, err := s.Directory.CurrentSubjects(ctx, verified.Tenant(), verified.Subject(), purpose, slices.Clone(roles), slices.Clone(organizations))
	if err != nil {
		return nil, fmt.Errorf("read current subjects: %w", err)
	}
	for _, subject := range subjects {
		if err := subject.Ref.Validate(); err != nil || subject.Ref.Tenant != verified.Tenant() || subject.Organization.IsZero() || subject.Organization.Tenant != verified.Tenant() {
			return nil, fmt.Errorf("%w: subject is not tenant-scoped", errAgentDirectoryFacts)
		}
	}
	return slices.Clone(subjects), nil
}

// TenantAgentFieldSource adapts current field policy. It intentionally has no
// requested-fields argument: the policy reader chooses the allowed mask.
type TenantAgentFieldSource struct{ Policy AgentFieldPolicy }

// NewTenantAgentFieldSource constructs a current field source.
func NewTenantAgentFieldSource(policy AgentFieldPolicy) (*TenantAgentFieldSource, error) {
	if policy == nil {
		return nil, fmt.Errorf("%w: field policy is required", errAgentDirectoryFacts)
	}
	return &TenantAgentFieldSource{Policy: policy}, nil
}

// ResolveFields reads the current field mask for purpose and exact subjects.
func (s *TenantAgentFieldSource) ResolveFields(ctx context.Context, principal *trust.Principal, purpose string, roles, organizations []string, subjects []agentgate.Subject) ([]authz.FieldID, error) {
	verified, err := trustedAgentPrincipal(ctx, principal)
	if err != nil || s == nil || s.Policy == nil || strings.TrimSpace(purpose) == "" || roles == nil {
		return nil, errAgentDirectoryFacts
	}
	fields, err := s.Policy.CurrentFields(ctx, verified, purpose, slices.Clone(roles), slices.Clone(organizations), slices.Clone(subjects))
	if err != nil {
		return nil, fmt.Errorf("read current field policy: %w", err)
	}
	seen := make(map[authz.FieldID]struct{}, len(fields))
	for _, field := range fields {
		if _, ok := authz.FieldRegistry[field]; !ok {
			return nil, fmt.Errorf("%w: unknown field policy result", errAgentDirectoryFacts)
		}
		if _, ok := seen[field]; ok {
			return nil, fmt.Errorf("%w: duplicate field policy result", errAgentDirectoryFacts)
		}
		seen[field] = struct{}{}
	}
	slices.Sort(fields)
	return fields, nil
}

func trustedAgentPrincipal(ctx context.Context, principal *trust.Principal) (*trust.Principal, error) {
	if principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant().Validate() != nil || strings.TrimSpace(principal.Subject()) == "" {
		return nil, errAgentDirectoryFacts
	}
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.SubjectKind() != trust.SubjectKindHuman || verified.Subject() != principal.Subject() || verified.Tenant() != principal.Tenant() {
		return nil, errAgentDirectoryFacts
	}
	return verified, nil
}

func normalizeFacts(input []string) []string {
	seen := make(map[string]struct{}, len(input))
	out := make([]string, 0, len(input))
	for _, item := range input {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}
