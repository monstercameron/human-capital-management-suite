// Cross-company document grants and egress gating (HUB-015) at the
// application boundary: a thin pass-through to documenthubstore.Store's
// bilateral grant, so a transport handler never touches the store's
// transaction directly.
package application

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
)

// ProposeDocumentCrossCompanyGrant records the host half of a bilateral
// cross-company document grant.
func (s documentService) ProposeDocumentCrossCompanyGrant(ctx context.Context, hostTenant, actorID string, terms documenthubstore.CrossCompanyGrantTerms) (documenthubstore.CrossCompanyGrant, error) {
	return s.store.ProposeCrossCompanyGrant(ctx, hostTenant, actorID, terms, time.Now())
}

// AcceptDocumentCrossCompanyGrant records the named consumer tenant's
// consent to a proposed grant.
func (s documentService) AcceptDocumentCrossCompanyGrant(ctx context.Context, hostTenant, grantID, consumerTenant string) (documenthubstore.CrossCompanyGrant, error) {
	return s.store.AcceptCrossCompanyGrant(ctx, hostTenant, grantID, consumerTenant, time.Now())
}

// AcceptCrossCompanyGrant adapts the authenticated transport caller to the
// store's bilateral-consent operation. The caller's tenant is the consumer;
// accepting for the host's own tenant would collapse the two-party gate.
func (s documentService) AcceptCrossCompanyGrant(ctx context.Context, tenant, actor, hostTenant, grantID string) (transportdocument.CrossCompanyGrant, error) {
	if tenant == "" || tenant != strings.TrimSpace(tenant) || actor == "" || actor != strings.TrimSpace(actor) ||
		hostTenant == "" || hostTenant != strings.TrimSpace(hostTenant) || grantID == "" ||
		grantID != strings.TrimSpace(grantID) || tenant == hostTenant {
		return transportdocument.CrossCompanyGrant{}, documenthubstore.ErrCrossCompanyTerms
	}
	grant, err := s.AcceptDocumentCrossCompanyGrant(ctx, hostTenant, grantID, tenant)
	return transportdocument.CrossCompanyGrant(grant), err
}

// RevokeDocumentCrossCompanyGrant closes one bilateral grant.
func (s documentService) RevokeDocumentCrossCompanyGrant(ctx context.Context, hostTenant, grantID, actorID string) error {
	return s.store.RevokeCrossCompanyGrant(ctx, hostTenant, grantID, actorID, time.Now())
}

// AuthorizeDocumentCrossCompanyRead gates one cross-company read or export:
// both a currently accepted bilateral grant and an explicit document grant
// must admit it.
func (s documentService) AuthorizeDocumentCrossCompanyRead(ctx context.Context, hostTenant, documentID, consumerTenant, subjectID, action string) error {
	return s.store.AuthorizeCrossCompanyRead(ctx, hostTenant, documentID, consumerTenant, subjectID, action, time.Now())
}
