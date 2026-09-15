// Package operatorjournal is the durable [operator.Journal] over
// operator_control_receipt (migration 00289). It gives the governed operator
// gateway restart-safe idempotency and an intervention audit trail: a receipt
// recorded before a restart replays after it, and a completed control never
// executes twice.
package operatorjournal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// outcomeAborted marks a pending receipt whose action provably had no effect.
// It is storage-only: Lookup and Begin treat an aborted row as absent.
const outcomeAborted = "ABORTED"

// ErrInvalid reports a missing dependency or malformed receipt.
var ErrInvalid = errors.New("operatorjournal: invalid input")

// ErrNoPending reports a Complete or Abort with no matching pending receipt.
var ErrNoPending = errors.New("operatorjournal: no matching pending receipt")

// TenantIDs maps a tenant key to its storage identity.
type TenantIDs func(values.TenantId) (uuid.UUID, error)

// Journal is the PostgreSQL-backed operator journal.
type Journal struct {
	DB        dbport.Beginner
	TenantIDs TenantIDs
}

var _ operator.Journal = (*Journal)(nil)

func (j *Journal) tx(ctx context.Context, tenant values.TenantId, key string, fn func(dbport.Tx, uuid.UUID) error) error {
	if j == nil || j.DB == nil || j.TenantIDs == nil {
		return fmt.Errorf("%w: database and tenant mapping are required", ErrInvalid)
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("%w: idempotency key is required", ErrInvalid)
	}
	tenantID, err := j.TenantIDs(tenant)
	if err != nil {
		return err
	}
	if tenantID == uuid.Nil {
		return fmt.Errorf("%w: tenant %s has no storage identity", ErrInvalid, tenant)
	}
	tx, err := j.DB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("operatorjournal: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	// Serialize every writer for one key so Begin's read-then-write cannot
	// race a concurrent Begin, Complete or Abort.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 22002))`, tenantID.String()+"|"+key); err != nil {
		return fmt.Errorf("operatorjournal: serialize key: %w", err)
	}
	if err := fn(tx, tenantID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("operatorjournal: commit: %w", err)
	}
	return nil
}

func load(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, key string) (operator.Receipt, string, bool, error) {
	var body []byte
	var outcome string
	err := tx.QueryRow(ctx, `SELECT receipt, outcome FROM operator_control_receipt WHERE tenant_id = $1 AND idempotency_key = $2`,
		tenantID, key).Scan(&body, &outcome)
	if errors.Is(err, dbport.ErrNoRows) {
		return operator.Receipt{}, "", false, nil
	}
	if err != nil {
		return operator.Receipt{}, "", false, fmt.Errorf("operatorjournal: load receipt: %w", err)
	}
	var r operator.Receipt
	if err := json.Unmarshal(body, &r); err != nil {
		return operator.Receipt{}, "", false, fmt.Errorf("operatorjournal: decode receipt: %w", err)
	}
	return r, outcome, true, nil
}

func encode(r operator.Receipt) ([]byte, error) {
	if r.Kind == "" || r.Outcome == "" || r.RequestDigest == "" || r.RecordedAt.IsZero() {
		return nil, fmt.Errorf("%w: receipt needs kind, outcome, request digest and instant", ErrInvalid)
	}
	return json.Marshal(r)
}

// Lookup implements operator.Journal.
func (j *Journal) Lookup(ctx context.Context, tenant values.TenantId, key string) (operator.Receipt, bool, error) {
	var out operator.Receipt
	var found bool
	err := j.tx(ctx, tenant, key, func(tx dbport.Tx, tenantID uuid.UUID) error {
		r, outcome, ok, err := load(ctx, tx, tenantID, key)
		if err != nil {
			return err
		}
		out, found = r, ok && outcome != outcomeAborted
		return nil
	})
	if err != nil || !found {
		return operator.Receipt{}, false, err
	}
	return out, true, nil
}

// Begin implements operator.Journal.
func (j *Journal) Begin(ctx context.Context, pending operator.Receipt) (operator.Receipt, bool, error) {
	body, err := encode(pending)
	if err != nil {
		return operator.Receipt{}, false, err
	}
	var existing operator.Receipt
	var replay bool
	err = j.tx(ctx, pending.Tenant, pending.IdempotencyKey, func(tx dbport.Tx, tenantID uuid.UUID) error {
		prior, outcome, ok, err := load(ctx, tx, tenantID, pending.IdempotencyKey)
		if err != nil {
			return err
		}
		switch {
		case ok && outcome != outcomeAborted:
			existing, replay = prior, true
			return nil
		case ok:
			_, err = tx.Exec(ctx, `UPDATE operator_control_receipt
				SET kind = $3, outcome = $4, request_digest = $5, receipt = $6, recorded_at = $7, updated_at = now()
				WHERE tenant_id = $1 AND idempotency_key = $2 AND outcome = 'ABORTED'`,
				tenantID, pending.IdempotencyKey, string(pending.Kind), string(pending.Outcome), pending.RequestDigest, body, pending.RecordedAt.UTC())
		default:
			_, err = tx.Exec(ctx, `INSERT INTO operator_control_receipt
				(tenant_id, idempotency_key, kind, outcome, request_digest, receipt, recorded_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				tenantID, pending.IdempotencyKey, string(pending.Kind), string(pending.Outcome), pending.RequestDigest, body, pending.RecordedAt.UTC())
		}
		if err != nil {
			return fmt.Errorf("operatorjournal: record pending receipt: %w", err)
		}
		return nil
	})
	if err != nil {
		return operator.Receipt{}, false, err
	}
	if replay {
		return existing, true, nil
	}
	return pending, false, nil
}

// transition moves the pending receipt for final's key, fenced on outcome
// PENDING and the same request digest.
func (j *Journal) transition(ctx context.Context, r operator.Receipt, outcome string, body []byte) error {
	return j.tx(ctx, r.Tenant, r.IdempotencyKey, func(tx dbport.Tx, tenantID uuid.UUID) error {
		n, err := tx.Exec(ctx, `UPDATE operator_control_receipt
			SET outcome = $3, receipt = $4, recorded_at = $5, updated_at = now()
			WHERE tenant_id = $1 AND idempotency_key = $2 AND outcome = $6 AND request_digest = $7`,
			tenantID, r.IdempotencyKey, outcome, body, r.RecordedAt.UTC(), string(operator.OutcomePending), r.RequestDigest)
		if err != nil {
			return fmt.Errorf("operatorjournal: transition receipt: %w", err)
		}
		if n != 1 {
			return fmt.Errorf("%w for %s", ErrNoPending, r.IdempotencyKey)
		}
		return nil
	})
}

// Complete implements operator.Journal.
func (j *Journal) Complete(ctx context.Context, final operator.Receipt) error {
	body, err := encode(final)
	if err != nil {
		return err
	}
	return j.transition(ctx, final, string(final.Outcome), body)
}

// Abort implements operator.Journal.
func (j *Journal) Abort(ctx context.Context, pending operator.Receipt) error {
	body, err := encode(pending)
	if err != nil {
		return err
	}
	return j.transition(ctx, pending, outcomeAborted, body)
}
