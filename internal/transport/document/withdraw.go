// WithdrawDocument and RestoreDocument are the recoverable-removal pair for
// DOCS-07: withdrawal clears the document's live default-scope pointer
// (documenthubstore Store.Withdraw) without touching any immutable
// version or record, and restore redeploys the withdrawn version while
// the scope is still empty.
package document

import (
	"context"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// WithdrawService is the optional recoverable-removal port. A composition
// whose service does not provide it answers UNAVAILABLE.
type WithdrawService interface {
	// WithdrawDocument withdraws the document's live default-scope
	// deployment and returns the version that was live, for an Undo.
	WithdrawDocument(ctx context.Context, tenant, actor, documentID, reason string) (versionID string, err error)
	// RestoreDocument redeploys versionID to the default scope, undoing a
	// prior WithdrawDocument.
	RestoreDocument(ctx context.Context, tenant, actor, documentID, versionID string) error
}

func (s *server) WithdrawDocument(ctx context.Context, req *documentv1.WithdrawDocumentRequest) (*documentv1.WithdrawDocumentResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || !validID(req.GetDocumentId()) {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_id", "a document ID is required")
	}
	withdraw, ok := s.service.(WithdrawService)
	if !ok {
		return nil, envelope.New(envelope.CodeUnavailable, "document.service.unavailable", "document withdrawal is not configured")
	}
	versionID, err := withdraw.WithdrawDocument(ctx, tenant, actor, req.GetDocumentId(), req.GetReason())
	if err != nil {
		return nil, owned(err)
	}
	return &documentv1.WithdrawDocumentResponse{VersionId: versionID}, nil
}

func (s *server) RestoreDocument(ctx context.Context, req *documentv1.RestoreDocumentRequest) (*documentv1.RestoreDocumentResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || !validCommentRef(req.GetDocumentId(), req.GetVersionId()) {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_version_ref", "a document and version ID are required")
	}
	withdraw, ok := s.service.(WithdrawService)
	if !ok {
		return nil, envelope.New(envelope.CodeUnavailable, "document.service.unavailable", "document restore is not configured")
	}
	if err := withdraw.RestoreDocument(ctx, tenant, actor, req.GetDocumentId(), req.GetVersionId()); err != nil {
		return nil, owned(err)
	}
	return &documentv1.RestoreDocumentResponse{}, nil
}
