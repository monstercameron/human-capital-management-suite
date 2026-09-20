package authz

import (
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// effectiveRolesOf returns the role set an evaluation governs by: the
// server-resolved durable assignment when the caller supplies one, else the
// principal's own credential roles.
//
// A non-nil override always wins, even when it is empty: an empty durable
// assignment authorizes nothing, and falling back to the credential in that
// case would hand back exactly the revoked authority the resolution removed.
// A nil override keeps the legacy behavior for callers with no role store;
// those callers must pass their resolved set to be covered by RBAC-RT-002.
func effectiveRolesOf(principal *trust.Principal, override []string) []string {
	if override != nil {
		return override
	}
	return principal.Roles()
}
