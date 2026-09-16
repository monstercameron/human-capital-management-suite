package operatorjournal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrNoOutstandingObligation reports a discharge against an obligation that
// is unknown or already reviewed.
var ErrNoOutstandingObligation = errors.New("operatorjournal: no outstanding obligation")

// The journal is also the durable bypass-obligation store (WF-RUN-039): a
// bypass and the debt it leaves behind live in one journal, so a composition
// wires one dependency and a bypass recorded before a restart is still
// outstanding after it.
var _ operator.ObligationStore = (*Journal)(nil)

// RecordObligation implements operator.ObligationStore over
// operator_bypass_obligation (migration 00310). Recording the same obligation
// id twice is the same obligation and changes nothing.
func (j *Journal) RecordObligation(ctx context.Context, o operator.Obligation) error {
	if err := o.Verify(); err != nil {
		return err
	}
	if strings.TrimSpace(o.ID) == "" || strings.TrimSpace(o.IdempotencyKey) == "" ||
		strings.TrimSpace(o.Operator) == "" || strings.TrimSpace(o.Approver) == "" ||
		o.RecordedAt.IsZero() || !o.DueAt.After(o.RecordedAt) {
		return fmt.Errorf("%w: an obligation names its id, key, operator, approver and a due review after it was recorded", ErrInvalid)
	}
	body, err := json.Marshal(o)
	if err != nil {
		return fmt.Errorf("operatorjournal: encode obligation: %w", err)
	}
	return j.tx(ctx, o.Tenant, o.ID, func(tx dbport.Tx, tenantID uuid.UUID) error {
		if _, err := tx.Exec(ctx, `INSERT INTO operator_bypass_obligation
			(tenant_id, obligation_id, idempotency_key, kind, family, operator_ref, approver_ref, obligation, recorded_at, due_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (tenant_id, obligation_id) DO NOTHING`,
			tenantID, o.ID, o.IdempotencyKey, string(o.Kind), string(o.Family), o.Operator, o.Approver,
			body, o.RecordedAt.UTC(), o.DueAt.UTC()); err != nil {
			return fmt.Errorf("operatorjournal: record obligation: %w", err)
		}
		return nil
	})
}

// OutstandingObligations implements operator.ObligationStore. It returns every
// undischarged obligation of tenant, oldest due date first.
func (j *Journal) OutstandingObligations(ctx context.Context, tenant values.TenantId) ([]operator.Obligation, error) {
	var out []operator.Obligation
	err := j.tx(ctx, tenant, outstandingLockKey, func(tx dbport.Tx, tenantID uuid.UUID) error {
		rows, err := tx.Query(ctx, `SELECT obligation FROM operator_bypass_obligation
			WHERE tenant_id = $1 AND reviewed_at IS NULL ORDER BY due_at, obligation_id`, tenantID)
		if err != nil {
			return fmt.Errorf("operatorjournal: load outstanding obligations: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var body []byte
			if err := rows.Scan(&body); err != nil {
				return fmt.Errorf("operatorjournal: scan obligation: %w", err)
			}
			var o operator.Obligation
			if err := json.Unmarshal(body, &o); err != nil {
				return fmt.Errorf("operatorjournal: decode obligation: %w", err)
			}
			// A stored obligation whose content no longer matches its sealed
			// digest is evidence of tampering, not an obligation.
			if err := o.Verify(); err != nil {
				return err
			}
			out = append(out, o)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// outstandingLockKey serializes a tenant's obligation reads against nothing in
// particular; j.tx demands a non-blank key for its per-key advisory lock.
const outstandingLockKey = "operator-bypass-obligations"

// DischargeObligation implements operator.ObligationStore. It records review
// against the still-outstanding obligation id and returns the discharged
// obligation. A second discharge, or one against an unknown id, changes
// nothing and reports [ErrNoOutstandingObligation].
func (j *Journal) DischargeObligation(ctx context.Context, tenant values.TenantId, id string, review operator.ObligationReview) (operator.Obligation, error) {
	var out operator.Obligation
	err := j.tx(ctx, tenant, id, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var body []byte
		err := tx.QueryRow(ctx, `SELECT obligation FROM operator_bypass_obligation
			WHERE tenant_id = $1 AND obligation_id = $2 AND reviewed_at IS NULL`, tenantID, id).Scan(&body)
		if errors.Is(err, dbport.ErrNoRows) {
			return fmt.Errorf("%w %s", ErrNoOutstandingObligation, id)
		}
		if err != nil {
			return fmt.Errorf("operatorjournal: load obligation: %w", err)
		}
		var o operator.Obligation
		if err := json.Unmarshal(body, &o); err != nil {
			return fmt.Errorf("operatorjournal: decode obligation: %w", err)
		}
		if err := o.Verify(); err != nil {
			return err
		}
		// The reviewer rules are the operator kernel's, not this package's:
		// Discharged applies them and returns the sealed result, or refuses.
		discharged, err := o.Discharged(review)
		if err != nil {
			return err
		}
		sealedBody, err := json.Marshal(discharged)
		if err != nil {
			return fmt.Errorf("operatorjournal: encode discharged obligation: %w", err)
		}
		n, err := tx.Exec(ctx, `UPDATE operator_bypass_obligation
			SET obligation = $3, reviewed_at = $4, review_outcome = $5, reviewer_ref = $6, updated_at = now()
			WHERE tenant_id = $1 AND obligation_id = $2 AND reviewed_at IS NULL`,
			tenantID, id, sealedBody, review.At.UTC(), string(review.Outcome), strings.TrimSpace(review.Reviewer))
		if err != nil {
			return fmt.Errorf("operatorjournal: discharge obligation: %w", err)
		}
		if n != 1 {
			return fmt.Errorf("%w %s", ErrNoOutstandingObligation, id)
		}
		out = discharged
		return nil
	})
	if err != nil {
		return operator.Obligation{}, err
	}
	return out, nil
}
