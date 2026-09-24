// Package rulepayloadstore persists immutable workflow rule and transform
// program bodies with tenant isolation and digest verification.
package rulepayloadstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/rulepayload"
)

var (
	ErrInvalid        = errors.New("rulepayloadstore: invalid payload")
	ErrNotFound       = errors.New("rulepayloadstore: payload not found")
	ErrImmutable      = errors.New("rulepayloadstore: published version is immutable")
	ErrDigestMismatch = errors.New("rulepayloadstore: payload digest mismatch")
)

type DB interface{ dbport.Beginner }

type Store struct{ db DB }

func New(db DB) *Store { return &Store{db: db} }

// TenantResolver adapts the durable store to workflow compilation and
// simulation while keeping tenant selection explicit at composition time.
type TenantResolver struct {
	store    *Store
	tenantID uuid.UUID
	ctx      context.Context
}

func (s *Store) ForTenant(ctx context.Context, tenantID uuid.UUID) TenantResolver {
	return TenantResolver{store: s, tenantID: tenantID, ctx: ctx}
}

func (r TenantResolver) Resolve(ref workflow.Reference, digest string) (rulepayload.Payload, error) {
	if r.store == nil {
		return rulepayload.Payload{}, ErrInvalid
	}
	return r.store.Resolve(r.ctx, r.tenantID, ref, digest)
}

func (r TenantResolver) ResolveReference(ref workflow.Reference) (workflow.ResolvedReference, bool) {
	if r.store == nil {
		return workflow.ResolvedReference{}, false
	}
	var resolved workflow.ResolvedReference
	err := r.store.withTenant(r.ctx, r.tenantID, func(tx dbport.Tx) error {
		var digest string
		var body []byte
		if err := tx.QueryRow(r.ctx, `SELECT digest, payload FROM workflow_executable_payload
			WHERE tenant_id=$1 AND reference_kind=$2 AND reference_id=$3 AND reference_version=$4`,
			r.tenantID, ref.Kind, ref.ID, ref.Version).Scan(&digest, &body); err != nil {
			return err
		}
		payload, err := rulepayload.Unmarshal(body)
		if err != nil || payload.Ref != ref || payload.Digest != digest {
			return ErrDigestMismatch
		}
		resolved = workflow.ResolvedReference{Kind: ref.Kind, ID: ref.ID, Version: ref.Version, Digest: digest, Status: payload.Status}
		return nil
	})
	return resolved, err == nil
}

var _ workflow.ReferenceResolver = TenantResolver{}

// Publish appends one executable under its tenant-scoped exact reference.
// Republishing the same payload is idempotent; changing its body, kind or
// lifecycle status under the same version is refused.
func (s *Store) Publish(ctx context.Context, tenantID uuid.UUID, payload rulepayload.Payload) error {
	if s == nil || s.db == nil || tenantID == uuid.Nil {
		return ErrInvalid
	}
	body, err := rulepayload.Marshal(payload)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	validated, err := rulepayload.Unmarshal(body)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO workflow_executable_payload
			(tenant_id, reference_kind, reference_id, reference_version, payload_kind, digest, payload)
			VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb) ON CONFLICT DO NOTHING`,
			tenantID, validated.Ref.Kind, validated.Ref.ID, validated.Ref.Version, validated.Kind, validated.Digest, string(body)); err != nil {
			return fmt.Errorf("rulepayloadstore: publish payload: %w", err)
		}
		var storedBody []byte
		var storedDigest string
		if err := tx.QueryRow(ctx, `SELECT digest, payload FROM workflow_executable_payload
			WHERE tenant_id=$1 AND reference_kind=$2 AND reference_id=$3 AND reference_version=$4`,
			tenantID, validated.Ref.Kind, validated.Ref.ID, validated.Ref.Version).Scan(&storedDigest, &storedBody); err != nil {
			return fmt.Errorf("rulepayloadstore: verify publication: %w", err)
		}
		stored, err := rulepayload.Unmarshal(storedBody)
		if err != nil {
			return fmt.Errorf("%w: stored bytes fail validation: %v", ErrDigestMismatch, err)
		}
		if storedDigest != validated.Digest || stored.Kind != validated.Kind || stored.Status != validated.Status {
			return fmt.Errorf("%w: %s@%s already resolves to another payload", ErrImmutable, validated.Ref.ID, validated.Ref.Version)
		}
		return nil
	})
}

// Resolve returns the exact stored body only when the requested digest matches.
func (s *Store) Resolve(ctx context.Context, tenantID uuid.UUID, ref workflow.Reference, digest string) (rulepayload.Payload, error) {
	if s == nil || s.db == nil || tenantID == uuid.Nil || ref.ID == "" || ref.Version == "" || digest == "" {
		return rulepayload.Payload{}, ErrInvalid
	}
	var payload rulepayload.Payload
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		var storedDigest string
		var body []byte
		if err := tx.QueryRow(ctx, `SELECT digest, payload FROM workflow_executable_payload
			WHERE tenant_id=$1 AND reference_kind=$2 AND reference_id=$3 AND reference_version=$4`,
			tenantID, ref.Kind, ref.ID, ref.Version).Scan(&storedDigest, &body); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("rulepayloadstore: resolve payload: %w", err)
		}
		if storedDigest != digest {
			return ErrDigestMismatch
		}
		decoded, err := rulepayload.Unmarshal(body)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrDigestMismatch, err)
		}
		if decoded.Ref != ref || decoded.Digest != digest {
			return ErrDigestMismatch
		}
		payload = decoded
		return nil
	})
	return payload, err
}

func (s *Store) withTenant(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("rulepayloadstore: begin: %w", err)
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
		_ = tx.Rollback(ctx)
		return fmt.Errorf("rulepayloadstore: commit: %w", err)
	}
	return nil
}
