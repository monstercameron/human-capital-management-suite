package journey

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// diagnosticsAuthorized reports whether principal may receive PROMOUX-008's
// authorized diagnostics disclosure: the raw entity refs, digests, workflow
// instance/node internals and evidence references an ordinary promotion
// review never carries. Unlike requirePageAction's page/action gate, this
// check fails closed by default -- a nil RoleAccess dependency, an empty
// permission table, or the absence of an explicit grant on
// [roleaccess.PageJourneyDiagnostics] all deny -- because diagnostics
// authority never existed before this todo, so there is no prior behavior a
// permissive rolling-upgrade default would need to preserve. The two
// administrative roles are read from the server-side role set, mirroring
// requireRoleAdministrator elsewhere in this package: a revoked
// administrator whose durable assignment no longer holds the role is
// refused, while an administrator with no durable assignment still resolves
// through the admitted credential roles.
func (s *server) diagnosticsAuthorized(ctx context.Context, principal *trust.Principal) bool {
	if principal == nil {
		return false
	}
	roles := s.effectiveRoles(ctx, principal)
	if isAdministratorRoleSet(roles) {
		return true
	}
	if s.deps.RoleAccess == nil {
		return false
	}
	snapshot, err := s.deps.RoleAccess.Load(ctx, principal.Tenant(), principal.OrganizationScopeID())
	if err != nil {
		return false
	}
	permissions := roleaccess.EffectivePagePermissions(snapshot, roleaccess.AssignedRoles(snapshot, principal.Subject(), roles))
	return roleaccess.CanPageAction(permissions, roleaccess.PageJourneyDiagnostics, roleaccess.ActionView)
}
