package application

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errPersonaCatalogPermissionDenied = errors.New("persona catalog permission denied")

// PersonaCatalogRoleAuthorizer checks the durable tenant page grant required
// to read the persona administration catalog. It never adds default grants.
type PersonaCatalogRoleAuthorizer struct {
	Roles roleaccess.Store
}

// AuthorizePersonaCatalog permits only an explicitly granted persona-admin
// View action for the principal's currently assigned roles.
func (a PersonaCatalogRoleAuthorizer) AuthorizePersonaCatalog(ctx context.Context, principal *trust.Principal, tenant values.TenantId) error {
	if ctx == nil || a.Roles == nil || principal == nil || strings.TrimSpace(principal.Subject()) == "" ||
		principal.Tenant() != tenant || tenant.Validate() != nil {
		return errPersonaCatalogPermissionDenied
	}
	snapshot, err := a.Roles.Load(ctx, tenant, principal.OrganizationScopeID())
	if err != nil {
		return errPersonaCatalogPermissionDenied
	}
	roles := roleaccess.AssignedRoles(snapshot, principal.Subject(), principal.Roles())
	permissions := roleaccess.EffectivePagePermissions(snapshot, roles)
	if !roleaccess.CanPageAction(permissions, string(productui.PagePersonaAdmin), roleaccess.ActionView) {
		return errPersonaCatalogPermissionDenied
	}
	return nil
}
