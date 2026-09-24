// Ownership transfer after departure (HUB-040) at the application
// boundary: a thin pass-through to documenthubstore.Store, so a transport
// handler never touches the store's transaction directly.
package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
)

// transportOwnershipTransfer converts the store's evidence record to the
// wire shape; field names are identical so every existing
// TestTodo_HUB_040* assertion (e.g. transfer.PriorOwnerID) holds unchanged.
func transportOwnershipTransfer(t documenthubstore.OwnershipTransfer) transportdocument.OwnershipTransfer {
	return transportdocument.OwnershipTransfer{
		ID: t.ID, DocumentID: t.DocumentID, PriorOwnerID: t.PriorOwnerID, SuccessorOwnerID: t.SuccessorOwnerID,
		Reason: t.Reason, TransferredBy: t.TransferredBy, CreatedAt: t.CreatedAt,
	}
}

// TransferDocumentOwnership moves custody of a document to a successor
// custodian; this is also transportdocument.Service's TransferDocumentOwnership
// (HUB-040), so it is the RPC-reachable definition.
func (s documentService) TransferDocumentOwnership(ctx context.Context, tenantID, actorID, documentID, successorOwnerID, reason string) (transportdocument.OwnershipTransfer, error) {
	t, err := s.store.TransferOwnership(ctx, tenantID, documenthubstore.TransferInput{
		DocumentID: documentID, SuccessorOwnerID: successorOwnerID, ActorID: actorID, Reason: reason,
	})
	if err != nil {
		return transportdocument.OwnershipTransfer{}, err
	}
	return transportOwnershipTransfer(t), nil
}

// DocumentOwnershipHistory lists ownership transfers for one document,
// newest first.
func (s documentService) DocumentOwnershipHistory(ctx context.Context, tenantID, documentID string) ([]documenthubstore.OwnershipTransfer, error) {
	return s.store.TransferHistory(ctx, tenantID, documentID)
}

// ListDocumentOwnershipHistory implements transportdocument.Service
// (HUB-040): the actor must currently be able to read the document, so
// history never reveals prior custodians to someone who cannot read the
// document today.
func (s documentService) ListDocumentOwnershipHistory(ctx context.Context, tenantID, actorID, documentID string) ([]transportdocument.OwnershipTransfer, error) {
	if err := s.store.Authorize(ctx, tenantID, documentID, "person", actorID, documenthubstore.ActionRead); err != nil {
		return nil, err
	}
	rows, err := s.DocumentOwnershipHistory(ctx, tenantID, documentID)
	if err != nil {
		return nil, err
	}
	out := make([]transportdocument.OwnershipTransfer, 0, len(rows))
	for _, t := range rows {
		out = append(out, transportOwnershipTransfer(t))
	}
	return out, nil
}

// DocumentCustodian returns the live owner of one document.
func (s documentService) DocumentCustodian(ctx context.Context, tenantID, documentID string) (string, error) {
	return s.store.CurrentCustodian(ctx, tenantID, documentID)
}
