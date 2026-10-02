package documenthubstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Workspace-wide access is the ordinary hub person/read decision for every
// current workspace member. The directory supplies that complete set; neither
// a folder nor a deployment creates a read grant. Empty membership fails closed.
const workspaceReadSQL = `NOT EXISTS (SELECT 1 FROM unnest($2::text[]) member(subject)
	WHERE NOT EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id AND g.document_id=d.id
		AND g.subject_kind='person' AND g.subject_id=member.subject AND g.action='read' AND g.effect='allow'
		AND NOT g.revoked AND (g.expires_at IS NULL OR g.expires_at>now()))
	OR EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id AND g.document_id=d.id
		AND g.subject_kind='person' AND g.subject_id=member.subject AND g.action='read' AND g.effect='deny'
		AND NOT g.revoked AND (g.expires_at IS NULL OR g.expires_at>now())))`

const workspaceSelectionSQL = ` FROM document d
	JOIN LATERAL (SELECT v.* FROM document_active_pointer p
		JOIN document_deployment dep ON dep.tenant_id=p.tenant_id AND dep.id=p.deployment_id
		JOIN document_version v ON v.tenant_id=p.tenant_id AND v.document_id=p.document_id AND v.id=p.version_id
		WHERE p.tenant_id=d.tenant_id AND p.document_id=d.id AND dep.version_id=v.id AND v.status<>'retired'
		AND ((p.scope_kind='default' AND p.scope_id='') OR (p.scope_kind='placement' AND dep.custodian_id<>'' AND dep.review_due_at IS NOT NULL))
		ORDER BY (p.scope_kind='default') DESC,dep.effective_at DESC,v.id DESC LIMIT 1) v ON true
	WHERE d.tenant_id=$1 AND d.lifecycle<>'DISPOSED' AND ` + workspaceReadSQL

var ErrWorkspaceMeaningUnavailable = errors.New("document search: workspace meaning index unavailable")

type WorkspaceSectionHit struct {
	DocumentID, VersionID, Title, OwnerID, SectionAnchor, Text string
	Score                                                      float64
}

type WorkspaceIndexStatus struct {
	Documents, Sections, Pending int
	IndexedAt                    time.Time
}

func validWorkspaceMembers(members []string) bool {
	if len(members) == 0 {
		return false
	}
	for _, member := range members {
		if strings.TrimSpace(member) == "" {
			return false
		}
	}
	return true
}

// SearchWorkspaceMeaning uses the hub's section ranking over a grant-filtered
// published candidate set. Only authorized versions' vectors are ever loaded.
func (s *Store) SearchWorkspaceMeaning(ctx context.Context, tenant, actor, query, model string, vector []float32, members []string) ([]WorkspaceSectionHit, error) {
	if !validWorkspaceMembers(members) || actor == "" {
		return nil, ErrDenied
	}
	if model == "" || len(vector) == 0 {
		return nil, ErrWorkspaceMeaningUnavailable
	}
	var out []WorkspaceSectionHit
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT d.id,d.owner_id,v.id,v.title,v.normalized_markdown,v.created_at`+workspaceSelectionSQL+`
		AND EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id=$3 AND g.action='read' AND g.effect='allow' AND NOT g.revoked AND (g.expires_at IS NULL OR g.expires_at>now()))
		AND NOT EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id=$3 AND g.action='read' AND g.effect='deny' AND NOT g.revoked AND (g.expires_at IS NULL OR g.expires_at>now()))`, tenant, members, actor)
		if err != nil {
			return err
		}
		var candidates []searchCandidate
		for rows.Next() {
			var c searchCandidate
			if err := rows.Scan(&c.id, &c.owner, &c.versionID, &c.title, &c.markdown, &c.updated); err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		vectors, err := bestSections(ctx, tx, tenant, model, vector, candidates)
		if err != nil {
			return err
		}
		if len(candidates) > 0 && len(vectors) == 0 {
			return ErrWorkspaceMeaningUnavailable
		}
		hits := rankSearch(SearchMeaning, query, lexTokenize(query), candidates, vectors)
		byID := map[string]searchCandidate{}
		for _, c := range candidates {
			byID[c.id] = c
		}
		for _, hit := range hits[:min(len(hits), 5)] {
			c := byID[hit.id]
			if err := authorizeTx(ctx, tx, tenant, c.id, "person", actor, ActionRead); err != nil {
				if errors.Is(err, ErrDenied) {
					continue
				}
				return err
			}
			section := vectors[c.id].blockID
			text := c.markdown
			if section != "" {
				text = ""
				for _, sec := range SplitSections(c.markdown) {
					if sec.BlockID == section {
						text = sec.Text
						break
					}
				}
			}
			if text == "" {
				continue
			}
			out = append(out, WorkspaceSectionHit{DocumentID: c.id, VersionID: c.versionID, Title: c.title, OwnerID: c.owner, SectionAnchor: section, Text: text, Score: hit.score})
		}
		return nil
	})
	return out, err
}

func (s *Store) WorkspaceDocumentReadable(ctx context.Context, tenant, document string, members []string) (bool, error) {
	if !validWorkspaceMembers(members) {
		return false, ErrDenied
	}
	var readable bool
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM document d WHERE d.tenant_id=$1 AND d.id=$3 AND d.lifecycle<>'DISPOSED' AND `+workspaceReadSQL+`)`, tenant, members, document).Scan(&readable)
	})
	return readable, err
}

// WorkspaceIndexStatus counts current published versions, never stale vectors.
func (s *Store) WorkspaceIndexStatus(ctx context.Context, tenant, model string, members []string) (WorkspaceIndexStatus, error) {
	var out WorkspaceIndexStatus
	var indexedAt *time.Time
	if !validWorkspaceMembers(members) {
		return out, ErrDenied
	}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*),COALESCE(sum((SELECT count(*) FROM document_section_vector sv WHERE sv.tenant_id=d.tenant_id AND sv.version_id=v.id AND sv.model_id=$3)),0),
		count(*) FILTER(WHERE NOT EXISTS(SELECT 1 FROM document_section_vector sv WHERE sv.tenant_id=d.tenant_id AND sv.version_id=v.id AND sv.model_id=$3)),
		max((SELECT max(j.updated_at) FROM document_index_job j WHERE j.tenant_id=d.tenant_id AND j.version_id=v.id AND j.model_id=$3 AND j.status='done'))`+workspaceSelectionSQL, tenant, members, model).Scan(&out.Documents, &out.Sections, &out.Pending, &indexedAt)
	})
	if indexedAt != nil {
		out.IndexedAt = *indexedAt
	}
	return out, err
}

// IndexReceipt counts all tenant index vectors for preparation receipts.
func (s *Store) IndexReceipt(ctx context.Context, tenant, model string) (int, int, error) {
	var documents, sections int
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(DISTINCT document_id),count(*) FROM document_section_vector WHERE tenant_id=$1 AND model_id=$2`, tenant, model).Scan(&documents, &sections)
	})
	return documents, sections, err
}
