// Package idempotencystore persists the exactly-once claim records owned
// by INTAPI-005. It owns no execution decisions: the endpoint coordinator
// decides what to execute, while this package supplies tenant-scoped
// PostgreSQL storage where the first claim of a key wins, a repeat with
// the same request digest replays the stored result, and a repeat with a
// different digest is refused. The advisory lock serializes concurrent
// first claims of one key inside the transaction, which is what makes
// exactly-once hold across replicas.
package idempotencystore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// DB is the only capability needed to open the short transactions used by
// a Store. pgx connections, pools and pgtest connections satisfy it.
type DB interface{ dbport.Beginner }

// Store implements the INTAPI-005 durable idempotency boundary.
type Store struct{ db DB }

// New creates an idempotency-key store over a caller-owned database handle.
func New(db DB) *Store { return &Store{db: db} }

// Outcome is what a Claim decides.
type Outcome int

const (
	// OutcomeExecute means this caller won the key and must execute, then
	// record the result with Complete.
	OutcomeExecute Outcome = iota + 1
	// OutcomeReplay means the key completed earlier with the same request
	// digest; Replay carries the original result.
	OutcomeReplay
	// OutcomeInFlight means another execution of the same request is still
	// running; this caller must not execute.
	OutcomeInFlight
)

// Replay is the original result of a completed key.
type Replay struct {
	Result      []byte
	CompletedAt time.Time
}

// Claim decides whether the caller may execute capability with key for the
// request identified by requestDigest. ttl bounds how long the claim lives:
// an expired key is reclaimed and behaves as new.
func (s *Store) Claim(ctx context.Context, tenantID uuid.UUID, capability, key, requestDigest string, ttl time.Duration) (Outcome, Replay, error) {
	if tenantID == uuid.Nil {
		return 0, Replay{}, failure(CodeTenantRequired, "idempotency_key", key, errors.New("tenant is required"))
	}
	if capability == "" || key == "" || requestDigest == "" {
		return 0, Replay{}, failure(CodeInvalid, "idempotency_key", key, errors.New("capability, key and request digest are required"))
	}
	if ttl <= 0 {
		return 0, Replay{}, failure(CodeInvalid, "idempotency_key", key, errors.New("a positive time to live is required"))
	}
	now := time.Now().UTC()
	var outcome Outcome
	var replay Replay
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
			tenantID.String()+":idempotency:"+capability+":"+key); err != nil {
			return failure(CodeDatabase, "idempotency_key", key, err)
		}
		var status, storedDigest string
		var result []byte
		var completedAt *time.Time
		var expiresAt time.Time
		err := tx.QueryRow(ctx, `
			SELECT status, request_digest, result, completed_at, expires_at
			FROM idempotency_key
			WHERE tenant_id=$1 AND capability=$2 AND key=$3`,
			tenantID, capability, key).Scan(&status, &storedDigest, &result, &completedAt, &expiresAt)
		if errors.Is(err, dbport.ErrNoRows) {
			if err := insertClaim(ctx, tx, tenantID, capability, key, requestDigest, now.Add(ttl)); err != nil {
				return err
			}
			outcome = OutcomeExecute
			return nil
		}
		if err != nil {
			return failure(CodeDatabase, "idempotency_key", key, err)
		}
		if !now.Before(expiresAt.UTC()) {
			if _, err := tx.Exec(ctx, `
				DELETE FROM idempotency_key
				WHERE tenant_id=$1 AND capability=$2 AND key=$3`,
				tenantID, capability, key); err != nil {
				return failure(CodeDatabase, "idempotency_key", key, err)
			}
			if err := insertClaim(ctx, tx, tenantID, capability, key, requestDigest, now.Add(ttl)); err != nil {
				return err
			}
			outcome = OutcomeExecute
			return nil
		}
		if storedDigest != requestDigest {
			return failure(CodeConflict, "idempotency_key", key, ErrDigestConflict)
		}
		if status == statusCompleted {
			outcome = OutcomeReplay
			replay = Replay{Result: append([]byte(nil), result...)}
			if completedAt != nil {
				replay.CompletedAt = completedAt.UTC()
			}
			return nil
		}
		outcome = OutcomeInFlight
		return nil
	})
	if err != nil {
		return 0, Replay{}, err
	}
	return outcome, replay, nil
}

func insertClaim(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, capability, key, requestDigest string, expiresAt time.Time) error {
	affected, err := tx.Exec(ctx, `
		INSERT INTO idempotency_key (tenant_id, capability, key, request_digest, status, expires_at)
		VALUES ($1,$2,$3,$4,'in_progress',$5) ON CONFLICT DO NOTHING`,
		tenantID, capability, key, requestDigest, expiresAt)
	if err != nil {
		return failure(CodeDatabase, "idempotency_key", key, err)
	}
	if affected == 0 {
		return failure(CodeDatabase, "idempotency_key", key, errors.New("concurrent claim lost the insert"))
	}
	return nil
}

// Complete records the winner's result and releases the key to replays. It
// refuses unknown keys, keys owned by another tenant (invisible under RLS),
// and keys that already completed: exactly one completion exists per
// execution.
func (s *Store) Complete(ctx context.Context, tenantID uuid.UUID, capability, key string, result []byte) error {
	if tenantID == uuid.Nil {
		return failure(CodeTenantRequired, "idempotency_key", key, errors.New("tenant is required"))
	}
	if capability == "" || key == "" {
		return failure(CodeInvalid, "idempotency_key", key, errors.New("capability and key are required"))
	}
	now := time.Now().UTC()
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
			tenantID.String()+":idempotency:"+capability+":"+key); err != nil {
			return failure(CodeDatabase, "idempotency_key", key, err)
		}
		affected, err := tx.Exec(ctx, `
			UPDATE idempotency_key
			SET status='completed', result=$4, completed_at=$5
			WHERE tenant_id=$1 AND capability=$2 AND key=$3 AND status='in_progress'`,
			tenantID, capability, key, result, now)
		if err != nil {
			return failure(CodeDatabase, "idempotency_key", key, err)
		}
		if affected == 0 {
			return failure(CodeNotFound, "idempotency_key", key, ErrNoClaim)
		}
		return nil
	})
}

func (s *Store) withTenant(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	if s == nil || s.db == nil {
		return failure(CodeInvalid, "store", "", errors.New("database is required"))
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return failure(CodeDatabase, "transaction", "", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return failure(CodeDatabase, "transaction", "", err)
	}
	return nil
}
