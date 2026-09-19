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
	featureID := "actions"
	if action == roleaccess.ActionView {
		featureID = "content"
	}
	return s.requireFeatureAction(ctx, principal, inv, pageID, featureID, action)
}

// requireFeatureAction enforces a feature grant beneath the page boundary.
// Existing deployments with no feature rows retain page-only behavior during
// rolling upgrade; once any feature policy exists, missing grants deny.
func (s *server) requireFeatureAction(ctx context.Context, principal *trust.Principal, inv *transport.Invocation, pageID, featureID, action string) error {
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
	if len(snapshot.FeaturePermissions) == 0 && roleaccess.CanPageAction(permissions, pageID, action) {
		return nil
	}
	features := roleaccess.EffectiveFeaturePermissions(snapshot, roles)
	if roleaccess.CanFeatureAction(permissions, features, pageID, featureID, action) {
		return nil
	}
	return envelope.New(envelope.CodePermissionDenied, "journey.feature_action.denied", "the assigned role does not permit this action on the page feature").
		WithCorrelation(inv.RequestID()).WithEvidence(evidence(principal))
}

type featureAccessRequest struct {
	pageID    string
	featureID string
}

// requireAnyFeatureView supports records legitimately reachable from more
// than one product surface without weakening either surface's page boundary.
func (s *server) requireAnyFeatureView(ctx context.Context, principal *trust.Principal, inv *transport.Invocation, requests ...featureAccessRequest) error {
	var first error
	for _, request := range requests {
		err := s.requireFeatureAction(ctx, principal, inv, request.pageID, request.featureID, roleaccess.ActionView)
		if err == nil {
			return nil
		}
		if first == nil {
			first = err
		}
	}
	return first
}
