// GetDocumentVersion reads one immutable version by ID for HUB-033's
// side-by-side compare view. It never mutates and never exposes a version
// the caller cannot read: an unauthorized or unknown version reports only
// readable=false, the same non-disclosure GetDocumentPreviews already uses
// for documents.
package document

import (
	"context"
	"time"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// DocumentVersion is one immutable version read for comparison. Everything
// but Readable is withheld when Readable is false.
type DocumentVersion struct {
	DocumentID, VersionID, Title, Markdown, ContentHash string
	CreatedAt                                           time.Time
	Readable                                            bool
}

// VersionCompareService is the optional single-version read port. A
// composition whose service does not provide it answers UNAVAILABLE.
type VersionCompareService interface {
	GetDocumentVersion(ctx context.Context, tenant, actor, documentID, versionID string) (DocumentVersion, error)
}

func (s *server) GetDocumentVersion(ctx context.Context, req *documentv1.GetDocumentVersionRequest) (*documentv1.GetDocumentVersionResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || !validCommentRef(req.GetDocumentId(), req.GetVersionId()) {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_version_ref", "a document and version ID are required")
	}
	versions, ok := s.service.(VersionCompareService)
	if !ok {
		return nil, envelope.New(envelope.CodeUnavailable, "document.service.unavailable", "document version compare is not configured")
	}
	row, err := versions.GetDocumentVersion(ctx, tenant, actor, req.GetDocumentId(), req.GetVersionId())
	if err != nil {
		return nil, owned(err)
	}
	if !row.Readable {
		return &documentv1.GetDocumentVersionResponse{DocumentId: req.GetDocumentId(), VersionId: req.GetVersionId()}, nil
	}
	out := &documentv1.GetDocumentVersionResponse{
		Readable: true, DocumentId: row.DocumentID, VersionId: row.VersionID,
		Title: row.Title, Markdown: row.Markdown, ContentHash: row.ContentHash,
	}
	if !row.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(row.CreatedAt)
	}
	return out, nil
}
