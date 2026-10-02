package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type WorkspaceDocumentSearchStatusReader interface {
	WorkspaceDocumentSearchStatus(context.Context, string) (WorkspaceDocumentSearchStatus, error)
}

// WorkspacePersonaCatalogVersions enriches Assistant's metadata after the
// tenant-scoped catalog read. Policy Helper's projection remains unchanged.
type WorkspacePersonaCatalogVersions struct {
	Base   PersonaCatalogVersionReader
	Search WorkspaceDocumentSearchStatusReader
}

func (r WorkspacePersonaCatalogVersions) ListPersonaCatalogVersions(ctx context.Context, tenant values.TenantId) ([]PersonaCatalogVersion, error) {
	if r.Base == nil {
		return nil, ErrPersonaCatalogDenied
	}
	rows, err := r.Base.ListPersonaCatalogVersions(ctx, tenant)
	if err != nil {
		return nil, err
	}
	out := append([]PersonaCatalogVersion(nil), rows...)
	for i, row := range out {
		if row.Profile.Profile.PersonaID != localAgentDemoAssistantPersonaID {
			continue
		}
		if r.Search == nil {
			out[i].WorkspaceDocuments, out[i].WorkspacePending = 0, 0
			out[i].WorkspaceIndexedAt = WorkspaceDocumentSearchStatus{}.WorkspaceIndexedAt
			continue
		}
		status, err := r.Search.WorkspaceDocumentSearchStatus(ctx, tenant.String())
		if err != nil {
			out[i].WorkspaceDocuments, out[i].WorkspacePending = 0, 0
			out[i].WorkspaceIndexedAt = WorkspaceDocumentSearchStatus{}.WorkspaceIndexedAt
			continue
		}
		out[i].WorkspaceDocuments, out[i].WorkspaceIndexedAt, out[i].WorkspacePending = status.WorkspaceDocuments, status.WorkspaceIndexedAt, status.WorkspacePending
	}
	return out, nil
}
