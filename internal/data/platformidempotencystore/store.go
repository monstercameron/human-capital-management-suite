// Package platformidempotencystore persists the cross-layer idempotency
// lifecycle used by ingress and effect adapters. It delegates lifecycle
// decisions to the pure platform/idempotency kernel and owns only tenant-bound
// PostgreSQL durability.
package platformidempotencystore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/idempotency"
)

// DB is the transaction capability required by the store.
type DB interface{ dbport.Beginner }

// PostgresStore implements idempotency.Store. Every operation opens a short
// transaction and establishes the tenant with SET LOCAL before accessing rows.
type PostgresStore struct{ db DB }

func New(db DB) *PostgresStore { return &PostgresStore{db: db} }

func (s *PostgresStore) Reserve(req idempotency.Request) (idempotency.Resolution, error) {
	if s == nil || s.db == nil {
		return idempotency.Resolution{}, errors.New("platformidempotencystore: database is required")
	}
	tenant, err := uuid.Parse(req.Identity.Tenant)
	if err != nil || tenant == uuid.Nil {
		return idempotency.Resolution{}, idempotency.ErrInvalidRequest
	}
	var resolution idempotency.Resolution
	var decisionErr error
	err = s.withTenant(tenant, func(ctx context.Context, tx dbport.Tx) error {
		if err := lockIdentity(ctx, tx, req.Identity); err != nil {
			return err
		}
		old, found, err := lookup(ctx, tx, req.Identity)
		if err != nil {
			return err
		}
		registry := idempotency.NewRegistry()
		if found {
			registry = idempotency.NewRegistryWithRecord(old)
		}
		resolution, decisionErr = registry.Reserve(req)
		// The pure kernel reports the prior row on conflicts and the advanced
		// expiry state on protected reuse. Persisting the returned state before
		// returning its error makes expiry decisions durable too.
		if resolution.Record.Identity.Tenant != "" {
			if err := writeRecord(ctx, tx, resolution.Record); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return idempotency.Resolution{}, err
	}
	return resolution, decisionErr
}

func (s *PostgresStore) Complete(identity idempotency.Identity, digest, resultRef, effectRef string, now time.Time) (idempotency.Record, error) {
	if s == nil || s.db == nil {
		return idempotency.Record{}, errors.New("platformidempotencystore: database is required")
	}
	tenant, err := uuid.Parse(identity.Tenant)
	if err != nil || tenant == uuid.Nil {
		return idempotency.Record{}, idempotency.ErrInvalidRequest
	}
	var completed idempotency.Record
	err = s.withTenant(tenant, func(ctx context.Context, tx dbport.Tx) error {
		if err := lockIdentity(ctx, tx, identity); err != nil {
			return err
		}
		old, found, err := lookup(ctx, tx, identity)
		if err != nil {
			return err
		}
		if !found {
			return idempotency.ErrNotFound
		}
		registry := idempotency.NewRegistryWithRecord(old)
		completed, err = registry.Complete(identity, digest, resultRef, effectRef, now)
		if err != nil {
			return err
		}
		return writeRecord(ctx, tx, completed)
	})
	return completed, err
}

func (s *PostgresStore) Lookup(identity idempotency.Identity) (idempotency.Record, error) {
	if s == nil || s.db == nil {
		return idempotency.Record{}, errors.New("platformidempotencystore: database is required")
	}
	tenant, err := uuid.Parse(identity.Tenant)
	if err != nil || tenant == uuid.Nil {
		return idempotency.Record{}, idempotency.ErrInvalidRequest
	}
	var record idempotency.Record
	err = s.withTenant(tenant, func(ctx context.Context, tx dbport.Tx) error {
		var found bool
		var lookupErr error
		record, found, lookupErr = lookup(ctx, tx, identity)
		if lookupErr != nil {
			return lookupErr
		}
		if !found {
			return idempotency.ErrNotFound
		}
		return nil
	})
	return record, err
}

// ExpireTenant reclaims expired non-tombstone rows and compacts protected
// records. The caller supplies the policy-owned tenant and cutoff; the store
// never reads a wall clock or sweeps outside that tenant's RLS scope.
func (s *PostgresStore) ExpireTenant(tenantText string, cutoff time.Time) (int64, error) {
	tenant, err := uuid.Parse(tenantText)
	if err != nil || tenant == uuid.Nil || cutoff.IsZero() {
		return 0, idempotency.ErrInvalidRequest
	}
	var count int64
	err = s.withTenant(tenant, func(ctx context.Context, tx dbport.Tx) error {
		deleted, err := tx.Exec(ctx, `DELETE FROM platform_idempotency_record
			WHERE tenant_id=$1 AND expires_at <= $2 AND expiry_mode='ALLOW_REUSE' AND tombstone=false`, tenant, cutoff.UTC())
		if err != nil {
			return fmt.Errorf("platformidempotencystore: delete expired rows: %w", err)
		}
		compacted, err := tx.Exec(ctx, `UPDATE platform_idempotency_record SET
			state=CASE WHEN tombstone THEN 'TOMBSTONE' ELSE 'EXPIRED' END,
			execution_ref='', result_ref='', effect_ref=''
			WHERE tenant_id=$1 AND expires_at <= $2 AND state IN ('IN_PROGRESS','COMPLETED')`, tenant, cutoff.UTC())
		if err != nil {
			return fmt.Errorf("platformidempotencystore: compact expired rows: %w", err)
		}
		count = deleted + compacted
		return nil
	})
	return count, err
}

func (s *PostgresStore) withTenant(tenant uuid.UUID, fn func(context.Context, dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("platformidempotencystore: begin: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("platformidempotencystore: commit: %w", err)
	}
	return nil
}

func lockIdentity(ctx context.Context, tx dbport.Tx, identity idempotency.Identity) error {
	lockKey := fmt.Sprintf("platform-idempotency:%s:%s:%s:%s:%s", identity.Tenant, identity.Capability, identity.EffectScope, identity.Key, identity.Principal)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return fmt.Errorf("platformidempotencystore: lock identity: %w", err)
	}
	return nil
}

func lookup(ctx context.Context, tx dbport.Tx, identity idempotency.Identity) (idempotency.Record, bool, error) {
	var r idempotency.Record
	var state, mode string
	err := tx.QueryRow(ctx, `SELECT tenant_id::text, capability, effect_scope, idempotency_key, principal,
		request_digest, state, layer, execution_ref, result_ref, effect_ref, created_at, expires_at,
		replay_count, tombstone, expiry_mode
		FROM platform_idempotency_record WHERE tenant_id=$1 AND capability=$2 AND effect_scope=$3
		AND idempotency_key=$4 AND principal=$5`, identity.Tenant, identity.Capability, identity.EffectScope, identity.Key, identity.Principal).
		Scan(&r.Identity.Tenant, &r.Identity.Capability, &r.Identity.EffectScope, &r.Identity.Key, &r.Identity.Principal,
			&r.RequestDigest, &state, &r.Layer, &r.ExecutionRef, &r.ResultRef, &r.EffectRef,
			&r.CreatedAt, &r.ExpiresAt, &r.ReplayCount, &r.Tombstone, &mode)
	if errors.Is(err, dbport.ErrNoRows) {
		return idempotency.Record{}, false, nil
	}
	if err != nil {
		return idempotency.Record{}, false, fmt.Errorf("platformidempotencystore: read record: %w", err)
	}
	r.State = idempotency.State(state)
	r.ExpiryMode = idempotency.ExpiryMode(mode)
	r.CreatedAt = r.CreatedAt.UTC()
	r.ExpiresAt = r.ExpiresAt.UTC()
	return r, true, nil
}

func writeRecord(ctx context.Context, tx dbport.Tx, r idempotency.Record) error {
	mode := r.ExpiryMode
	if mode == "" {
		mode = idempotency.RejectReuse
	}
	_, err := tx.Exec(ctx, `INSERT INTO platform_idempotency_record
		(tenant_id, capability, effect_scope, idempotency_key, principal, request_digest, state,
		 layer, execution_ref, result_ref, effect_ref, created_at, expires_at, replay_count, expiry_mode, tombstone)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (tenant_id, capability, effect_scope, idempotency_key, principal) DO UPDATE SET
		request_digest=EXCLUDED.request_digest, state=EXCLUDED.state, layer=EXCLUDED.layer,
		execution_ref=EXCLUDED.execution_ref, result_ref=EXCLUDED.result_ref, effect_ref=EXCLUDED.effect_ref,
		created_at=EXCLUDED.created_at, expires_at=EXCLUDED.expires_at, replay_count=EXCLUDED.replay_count,
		expiry_mode=EXCLUDED.expiry_mode, tombstone=EXCLUDED.tombstone`,
		r.Identity.Tenant, r.Identity.Capability, r.Identity.EffectScope, r.Identity.Key, r.Identity.Principal,
		r.RequestDigest, string(r.State), r.Layer, r.ExecutionRef, r.ResultRef, r.EffectRef,
		r.CreatedAt.UTC(), r.ExpiresAt.UTC(), r.ReplayCount, string(mode), r.Tombstone)
	if err != nil {
		return fmt.Errorf("platformidempotencystore: write record: %w", err)
	}
	return nil
}

var _ idempotency.Store = (*PostgresStore)(nil)
