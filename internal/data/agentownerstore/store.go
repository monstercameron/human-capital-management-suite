// Package agentownerstore reads current operational grants and joins owner
// identity to durable tasks. Task pause and its incident audit commit together.
package agentownerstore

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/ownerops"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type Store struct {
	db     dbport.Beginner
	mapper func(values.TenantId) uuid.UUID
	now    func() time.Time
	tasks  *agentrunstore.Store
}

func New(db dbport.Beginner, mapper func(values.TenantId) uuid.UUID, now func() time.Time) (*Store, error) {
	if now == nil {
		return nil, ownerops.ErrInvalid
	}
	tasks, err := agentrunstore.New(db, mapper)
	if err != nil {
		return nil, err
	}
	return &Store{db, mapper, now, tasks}, nil
}
func (s *Store) begin(ctx context.Context, tenant string) (dbport.Tx, uuid.UUID, error) {
	if s == nil || values.TenantId(tenant).Validate() != nil {
		return nil, uuid.Nil, ownerops.ErrDenied
	}
	id := s.mapper(values.TenantId(tenant))
	if id == uuid.Nil {
		return nil, id, ownerops.ErrDenied
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, id, err
	}
	if err = tenancy.WithTenant(ctx, tx, id); err != nil {
		_ = tx.Rollback(ctx)
		return nil, id, err
	}
	return tx, id, nil
}

// ResolveScope loads exact current grants for one authenticated principal and
// one purpose. Task-specific grants cannot authorize tenant-wide queries.
func (s *Store) ResolveScope(ctx context.Context, tenant, subject string, audience ownerops.Audience, purpose, taskID string) (ownerops.Scope, error) {
	tx, id, err := s.begin(ctx, tenant)
	if err != nil {
		return ownerops.Scope{}, err
	}
	defer tx.Rollback(ctx)
	scope, err := s.scope(ctx, tx, id, tenant, subject, audience, purpose, taskID)
	return scope, err
}

