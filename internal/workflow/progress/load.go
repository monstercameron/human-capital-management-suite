package progress

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// liveStatuses are the instance statuses LoadSnapshots reads: every status
// [Detect] can hold an expectation for.
var liveStatuses = []string{"CREATED", "RUNNING", "WAITING", "PAUSE_REQUESTED", "CANCELLING", "BLOCKED"}

// openWorkItemStatuses are the work item statuses still awaiting a person.
var openWorkItemStatuses = []string{"CREATED", "ROUTED", "ASSIGNED", "AVAILABLE", "CLAIMED", "IN_PROGRESS", "RETURNED", "ESCALATED"}

// LoadSnapshots reads one tenant's live instances and every runtime row that
// states what should move them next. It issues SELECT statements only, each
// bound to tenant explicitly, and never writes: the detector must not be able
// to alter business or runtime state (WF-RUN-020 REFACTOR).
//
// limit bounds how many instances one sweep reads, oldest first, so a sweep
// over a large tenant is paged rather than unbounded.
func LoadSnapshots(ctx context.Context, q dbport.Querier, tenant uuid.UUID, limit int) (ret0 []Snapshot, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.progress.load_snapshots", observe.Attrs{observe.KeyTenant: tenant.String()})
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if tenant == uuid.Nil {
		return nil, fmt.Errorf("progress: tenant is required")
	}
	if limit < 1 || limit > 10_000 {
		return nil, fmt.Errorf("progress: limit %d must be between 1 and 10000", limit)
	}
	rows, err := q.Query(ctx, `
		SELECT i.instance_id, i.workflow_id, i.runtime_status, i.correlation_id, i.created_at,
		       coalesce(i.started_at, i.created_at),
		       greatest(i.created_at, coalesce(i.started_at, i.created_at),
		                coalesce((SELECT max(n.recorded_at) FROM workflow_node_execution n WHERE n.tenant_id = i.tenant_id AND n.instance_id = i.instance_id), i.created_at))
		FROM workflow_instance i
		WHERE i.tenant_id = $1 AND i.runtime_status = ANY($2)
		ORDER BY i.created_at, i.instance_id
		LIMIT $3`, tenant, liveStatuses, limit)
	if err != nil {
		return nil, fmt.Errorf("progress: read live instances: %w", err)
	}
	var snaps []Snapshot
	index := map[string]int{}
	for rows.Next() {
		var id uuid.UUID
		var s Snapshot
		if err := rows.Scan(&id, &s.Instance.WorkflowID, &s.Instance.RuntimeStatus, &s.Instance.CorrelationID,
			&s.Instance.CreatedAt, &s.Instance.StartedAt, &s.Instance.LastRecordedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("progress: scan instance: %w", err)
		}
		s.Instance.TenantID = tenant.String()
		s.Instance.InstanceID = id.String()
		index[s.Instance.InstanceID] = len(snaps)
		snaps = append(snaps, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("progress: read live instances: %w", err)
	}
	if len(snaps) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(snaps))
	for _, s := range snaps {
		ids = append(ids, uuid.MustParse(s.Instance.InstanceID))
	}
	if err := loadEach(ctx, q, `SELECT instance_id, timer_id::text, node_id, timer_kind, timer_state, fires_at
		FROM workflow_timer WHERE tenant_id = $1 AND instance_id = ANY($2) AND timer_state = 'PENDING'`, tenant, ids,
		func(scan func(...any) error) (string, error) {
			var inst uuid.UUID
			var t Timer
			if err := scan(&inst, &t.TimerID, &t.NodeID, &t.Kind, &t.State, &t.FiresAt); err != nil {
				return "", err
			}
			snaps[index[inst.String()]].Timers = append(snaps[index[inst.String()]].Timers, t)
			return "", nil
		}); err != nil {
		return nil, fmt.Errorf("progress: read timers: %w", err)
	}
	if err := loadEach(ctx, q, `SELECT instance_id, ready_work_id::text, node_id, ready_state, attempt, eligible_at
		FROM workflow_ready_work WHERE tenant_id = $1 AND instance_id = ANY($2) AND ready_state IN ('READY','DISPATCHED')`, tenant, ids,
		func(scan func(...any) error) (string, error) {
			var inst uuid.UUID
			var r ReadyWork
			if err := scan(&inst, &r.ReadyWorkID, &r.NodeID, &r.State, &r.Attempt, &r.EligibleAt); err != nil {
				return "", err
			}
			snaps[index[inst.String()]].ReadyWork = append(snaps[index[inst.String()]].ReadyWork, r)
			return "", nil
		}); err != nil {
		return nil, fmt.Errorf("progress: read ready work: %w", err)
	}
	if err := loadEach(ctx, q, `SELECT instance_id, subscription_id::text, node_id, signal_name, subscription_state, coalesce(expires_at, 'epoch'::timestamptz)
		FROM workflow_signal_subscription WHERE tenant_id = $1 AND instance_id = ANY($2) AND subscription_state = 'OPEN'`, tenant, ids,
		func(scan func(...any) error) (string, error) {
			var inst uuid.UUID
			var sub Subscription
			if err := scan(&inst, &sub.SubscriptionID, &sub.NodeID, &sub.SignalName, &sub.State, &sub.ExpiresAt); err != nil {
				return "", err
			}
			if sub.ExpiresAt.Equal(time.Unix(0, 0).UTC()) {
				sub.ExpiresAt = time.Time{}
			}
			snaps[index[inst.String()]].Subscriptions = append(snaps[index[inst.String()]].Subscriptions, sub)
			return "", nil
		}); err != nil {
		return nil, fmt.Errorf("progress: read signal subscriptions: %w", err)
	}
	if err := loadEach(ctx, q, `SELECT w.workflow_instance_id, w.work_item_id::text, w.node_id, w.status,
		       s.work_item_id IS NOT NULL, coalesce(s.breach_state, ''), coalesce(s.escalate_at, 'epoch'::timestamptz)
		FROM work_item w
		LEFT JOIN work_item_sla s ON s.tenant_id = w.tenant_id AND s.work_item_id = w.work_item_id
		WHERE w.tenant_id = $1 AND w.workflow_instance_id = ANY($2) AND w.status = ANY($3)`, tenant, ids,
		func(scan func(...any) error) (string, error) {
			var inst uuid.UUID
			var h HumanTask
			if err := scan(&inst, &h.WorkItemID, &h.NodeID, &h.Status, &h.HasSLA, &h.BreachState, &h.EscalateAt); err != nil {
				return "", err
			}
			h.Open = true
			snaps[index[inst.String()]].HumanTasks = append(snaps[index[inst.String()]].HumanTasks, h)
			return "", nil
		}, openWorkItemStatuses); err != nil {
		return nil, fmt.Errorf("progress: read human tasks: %w", err)
	}
	if err := loadEach(ctx, q, `SELECT instance_id, node_id, attempt, status, coalesce(error_class, '')
		FROM workflow_node_execution WHERE tenant_id = $1 AND instance_id = ANY($2)`, tenant, ids,
		func(scan func(...any) error) (string, error) {
			var inst uuid.UUID
			var a NodeAttempt
			if err := scan(&inst, &a.NodeID, &a.Attempt, &a.Status, &a.ErrorClass); err != nil {
				return "", err
			}
			snaps[index[inst.String()]].Attempts = append(snaps[index[inst.String()]].Attempts, a)
			return "", nil
		}); err != nil {
		return nil, fmt.Errorf("progress: read node attempts: %w", err)
	}
	prefixes := make([]string, 0, len(ids))
	for _, id := range ids {
		prefixes = append(prefixes, id.String())
	}
	leaseRows, err := q.Query(ctx, `SELECT lease_id::text, resource_kind, resource_id, lease_state, holder_id, expires_at, heartbeat_at
		FROM workflow_lease
		WHERE tenant_id = $1 AND lease_state = 'HELD' AND resource_kind IN ('WORKFLOW_INSTANCE','NODE_EXECUTION')
		  AND split_part(resource_id, '/', 1) = ANY($2)`, tenant, prefixes)
	if err != nil {
		return nil, fmt.Errorf("progress: read leases: %w", err)
	}
	defer leaseRows.Close()
	for leaseRows.Next() {
		var l Lease
		if err := leaseRows.Scan(&l.LeaseID, &l.ResourceKind, &l.ResourceID, &l.State, &l.HolderID, &l.ExpiresAt, &l.HeartbeatAt); err != nil {
			return nil, fmt.Errorf("progress: scan lease: %w", err)
		}
		inst, _, _ := strings.Cut(l.ResourceID, "/")
		if i, ok := index[inst]; ok {
			snaps[i].Leases = append(snaps[i].Leases, l)
		}
	}
	if err := leaseRows.Err(); err != nil {
		return nil, fmt.Errorf("progress: read leases: %w", err)
	}
	return snaps, nil
}

// loadEach runs one tenant- and instance-bound query and hands each row to
// scanRow.
func loadEach(ctx context.Context, q dbport.Querier, sql string, tenant uuid.UUID, ids []uuid.UUID,
	scanRow func(scan func(...any) error) (string, error), extra ...any) error {
	args := append([]any{tenant, ids}, extra...)
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if _, err := scanRow(rows.Scan); err != nil {
			return err
		}
	}
	return rows.Err()
}
