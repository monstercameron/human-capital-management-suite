// ListDocumentVersions lists a document's immutable versions for the
// compare pickers (DOCS-01). It is a read: it never mutates and, like
// GetDocumentVersion, exposes nothing about a version the caller cannot
// read beyond what the store's own history redaction already allows.
package document

import (
	"context"
	"time"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// DocumentVersionSummary is one entry in a document's version history,
// already policy-redacted by the store. AuthorID is sent only when the
// store can name the version's author; IsCurrent marks the version the
// document currently resolves to.
type DocumentVersionSummary struct {
	VersionID, Title, AuthorID string
	CreatedAt                  time.Time
	IsCurrent, Redacted        bool
}

// VersionsService is the optional version-history port. A composition
// whose service does not provide it answers UNAVAILABLE.
type VersionsService interface {
	ListDocumentVersions(ctx context.Context, tenant, actor, documentID string) ([]DocumentVersionSummary, error)
}

func (s *server) ListDocumentVersions(ctx context.Context, req *documentv1.ListDocumentVersionsRequest) (*documentv1.ListDocumentVersionsResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || !validID(req.GetDocumentId()) {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_id", "a document ID is required")
	}
	versions, ok := s.service.(VersionsService)
	if !ok {
		return nil, envelope.New(envelope.CodeUnavailable, "document.service.unavailable", "document version history is not configured")
	}
	rows, err := versions.ListDocumentVersions(ctx, tenant, actor, req.GetDocumentId())
	if err != nil {
		return nil, owned(err)
	}
	out := &documentv1.ListDocumentVersionsResponse{Versions: make([]*documentv1.DocumentVersionSummary, 0, len(rows))}
	for _, row := range rows {
		v := &documentv1.DocumentVersionSummary{
			VersionId: row.VersionID, Title: row.Title, AuthorId: row.AuthorID,
			IsCurrent: row.IsCurrent, Redacted: row.Redacted,
		}
		if !row.CreatedAt.IsZero() {
			v.CreatedAt = timestamppb.New(row.CreatedAt)
		}
		out.Versions = append(out.Versions, v)
	}
	return out, nil
}
