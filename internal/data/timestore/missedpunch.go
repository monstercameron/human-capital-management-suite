package timestore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// MissedPunchStatus is the request's current mutable status.
type MissedPunchStatus string

const (
	MissedPunchPending  MissedPunchStatus = "PENDING"
	MissedPunchApproved MissedPunchStatus = "APPROVED"
	MissedPunchRejected MissedPunchStatus = "REJECTED"
)

// MissedPunchRequest is one worker-submitted missed or wrong-punch request
// (TCLOCK-011).
type MissedPunchRequest struct {
	TenantID      string
	ID            string
	WorkerRef     string
	TimecardID    string // empty when not yet linked to a timecard
	ClaimedTime   time.Time
	Reason        string
	RequestedBy   string
	SupervisorRef string
	Status        MissedPunchStatus
	Revision      int64
	PeriodClosed  bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// MissedPunchDecision is one append-only supervisor decision against a
// request.
type MissedPunchDecision struct {
	TenantID  string
	ID        string
	RequestID string
	Decision  MissedPunchStatus // APPROVED or REJECTED
	DecidedBy string
	Reason    string
	ReopenRef string
	DecidedAt time.Time
}

// ErrSelfApproval is returned by DecideMissedPunch when the deciding
// supervisor is the same actor who submitted the request: segregation of
// duties forbids a supervisor approving or rejecting their own request.
var ErrSelfApproval = errors.New("time missed-punch self-approval forbidden")

// SubmitMissedPunchRequest creates a new request in PENDING status.
func (s *Store) SubmitMissedPunchRequest(ctx context.Context, tenant string, r MissedPunchRequest) (MissedPunchRequest, error) {
	if tenant == "" || r.TenantID != tenant || r.ID == "" || r.WorkerRef == "" || r.Reason == "" ||
		r.RequestedBy == "" || r.SupervisorRef == "" || r.ClaimedTime.IsZero() {
		return MissedPunchRequest{}, ErrInvalid
	}
	if r.RequestedBy == r.SupervisorRef {
		return MissedPunchRequest{}, ErrSelfApproval
	}
	var timecardID any
	if r.TimecardID != "" {
		timecardID = r.TimecardID
	}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO missed_punch_request(tenant_id,id,worker_ref,timecard_id,claimed_time,reason,requested_by,supervisor_ref,status,period_closed) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'PENDING',$9)`,
			tenant, r.ID, r.WorkerRef, timecardID, r.ClaimedTime, r.Reason, r.RequestedBy, r.SupervisorRef, r.PeriodClosed)
		return err
	})
	if err != nil {
		return MissedPunchRequest{}, err
	}
	r.Status = MissedPunchPending
	r.Revision = 1
	return r, nil
}

// GetMissedPunchRequest reads one request by id.
func (s *Store) GetMissedPunchRequest(ctx context.Context, tenant, id string) (MissedPunchRequest, error) {
	if tenant == "" || id == "" {
		return MissedPunchRequest{}, ErrInvalid
	}
	var r MissedPunchRequest
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var status string
		var timecardID *string
		row := tx.QueryRow(ctx, `SELECT tenant_id,id,worker_ref,timecard_id,claimed_time,reason,requested_by,supervisor_ref,status,revision,period_closed,created_at,updated_at FROM missed_punch_request WHERE tenant_id=$1 AND id=$2`, tenant, id)
		if err := row.Scan(&r.TenantID, &r.ID, &r.WorkerRef, &timecardID, &r.ClaimedTime, &r.Reason, &r.RequestedBy, &r.SupervisorRef, &status, &r.Revision, &r.PeriodClosed, &r.CreatedAt, &r.UpdatedAt); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		r.Status = MissedPunchStatus(status)
		if timecardID != nil {
			r.TimecardID = *timecardID
		}
		return nil
	})
	if err != nil {
		return MissedPunchRequest{}, err
	}
	return r, nil
}

// DecideMissedPunch records a supervisor decision. It rejects a decision
// against a closed period unless reopenRef names the typed reopen that
// authorized it, enforces segregation of duties, and guards the request's
// PENDING status so two concurrent decisions on the same request can only
// have one winner (TCLOCK-011 Race).
func (s *Store) DecideMissedPunch(ctx context.Context, tenant, requestID string, decision MissedPunchStatus, decidedBy, reason, reopenRef string) (MissedPunchRequest, error) {
	if tenant == "" || requestID == "" || decidedBy == "" || (decision != MissedPunchApproved && decision != MissedPunchRejected) {
		return MissedPunchRequest{}, ErrInvalid
	}
	var out MissedPunchRequest
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var status string
		var requestedBy string
		var periodClosed bool
		row := tx.QueryRow(ctx, `SELECT status,requested_by,period_closed FROM missed_punch_request WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, requestID)
		if err := row.Scan(&status, &requestedBy, &periodClosed); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if requestedBy == decidedBy {
			return ErrSelfApproval
		}
		if periodClosed && reopenRef == "" {
			return ErrInvalid
		}
		tag, err := tx.Exec(ctx, `UPDATE missed_punch_request SET status=$1,revision=revision+1,updated_at=now() WHERE tenant_id=$2 AND id=$3 AND status='PENDING'`,
			string(decision), tenant, requestID)
		if err != nil {
			return err
		}
		if tag != 1 {
			return ErrRevisionConflict
		}
		if _, err := tx.Exec(ctx, `INSERT INTO missed_punch_decision(tenant_id,id,request_id,decision,decided_by,reason,reopen_ref) VALUES($1,$2,$3,$4,$5,$6,$7)`,
			tenant, uuid.NewString(), requestID, string(decision), decidedBy, reason, reopenRef); err != nil {
			return err
		}
		return s.loadMissedPunchLocked(ctx, tx, tenant, requestID, &out)
	})
	if err != nil {
		return MissedPunchRequest{}, err
	}
	return out, nil
}

func (s *Store) loadMissedPunchLocked(ctx context.Context, tx dbport.Tx, tenant, id string, out *MissedPunchRequest) error {
	var status string
	var timecardID *string
	row := tx.QueryRow(ctx, `SELECT tenant_id,id,worker_ref,timecard_id,claimed_time,reason,requested_by,supervisor_ref,status,revision,period_closed,created_at,updated_at FROM missed_punch_request WHERE tenant_id=$1 AND id=$2`, tenant, id)
	if err := row.Scan(&out.TenantID, &out.ID, &out.WorkerRef, &timecardID, &out.ClaimedTime, &out.Reason, &out.RequestedBy, &out.SupervisorRef, &status, &out.Revision, &out.PeriodClosed, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return err
	}
	out.Status = MissedPunchStatus(status)
	if timecardID != nil {
		out.TimecardID = *timecardID
	}
	return nil
}

// MissedPunchDecisions returns the append-only decision trail for a request.
func (s *Store) MissedPunchDecisions(ctx context.Context, tenant, requestID string) ([]MissedPunchDecision, error) {
	if tenant == "" || requestID == "" {
		return nil, ErrInvalid
	}
	out := make([]MissedPunchDecision, 0)
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id,id,request_id,decision,decided_by,reason,reopen_ref,decided_at FROM missed_punch_decision WHERE tenant_id=$1 AND request_id=$2 ORDER BY decided_at`, tenant, requestID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d MissedPunchDecision
			var decision string
			if err := rows.Scan(&d.TenantID, &d.ID, &d.RequestID, &decision, &d.DecidedBy, &d.Reason, &d.ReopenRef, &d.DecidedAt); err != nil {
				return err
			}
			d.Decision = MissedPunchStatus(decision)
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
