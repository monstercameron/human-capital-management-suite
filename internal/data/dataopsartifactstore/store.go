// Package dataopsartifactstore persists the immutable inputs used by
// digest-bound DataOps diagnostic follow-ups.
package dataopsartifactstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const MaxPayloadBytes = 16 << 20

var (
	ErrInvalid            = errors.New("dataopsartifactstore: invalid artifact")
	ErrConflict           = errors.New("dataopsartifactstore: digest conflicts with stored artifact")
	ErrNotFound           = errors.New("dataopsartifactstore: artifact not found")
	ErrDatabaseCapability = errors.New("dataopsartifactstore: database capability is required")
)

type DB interface{ dbport.Beginner }

// Store persists artifacts keyed by authenticated tenant, kind and digest.
type Store struct {
	db         DB
	tenantUUID func(values.TenantId) uuid.UUID
}

func New(db DB, tenantUUID func(values.TenantId) uuid.UUID) *Store {
	return &Store{db: db, tenantUUID: tenantUUID}
}

// Put appends an artifact. Repeating the same bytes is idempotent; a different
// body under the same digest is an integrity conflict.
func (s *Store) Put(ctx context.Context, tenant values.TenantId, kind, digest string, payload []byte) error {
	if s == nil || s.db == nil || s.tenantUUID == nil {
		return ErrDatabaseCapability
	}
	tenantID := s.tenantUUID(tenant)
	if tenantID == uuid.Nil || (kind != "diff" && kind != "repair_plan") || strings.TrimSpace(digest) == "" || len(digest) > 256 || len(payload) == 0 || len(payload) > MaxPayloadBytes {
		return ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	var prior []byte
	err = tx.QueryRow(ctx, `SELECT payload FROM dataops_diagnostic_artifact WHERE tenant_id=$1 AND artifact_kind=$2 AND artifact_digest=$3`, tenantID, kind, digest).Scan(&prior)
	if err == nil {
		if !bytes.Equal(prior, payload) {
			return ErrConflict
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return err
	}
	rows, err := tx.Exec(ctx, `INSERT INTO dataops_diagnostic_artifact (tenant_id, artifact_kind, artifact_digest, payload) VALUES ($1,$2,$3,$4) ON CONFLICT (tenant_id, artifact_kind, artifact_digest) DO NOTHING`, tenantID, kind, digest, payload)
	if err != nil {
		return fmt.Errorf("dataopsartifactstore: insert: %w", err)
	}
	if rows == 0 {
		var concurrent []byte
		if err := tx.QueryRow(ctx, `SELECT payload FROM dataops_diagnostic_artifact WHERE tenant_id=$1 AND artifact_kind=$2 AND artifact_digest=$3`, tenantID, kind, digest).Scan(&concurrent); err != nil {
			return err
		}
		if !bytes.Equal(concurrent, payload) {
			return ErrConflict
		}
	}
	return tx.Commit(ctx)
}

// Get loads one artifact without widening the tenant supplied by the caller.
func (s *Store) Get(ctx context.Context, tenant values.TenantId, kind, digest string) ([]byte, error) {
	if s == nil || s.db == nil || s.tenantUUID == nil {
		return nil, ErrDatabaseCapability
	}
	tenantID := s.tenantUUID(tenant)
	if tenantID == uuid.Nil || (kind != "diff" && kind != "repair_plan") || strings.TrimSpace(digest) == "" {
		return nil, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return nil, err
	}
	var payload []byte
	err = tx.QueryRow(ctx, `SELECT payload FROM dataops_diagnostic_artifact WHERE tenant_id=$1 AND artifact_kind=$2 AND artifact_digest=$3`, tenantID, kind, digest).Scan(&payload)
	if errors.Is(err, dbport.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return payload, nil
}
