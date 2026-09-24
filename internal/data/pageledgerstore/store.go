// Package pageledgerstore persists tenant-bound product page revisions and
// rollouts through the pageledger port. Digest calculation and validation
// remain in the productui owner.
package pageledgerstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pageledger"
)

// Store is the PostgreSQL adapter for pageledger.Store.
type Store struct {
	DB       dbport.Beginner
	TenantID func(string) uuid.UUID
}

var _ pageledger.Store = (*Store)(nil)

// New builds a tenant-mapped store over the application's database port.
func New(db dbport.Beginner, tenantID func(string) uuid.UUID) *Store {
	return &Store{DB: db, TenantID: tenantID}
}

func (s *Store) beginTenant(ctx context.Context, tenant string) (dbport.Tx, uuid.UUID, error) {
	if s == nil || s.DB == nil || s.TenantID == nil {
		return nil, uuid.Nil, errors.New("pageledgerstore: database is nil")
	}
	id := s.TenantID(tenant)
	if id == uuid.Nil {
		return nil, uuid.Nil, fmt.Errorf("pageledgerstore: tenant %q does not resolve", tenant)
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, id.String()); err != nil {
		_ = tx.Rollback(ctx)
		return nil, uuid.Nil, fmt.Errorf("pageledgerstore: set tenant: %w", err)
	}
	return tx, id, nil
}

func (s *Store) PutRevision(ctx context.Context, tenant string, page string, version int64, digest string, encoded []byte) error {
	tx, tenantUUID, err := s.beginTenant(ctx, tenant)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	_, err = tx.Exec(ctx, `INSERT INTO page_definition_revision (tenant_id, page_id, version, digest, payload) VALUES ($1,$2,$3,$4,$5::jsonb) ON CONFLICT (tenant_id,page_id,version) DO NOTHING`, tenantUUID, page, version, digest, string(encoded))
	if err != nil {
		return fmt.Errorf("pageledgerstore: insert revision: %w", err)
	}
	var same bool
	if err := tx.QueryRow(ctx, `SELECT digest=$4 AND payload=$5::jsonb FROM page_definition_revision WHERE tenant_id=$1 AND page_id=$2 AND version=$3`, tenantUUID, page, version, digest, string(encoded)).Scan(&same); err != nil {
		return err
	}
	if !same {
		return fmt.Errorf("pageledgerstore: immutable revision conflict for %q version %d", page, version)
	}
	return tx.Commit(ctx)
}

