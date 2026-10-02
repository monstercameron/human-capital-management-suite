package documenthubstore

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ListReadableDeployedDocumentsByTitle returns the documents of one tenant whose
// deployed version carries exactly this title and that the named reader may read
// now, newest deployed version per document. It is the title-only way to find a
// document an agent's answer cited by name: placement in a conversation is not
// required, the hub's own read check decides. A live deny wins, and an unreadable
// document is omitted without revealing its identifier.
func (s *Store) ListReadableDeployedDocumentsByTitle(ctx context.Context, tenantID, title, readerKind, readerID string) ([]OfficialPlacementDocument, error) {
	if s == nil || ctx == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(title) == "" || readerKind != "person" || strings.TrimSpace(readerID) == "" {
		return nil, ErrDenied
	}
	var out []OfficialPlacementDocument
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var candidates []OfficialPlacementDocument
		rows, err := tx.Query(ctx, `SELECT DISTINCT ON (v.document_id) v.document_id,v.id,v.title
			FROM document_version v
			JOIN document d ON d.tenant_id=v.tenant_id AND d.id=v.document_id
			WHERE v.tenant_id=$1 AND v.title=$2 AND v.status='deployed' AND d.lifecycle<>'DISPOSED'
			ORDER BY v.document_id,v.created_at DESC,v.id DESC
			LIMIT 50`, tenantID, title)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item OfficialPlacementDocument
			if err := rows.Scan(&item.DocumentID, &item.VersionID, &item.Title); err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, item)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, item := range candidates {
			if err := authorizeTx(ctx, tx, tenantID, item.DocumentID, readerKind, readerID, ActionRead); err != nil {
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
	return out, nil
}
