package agenttriggerstore

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// ScheduleSourceRunner routes native schedule rows to their source database.
// FenceDB is a separately bounded pool so waiting source controls cannot fill
// the ordinary source request pool needed by enclosed authority rechecks.
type ScheduleSourceRunner struct {
	DB, FenceDB dbport.Beginner
}

func (r ScheduleSourceRunner) RunTenantTx(ctx context.Context, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	return runSourceTx(ctx, r.DB, tenant, fn)
}
func (r ScheduleSourceRunner) RunTenantFenceTx(ctx context.Context, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	return runSourceTx(ctx, r.FenceDB, tenant, fn)
}
func runSourceTx(ctx context.Context, database dbport.Beginner, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	if ctx == nil || database == nil || tenant == uuid.Nil || fn == nil {
		return scheduled.ErrInvalidSchedule
	}
	tx, err := database.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+tenancy.AppRole); err != nil {
		return err
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ScheduleExecutions reads current accepted work in the independent Agent
// database. It never writes either source controls or execution metadata.
type ScheduleExecutions interface {
	Outstanding(context.Context, uuid.UUID, []string) (map[string]bool, error)
}

type AgentScheduleExecutions struct{ Database TenantTxRunner }

func (r AgentScheduleExecutions) Outstanding(ctx context.Context, tenant uuid.UUID, keys []string) (map[string]bool, error) {
	if r.Database == nil || tenant == uuid.Nil {
		return nil, scheduled.ErrInvalidSchedule
	}
	result := make(map[string]bool, len(keys))
	if len(keys) == 0 {
		return result, nil
	}
	for _, key := range keys {
		result[key] = true
	}
	err := r.Database.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT q.source_ref,q.decision,e.state FROM agent_run_request q LEFT JOIN agent_run_execution e ON e.tenant_id=q.tenant_id AND e.admission_id=q.request_id WHERE q.tenant_id=$1 AND q.source_kind='SCHEDULE' AND q.source_ref=ANY($2::text[])`, tenant, keys)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var key, decision string
			var state *string
			if err := rows.Scan(&key, &decision, &state); err != nil {
				return err
			}
			result[key] = decision == "ACCEPTED" && (state == nil || *state == "READY" || *state == "RUNNING" || *state == "WAITING" || *state == "RECONCILING")
		}
		return rows.Err()
	})
	return result, err
}

func NewScheduleSource(runner ScheduleSourceRunner, tenantID uuid.UUID, tenant string, executions ScheduleExecutions) (*Store, error) {
	if runner.DB == nil || runner.FenceDB == nil || executions == nil {
		return nil, scheduled.ErrInvalidSchedule
	}
	store, err := New(runner, tenantID, tenant)
	if err != nil {
		return nil, err
	}
	store.executions = executions
	return store, nil
}

func (s *Store) outstandingKeys(ctx context.Context, tx dbport.Tx, scheduleID string) ([]string, error) {
	if s.executions == nil {
		return nil, scheduled.ErrInvalidSchedule
	}
	rows, err := tx.Query(ctx, `SELECT source_key FROM agent_schedule_outbox WHERE tenant_id=$1 AND schedule_id=$2 ORDER BY enqueued_at,source_key`, s.tenantID, scheduleID)
	if err != nil {
		return nil, err
	}
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	states, err := s.executions.Outstanding(ctx, s.tenantID, keys)
	if err != nil {
		return nil, err
	}
	var outstanding []string
	for _, key := range keys {
		state, ok := states[key]
		if !ok {
			return nil, errors.New("schedule execution reader omitted a source key")
		}
		if state {
			outstanding = append(outstanding, key)
		}
	}
	return outstanding, nil
}
