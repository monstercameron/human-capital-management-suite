package application

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/workflowbridge"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

type AgentWorkflowRecoveryRuntime interface {
	GetAdmission(context.Context, string, string) (agentrun.Record, error)
	Recover(context.Context, string, string) (runstate.Run, error)
}

// Sweep reloads the durable source inbox after a worker restart. Early terminal
// results are retried until a matching SIGNAL wait exists; accepted receipts
// are skipped so recurring recovery cannot enqueue a second continuation.
func (b AgentWorkflowCompletionBridge) Sweep(ctx context.Context, runtime AgentWorkflowRecoveryRuntime, tenant string, limit int) (int, error) {
	if runtime == nil || b.Evidence == nil || b.Now == nil || b.Core == nil || b.ResolveTenant == nil || limit < 1 || limit > 1000 {
		return 0, ErrAgentWorkflowInput
	}
	delivered := 0
	var failures []error
	for cursor := ""; ; {
		ids, err := b.pendingIDs(ctx, tenant, cursor, limit)
		if err != nil {
			return delivered, err
		}
		for _, id := range ids {
			cursor = id
			record, err := runtime.GetAdmission(ctx, tenant, id)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			if record.Request.Source.Kind != agentrun.SourceWorkflow {
				failures = append(failures, workflowbridge.ErrEvidence)
				continue
			}
			accepted, err := b.wasAccepted(ctx, tenant, record.ID)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			if accepted {
				continue
			}
			if record.Decision == agentrun.DecisionAccepted {
				run, readErr := b.Evidence.GetRun(ctx, tenant, record.ID)
				if errors.Is(readErr, runstate.ErrNotFound) {
					run, readErr = runtime.Recover(ctx, tenant, record.ID)
				}
				if readErr != nil {
					failures = append(failures, readErr)
					continue
				}
				now := b.Now().UTC()
				if !run.Deadline.After(now) && (run.State == runstate.StateReady || run.State == runstate.StateRunning || run.State == runstate.StateWaiting || run.State == runstate.StateReconciling) ||
					run.State == runstate.StateRunning && run.Lease != nil && !run.Lease.Until.After(now) {
					if _, err = runtime.Recover(ctx, tenant, record.ID); err != nil {
						failures = append(failures, err)
						continue
					}
				}
			}
			receipt, err := b.Deliver(ctx, tenant, record.ID)
			if errors.Is(err, workflowbridge.ErrPending) {
				continue
			}
			if err != nil {
				failures = append(failures, err)
				continue
			}
			for _, disposition := range receipt.Dispositions {
				if disposition.Status.Accepted() {
					delivered++
					break
				}
			}
		}
		if len(ids) < limit {
			return delivered, errors.Join(failures...)
		}
	}
}

// Page over the workflow owner's live correlations, rather than the oldest
// admission rows. Terminal history cannot starve newer waits. The cursor is
// advanced across pending work too, so a slow agent does not block later runs.
func (b AgentWorkflowCompletionBridge) pendingIDs(ctx context.Context, tenant, after string, limit int) ([]string, error) {
	tid := b.ResolveTenant(tenant)
	if tid == uuid.Nil {
		return nil, ErrAgentWorkflowInput
	}
	tx, err := b.Core.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = tenancy.WithTenant(ctx, tx, tid); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT correlation_value FROM workflow_signal_subscription WHERE tenant_id=$1 AND event_type=$2 AND correlation_key=$3 AND expected_schema_ref=$4 AND subscription_state='OPEN' AND correlation_value>$5 ORDER BY correlation_value LIMIT $6`, tid, workflowbridge.EventType, workflowbridge.CorrelationKey, workflowbridge.SchemaRef, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (b AgentWorkflowCompletionBridge) wasAccepted(ctx context.Context, tenant, id string) (bool, error) {
	tx, err := b.Core.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	tid := b.ResolveTenant(tenant)
	if err = tenancy.WithTenant(ctx, tx, tid); err != nil {
		return false, err
	}
	var accepted bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workflow_signal s JOIN workflow_signal_disposition d ON d.tenant_id=s.tenant_id AND d.signal_id=s.signal_id WHERE s.tenant_id=$1 AND s.event_type=$2 AND s.correlation_key=$3 AND s.dedupe_token=$4 AND d.status='ACCEPTED')`, tid, workflowbridge.EventType, workflowbridge.CorrelationKey, id).Scan(&accepted)
	return accepted, err
}
