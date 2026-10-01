package documenthubstore

import (
	"context"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// OfficialPlacementSearchHit carries the current document owner with the
// scoped lexical match, without constructing owner facts from policy text.
type OfficialPlacementSearchHit struct {
	SearchHit
	OwnerID string
}

// SearchOfficialPlacementLexical restricts candidates in SQL to current official
// placements in one exact scope, before exposing titles, counts or source bytes.
func (s *Store) SearchOfficialPlacementLexical(ctx context.Context, tenantID, scopeID, query, readerKind, readerID string) ([]OfficialPlacementSearchHit, error) {
	terms := lexTokenize(query)
	if len(terms) == 0 {
		return nil, nil
	}
	var hits []OfficialPlacementSearchHit
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT t.document_id, t.version_id, v.title,
				sum(t.hits * CASE t.field WHEN 'title' THEN 3 WHEN 'heading' THEN 2 ELSE 1 END),
				count(DISTINCT t.term), (SELECT doc.owner_id FROM document doc WHERE doc.tenant_id=t.tenant_id AND doc.id=t.document_id)
			FROM document_search_term t
			JOIN document_version v ON v.tenant_id=t.tenant_id AND v.id=t.version_id
			WHERE t.tenant_id=$1 AND t.term = ANY($2)
			  AND v.status <> 'retired'
			  AND EXISTS (SELECT 1 FROM document_active_pointer p
					JOIN document_deployment d ON d.tenant_id=p.tenant_id AND d.id=p.deployment_id
					WHERE p.tenant_id=t.tenant_id AND p.document_id=t.document_id AND d.version_id=t.version_id AND p.version_id=t.version_id AND p.scope_kind='placement' AND p.scope_id=$5 AND d.scope_kind='placement' AND d.scope_id=$5 AND d.custodian_id<>'' AND d.review_due_at IS NOT NULL)
			  AND EXISTS (SELECT 1 FROM document doc WHERE doc.tenant_id=t.tenant_id AND doc.id=t.document_id
					AND (doc.home<>'PERSONAL' OR EXISTS (SELECT 1 FROM document_grant shared
					  WHERE shared.tenant_id=doc.tenant_id AND shared.document_id=doc.id
					  AND shared.subject_kind='person' AND shared.subject_id<>doc.owner_id AND shared.action='read'
					  AND shared.effect='allow' AND shared.revoked=false AND (shared.expires_at IS NULL OR shared.expires_at>now())
					  AND NOT EXISTS (SELECT 1 FROM document_grant deny WHERE deny.tenant_id=shared.tenant_id
					    AND deny.document_id=shared.document_id AND deny.subject_kind='person' AND deny.subject_id=shared.subject_id
					    AND deny.action='read' AND deny.effect='deny' AND deny.revoked=false AND (deny.expires_at IS NULL OR deny.expires_at>now())))))
			  AND EXISTS (SELECT 1 FROM document_grant g
					WHERE g.tenant_id=t.tenant_id AND g.document_id=t.document_id
					  AND g.subject_kind=$3 AND g.subject_id=$4 AND g.action='read'
					  AND g.effect='allow' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now()))
			  AND NOT EXISTS (SELECT 1 FROM document_grant g
					WHERE g.tenant_id=t.tenant_id AND g.document_id=t.document_id
					  AND g.subject_kind=$3 AND g.subject_id=$4 AND g.action='read'
					  AND g.effect='deny' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now()))
			GROUP BY t.tenant_id, t.document_id, t.version_id, v.title`,
			tenantID, terms, readerKind, readerID, scopeID)
		if err != nil {
			return err
		}
		var candidates []OfficialPlacementSearchHit
		for rows.Next() {
			var h OfficialPlacementSearchHit
			if err := rows.Scan(&h.DocumentID, &h.VersionID, &h.Title, &h.Score, &h.MatchedTerms, &h.OwnerID); err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, h)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, h := range candidates {
			if err := authorizeTx(ctx, tx, tenantID, h.DocumentID, readerKind, readerID, ActionRead); err != nil {
				continue
			}
			hits = append(hits, h)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		if hits[i].DocumentID != hits[j].DocumentID {
			return hits[i].DocumentID < hits[j].DocumentID
		}
		return hits[i].VersionID < hits[j].VersionID
	})
	return hits, nil
}
