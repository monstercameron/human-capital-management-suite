package journey

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// requirePageAction enforces the same durable page/action grant advertised by
// the product shell. Page IDs and actions are supplied by the handler, never
// by wire input. Missing permission data denies rather than falling back to
// credential roles.
//
// Handlers that run more than one gate build one [requestAuth] and call its
// method instead, so the RPC loads the snapshot once; this wrapper keeps the
// single-gate callers - and the contract tests - on the same implementation.
func (s *server) requirePageAction(ctx context.Context, principal *trust.Principal, inv *transport.Invocation, pageID, action string) error {
	return s.authorize(principal, inv.RequestID()).requirePageAction(ctx, pageID, action)
}

// FeatureRegistry reports whether the product registry declares featureID on
// pageID. The registry is the only authority for which page/feature pairs
// exist: rows for an undeclared pair grant nothing, so a page is judged at
// feature level exactly when its registry entry declares the feature, never
// based on which rows a tenant happens to hold.
//
// It is a port so the gate does not hard-wire where the catalog lives: the
// composition injects it through [Dependencies.Features], and a nil port
// reads the product registry, which is what every deployment serves today.
type FeatureRegistry func(pageID, featureID string) bool

// registryFeatureDeclared is the product-registry FeatureRegistry: the
// default behind a nil [Dependencies.Features].
func registryFeatureDeclared(pageID, featureID string) bool {
	for _, definition := range productui.FeatureDefinitionsForPage(productui.PageID(strings.ToLower(strings.TrimSpace(pageID)))) {
		if strings.EqualFold(string(definition.ID), strings.TrimSpace(featureID)) {
			return true
		}
	}
	return false
}

// requireFeatureAction enforces a feature grant beneath the page boundary.
// Missing permission data denies: an unconfigured store, an empty page
// permission table, a page grant without its feature rows, or a pair the
// product registry does not declare all refuse. There is no page-only
// fallback; bootstrap seeds every tenant's default page and feature rows so
// legitimate grants keep working.
func (s *server) requireFeatureAction(ctx context.Context, principal *trust.Principal, inv *transport.Invocation, pageID, featureID, action string) error {
	return s.authorize(principal, inv.RequestID()).requireFeatureAction(ctx, pageID, featureID, action)
}

// featureAccessDenied refuses a page/feature action with no grant behind it,
// including the fail-closed answer for an unconfigured role-access store.
func featureAccessDenied(principal *trust.Principal, requestID string) error {
	return envelope.New(envelope.CodePermissionDenied, "journey.feature_action.denied", "the assigned role does not permit this action on the page feature").
		WithCorrelation(requestID).WithEvidence(evidence(principal))
}

type featureAccessRequest struct {
	pageID    string
	featureID string
	// action is the gated operation. Empty means the view action; the
	// long-standing read alternatives predate per-action requests and leave
	// it unset.
	action string
}

// resolveAction returns the gated operation for one alternative.
func (request featureAccessRequest) resolveAction() string {
	if strings.TrimSpace(request.action) == "" {
		return roleaccess.ActionView
	}
	return request.action
}

// requireAnyFeatureView supports records legitimately reachable from more
// than one product surface without weakening either surface's page boundary.
func (s *server) requireAnyFeatureView(ctx context.Context, principal *trust.Principal, inv *transport.Invocation, requests ...featureAccessRequest) error {
	return s.authorize(principal, inv.RequestID()).requireAnyFeatureView(ctx, requests...)
}
