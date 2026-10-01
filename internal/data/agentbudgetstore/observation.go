package agentbudgetstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// SettledTaskUsage returns durable settled usage for exactly one task in the
// requested tenant. In-flight reservations are never included.
func (s *Store) SettledTaskUsage(ctx context.Context, tenant values.TenantId, taskID string) (agentbudget.SettledTaskUsage, error) {
	if taskID == "" {
		return agentbudget.SettledTaskUsage{}, fmt.Errorf("%w: task id is required", ErrInvalid)
	}
	tx, tenantUUID, err := s.begin(ctx, string(tenant))
	if err != nil {
		return agentbudget.SettledTaskUsage{}, err
	}
	defer tx.Rollback(ctx)
	var result agentbudget.SettledTaskUsage
	var wall int64
	err = tx.QueryRow(ctx, `SELECT tenant_ref, task_id, used_steps, used_tokens, used_wall_ns, used_spend
		FROM agent_budget_task WHERE tenant_id=$1 AND task_id=$2`, tenantUUID, taskID).Scan(
		&result.TenantID, &result.TaskID, &result.Usage.Steps, &result.Usage.Tokens, &wall, &result.Usage.SpendMicros)
	if errors.Is(err, dbport.ErrNoRows) {
		return agentbudget.SettledTaskUsage{}, fmt.Errorf("%w: %s", ErrTaskMissing, taskID)
	}
	if err != nil {
		return agentbudget.SettledTaskUsage{}, fmt.Errorf("agentbudgetstore: read settled task usage: %w", err)
	}
	if result.TenantID != string(tenant) || result.TaskID != taskID {
		return agentbudget.SettledTaskUsage{}, fmt.Errorf("%w: task usage identity mismatch", ErrInvalid)
	}
	result.Usage.WallClock = time.Duration(wall)
	if err := tx.Commit(ctx); err != nil {
		return agentbudget.SettledTaskUsage{}, fmt.Errorf("agentbudgetstore: commit settled task usage: %w", err)
	}
	return result, nil
}
