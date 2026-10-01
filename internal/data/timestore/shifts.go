package timestore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ShiftStatus is the crew shift aggregate's lifecycle state (FTIME-006).
type ShiftStatus string

const (
	ShiftDraft     ShiftStatus = "DRAFT"
	ShiftPublished ShiftStatus = "PUBLISHED"
	ShiftCancelled ShiftStatus = "CANCELLED"
)

// ShiftHistoryKind discriminates one append-only crew_shift_history row.
type ShiftHistoryKind string

const (
	ShiftHistoryDrafted    ShiftHistoryKind = "DRAFTED"
	ShiftHistoryPublished  ShiftHistoryKind = "PUBLISHED"
	ShiftHistoryCancelled  ShiftHistoryKind = "CANCELLED"
	ShiftHistoryReassigned ShiftHistoryKind = "REASSIGNED"
)

// Shift is the draft/published crew shift aggregate row (FTIME-006/007).
type Shift struct {
	TenantID   string
	ID         string
	WorkerRef  string
	SiteRef    string
	ProjectRef string
	Revision   int64
	Status     ShiftStatus
	WorkStart  time.Time
	WorkEnd    time.Time
	Payload    json.RawMessage
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ShiftHistoryEntry is one immutable prior version of a shift.
type ShiftHistoryEntry struct {
	TenantID       string
	ID             string
	ShiftID        string
	Revision       int64
	Kind           ShiftHistoryKind
	ApprovedBy     string
	IdempotencyKey string
	Payload        json.RawMessage
	CreatedAt      time.Time
}

// CreateShift opens a new draft shift at revision 1 and records its first
// history entry.
func (s *Store) CreateShift(ctx context.Context, tenant string, sh Shift, idempotencyKey string) error {
	if tenant == "" || sh.TenantID != tenant || sh.ID == "" || sh.WorkerRef == "" || sh.SiteRef == "" || sh.ProjectRef == "" ||
		sh.Status != ShiftDraft || sh.WorkStart.IsZero() || sh.WorkEnd.IsZero() || !sh.WorkEnd.After(sh.WorkStart) || idempotencyKey == "" {
		return ErrInvalid
	}
	payload := sh.Payload
	if payload == nil {
		payload = json.RawMessage(`{}`)
	}
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO crew_shift(tenant_id,id,worker_ref,site_ref,project_ref,revision,status,work_start,work_end,payload) VALUES($1,$2,$3,$4,$5,1,$6,$7,$8,$9::jsonb)`,
			tenant, sh.ID, sh.WorkerRef, sh.SiteRef, sh.ProjectRef, string(ShiftDraft), sh.WorkStart, sh.WorkEnd, []byte(payload))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO crew_shift_history(tenant_id,id,shift_id,revision,kind,idempotency_key,payload) VALUES($1,$2,$3,1,$4,$5,$6::jsonb)`,
			tenant, uuid.NewString(), sh.ID, string(ShiftHistoryDrafted), idempotencyKey, []byte(payload))
		return err
	})
}

// GetShift reads the current aggregate row.
func (s *Store) GetShift(ctx context.Context, tenant, id string) (Shift, error) {
	if tenant == "" || id == "" {
		return Shift{}, ErrInvalid
	}
	var sh Shift
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return s.loadShiftLocked(ctx, tx, tenant, id, &sh)
	})
	if err != nil {
		return Shift{}, err
	}
	return sh, nil
}

