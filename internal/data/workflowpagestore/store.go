// Package workflowpagestore persists tenant-owned workflow input page
// revisions. Published versions and draft revisions are append-only; a page
// run points at the exact page_version reference it used.
package workflowpagestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

var (
	ErrInvalid = errors.New("workflowpagestore: invalid page record")
	ErrMissing = errors.New("workflowpagestore: page version not found")
)

type DB interface{ dbport.Beginner }

// PageVersion is one immutable tenant-published page definition.
type PageVersion struct {
	TenantID         uuid.UUID
	WorkflowKey      string
	WorkflowVersion  int
	PageID           string
	PageVersion      int
	Definition       json.RawMessage
	DefinitionDigest string
	GeneratedDefault bool
	PublishedAt      time.Time
	PublishedBy      string
}

// Draft is one immutable tenant draft revision. A subsequent save creates a
// higher DraftVersion row instead of replacing this row.
type Draft struct {
	TenantID         uuid.UUID
	DraftID          uuid.UUID
	WorkflowKey      string
	WorkflowVersion  int
	PageID           string
	DraftVersion     int64
	Definition       json.RawMessage
	DefinitionDigest string
	Author           string
	RecordedAt       time.Time
}

type Store struct{ DB DB }

func (s Store) begin(ctx context.Context, tenant uuid.UUID) (dbport.Tx, error) {
	if s.DB == nil || tenant == uuid.Nil {
		return nil, fmt.Errorf("%w: database and tenant are required", ErrInvalid)
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("workflowpagestore: begin: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

func validateCommon(tenant uuid.UUID, workflowKey, pageID, digest string, workflowVersion, pageVersion int, definition []byte, actor string) error {
	if tenant == uuid.Nil || strings.TrimSpace(workflowKey) == "" || strings.TrimSpace(pageID) == "" || strings.TrimSpace(digest) == "" || strings.TrimSpace(actor) == "" || workflowVersion < 1 || pageVersion < 1 || len(definition) == 0 {
		return ErrInvalid
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(definition, &object); err != nil || object == nil {
		return fmt.Errorf("%w: definition must be a JSON object", ErrInvalid)
	}
	return nil
}

// Publish appends one immutable published version and returns the stored
// timestamp. Reusing the same identity is rejected by the primary key.
func (s Store) Publish(ctx context.Context, page PageVersion) error {
	if err := validateCommon(page.TenantID, page.WorkflowKey, page.PageID, page.DefinitionDigest, page.WorkflowVersion, page.PageVersion, page.Definition, page.PublishedBy); err != nil {
		return err
	}
	tx, err := s.begin(ctx, page.TenantID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	when := page.PublishedAt.UTC()
	if when.IsZero() {
		when = time.Now().UTC()
	}
	_, err = tx.Exec(ctx, `INSERT INTO workflow_page_version
        (tenant_id,workflow_key,workflow_version,page_id,page_version,definition,definition_digest,generated_default,published_at,published_by)
        VALUES($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10)`, page.TenantID, page.WorkflowKey, page.WorkflowVersion, page.PageID, page.PageVersion, string(page.Definition), page.DefinitionDigest, page.GeneratedDefault, when, page.PublishedBy)
	if err != nil {
		return fmt.Errorf("workflowpagestore: publish: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("workflowpagestore: publish commit: %w", err)
	}
	return nil
}

// SaveDraft appends one draft revision.
func (s Store) SaveDraft(ctx context.Context, draft Draft) error {
	if err := validateCommon(draft.TenantID, draft.WorkflowKey, draft.PageID, draft.DefinitionDigest, draft.WorkflowVersion, int(draft.DraftVersion), draft.Definition, draft.Author); err != nil {
		return err
	}
	if draft.DraftID == uuid.Nil {
		return fmt.Errorf("%w: draft id is required", ErrInvalid)
	}
	tx, err := s.begin(ctx, draft.TenantID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	when := draft.RecordedAt.UTC()
	if when.IsZero() {
		when = time.Now().UTC()
	}
	_, err = tx.Exec(ctx, `INSERT INTO workflow_page_draft
        (tenant_id,draft_id,workflow_key,workflow_version,page_id,draft_version,definition,definition_digest,author,recorded_at)
        VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10)`, draft.TenantID, draft.DraftID, draft.WorkflowKey, draft.WorkflowVersion, draft.PageID, draft.DraftVersion, string(draft.Definition), draft.DefinitionDigest, draft.Author, when)
	if err != nil {
		return fmt.Errorf("workflowpagestore: save draft: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("workflowpagestore: save draft commit: %w", err)
	}
	return nil
}

// Get returns one published version under the tenant's RLS scope.
func (s Store) Get(ctx context.Context, tenant uuid.UUID, workflowKey string, workflowVersion, pageVersion int, pageID string) (PageVersion, error) {
	tx, err := s.begin(ctx, tenant)
	if err != nil {
		return PageVersion{}, err
	}
	defer tx.Rollback(ctx)
	var out PageVersion
	err = tx.QueryRow(ctx, `SELECT tenant_id,workflow_key,workflow_version,page_id,page_version,definition,definition_digest,generated_default,published_at,published_by
        FROM workflow_page_version WHERE tenant_id=$1 AND workflow_key=$2 AND workflow_version=$3 AND page_id=$4 AND page_version=$5`, tenant, workflowKey, workflowVersion, pageID, pageVersion).
		Scan(&out.TenantID, &out.WorkflowKey, &out.WorkflowVersion, &out.PageID, &out.PageVersion, &out.Definition, &out.DefinitionDigest, &out.GeneratedDefault, &out.PublishedAt, &out.PublishedBy)
	if errors.Is(err, dbport.ErrNoRows) {
		return PageVersion{}, ErrMissing
	}
	if err != nil {
		return PageVersion{}, fmt.Errorf("workflowpagestore: get: %w", err)
	}
	return out, nil
}

// List returns published versions in page-version order.
func (s Store) List(ctx context.Context, tenant uuid.UUID, workflowKey string, workflowVersion int, pageID string) ([]PageVersion, error) {
	tx, err := s.begin(ctx, tenant)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT tenant_id,workflow_key,workflow_version,page_id,page_version,definition,definition_digest,generated_default,published_at,published_by
        FROM workflow_page_version WHERE tenant_id=$1 AND workflow_key=$2 AND workflow_version=$3 AND page_id=$4 ORDER BY page_version`, tenant, workflowKey, workflowVersion, pageID)
	if err != nil {
		return nil, fmt.Errorf("workflowpagestore: list: %w", err)
	}
	defer rows.Close()
	var out []PageVersion
	for rows.Next() {
		var p PageVersion
		if err := rows.Scan(&p.TenantID, &p.WorkflowKey, &p.WorkflowVersion, &p.PageID, &p.PageVersion, &p.Definition, &p.DefinitionDigest, &p.GeneratedDefault, &p.PublishedAt, &p.PublishedBy); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
