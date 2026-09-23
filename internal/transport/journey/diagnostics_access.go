package journey

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// diagnosticsAuthorized reports whether principal may receive PROMOUX-008's
// authorized diagnostics disclosure: the raw entity refs, digests, workflow
// instance/node internals and evidence references an ordinary promotion
// review never carries. It is [roleaccess.CanDiscloseDiagnostics] over the
// principal's server-side role set -- the one rule (RBAC-RT-005) the engine's
// own redaction flag and the inspection gate use too, so the three can never
// disagree again. The check fails closed by default -- a nil principal, a
// nil RoleAccess dependency, a load failure, an empty permission table, or
// the absence of an explicit grant all deny -- because diagnostics authority
// never existed before this decision point, so there is no prior behavior a
// permissive rolling-upgrade default would need to preserve. The roles are
// the server-side set, mirroring requireRoleAdministrator elsewhere in this
// package: a revoked administrator whose durable assignment no longer holds
// the role is refused, while an administrator with no durable assignment
// still resolves through the admitted credential roles.
func (s *server) diagnosticsAuthorized(ctx context.Context, principal *trust.Principal) bool {
	if principal == nil || s.deps.RoleAccess == nil {
		return false
	}
	snapshot, err := s.deps.RoleAccess.Load(ctx, principal.Tenant(), principal.OrganizationScopeID())
	if err != nil {
		return false
	}
	return roleaccess.CanDiscloseDiagnostics(snapshot, principal.Subject(), s.effectiveRoles(ctx, principal))
}
