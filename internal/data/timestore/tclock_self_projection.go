package timestore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// SelfProjection is the durable CAS state for a worker's own clock.
type SelfProjection struct {
	TenantID, WorkerRef, AssignmentRef string
	Revision                           int64
	StatusCode                         string
	LastEventAt                        time.Time
}

// EnsureSelfClockProjection creates the initial revision from current durable
// session and observation facts after worker/profile authority has resolved.
// ON CONFLICT preserves an existing projection and its CAS history.
func EnsureSelfClockProjection(ctx context.Context, tx dbport.Tx, tenant, worker, assignment string) error {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(worker) == "" || strings.TrimSpace(assignment) == "" {
		return ErrInvalid
	}
	var scopedTenant string
	if err := tx.QueryRow(ctx, `SELECT current_setting('hcmnext.tenant_id', true)`).Scan(&scopedTenant); err != nil {
		return err
	}
	if scopedTenant != tenant {
		return ErrInvalid
	}
	status := "CLOCKED_OUT"
	var sessionStatus string
	// An edit to a historical closed session must not hide a current shift.
	err := tx.QueryRow(ctx, `SELECT status FROM time_session WHERE tenant_id=$1 AND worker_ref=$2 AND assignment_ref=$3 ORDER BY (status IN ('OPEN','ON_BREAK')) DESC, opened_at DESC, updated_at DESC, id DESC LIMIT 1`, tenant, worker, assignment).Scan(&sessionStatus)
	if err != nil && !errors.Is(err, dbport.ErrNoRows) {
		return err
	}
	if err == nil {
		switch sessionStatus {
		case "OPEN":
			status = "CLOCKED_IN"
		case "ON_BREAK":
			status = "ON_BREAK"
		case "CLOSED", "AUTO_CLOSED":
			status = "CLOCKED_OUT"
		default:
			return fmt.Errorf("%w: unknown session status %q", ErrInvalid, sessionStatus)
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO time_self_clock_projection (tenant_id, worker_ref, assignment_ref, status_code, last_event_at) VALUES ($1,$2,$3,$4,(SELECT max(occurred_at) FROM time_observation WHERE tenant_id=$1 AND worker_ref=$2 AND assignment_ref=$3)) ON CONFLICT (tenant_id, worker_ref, assignment_ref) DO NOTHING`, tenant, worker, assignment, status)
	return err
}

// LoadSelfProjection locks and loads the projection inside an existing tenant
// transaction. Punch commit callers use this as their CAS snapshot.
func LoadSelfProjection(ctx context.Context, tx dbport.Tx, tenant, worker, assignment string) (SelfProjection, error) {
	var p SelfProjection
	err := tx.QueryRow(ctx, `SELECT tenant_id, worker_ref, assignment_ref, revision, status_code, COALESCE(last_event_at, 'epoch'::timestamptz) FROM time_self_clock_projection WHERE tenant_id=$1 AND tenant_id=current_setting('hcmnext.tenant_id',true) AND worker_ref=$2 AND assignment_ref=$3 FOR UPDATE`, tenant, worker, assignment).Scan(&p.TenantID, &p.WorkerRef, &p.AssignmentRef, &p.Revision, &p.StatusCode, &p.LastEventAt)
	return p, err
}

// BumpSelfProjection advances a locked projection after the punch mutation.
// expectedRevision is mandatory and is compared in the update predicate.
func BumpSelfProjection(ctx context.Context, tx dbport.Tx, tenant, worker, assignment string, expectedRevision int64, statusCode string, lastEventAt time.Time) (int64, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(worker) == "" || strings.TrimSpace(assignment) == "" || expectedRevision <= 0 || strings.TrimSpace(statusCode) == "" {
		return 0, ErrInvalid
	}
	var next int64
	err := tx.QueryRow(ctx, `UPDATE time_self_clock_projection SET revision=revision+1, status_code=$4, last_event_at=$5, updated_at=now() WHERE tenant_id=$1 AND tenant_id=current_setting('hcmnext.tenant_id',true) AND worker_ref=$2 AND assignment_ref=$3 AND revision=$6 RETURNING revision`, tenant, worker, assignment, statusCode, lastEventAt, expectedRevision).Scan(&next)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return 0, ErrRevisionConflict
		}
		return 0, err
	}
	return next, nil
}