func (s *Store) loadShiftLocked(ctx context.Context, tx dbport.Tx, tenant, id string, out *Shift) error {
	var status string
	var payload []byte
	row := tx.QueryRow(ctx, `SELECT tenant_id,id,worker_ref,site_ref,project_ref,revision,status,work_start,work_end,payload,created_at,updated_at FROM crew_shift WHERE tenant_id=$1 AND id=$2`, tenant, id)
	if err := row.Scan(&out.TenantID, &out.ID, &out.WorkerRef, &out.SiteRef, &out.ProjectRef, &out.Revision, &status, &out.WorkStart, &out.WorkEnd, &payload, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	out.Status = ShiftStatus(status)
	out.Payload = append(json.RawMessage(nil), payload...)
	return nil
}

// PublishShift, CancelShift and ReassignShift all go through this one
// revision-guarded mutation: expected must match the aggregate's current
// revision, the mutation supplies the new status, worker (for a reassign)
// and payload, and every call is idempotent per idempotencyKey so a retried
// publish is never double-applied. Prior versions are retained append-only
// in crew_shift_history; nothing already written there is ever touched.
func (s *Store) mutateShift(ctx context.Context, tenant, id string, expected int64, kind ShiftHistoryKind, approvedBy, idempotencyKey string, mutate func(Shift) (Shift, error)) (Shift, error) {
	if tenant == "" || id == "" || expected <= 0 || idempotencyKey == "" || mutate == nil {
		return Shift{}, ErrInvalid
	}
	var out Shift
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var existingRevision int64
		err := tx.QueryRow(ctx, `SELECT revision FROM crew_shift_history WHERE tenant_id=$1 AND shift_id=$2 AND kind=$3 AND idempotency_key=$4`, tenant, id, string(kind), idempotencyKey).Scan(&existingRevision)
		if err == nil {
			return s.loadShiftLocked(ctx, tx, tenant, id, &out)
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		var current Shift
		if err := s.loadShiftLocked(ctx, tx, tenant, id, &current); err != nil {
			return err
		}
		if current.Revision != expected {
			return ErrRevisionConflict
		}
		result, err := mutate(current)
		if err != nil {
			return err
		}
		result.Revision = expected + 1
		payload := result.Payload
		if payload == nil {
			payload = json.RawMessage(`{}`)
		}
		tag, err := tx.Exec(ctx, `UPDATE crew_shift SET worker_ref=$1,status=$2,work_start=$3,work_end=$4,payload=$5::jsonb,revision=$6,updated_at=now() WHERE tenant_id=$7 AND id=$8 AND revision=$9`,
			result.WorkerRef, string(result.Status), result.WorkStart, result.WorkEnd, []byte(payload), result.Revision, tenant, id, expected)
		if err != nil {
			return err
		}
		if tag != 1 {
			return ErrRevisionConflict
		}
		_, err = tx.Exec(ctx, `INSERT INTO crew_shift_history(tenant_id,id,shift_id,revision,kind,approved_by,idempotency_key,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`,
			tenant, uuid.NewString(), id, result.Revision, string(kind), approvedBy, idempotencyKey, []byte(payload))
		if err != nil {
			return err
		}
		return s.loadShiftLocked(ctx, tx, tenant, id, &out)
	})
	if err != nil {
		return Shift{}, err
	}
	return out, nil
}

// PublishShift checks the caller's mutate closure result and records who
// approved it. The closure is expected to have already run eligibility,
// qualification, project access, overlap, rest and notice-policy checks
// (FTIME-006 GREEN); this store only enforces the revision and identity of
// the approver.
func (s *Store) PublishShift(ctx context.Context, tenant, id string, expected int64, approvedBy, idempotencyKey string, mutate func(Shift) (Shift, error)) (Shift, error) {
	if approvedBy == "" {
		return Shift{}, ErrInvalid
	}
	return s.mutateShift(ctx, tenant, id, expected, ShiftHistoryPublished, approvedBy, idempotencyKey, func(cur Shift) (Shift, error) {
		result, err := mutate(cur)
		if err != nil {
			return Shift{}, err
		}
		result.Status = ShiftPublished
		return result, nil
	})
}

// CancelShift marks the shift cancelled while retaining every prior
// version.
func (s *Store) CancelShift(ctx context.Context, tenant, id string, expected int64, approvedBy, idempotencyKey string) (Shift, error) {
	if approvedBy == "" {
		return Shift{}, ErrInvalid
	}
	return s.mutateShift(ctx, tenant, id, expected, ShiftHistoryCancelled, approvedBy, idempotencyKey, func(cur Shift) (Shift, error) {
		cur.Status = ShiftCancelled
		return cur, nil
	})
}

// ReassignShift moves a published or draft shift to a new worker.
func (s *Store) ReassignShift(ctx context.Context, tenant, id string, expected int64, newWorkerRef, approvedBy, idempotencyKey string) (Shift, error) {
	if approvedBy == "" || newWorkerRef == "" {
		return Shift{}, ErrInvalid
	}
	return s.mutateShift(ctx, tenant, id, expected, ShiftHistoryReassigned, approvedBy, idempotencyKey, func(cur Shift) (Shift, error) {
		cur.WorkerRef = newWorkerRef
		return cur, nil
	})
}

// ShiftHistory returns every retained version of id, oldest first.
func (s *Store) ShiftHistory(ctx context.Context, tenant, id string) ([]ShiftHistoryEntry, error) {
	if tenant == "" || id == "" {
		return nil, ErrInvalid
	}
	out := make([]ShiftHistoryEntry, 0)
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id,id,shift_id,revision,kind,approved_by,idempotency_key,payload,created_at FROM crew_shift_history WHERE tenant_id=$1 AND shift_id=$2 ORDER BY revision`, tenant, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e ShiftHistoryEntry
			var kind string
			var payload []byte
			if err := rows.Scan(&e.TenantID, &e.ID, &e.ShiftID, &e.Revision, &kind, &e.ApprovedBy, &e.IdempotencyKey, &payload, &e.CreatedAt); err != nil {
				return err
			}
			e.Kind = ShiftHistoryKind(kind)
			e.Payload = append(json.RawMessage(nil), payload...)
			out = append(out, e)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
