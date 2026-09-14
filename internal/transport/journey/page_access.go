package journey

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// requirePageAction enforces the same durable page/action grant advertised by
// the product shell. Page IDs and actions are supplied by the handler, never
// by wire input. A nil store or an empty permission table is the deliberate
// rolling-upgrade compatibility path; once policy exists, failures close the
// mutation rather than falling back to credential roles.
func (s *server) requirePageAction(ctx context.Context, principal *trust.Principal, inv *transport.Invocation, pageID, action string) error {
	if s.deps.RoleAccess == nil {
		return nil
	}
	snapshot, err := s.deps.RoleAccess.Load(ctx, principal.Tenant(), principal.OrganizationScopeID())
	if err != nil {
		return roleAccessError(err, principal, inv.RequestID(), "authorize_page_action")
	}
	if len(snapshot.PagePermissions) == 0 {
		return nil
	}
	roles := roleaccess.AssignedRoles(snapshot, principal.Subject(), principal.Roles())
	permissions := roleaccess.EffectivePagePermissions(snapshot, roles)
	if roleaccess.CanPageAction(permissions, pageID, action) {
		return nil
	}
	return envelope.New(envelope.CodePermissionDenied, "journey.page_action.denied", "the assigned role does not permit this action on the page").
		WithCorrelation(inv.RequestID()).WithEvidence(evidence(principal))
}

// requireAnyPageView admits a read that backs more than one page when the
// caller may view at least one of them, and otherwise refuses exactly as
// [server.requirePageAction] does.
func (s *server) requireAnyPageView(ctx context.Context, principal *trust.Principal, inv *transport.Invocation, pageIDs ...string) error {
	var denied error
	for _, pageID := range pageIDs {
		err := s.requirePageAction(ctx, principal, inv, pageID, roleaccess.ActionView)
		if err == nil {
			return nil
		}
		denied = err
	}
	return denied
}
