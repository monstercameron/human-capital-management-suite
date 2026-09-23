package documenthubstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// DocumentSummary is the bounded, authorized personal document projection.
// Team and channel documents require live audience eligibility and are not
// exposed by this path until that authority is composed.
type DocumentSummary struct {
	ID, Title, OwnerID, VersionID string
	Status                        string
	UpdatedAt                     time.Time
	Shared, CanManage, CanEdit    bool
}

type ListOptions struct {
	Limit      int
	Query      string
	Collection string
	BeforeTime time.Time
	BeforeID   string
}

// CreatePersonalDocument atomically creates the private document, owner
// grants, first immutable candidate, and its outbox event.
func (s *Store) CreatePersonalDocument(ctx context.Context, tenantID, ownerID, title, markdown string) (string, Version, error) {
	if strings.TrimSpace(ownerID) == "" || strings.TrimSpace(title) == "" {
		return "", Version{}, errors.New("document: owner and title are required")
	}
	id := "doc-" + uuid.NewString()
	var version Version
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO document(id,tenant_id,owner_id,home) VALUES($1,$2,$3,'PERSONAL')`, id, tenantID, ownerID); err != nil {
			return err
		}
		if err := bootstrapOwnerTx(ctx, tx, tenantID, id, ownerID); err != nil {
			return err
		}
		var err error
		version, err = insertVersionTx(ctx, tx, tenantID, Version{
			DocumentID: id, CreatorID: ownerID, Title: title, Markdown: markdown,
		})
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO document_outbox(tenant_id,aggregate_id,event_type,payload)
			VALUES($1::text,$2::text,'document.created',jsonb_build_object('document_id',$2::text,'version_id',$3::text))`, tenantID, id, version.ID)
		return err
	})
	if err != nil {
		return "", Version{}, err
	}
	return id, version, nil
}

// ListPersonalDocuments returns personal documents owned by or explicitly
// shared with the actor. A recipient sees only an active deployment, while
// the owner can see their latest candidate. A live deny always wins.
func (s *Store) ListPersonalDocuments(ctx context.Context, tenantID, actorID string, limit int) ([]DocumentSummary, error) {
	return s.ListPersonalDocumentsPage(ctx, tenantID, actorID, ListOptions{Limit: limit})
}

// ListPersonalDocumentsPage applies live authorization to each bounded page.
func (s *Store) ListPersonalDocumentsPage(ctx context.Context, tenantID, actorID string, options ListOptions) ([]DocumentSummary, error) {
	if actorID == "" {
		return nil, ErrDenied
	}
	if options.Limit <= 0 || options.Limit > 101 {
		options.Limit = 101
	}
	if options.Collection != "private" && options.Collection != "shared" {
		options.Collection = "all"
	}
	query := strings.TrimSpace(options.Query)
	if len(query) > 200 {
		return nil, errors.New("document: query is too long")
	}
	var terms []string
	if query != "" {
		seen := map[string]bool{}
		for _, term := range lexTokenize(query) {
			if !seen[term] {
				terms = append(terms, term)
				seen[term] = true
			}
		}
		if len(terms) == 0 {
			return []DocumentSummary{}, nil
		}
	}
	var out []DocumentSummary
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT d.id, d.owner_id,
			COALESCE(v.title, ''), COALESCE(v.id, ''),
			CASE WHEN d.owner_id=$2 THEN 'private' ELSE 'shared' END,
			COALESCE(v.created_at, d.created_at),
			EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id<>d.owner_id AND g.action='read' AND g.effect='allow' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now()) AND NOT EXISTS (SELECT 1 FROM document_grant deny WHERE deny.tenant_id=g.tenant_id AND deny.document_id=g.document_id AND deny.subject_kind='person' AND deny.subject_id=g.subject_id AND deny.action='read' AND deny.effect='deny' AND deny.revoked=false AND (deny.expires_at IS NULL OR deny.expires_at>now()))),
			(d.owner_id=$2 AND EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id=$2 AND g.action='manage' AND g.effect='allow' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now())) AND NOT EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id=$2 AND g.action='manage' AND g.effect='deny' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now()))),
			(d.owner_id=$2 AND EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id=$2 AND g.action='propose' AND g.effect='allow' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now())) AND NOT EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id=$2 AND g.action='propose' AND g.effect='deny' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now())))
			FROM document d
			LEFT JOIN document_active_pointer p ON p.tenant_id=d.tenant_id AND p.document_id=d.id
				AND p.scope_kind='default' AND p.scope_id=''
			LEFT JOIN LATERAL (
				SELECT x.id,x.title,x.normalized_markdown,x.created_at FROM document_version x
				WHERE x.tenant_id=d.tenant_id AND x.document_id=d.id
					AND x.status<>'retired'
					AND ((d.owner_id=$2 AND $5::text[] IS NULL) OR x.id=p.version_id)
				ORDER BY x.created_at DESC,x.id DESC LIMIT 1
			) v ON true
			WHERE d.tenant_id=$1 AND d.home='PERSONAL' AND d.lifecycle<>'DISPOSED'
				AND EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id
					AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id=$2
					AND g.action='read' AND g.effect='allow' AND g.revoked=false
					AND (g.expires_at IS NULL OR g.expires_at>now()))
				AND NOT EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id
					AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id=$2
					AND g.action='read' AND g.effect='deny' AND g.revoked=false
					AND (g.expires_at IS NULL OR g.expires_at>now()))
				AND (d.owner_id=$2 OR p.version_id IS NOT NULL)
				AND ($4::text='all' OR ($4::text='private' AND d.owner_id=$2) OR ($4::text='shared' AND d.owner_id<>$2))
				AND ($5::text[] IS NULL OR (p.version_id IS NOT NULL
					AND EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=d.tenant_id AND g.document_id=d.id AND g.subject_kind='person' AND g.subject_id<>d.owner_id AND g.action='read' AND g.effect='allow' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now()) AND NOT EXISTS (SELECT 1 FROM document_grant deny WHERE deny.tenant_id=g.tenant_id AND deny.document_id=g.document_id AND deny.subject_kind='person' AND deny.subject_id=g.subject_id AND deny.action='read' AND deny.effect='deny' AND deny.revoked=false AND (deny.expires_at IS NULL OR deny.expires_at>now())))
					AND ((SELECT count(DISTINCT t.term) FROM document_search_term t WHERE t.tenant_id=d.tenant_id AND t.document_id=d.id AND t.version_id=v.id AND t.term=ANY($5::text[]))=cardinality($5::text[])
					OR (NOT EXISTS (SELECT 1 FROM document_search_term t WHERE t.tenant_id=d.tenant_id AND t.version_id=v.id)
						AND (position(lower($8::text) in lower(v.title))>0 OR position(lower($8::text) in lower(v.normalized_markdown))>0)))))
				AND ($6::timestamptz IS NULL OR (COALESCE(v.created_at,d.created_at),d.id)<($6::timestamptz,$7::text))
			ORDER BY COALESCE(v.created_at,d.created_at) DESC,d.id DESC LIMIT $3`,
			tenantID, actorID, options.Limit, options.Collection, terms, nullableTime(options.BeforeTime), options.BeforeID, query)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d DocumentSummary
			if err := rows.Scan(&d.ID, &d.OwnerID, &d.Title, &d.VersionID, &d.Status, &d.UpdatedAt, &d.Shared, &d.CanManage, &d.CanEdit); err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