func (s *Store) LoadRevisions(ctx context.Context, tenant string) ([]pageledger.RevisionRow, error) {
	tx, tenantUUID, err := s.beginTenant(ctx, tenant)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	rows, err := tx.Query(ctx, `SELECT page_id,version,digest,payload::text FROM page_definition_revision WHERE tenant_id=$1 ORDER BY page_id,version`, tenantUUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []pageledger.RevisionRow
	for rows.Next() {
		var page, digest, payload string
		var version int64
		if err := rows.Scan(&page, &version, &digest, &payload); err != nil {
			return nil, err
		}
		result = append(result, pageledger.RevisionRow{Page: page, Version: version, Digest: digest, Payload: []byte(payload)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) PutRollout(ctx context.Context, tenant string, page string, recordVersion int64, targetVersion int64, digest string, encoded []byte) error {
	tx, tenantUUID, err := s.beginTenant(ctx, tenant)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	// Preserve the public (tenant,page,target-version) rollout key from 00332.
	// It anchors the first staged publication for each target revision; the
	// event table below keeps later rollback history append-only by record seq.
	_, err = tx.Exec(ctx, `INSERT INTO page_rollout (tenant_id,page_id,version,digest,payload) VALUES ($1,$2,$3,$4,$5::jsonb) ON CONFLICT (tenant_id,page_id,version) DO NOTHING`, tenantUUID, page, targetVersion, digest, string(encoded))
	if err != nil {
		return fmt.Errorf("pageledgerstore: insert rollout anchor: %w", err)
	}
	var sameTarget bool
	if err := tx.QueryRow(ctx, `SELECT digest=$4 FROM page_rollout WHERE tenant_id=$1 AND page_id=$2 AND version=$3`, tenantUUID, page, targetVersion, digest).Scan(&sameTarget); err != nil {
		return err
	}
	if !sameTarget {
		return fmt.Errorf("pageledgerstore: rollout target conflict for %q version %d", page, targetVersion)
	}
	_, err = tx.Exec(ctx, `INSERT INTO page_rollout_event (tenant_id,page_id,record_version,target_version,digest,payload) VALUES ($1,$2,$3,$4,$5,$6::jsonb) ON CONFLICT (tenant_id,page_id,record_version) DO NOTHING`, tenantUUID, page, recordVersion, targetVersion, digest, string(encoded))
	if err != nil {
		return fmt.Errorf("pageledgerstore: insert rollout: %w", err)
	}
	var same bool
	if err := tx.QueryRow(ctx, `SELECT target_version=$4 AND digest=$5 AND payload=$6::jsonb FROM page_rollout_event WHERE tenant_id=$1 AND page_id=$2 AND record_version=$3`, tenantUUID, page, recordVersion, targetVersion, digest, string(encoded)).Scan(&same); err != nil {
		return err
	}
	if !same {
		return fmt.Errorf("pageledgerstore: immutable rollout conflict for %q record version %d", page, recordVersion)
	}
	return tx.Commit(ctx)
}

func (s *Store) LoadRollouts(ctx context.Context, tenant string) ([]pageledger.RolloutRow, error) {
	tx, tenantUUID, err := s.beginTenant(ctx, tenant)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	rows, err := tx.Query(ctx, `SELECT page_id,record_version,target_version,digest,payload::text FROM page_rollout_event WHERE tenant_id=$1 ORDER BY page_id,record_version`, tenantUUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []pageledger.RolloutRow
	for rows.Next() {
		var page, digest, payload string
		var recordVersion, targetVersion int64
		if err := rows.Scan(&page, &recordVersion, &targetVersion, &digest, &payload); err != nil {
			return nil, err
		}
		result = append(result, pageledger.RolloutRow{Page: page, RecordVersion: recordVersion, TargetVersion: targetVersion, Digest: digest, Payload: []byte(payload)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) PutRetirement(ctx context.Context, tenant string, page string, digest string, encoded []byte) error {
	tx, tenantUUID, err := s.beginTenant(ctx, tenant)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	_, err = tx.Exec(ctx, `INSERT INTO page_retirement (tenant_id,page_id,digest,payload) VALUES ($1,$2,$3,$4::jsonb) ON CONFLICT (tenant_id,page_id) DO NOTHING`, tenantUUID, page, digest, string(encoded))
	if err != nil {
		return fmt.Errorf("pageledgerstore: insert retirement: %w", err)
	}
	var same bool
	if err := tx.QueryRow(ctx, `SELECT digest=$3 AND payload=$4::jsonb FROM page_retirement WHERE tenant_id=$1 AND page_id=$2`, tenantUUID, page, digest, string(encoded)).Scan(&same); err != nil {
		return err
	}
	if !same {
		return fmt.Errorf("pageledgerstore: immutable retirement conflict for %q", page)
	}
	return tx.Commit(ctx)
}

func (s *Store) LoadRetirements(ctx context.Context, tenant string) ([]pageledger.RetirementRow, error) {
	tx, tenantUUID, err := s.beginTenant(ctx, tenant)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	rows, err := tx.Query(ctx, `SELECT page_id,digest,payload::text FROM page_retirement WHERE tenant_id=$1 ORDER BY page_id`, tenantUUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []pageledger.RetirementRow
	for rows.Next() {
		var page, digest, payload string
		if err := rows.Scan(&page, &digest, &payload); err != nil {
			return nil, err
		}
		result = append(result, pageledger.RetirementRow{Page: page, Digest: digest, Payload: []byte(payload)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}
