// GetDocumentBacklinks lists inbound links to a document from sources the
// caller may also read (HUB-035/HUB-022). A source the caller cannot read
// is left out of the response entirely, never named with a state.
package document

import (
	"context"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// Backlink is one inbound link from a jointly readable source.
type Backlink struct {
	SourceDocumentID, SourceVersionID, SourceTitle string
	Label, Block, State                            string
}

// BacklinksService is the optional backlinks port. A composition whose
// service does not provide it answers UNAVAILABLE.
type BacklinksService interface {
	DocumentBacklinks(ctx context.Context, tenant, actor, documentID string) ([]Backlink, error)
}

func (s *server) GetDocumentBacklinks(ctx context.Context, req *documentv1.GetDocumentBacklinksRequest) (*documentv1.GetDocumentBacklinksResponse, error) {
	tenant, actor, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || !validID(req.GetDocumentId()) {
		return nil, envelope.New(envelope.CodeInvalidArgument, "document.invalid_id", "a document ID is required")
	}
	backlinks, ok := s.service.(BacklinksService)
	if !ok {
		return nil, envelope.New(envelope.CodeUnavailable, "document.service.unavailable", "document backlinks are not configured")
	}
	rows, err := backlinks.DocumentBacklinks(ctx, tenant, actor, req.GetDocumentId())
	if err != nil {
		return nil, owned(err)
	}
	out := &documentv1.GetDocumentBacklinksResponse{Backlinks: make([]*documentv1.DocumentBacklink, 0, len(rows))}
	for _, row := range rows {
		out.Backlinks = append(out.Backlinks, &documentv1.DocumentBacklink{
			SourceDocumentId: row.SourceDocumentID, SourceVersionId: row.SourceVersionID, SourceTitle: row.SourceTitle,
			Label: row.Label, BlockId: row.Block, State: row.State,
		})
	}
	return out, nil
}