// ReadTaskIDs inventories only currently granted exact targets. It does not
// upgrade a task-specific grant into tenant-wide operational access.
func (s *Store) ReadTaskIDs(ctx context.Context, tenant, subject string, audience ownerops.Audience, purpose string) ([]string, error) {
	tx, id, err := s.begin(ctx, tenant)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT DISTINCT task_id FROM agent_owner_grant WHERE tenant_id=$1 AND principal_id=$2 AND audience=$3 AND purpose=$4 AND task_id<>'' AND $5=ANY(capabilities) AND not_before<=$6 AND expires_at>$6 AND revoked_at IS NULL ORDER BY task_id`, id, subject, string(audience), purpose, ownerops.CapabilityRead, s.now().UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []string{}
	for rows.Next() {
		var task string
		if err = rows.Scan(&task); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}
func (s *Store) scope(ctx context.Context, tx dbport.Tx, id uuid.UUID, tenant, subject string, audience ownerops.Audience, purpose, taskID string) (ownerops.Scope, error) {
	if subject == "" {
		return ownerops.Scope{}, ownerops.ErrDenied
	}
	rows, err := tx.Query(ctx, `SELECT capabilities FROM agent_owner_grant WHERE tenant_id=$1 AND principal_id=$2 AND audience=$3 AND purpose=$4 AND (task_id='' OR (task_id=$5 AND $5<>'')) AND not_before<=$6 AND expires_at>$6 AND revoked_at IS NULL ORDER BY grant_id`, id, subject, string(audience), purpose, taskID, s.now().UTC())
	if err != nil {
		return ownerops.Scope{}, err
	}
	defer rows.Close()
	scope := ownerops.Scope{TenantID: tenant, SubjectID: subject, Audience: audience, Purpose: purpose}
	for rows.Next() {
		var caps []string
		if err = rows.Scan(&caps); err != nil {
			return ownerops.Scope{}, err
		}
		scope.Capabilities = append(scope.Capabilities, caps...)
	}
	if err = rows.Err(); err != nil {
		return ownerops.Scope{}, err
	}
	if len(scope.Capabilities) == 0 {
		return ownerops.Scope{}, ownerops.ErrDenied
	}
	return scope, nil
}

// Records reads canonical task content only inside tenant RLS. Callers must
// apply ownerops.ProjectTasks before returning anything to a viewer.
func (s *Store) Records(ctx context.Context, tenant, taskID string) ([]ownerops.TaskRecord, error) {
	tasks, err := s.tasks.Scoped(values.TenantId(tenant))
	if err != nil {
		return nil, err
	}
	var source []agentrun.AgentTask
	if taskID != "" {
		task, e := tasks.Get(ctx, taskID)
		if e != nil {
			return nil, e
		}
		source = []agentrun.AgentTask{task}
	} else {
		source, err = tasks.List(ctx)
		if err != nil {
			return nil, err
		}
	}
	tx, id, err := s.begin(ctx, tenant)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	result := []ownerops.TaskRecord{}
	for _, task := range source {
		record := ownerops.TaskRecord{Task: task, Steps: []ownerops.StepMetric{}}
		err = tx.QueryRow(ctx, `SELECT owner_id,agent_id,agent_version,installation_id FROM agent_owner_binding WHERE tenant_id=$1 AND task_id=$2`, id, task.ID).Scan(&record.OwnerID, &record.AgentID, &record.Version, &record.InstallationID)
		if err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return nil, err
		}
		var spend, limit int64
		var pauseReason string
		err = tx.QueryRow(ctx, `SELECT used_spend,limit_spend,pause_reason FROM agent_budget_task WHERE tenant_id=$1 AND task_id=$2`, id, task.ID).Scan(&spend, &limit, &pauseReason)
		if err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return nil, err
		}
		record.SpendMicros = spend
		record.OverBudget = limit > 0 && (spend >= limit || pauseReason != "")
		record.Stalled = task.State == agentrun.StateRunning && s.now().Sub(task.UpdatedAt) > 15*time.Minute
		if task.Wake != nil && !task.Wake.DueAt.IsZero() {
			record.Stalled = record.Stalled || s.now().After(task.Wake.DueAt.Add(15*time.Minute))
		}
		for _, step := range task.Plan.Steps {
			metric := ownerops.StepMetric{StepID: step.ID}
			var wall int64
			if err = tx.QueryRow(ctx, `SELECT COALESCE(sum(wall_ns),0)::bigint,COALESCE(sum(spend),0)::bigint FROM agent_budget_event WHERE tenant_id=$1 AND task_id=$2 AND step_id=$3 AND kind='SETTLE'`, id, task.ID, step.ID).Scan(&wall, &metric.SpendMicros); err != nil {
				return nil, err
			}
			metric.Latency = time.Duration(wall)
			if task.Wake != nil && s.now().After(task.Wake.DueAt) && !task.Wake.DueAt.IsZero() {
				metric.WakeLag = s.now().Sub(task.Wake.DueAt)
			}
			if step.Attempt > 0 {
				metric.Retries = step.Attempt - 1
			}
			if step.Attempt > 10 {
				record.Looping = true
			}
			record.Steps = append(record.Steps, metric)
		}
		result = append(result, record)
	}
	return result, nil
}

// ApplyStop rechecks durable grants and binding in the same transaction as
// the state fence, revision check and immutable audit. Retrying the same exact
// request returns its existing receipt; reusing its key for a change conflicts.
func (s *Store) ApplyStop(ctx context.Context, command ownerops.StopCommand) (string, error) {
	if command.Kind != ownerops.PauseTask || command.TaskID == "" || command.ActorID == "" || command.RequestID == "" || command.IncidentID == "" || strings.TrimSpace(command.Reason) == "" || command.ExpectedRevision == 0 || command.ExpectedRevision >= math.MaxInt64 {
		return "", ownerops.ErrInvalid
	}
	tx, id, err := s.begin(ctx, command.TenantID)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "agent-owner-stop:"+id.String()+":"+command.RequestID)
	if err != nil {
		return "", err
	}
	audience := command.Audience
	if audience != ownerops.AudienceMember && audience != ownerops.AudienceOwner && audience != ownerops.AudienceOperator {
		return "", ownerops.ErrDenied
	}
	if audience == ownerops.AudienceOperator && command.Purpose != ownerops.PurposeOperatorOps || audience != ownerops.AudienceOperator && command.Purpose != ownerops.PurposeOwnerDashboard {
		return "", ownerops.ErrDenied
	}
	scope, err := s.scope(ctx, tx, id, command.TenantID, command.ActorID, audience, command.Purpose, command.TaskID)
	if err != nil {
		return "", err
	}
	var capability bool
	for _, cap := range scope.Capabilities {
		if cap == ownerops.CapabilityPause {
			capability = true
		}
	}
	if !capability {
		return "", ownerops.ErrDenied
	}
	var owner, user, state string
	var revision int64
	err = tx.QueryRow(ctx, `SELECT COALESCE(b.owner_id,''),t.user_id,t.state,t.version FROM agent_task t LEFT JOIN agent_owner_binding b ON b.tenant_id=t.tenant_id AND b.task_id=t.task_id WHERE t.tenant_id=$1 AND t.task_id=$2 FOR UPDATE OF t`, id, command.TaskID).Scan(&owner, &user, &state, &revision)
	if errors.Is(err, dbport.ErrNoRows) {
		return "", agentrun.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if audience == ownerops.AudienceMember && user != command.ActorID || audience == ownerops.AudienceOwner && owner != command.ActorID {
		return "", ownerops.ErrDenied
	}
	var receipt, priorTask, actor, purpose, incident, reason string
	var expected int64
	err = tx.QueryRow(ctx, `SELECT audit_id,task_id,actor_id,purpose,incident_id,reason,expected_revision FROM agent_owner_stop_audit WHERE tenant_id=$1 AND request_id=$2`, id, command.RequestID).Scan(&receipt, &priorTask, &actor, &purpose, &incident, &reason, &expected)
	if err == nil {
		if priorTask != command.TaskID || actor != command.ActorID || purpose != command.Purpose || incident != command.IncidentID || reason != command.Reason || expected != int64(command.ExpectedRevision) {
			return "", agentrun.ErrConflict
		}
		return receipt, nil
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return "", err
	}
	if revision != int64(command.ExpectedRevision) {
		return "", agentrun.ErrConflict
	}
	if state == "COMPLETED" || state == "FAILED" || state == "CANCELLED" || state == "EXPIRED" {
		return "", agentrun.ErrTerminal
	}
	if state != "PAUSED" {
		_, err = tx.Exec(ctx, `UPDATE agent_task SET paused_state=state,paused_wake=wake,state='PAUSED',wake=NULL,wake_kind=NULL,wake_key=NULL,wake_due_at=NULL,wake_stale_after=NULL,worker_lease='',model_session='',version=version+1,updated_at=$3, failure_code=CASE WHEN plan->'steps'->current_step->>'state'='RUNNING' AND (plan->'steps'->current_step->>'tier')::int>=2 THEN 'AMBIGUOUS_EFFECT' ELSE failure_code END WHERE tenant_id=$1 AND task_id=$2`, id, command.TaskID, s.now().UTC())
		if err != nil {
			return "", err
		}
	}
	receipt = uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO agent_owner_stop_audit(tenant_id,request_id,task_id,actor_id,purpose,incident_id,reason,expected_revision,audit_id,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, id, command.RequestID, command.TaskID, command.ActorID, command.Purpose, command.IncidentID, command.Reason, int64(command.ExpectedRevision), receipt, s.now().UTC())
	if err != nil {
		return "", err
	}
	return receipt, tx.Commit(ctx)
}

var _ ownerops.StopController = (*Store)(nil)

// BindTask records identity resolved by the canonical installation owner at
// admission. This persistence port is not a viewer or request-body operation.
func (s *Store) BindTask(ctx context.Context, tenant, taskID, ownerID, agentID, version, installationID string) error {
	if taskID == "" || ownerID == "" || agentID == "" || version == "" || installationID == "" {
		return ownerops.ErrInvalid
	}
	tx, id, err := s.begin(ctx, tenant)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	n, err := tx.Exec(ctx, `INSERT INTO agent_owner_binding(tenant_id,task_id,owner_id,agent_id,agent_version,installation_id) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, id, taskID, ownerID, agentID, version, installationID)
	if err != nil {
		return err
	}
	if n == 0 {
		var owner, agent, priorVersion, installation string
		err = tx.QueryRow(ctx, `SELECT owner_id,agent_id,agent_version,installation_id FROM agent_owner_binding WHERE tenant_id=$1 AND task_id=$2`, id, taskID).Scan(&owner, &agent, &priorVersion, &installation)
		if err != nil {
			return err
		}
		if owner != ownerID || agent != agentID || priorVersion != version || installation != installationID {
			return agentrun.ErrConflict
		}
	}
	return tx.Commit(ctx)
}
