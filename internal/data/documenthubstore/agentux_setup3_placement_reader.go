package documenthubstore

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// OfficialPlacementDocument is the bounded title projection used by Agent
// setup. It contains no document body or policy detail.
type OfficialPlacementDocument struct {
	DocumentID string
	VersionID  string
	Title      string
}

// ListReadableOfficialPlacementDocuments returns the current official
// documents in one exact conversation that the named reader may read now.
// A live deny wins, and an unreadable document is omitted without revealing
// its title or identifier.
func (s *Store) ListReadableOfficialPlacementDocuments(ctx context.Context, tenantID, conversationID, readerKind, readerID string) ([]OfficialPlacementDocument, error) {
	if s == nil || ctx == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(conversationID) == "" || readerKind != "person" || strings.TrimSpace(readerID) == "" {
		return nil, ErrDenied
	}
	var out []OfficialPlacementDocument
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var candidates []OfficialPlacementDocument
		rows, err := tx.Query(ctx, `SELECT p.document_id,p.version_id,v.title
			FROM document_active_pointer p
			JOIN document_deployment dep ON dep.tenant_id=p.tenant_id AND dep.id=p.deployment_id
			JOIN document d ON d.tenant_id=p.tenant_id AND d.id=p.document_id
			JOIN document_version v ON v.tenant_id=p.tenant_id AND v.document_id=p.document_id AND v.id=p.version_id
			WHERE p.tenant_id=$1 AND p.scope_kind='placement' AND p.scope_id=$2
			  AND dep.scope_kind='placement' AND dep.scope_id=$2 AND dep.version_id=p.version_id
			  AND dep.custodian_id<>'' AND dep.review_due_at IS NOT NULL
			  AND d.lifecycle<>'DISPOSED' AND v.status<>'retired'
			ORDER BY lower(v.title),v.title,p.document_id`, tenantID, conversationID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item OfficialPlacementDocument
			if err := rows.Scan(&item.DocumentID, &item.VersionID, &item.Title); err != nil {
				rows.Close()
				return err
			}
			if strings.TrimSpace(item.Title) == "" {
				continue
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
	sort.Slice(out, func(i, j int) bool {
		if strings.EqualFold(out[i].Title, out[j].Title) {
			return out[i].DocumentID < out[j].DocumentID
		}
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return out, nil
}
