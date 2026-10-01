// Package agentsettingstore persists the tenant agents setting (UXBLIND-122):
// whether people in a tenant may use agents. A tenant with no row has agents
// off. Every call runs in a transaction bound to one tenant
// (tenancy.WithTenant), so the tenant_isolation policy applies.
package agentsettingstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrInvalid means the tenant is unknown or the actor is empty.
	ErrInvalid = errors.New("agentsettingstore: invalid request")
	// ErrUnavailable means the store has no database.
	ErrUnavailable = errors.New("agentsettingstore: unavailable")
)

// DB is the transaction opener the store needs.
type DB interface{ dbport.Beginner }

// Store reads and writes tenant_agent_setting.
type Store struct {
	db         DB
	tenantUUID func(values.TenantId) uuid.UUID
}

// New builds a Store. tenantUUID maps a tenant key to its canonical UUID and
// must return uuid.Nil for unknown tenants.
func New(db DB, tenantUUID func(values.TenantId) uuid.UUID) (*Store, error) {
	if db == nil || tenantUUID == nil {
		return nil, fmt.Errorf("%w: database and canonical tenant mapper are required", ErrInvalid)
	}
	return &Store{db: db, tenantUUID: tenantUUID}, nil
}

func (s *Store) begin(ctx context.Context, tenant values.TenantId) (dbport.Tx, uuid.UUID, error) {
	if s == nil || s.db == nil {
		return nil, uuid.Nil, ErrUnavailable
	}
	id := s.tenantUUID(tenant)
	if id == uuid.Nil {
		return nil, uuid.Nil, fmt.Errorf("%w: unknown tenant %q", ErrInvalid, tenant)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("agentsettingstore: begin: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, id); err != nil {
		_ = tx.Rollback(ctx)
		return nil, uuid.Nil, err
	}
	return tx, id, nil
}

// AgentsEnabled reports the tenant's setting; no stored row means off.
func (s *Store) AgentsEnabled(ctx context.Context, tenant values.TenantId) (bool, error) {
	tx, id, err := s.begin(ctx, tenant)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var enabled bool
	err = tx.QueryRow(ctx, `SELECT enabled FROM tenant_agent_setting WHERE tenant_id = $1`, id).Scan(&enabled)
	if err != nil {
		if isNoRows(err) {
			return false, nil
		}
		return false, fmt.Errorf("agentsettingstore: read: %w", err)
	}
	return enabled, nil
}

// SetAgentsEnabled records the tenant's setting and the acting subject,
// advancing the revision by one.
func (s *Store) SetAgentsEnabled(ctx context.Context, tenant values.TenantId, enabled bool, actor string) error {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrInvalid)
	}
	tx, id, err := s.begin(ctx, tenant)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO tenant_agent_setting (tenant_id, enabled, revision, updated_by, updated_at)
VALUES ($1, $2, 1, $3, now())
ON CONFLICT (tenant_id) DO UPDATE SET enabled = EXCLUDED.enabled, revision = tenant_agent_setting.revision + 1,
    updated_by = EXCLUDED.updated_by, updated_at = now()`, id, enabled, actor); err != nil {
		return fmt.Errorf("agentsettingstore: write: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("agentsettingstore: commit: %w", err)
	}
	return nil
}

func isNoRows(err error) bool {
	return errors.Is(err, dbport.ErrNoRows)
}
