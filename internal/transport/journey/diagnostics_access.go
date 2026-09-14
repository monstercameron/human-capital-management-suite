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
// administrative roles are checked directly against the admitted
// credential, mirroring requireRoleAdministrator elsewhere in this package,
// so an administrator is authorized even on a tenant whose RoleAccess store
// has not yet been (re)bootstrapped with the new page id.
func (s *server) diagnosticsAuthorized(ctx context.Context, principal *trust.Principal) bool {
	if principal == nil {
		return false
	}
	if principal.HasRole("hcm_admin") || principal.HasRole("comp_admin") {
		return true
	}
	if s.deps.RoleAccess == nil {
		return false
	}
	snapshot, err := s.deps.RoleAccess.Load(ctx, principal.Tenant(), principal.OrganizationScopeID())
	if err != nil {
		return false
	}
	roles := roleaccess.AssignedRoles(snapshot, principal.Subject(), principal.Roles())
	permissions := roleaccess.EffectivePagePermissions(snapshot, roles)
	return roleaccess.CanPageAction(permissions, roleaccess.PageJourneyDiagnostics, roleaccess.ActionView)
}
