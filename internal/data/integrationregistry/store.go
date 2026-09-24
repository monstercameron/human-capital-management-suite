package integrationregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

var (
	ErrInvalidScope = errors.New("integrationregistry: tenant scope is required")
	ErrCorrupt      = errors.New("integrationregistry: stored connector publication is corrupt")
	ErrDatabase     = errors.New("integrationregistry: database capability is required")
)

// Scope selects tenant-wide publications when OrganizationScopeID is empty,
// and organization-specific publications otherwise.
type Scope struct {
	TenantID            uuid.UUID
	OrganizationScopeID string
}

type DB interface{ dbport.Beginner }

// Store persists full INTG-001 definitions. The row is append-only and keyed
// by tenant, organization scope, connector id and semantic version.
type Store struct{ db DB }

func New(db DB) *Store { return &Store{db: db} }

// Publish durably records a connectivity publication. Identical republishing
// succeeds; an altered definition under the same scoped version is refused.
func (s *Store) Publish(ctx context.Context, scope Scope, publication connectivity.Publication) error {
	if s == nil || s.db == nil {
		return ErrDatabase
	}
	if scope.TenantID == uuid.Nil || strings.TrimSpace(scope.OrganizationScopeID) != scope.OrganizationScopeID {
		return ErrInvalidScope
	}
	d := publication.Definition
	if err := d.Validate(); err != nil {
		return fmt.Errorf("integrationregistry: invalid definition: %w", err)
	}
	digest, err := d.Digest()
	if err != nil {
		return err
	}
	if publication.Digest != digest || strings.TrimSpace(publication.PublishedBy) == "" || publication.PublishedAt.IsZero() {
		return ErrCorrupt
	}
	body, err := json.Marshal(d)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, scope.TenantID); err != nil {
		return err
	}
	v := d.Version
	_, err = tx.Exec(ctx, `INSERT INTO published_connector_definition
		(tenant_id, scope_id, connector_id, version_major, version_minor, version_patch, definition, definition_digest, published_by, published_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`,
		scope.TenantID, scope.OrganizationScopeID, d.ConnectorID, int64(v.Major), int64(v.Minor), int64(v.Patch), body, digest, publication.PublishedBy, publication.PublishedAt.UTC())
	if err != nil {
		return err
	}
	var existingDigest string
	var existing []byte
	err = tx.QueryRow(ctx, `SELECT definition_digest, definition FROM published_connector_definition
		WHERE tenant_id=$1 AND scope_id=$2 AND connector_id=$3 AND version_major=$4 AND version_minor=$5 AND version_patch=$6`,
		scope.TenantID, scope.OrganizationScopeID, d.ConnectorID, int64(v.Major), int64(v.Minor), int64(v.Patch)).Scan(&existingDigest, &existing)
	if err != nil {
		return err
	}
	if existingDigest != digest {
		return connectivity.ErrImmutable
	}
	var saved connectivity.ConnectorDefinition
	if err := json.Unmarshal(existing, &saved); err != nil || saved.ConnectorID != d.ConnectorID || saved.Version != d.Version {
		return ErrCorrupt
	}
	return tx.Commit(ctx)
}

// LoadRegistry builds a fresh in-memory connectivity.Registry from durable
// tenant-wide and exact-organization publications. There is no implicit
// connector catalogue or incumbent provider fallback.
func (s *Store) LoadRegistry(ctx context.Context, tenantID uuid.UUID, organizationScopeID string) (*connectivity.Registry, error) {
	if s == nil || s.db == nil {
		return nil, ErrDatabase
	}
	if tenantID == uuid.Nil || strings.TrimSpace(organizationScopeID) != organizationScopeID {
		return nil, ErrInvalidScope
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT scope_id, connector_id, version_major, version_minor, version_patch,
		definition, definition_digest, published_by, published_at
		FROM published_connector_definition WHERE tenant_id=$1 AND (scope_id='' OR scope_id=$2)
		ORDER BY scope_id, connector_id, version_major, version_minor, version_patch`, tenantID, organizationScopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	registry := connectivity.NewRegistry()
	for rows.Next() {
		var rowScope string
		var connectorID, digest, publisher string
		var major, minor, patch int64
		var body []byte
		var publishedAt time.Time
		if err := rows.Scan(&rowScope, &connectorID, &major, &minor, &patch, &body, &digest, &publisher, &publishedAt); err != nil {
			return nil, err
		}
		if rowScope != "" && rowScope != organizationScopeID {
			return nil, ErrCorrupt
		}
		var d connectivity.ConnectorDefinition
		if err := json.Unmarshal(body, &d); err != nil || major < 0 || minor < 0 || patch < 0 || major > 4294967295 || minor > 4294967295 || patch > 4294967295 || d.ConnectorID != connectorID || d.Version != (connectivity.Version{Major: uint32(major), Minor: uint32(minor), Patch: uint32(patch)}) {
			return nil, ErrCorrupt
		}
		publication, err := registry.Publish(d, connectivity.PublicationMeta{PublishedBy: publisher, PublishedAt: publishedAt})
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
		if publication.Digest != digest {
			return nil, ErrCorrupt
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return registry, nil
}
