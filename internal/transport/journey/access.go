package journey

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// errRoleAccessUnconfigured marks a requestAuth snapshot outcome where the
// composition carries no role-access store. It is internal: every consumer
// maps it onto the refusal its callers already pin.
var errRoleAccessUnconfigured = errors.New("journey: role-access store is not configured")

// requestAuth answers every authorization gate of one RPC from a single
// role-snapshot load.
//
// Before it existed, each gate loaded the durable snapshot for itself, so one
// ListJourneys paid for the same bytes three times: the page gate, the
// diagnostics disclosure, and the role cache's own miss. The snapshot cannot
// change mid-RPC in a way the RPC should observe twice, so the first gate
// loads it and every later gate - page, feature, administrator, diagnostics,
// role set - reads the same bytes. A load failure surfaces at the first gate
// that needs it, mapped exactly as that gate mapped its own load before.
type requestAuth struct {
	server    *server
	principal *trust.Principal
	requestID string

	snapshot roleaccess.Snapshot
	loadErr  error
	loaded   bool
}

// authorize starts one RPC's authorization from its admitted principal and
// request id.
func (s *server) authorize(principal *trust.Principal, requestID string) *requestAuth {
	return &requestAuth{server: s, principal: principal, requestID: requestID}
}

// loadSnapshot loads the durable role-access snapshot once; every later gate
// of the RPC reuses the outcome, failure included.
func (a *requestAuth) loadSnapshot(ctx context.Context) (roleaccess.Snapshot, error) {
	if !a.loaded {
		a.loaded = true
		store := a.server.deps.RoleAccess
		if store == nil {
			a.loadErr = errRoleAccessUnconfigured
		} else {
			a.snapshot, a.loadErr = store.Load(ctx, a.principal.Tenant(), a.principal.OrganizationScopeID())
		}
	}
	return a.snapshot, a.loadErr
}

// roles resolves the principal's server-side role set from the shared
// snapshot: the durable assignment when the store carries one for the
// subject, else the admitted credential roles. It is [roleaccess.AssignedRoles]
// over the same bytes the gates read, so the set can never disagree with
// the grants evaluated beside it; on a load failure the admitted roles are
// returned, exactly as the retired resolver answered an outage.
func (a *requestAuth) roles(ctx context.Context) []string {
	admitted := a.principal.Roles()
	snapshot, err := a.loadSnapshot(ctx)
	if err != nil {
		return admitted
	}
	return roleaccess.AssignedRoles(snapshot, a.principal.Subject(), admitted)
}

// hasRole reports whether the principal's server-side role set holds role.
func (a *requestAuth) hasRole(ctx context.Context, role string) bool {
	return roleaccess.ContainsRole(a.roles(ctx), role)
}

// requireFeatureAction enforces a feature grant beneath the page boundary.
// Missing permission data denies: an unconfigured store, an empty page
// permission table, a page grant without its feature rows, or a pair the
// product registry does not declare all refuse.
func (a *requestAuth) requireFeatureAction(ctx context.Context, pageID, featureID, action string) error {
	if a.server.deps.RoleAccess == nil {
		return featureAccessDenied(a.principal, a.requestID)
	}
	if !a.server.featureRegistry()(pageID, featureID) {
		return featureAccessDenied(a.principal, a.requestID)
	}
	snapshot, err := a.loadSnapshot(ctx)
	if err != nil {
		return roleAccessError(err, a.principal, a.requestID, "authorize_page_action")
	}
	roles := roleaccess.AssignedRoles(snapshot, a.principal.Subject(), a.principal.Roles())
	permissions := roleaccess.EffectivePagePermissions(snapshot, roles)
	features := roleaccess.EffectiveFeaturePermissions(snapshot, roles)
	if roleaccess.CanFeatureAction(permissions, features, pageID, featureID, action) {
		return nil
	}
	return featureAccessDenied(a.principal, a.requestID)
}

// requirePageAction enforces the same durable page/action grant advertised by
// the product shell. Page IDs and actions are supplied by the handler, never
// by wire input. Missing permission data denies rather than falling back to
// credential roles.
func (a *requestAuth) requirePageAction(ctx context.Context, pageID, action string) error {
	featureID := "actions"
	if action == roleaccess.ActionView {
		featureID = "content"
	}
	return a.requireFeatureAction(ctx, pageID, featureID, action)
}

// requireAnyFeatureView supports records legitimately reachable from more
// than one product surface without weakening either surface's page boundary.
func (a *requestAuth) requireAnyFeatureView(ctx context.Context, requests ...featureAccessRequest) error {
	var first error
	for _, request := range requests {
		err := a.requireFeatureAction(ctx, request.pageID, request.featureID, request.resolveAction())
		if err == nil {
			return nil
		}
		if first == nil {
			first = err
		}
	}
	return first
}

// requireServedCall enforces the served-call table's grants for method.
func (a *requestAuth) requireServedCall(ctx context.Context, method string) error {
	gates, ok := gatesForServedCall(method)
	if !ok {
		return featureAccessDenied(a.principal, a.requestID)
	}
	requests := make([]featureAccessRequest, 0, len(gates))
	for _, gate := range gates {
		requests = append(requests, featureAccessRequest(gate))
	}
	return a.requireAnyFeatureView(ctx, requests...)
}

// roleSnapshot loads the durable role-access snapshot every role
// administration decision reads, failing closed when the store is missing
// or unreadable.
func (a *requestAuth) roleSnapshot(ctx context.Context) (roleaccess.Snapshot, error) {
	snapshot, err := a.loadSnapshot(ctx)
	if errors.Is(err, errRoleAccessUnconfigured) {
		_, storeErr := a.server.roleAccessStore(a.principal, a.requestID)
		return roleaccess.Snapshot{}, storeErr
	}
	if err != nil {
		return roleaccess.Snapshot{}, roleAccessError(err, a.principal, a.requestID, "load")
	}
	return snapshot, nil
}

// requireRoleAdministrator enforces the stored roles administration grant.
func (a *requestAuth) requireRoleAdministrator(ctx context.Context) error {
	snapshot, err := a.roleSnapshot(ctx)
	if err != nil {
		return err
	}
	if !snapshotAdministers(snapshot, a.roles(ctx)) {
		return roleRequiredError(a.principal, a.requestID)
	}
	return nil
}

// diagnosticsAuthorized reports whether principal may receive PROMOUX-008's
// authorized diagnostics disclosure, read from the shared snapshot. It fails
// closed by default - a nil principal, a nil RoleAccess dependency, a load
// failure, an empty permission table, or the absence of an explicit grant
// all deny - because diagnostics authority never existed before this
// decision point.
func (a *requestAuth) diagnosticsAuthorized(ctx context.Context) bool {
	if a.principal == nil || a.server.deps.RoleAccess == nil {
		return false
	}
	snapshot, err := a.loadSnapshot(ctx)
	if err != nil {
		return false
	}
	return roleaccess.CanDiscloseDiagnostics(snapshot, a.principal.Subject(), roleaccess.AssignedRoles(snapshot, a.principal.Subject(), a.principal.Roles()))
}
