package application

import (
	"context"
)

// ListPersonaAdminPlacementDocuments adapts the document store's authorized,
// title-only placement projection to the Agent setup catalog.
func (s documentService) ListPersonaAdminPlacementDocuments(ctx context.Context, tenant, actor, conversation string) ([]PersonaAdminPlacementDocument, error) {
	if s.store == nil {
		return nil, ErrPersonaCatalogDenied
	}
	rows, err := s.store.ListReadableOfficialPlacementDocuments(ctx, tenant, conversation, "person", actor)
	if err != nil {
		return nil, err
	}
	out := make([]PersonaAdminPlacementDocument, 0, len(rows))
	for _, row := range rows {
		out = append(out, PersonaAdminPlacementDocument{DocumentID: row.DocumentID, VersionID: row.VersionID, Title: row.Title})
	}
	return out, nil
}

var _ PersonaAdminPlacementDocumentReader = documentService{}
