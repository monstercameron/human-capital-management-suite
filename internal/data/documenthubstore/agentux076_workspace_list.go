package documenthubstore

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ListWorkspaceReadableDocuments is the title-only list behind "which documents
// can you read" (AGENTUX-076): the published documents every workspace member
// may read, restricted to those the named person may read now. It reads the same
// grant-filtered candidate set as the workspace search, so it names a document
// exactly when a search could return it, and it carries no body text.
func (s *Store) ListWorkspaceReadableDocuments(ctx context.Context, tenant, actor string, members []string) ([]OfficialPlacementDocument, error) {
	if s == nil || ctx == nil || !validWorkspaceMembers(members) || strings.TrimSpace(actor) == "" {
		return nil, ErrDenied
	}
	var out []OfficialPlacementDocument
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT d.id,v.id,v.title`+workspaceSelectionSQL+`
		AND EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id=$3 AND g.action='read' AND g.effect='allow' AND NOT g.revoked AND (g.expires_at IS NULL OR g.expires_at>now()))
		AND NOT EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id=$3 AND g.action='read' AND g.effect='deny' AND NOT g.revoked AND (g.expires_at IS NULL OR g.expires_at>now()))`, tenant, members, actor)
		if err != nil {
			return err
		}
		var candidates []OfficialPlacementDocument
		for rows.Next() {
			var item OfficialPlacementDocument
			if err := rows.Scan(&item.DocumentID, &item.VersionID, &item.Title); err != nil {
				rows.Close()
				return err
			}
			if strings.TrimSpace(item.Title) != "" {
				candidates = append(candidates, item)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		// The same authorization the document's own reads use has the last word.
		for _, item := range candidates {
			if err := authorizeTx(ctx, tx, tenant, item.DocumentID, "person", actor, ActionRead); err != nil {
				if errors.Is(err, ErrDenied) {
					continue
				}
				return err
			}
			out = append(out, item)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if strings.EqualFold(out[i].Title, out[j].Title) {
			return out[i].DocumentID < out[j].DocumentID
		}
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return out, nil
}
