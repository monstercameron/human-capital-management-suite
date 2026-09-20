package journey

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// effectiveRoles resolves the principal's server-side role set: the durable
// assignment when the RoleAccess store carries one for the subject, else the
// admitted credential roles (the rollout fallback for principals that are
// not assigned workers). A durable assignment wholly replaces the admitted
// claims; the credential never widens it.
//
// Resolution is cached for a short TTL and invalidated on assignment change
// (see SaveWorkerRoleAssignment). When the store is unconfigured or a load
// fails, the admitted roles are returned so an outage or an unwired tenant
// behaves exactly as before this todo; RBAC-RT-006 owns failing those paths
// closed.
func (s *server) effectiveRoles(ctx context.Context, principal *trust.Principal) []string {
	admitted := principal.Roles()
	if s.deps.RoleAccess == nil {
		return admitted
	}
	roles, err := s.roleResolver().Resolve(ctx, s.deps.RoleAccess, principal.Tenant(), principal.OrganizationScopeID(), principal.Subject(), admitted)
	if err != nil {
		return admitted
	}
	return roles
}

// roleResolver returns the server's short-TTL role cache, creating it on
// first use. The resolver lives on the server value that owns it, never in
// a package-level registry.
func (s *server) roleResolver() *roleaccess.Resolver {
	s.roleMu.Lock()
	defer s.roleMu.Unlock()
	if s.roleCache == nil {
		s.roleCache = roleaccess.NewResolver(roleaccess.DefaultResolverTTL, nil)
	}
	return s.roleCache
}

// hasEffectiveRole reports whether the principal's server-side role set
// holds role. It replaces direct credential checks (principal.HasRole) at
// every gate in this package.
func (s *server) hasEffectiveRole(ctx context.Context, principal *trust.Principal, role string) bool {
	return roleaccess.ContainsRole(s.effectiveRoles(ctx, principal), role)
}

// isAdministratorRoleSet reports whether the resolved set holds an HCM
// administration role. It reads the resolved set, never the credential.
func isAdministratorRoleSet(roles []string) bool {
	return roleaccess.HasAdministratorRole(roles)
}
