package timestore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// WorkflowDispatchLease is the durable ownership result for one consumer.
type WorkflowDispatchLease struct {
	Tenant, Consumer, Owner string
	Cursor                  int64
	LeaseUntil              time.Time
}

// WorkflowSessionRun is the immutable binding between one clock session and
// its workflow runtime instance.
type WorkflowSessionRun struct {
	TenantID, SessionID, WorkflowID, PlanDigest, StartKey, CorrelationID string
	InstanceID                                                           uuid.UUID
	CreatedAt                                                            time.Time
}

// ClaimWorkflowDispatch claims a consumer lease at its current durable
// cursor. Expired leases may be recovered; an active lease owned by another
// worker is reported as unavailable.
func (s *Store) ClaimWorkflowDispatch(ctx context.Context, tenant, consumer, owner string, now time.Time, lease time.Duration) (WorkflowDispatchLease, bool, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(consumer) == "" || strings.TrimSpace(owner) == "" || now.IsZero() || lease <= 0 {
		return WorkflowDispatchLease{}, false, ErrInvalid
	}
	var result WorkflowDispatchLease
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var cursor int64
		var priorOwner string
		var until *time.Time
		err := tx.QueryRow(ctx, `SELECT last_sequence,lease_owner,lease_until FROM time_workflow_dispatch_cursor WHERE tenant_id=$1 AND consumer=$2 FOR UPDATE`, tenant, consumer).Scan(&cursor, &priorOwner, &until)
		if errors.Is(err, dbport.ErrNoRows) {
			var inserted int64
			inserted, err = tx.Exec(ctx, `INSERT INTO time_workflow_dispatch_cursor(tenant_id,consumer,last_sequence,lease_owner,lease_until) VALUES($1,$2,0,$3,$4) ON CONFLICT (tenant_id,consumer) DO NOTHING`, tenant, consumer, owner, now.UTC().Add(lease))
			if err != nil {
				return err
			}
			if inserted == 1 {
				result = WorkflowDispatchLease{Tenant: tenant, Consumer: consumer, Owner: owner, LeaseUntil: now.UTC().Add(lease)}
				return nil
			}
			// Another consumer inserted the row between our SELECT and INSERT.
			// The row is locked by this transaction after the INSERT conflict.
			err = tx.QueryRow(ctx, `SELECT last_sequence,lease_owner,lease_until FROM time_workflow_dispatch_cursor WHERE tenant_id=$1 AND consumer=$2 FOR UPDATE`, tenant, consumer).Scan(&cursor, &priorOwner, &until)
		}
		if err != nil {
			return err
		}
		if priorOwner != "" && priorOwner != owner && until != nil && until.After(now.UTC()) {
			result = WorkflowDispatchLease{Tenant: tenant, Consumer: consumer, Owner: priorOwner, Cursor: cursor, LeaseUntil: until.UTC()}
			return nil
		}
		newUntil := now.UTC().Add(lease)
		if _, err = tx.Exec(ctx, `UPDATE time_workflow_dispatch_cursor SET lease_owner=$3,lease_until=$4,updated_at=$5 WHERE tenant_id=$1 AND consumer=$2`, tenant, consumer, owner, newUntil, now.UTC()); err != nil {
			return err
		}
		result = WorkflowDispatchLease{Tenant: tenant, Consumer: consumer, Owner: owner, Cursor: cursor, LeaseUntil: newUntil}
		return nil
	})
	if err != nil {
		return WorkflowDispatchLease{}, false, err
	}
	return result, result.Owner == owner, nil
}

// AdvanceWorkflowDispatch moves a held consumer cursor exactly once. The
// owner, expected cursor and active lease are all compare-and-swap guards.
func (s *Store) AdvanceWorkflowDispatch(ctx context.Context, tenant, consumer, owner string, expected, next int64, now time.Time) error {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(consumer) == "" || strings.TrimSpace(owner) == "" || expected < 0 || next < expected || now.IsZero() {
		return ErrInvalid
	}
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Exec(ctx, `UPDATE time_workflow_dispatch_cursor SET last_sequence=$5,updated_at=$6 WHERE tenant_id=$1 AND consumer=$2 AND lease_owner=$3 AND last_sequence=$4 AND lease_until>$6`, tenant, consumer, owner, expected, next, now.UTC())
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrRevisionConflict
		}
		return nil
	})
}

// BindWorkflowSessionRun records an immutable session-to-run identity. An
// exact replay returns success; any different binding is an idempotency
// conflict and cannot overwrite the original runtime identity.
func (s *Store) BindWorkflowSessionRun(ctx context.Context, binding WorkflowSessionRun) error {
	if binding.TenantID == "" || binding.SessionID == "" || binding.WorkflowID == "" || binding.PlanDigest == "" || binding.StartKey == "" || binding.CorrelationID == "" || binding.InstanceID == uuid.Nil || binding.CreatedAt.IsZero() {
		return ErrInvalid
	}
	created := binding.CreatedAt.UTC().Truncate(time.Microsecond)
	return s.RunTenantTx(ctx, binding.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO time_workflow_session_run(tenant_id,session_id,instance_id,workflow_id,plan_digest,start_key,correlation_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (tenant_id,session_id) DO NOTHING`, binding.TenantID, binding.SessionID, binding.InstanceID, binding.WorkflowID, binding.PlanDigest, binding.StartKey, binding.CorrelationID, created)
		if err != nil {
			return err
		}
		var got WorkflowSessionRun
		err = tx.QueryRow(ctx, `SELECT tenant_id,session_id,instance_id,workflow_id,plan_digest,start_key,correlation_id,created_at FROM time_workflow_session_run WHERE tenant_id=$1 AND session_id=$2`, binding.TenantID, binding.SessionID).Scan(&got.TenantID, &got.SessionID, &got.InstanceID, &got.WorkflowID, &got.PlanDigest, &got.StartKey, &got.CorrelationID, &got.CreatedAt)
		if err != nil {
			return err
		}
		if got.InstanceID != binding.InstanceID || got.WorkflowID != binding.WorkflowID || got.PlanDigest != binding.PlanDigest || got.StartKey != binding.StartKey || got.CorrelationID != binding.CorrelationID {
			return ErrIdempotencyConflict
		}
		return nil
	})
}

// LoadWorkflowSessionRun reads the immutable binding used to resume signals.
func (s *Store) LoadWorkflowSessionRun(ctx context.Context, tenant, session string) (WorkflowSessionRun, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(session) == "" {
		return WorkflowSessionRun{}, ErrInvalid
	}
	var got WorkflowSessionRun
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT tenant_id,session_id,instance_id,workflow_id,plan_digest,start_key,correlation_id,created_at FROM time_workflow_session_run WHERE tenant_id=$1 AND session_id=$2`, tenant, session).Scan(&got.TenantID, &got.SessionID, &got.InstanceID, &got.WorkflowID, &got.PlanDigest, &got.StartKey, &got.CorrelationID, &got.CreatedAt)
	})
	return got, err
}
