package timestore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// WorkOrderAllocation is one approved-time allocation row keyed to the
// timecard revision it was approved at (FTIME-005). A correction never
// rewrites an existing allocation: it allocates a delta against the
// corrected timecard's new revision, so every allocation ever recorded
// stays exactly as it was approved.
type WorkOrderAllocation struct {
	TenantID         string
	ID               string
	TimecardID       string
	TimecardRevision int64
	AllocationKey    string
	WorkOrderRef     string
	ProjectRef       string
	Minutes          int64
	SourceRef        string
	PayloadDigest    string
	Payload          json.RawMessage
	CreatedAt        time.Time
}

// Allocate writes one allocation row idempotently: the same
// (timecard, timecard revision, allocation key) with the same payload
// digest always returns the original row unchanged; the same key with a
// different digest is ErrIdempotencyConflict, which is what keeps one
// approved minute from ever being charged to two different amounts under
// the same key.
func (s *Store) Allocate(ctx context.Context, tenant string, a WorkOrderAllocation) (WorkOrderAllocation, error) {
	if tenant == "" || a.TenantID != tenant || a.ID == "" || a.TimecardID == "" || a.TimecardRevision <= 0 ||
		a.AllocationKey == "" || a.WorkOrderRef == "" || a.ProjectRef == "" || a.Minutes < 0 || a.PayloadDigest == "" {
		return WorkOrderAllocation{}, ErrInvalid
	}
	payload := a.Payload
	if payload == nil {
		payload = json.RawMessage(`{}`)
	}
	var out WorkOrderAllocation
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var existingID, existingDigest string
		err := tx.QueryRow(ctx, `SELECT id,payload_digest FROM work_order_allocation WHERE tenant_id=$1 AND timecard_id=$2 AND timecard_revision=$3 AND allocation_key=$4`,
			tenant, a.TimecardID, a.TimecardRevision, a.AllocationKey).Scan(&existingID, &existingDigest)
		if err == nil {
			if existingDigest != a.PayloadDigest {
				return ErrIdempotencyConflict
			}
			return s.loadAllocationLocked(ctx, tx, tenant, existingID, &out)
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO work_order_allocation(tenant_id,id,timecard_id,timecard_revision,allocation_key,work_order_ref,project_ref,minutes,source_ref,payload_digest,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb)`,
			tenant, a.ID, a.TimecardID, a.TimecardRevision, a.AllocationKey, a.WorkOrderRef, a.ProjectRef, a.Minutes, a.SourceRef, a.PayloadDigest, []byte(payload))
		if err != nil {
			return err
		}
		return s.loadAllocationLocked(ctx, tx, tenant, a.ID, &out)
	})
	if err != nil {
		return WorkOrderAllocation{}, err
	}
	return out, nil
}

func (s *Store) loadAllocationLocked(ctx context.Context, tx dbport.Tx, tenant, id string, out *WorkOrderAllocation) error {
	var payload []byte
	row := tx.QueryRow(ctx, `SELECT tenant_id,id,timecard_id,timecard_revision,allocation_key,work_order_ref,project_ref,minutes,source_ref,payload_digest,payload,created_at FROM work_order_allocation WHERE tenant_id=$1 AND id=$2`, tenant, id)
	return row.Scan(&out.TenantID, &out.ID, &out.TimecardID, &out.TimecardRevision, &out.AllocationKey, &out.WorkOrderRef, &out.ProjectRef, &out.Minutes, &out.SourceRef, &out.PayloadDigest, &payload, &out.CreatedAt)
}

// AllocationsForWorkOrder returns every allocation charged against
// workOrderRef, letting a caller reconcile a work order's total without
// re-deriving it from timecards.
func (s *Store) AllocationsForWorkOrder(ctx context.Context, tenant, workOrderRef string) ([]WorkOrderAllocation, error) {
	if tenant == "" || workOrderRef == "" {
		return nil, ErrInvalid
	}
	out := make([]WorkOrderAllocation, 0)
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id,id,timecard_id,timecard_revision,allocation_key,work_order_ref,project_ref,minutes,source_ref,payload_digest,payload,created_at FROM work_order_allocation WHERE tenant_id=$1 AND work_order_ref=$2 ORDER BY created_at`, tenant, workOrderRef)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a WorkOrderAllocation
			var payload []byte
			if err := rows.Scan(&a.TenantID, &a.ID, &a.TimecardID, &a.TimecardRevision, &a.AllocationKey, &a.WorkOrderRef, &a.ProjectRef, &a.Minutes, &a.SourceRef, &a.PayloadDigest, &payload, &a.CreatedAt); err != nil {
				return err
			}
			a.Payload = append(json.RawMessage(nil), payload...)
			out = append(out, a)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AllocationsForTimecardRevision returns every allocation charged against
// one exact timecard revision, which is what a corrected timecard's new
// revision uses to compute its delta against the prior revision's total.
func (s *Store) AllocationsForTimecardRevision(ctx context.Context, tenant, timecardID string, revision int64) ([]WorkOrderAllocation, error) {
	if tenant == "" || timecardID == "" || revision <= 0 {
		return nil, ErrInvalid
	}
	out := make([]WorkOrderAllocation, 0)
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id,id,timecard_id,timecard_revision,allocation_key,work_order_ref,project_ref,minutes,source_ref,payload_digest,payload,created_at FROM work_order_allocation WHERE tenant_id=$1 AND timecard_id=$2 AND timecard_revision=$3 ORDER BY created_at`, tenant, timecardID, revision)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a WorkOrderAllocation
			var payload []byte
			if err := rows.Scan(&a.TenantID, &a.ID, &a.TimecardID, &a.TimecardRevision, &a.AllocationKey, &a.WorkOrderRef, &a.ProjectRef, &a.Minutes, &a.SourceRef, &a.PayloadDigest, &payload, &a.CreatedAt); err != nil {
				return err
			}
			a.Payload = append(json.RawMessage(nil), payload...)
			out = append(out, a)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
