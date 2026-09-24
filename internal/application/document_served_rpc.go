// Application-boundary implementations of the transportdocument.Service
// methods that reach HUB-013/014 (team/channel placement), HUB-015
// (cross-company grants), HUB-030/031 (typed and agent search) and the
// HUB-040 ownership-transfer method whose transport-facing name collides
// with the existing store-typed pass-through in document_ownership.go.
// Each method here is the smallest correct conversion between the
// transport-neutral wire shapes (internal/transport/document) and the
// existing documenthubstore-typed application helpers already proven by
// TestTodo_HUB_015/040 and the HUB-030/031 search services; no
// authorization or business logic is duplicated here.
package application

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// documentPlacementScope is the only scope kind PlaceDocument and
// GetDocumentPlacement operate on: an official team/channel Docs-tab
// placement (HUB-014). The default deployment pointer is never reachable
// through these RPCs.
const documentPlacementScope = "placement"

func mapDocumentStoreError(err error, notFoundReason, deniedReason string) error {
	if err == nil {
		return nil
	}
	if _, ok := envelope.As(err); ok {
		return err
	}
	switch {
	case errors.Is(err, documenthubstore.ErrDenied), errors.Is(err, documenthubstore.ErrIneligibleAudience):
		return envelope.New(envelope.CodePermissionDenied, deniedReason, "the actor may not perform this document action")
	case errors.Is(err, documenthubstore.ErrPlacementInput), errors.Is(err, documenthubstore.ErrTransferInput),
		errors.Is(err, documenthubstore.ErrTransferSameOwner), errors.Is(err, documenthubstore.ErrCrossCompanyTerms),
		errors.Is(err, documenthubstore.ErrRouteInvalid):
		return envelope.New(envelope.CodeInvalidArgument, notFoundReason, "the request is invalid")
	case errors.Is(err, documenthubstore.ErrNoDeployment), errors.Is(err, documenthubstore.ErrRouteNotFound):
		return envelope.New(envelope.CodeNotFound, notFoundReason, "nothing is deployed in this scope")
	}
	return err
}

func transportDeployment(d documenthubstore.Deployment) transportdocument.Deployment {
	return transportdocument.Deployment{
		ID: d.ID, DocumentID: d.DocumentID, VersionID: d.VersionID, ScopeKind: d.ScopeKind, ScopeID: d.ScopeID,
		DeployerID: d.DeployerID, CustodianID: d.CustodianID, EffectiveAt: d.EffectiveAt, ReviewDueAt: d.ReviewDueAt,
		Official: d.IsOfficialPlacement(),
	}
}

// PlaceDocument implements transportdocument.Service: it deploys a
// reviewed version into a team/channel scope as an official placement
// (HUB-014). The store requires the actor hold MANAGE on top of Deploy's
// own DEPLOY check.
func (s documentService) PlaceDocument(ctx context.Context, tenantID, actorID, documentID, versionID, scopeID, expectedLive, custodianID string, reviewDueAt time.Time) (transportdocument.Deployment, error) {
	d, err := s.store.PlaceDocument(ctx, tenantID, documenthubstore.PlaceInput{
		DocumentID: documentID, VersionID: versionID, ScopeKind: documentPlacementScope, ScopeID: scopeID,
		ActorID: actorID, ExpectedLive: expectedLive, CustodianID: custodianID, ReviewDueAt: reviewDueAt,
	})
	if err != nil {
		return transportdocument.Deployment{}, mapDocumentStoreError(err, "document.invalid_placement", "document.placement_denied")
	}
	return transportDeployment(d), nil
}

// GetDocumentPlacement implements transportdocument.Service: it resolves
// the live official placement for one team/channel scope, rechecking the
// caller's own live eligibility for that audience (HUB-013) before any
// deployment is resolved. A nil audience authority (no chat composition)
// refuses every placement read rather than silently widening access.
func (s documentService) GetDocumentPlacement(ctx context.Context, tenantID, actorID, documentID, scopeID string) (transportdocument.Deployment, error) {
	if s.aud == nil {
		return transportdocument.Deployment{}, envelope.New(envelope.CodeUnavailable, "document.placement_unavailable", "placement audience eligibility is not configured")
	}
	d, err := s.store.ResolvePlacementDeployment(ctx, tenantID, documentID, documentPlacementScope, scopeID, actorID, s.aud)
	if err != nil {
		return transportdocument.Deployment{}, mapDocumentStoreError(err, "document.placement_unavailable", "document.placement_ineligible")
	}
	return transportDeployment(d), nil
}

// TransferDocumentOwnership and ListDocumentOwnershipHistory (HUB-040) are
// implemented in document_ownership.go.

func transportCrossCompanyGrant(g documenthubstore.CrossCompanyGrant) transportdocument.CrossCompanyGrant {
	return transportdocument.CrossCompanyGrant{
		ID: g.ID, DocumentID: g.DocumentID, HostTenant: g.HostTenant, ConsumerTenant: g.ConsumerTenant,
		Classification: g.Classification, Residency: g.Residency, Version: g.Version,
		Proposed: g.Proposed, AcceptedByHost: g.AcceptedByHost, AcceptedByConsumer: g.AcceptedByConsumer,
		ExpiresAt: g.ExpiresAt, RevokedAt: g.RevokedAt,
	}
}

// ProposeCrossCompanyGrant implements transportdocument.Service: the
// caller's own tenant is always the host proposing the grant (HUB-015).
func (s documentService) ProposeCrossCompanyGrant(ctx context.Context, tenantID, actorID, documentID, consumerTenant, classification, residency string, expiresAt time.Time) (transportdocument.CrossCompanyGrant, error) {
	g, err := s.ProposeDocumentCrossCompanyGrant(ctx, tenantID, actorID, documenthubstore.CrossCompanyGrantTerms{
		DocumentID: documentID, HostTenant: tenantID, ConsumerTenant: consumerTenant,
		Classification: classification, Residency: residency, ExpiresAt: expiresAt,
	})
	if err != nil {
		return transportdocument.CrossCompanyGrant{}, mapDocumentStoreError(err, "document.invalid_grant_proposal", "document.grant_propose_denied")
	}
	return transportCrossCompanyGrant(g), nil
}

// AcceptCrossCompanyGrant is implemented in document_crosscompany.go.

// RevokeCrossCompanyGrant implements transportdocument.Service: the
// caller's own tenant is always the host closing its own proposed or
// accepted grant.
func (s documentService) RevokeCrossCompanyGrant(ctx context.Context, tenantID, actorID, grantID string) error {
	if err := s.RevokeDocumentCrossCompanyGrant(ctx, tenantID, grantID, actorID); err != nil {
		return mapDocumentStoreError(err, "document.invalid_grant_revoke", "document.grant_revoke_denied")
	}
	return nil
}

// SearchDocuments and AgentSearchDocuments (HUB-030/031) are implemented in
// document_search.go, which composes the same newDocumentSearchService and
// newDocumentAgentService this file's siblings rely on.
