// Package agentrunstore persists agentrun tasks and wake receipts in
// PostgreSQL. The runtime state machine stays in package agentrun; this
// package only implements its TaskStore port with the same observable
// semantics as agentrun.MemoryStore: version-checked Save, duplicate Create
// refused, and ClaimWake deduplicated by event id.
//
// agentrun.TaskStore methods carry no tenant, so the port is exposed through
// ForTenant: a tenant-scoped adapter that runs every statement inside a
// transaction bound to that tenant (row-level security tenant_isolation) and
// refuses tasks that belong to another tenant.
//
// Round trip: Get(Create(task)) equals task. Timestamps are returned in UTC
// (the runtime already writes UTC) and timestamptz columns hold microsecond
// precision, so a caller that stores a task time finer than a microsecond
// reads it back truncated.
package agentrunstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrTenantMismatch is returned when a task names a different tenant than the
// scope it is written through. It wraps agentrun.ErrInvalid.
var ErrTenantMismatch = fmt.Errorf("%w: task belongs to another tenant", agentrun.ErrInvalid)

// DB is the transaction opener the store needs.
type DB interface{ dbport.Beginner }

// Store opens tenant-scoped views of the durable task tables.
type Store struct {
	db         DB
	tenantUUID func(values.TenantId) uuid.UUID
}

// New builds a Store. tenantUUID maps a tenant key to its canonical UUID and
// returns uuid.Nil for an unknown tenant.
func New(db DB, tenantUUID func(values.TenantId) uuid.UUID) (*Store, error) {
	if db == nil || tenantUUID == nil {
		return nil, fmt.Errorf("%w: database and canonical tenant mapper are required", agentrun.ErrInvalid)
	}
	return &Store{db: db, tenantUUID: tenantUUID}, nil
}

// ForTenant returns the agentrun.TaskStore for one tenant. List returns only
// that tenant's tasks, and a task whose TenantID differs from tenant is refused.
func (s *Store) ForTenant(_ context.Context, tenant values.TenantId) (agentrun.TaskStore, error) {
	scoped, err := s.Scoped(tenant)
	if err != nil {
		return nil, err
	}
	return scoped, nil
}

// Scoped is ForTenant with the concrete type, which also offers DueWaits.
func (s *Store) Scoped(tenant values.TenantId) (*TenantStore, error) {
	if s == nil || s.db == nil || s.tenantUUID == nil {
		return nil, fmt.Errorf("%w: nil store", agentrun.ErrInvalid)
	}
	text := string(tenant)
	if strings.TrimSpace(text) == "" || strings.TrimSpace(text) != text {
		return nil, fmt.Errorf("%w: tenant is required", agentrun.ErrInvalid)
	}
	id := s.tenantUUID(tenant)
	if id == uuid.Nil {
		return nil, fmt.Errorf("%w: unknown tenant", agentrun.ErrInvalid)
	}
	return &TenantStore{db: s.db, tenant: id, tenantRef: text}, nil
}

// TenantStore implements agentrun.TaskStore for one tenant.
type TenantStore struct {
	db        DB
	tenant    uuid.UUID
	tenantRef string
}

var _ agentrun.TaskStore = (*TenantStore)(nil)

const taskColumns = `task_id, tenant_ref, user_id, goal, constraints, plan, state, version, current_step,
	wake, paused_state, paused_wake, worker_lease, model_session, failure_code, failure_detail,
	ledger, created_at, updated_at, expires_at, last_wake_event, task_lineage`

func (s *TenantStore) begin(ctx context.Context) (dbport.Tx, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("agentrunstore: begin: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, s.tenant); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

// Create inserts a new task; an existing id yields agentrun.ErrConflict.
func (s *TenantStore) Create(ctx context.Context, task agentrun.AgentTask) error {
	return s.CreateWithEvents(ctx, task)
}

// CreateWithEvents atomically creates a task and its initial events.
func (s *TenantStore) CreateWithEvents(ctx context.Context, task agentrun.AgentTask, events ...agentrun.TaskEvent) error {
	f, err := s.encode(task)
	if err != nil {
		return err
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	n, err := tx.Exec(ctx, `INSERT INTO agent_task (tenant_id, task_id, tenant_ref, user_id, goal, constraints, plan, plan_digest, plan_confirmed,
		state, version, current_step, wake, wake_kind, wake_key, wake_due_at, wake_stale_after, paused_state, paused_wake,
		worker_lease, model_session, failure_code, failure_detail, ledger, created_at, updated_at, expires_at, last_wake_event, task_lineage)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9,$10,$11,$12,$13::jsonb,$14,$15,$16,$17,$18,$19::jsonb,$20,$21,$22,$23,$24::jsonb,$25,$26,$27,$28::jsonb,$29::jsonb)
		ON CONFLICT (tenant_id, task_id) DO NOTHING`, f.args(s.tenant)...)
	if err != nil {
		return fmt.Errorf("agentrunstore: create: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: task %q already exists", agentrun.ErrConflict, task.ID)
	}
	if err := appendEvents(ctx, tx, s.tenant, task.ID, events); err != nil {
		return err
	}
	return commit(ctx, tx)
}

// Get returns one task of this tenant.
func (s *TenantStore) Get(ctx context.Context, id string) (agentrun.AgentTask, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return agentrun.AgentTask{}, err
	}
	defer tx.Rollback(ctx)
	task, err := scanTask(tx.QueryRow(ctx, `SELECT `+taskColumns+` FROM agent_task WHERE tenant_id=$1 AND task_id=$2`, s.tenant, id))
	if errors.Is(err, dbport.ErrNoRows) {
		return agentrun.AgentTask{}, fmt.Errorf("%w: %s", agentrun.ErrNotFound, id)
	}
	if err != nil {
		return agentrun.AgentTask{}, err
	}
	return task, commit(ctx, tx)
}

// Save replaces a task when its stored version equals expectedVersion. The
// version check is part of the UPDATE, so of two concurrent saves with the same
// expected version exactly one wins and the other gets agentrun.ErrConflict.
func (s *TenantStore) Save(ctx context.Context, task agentrun.AgentTask, expectedVersion uint64) error {
	return s.SaveWithEvents(ctx, task, expectedVersion)
}

// SaveWithEvents atomically saves a versioned task and appends its events.
func (s *TenantStore) SaveWithEvents(ctx context.Context, task agentrun.AgentTask, expectedVersion uint64, events ...agentrun.TaskEvent) error {
	f, err := s.encode(task)
	if err != nil {
		return err
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if task.Version == expectedVersion+1 && expectedVersion <= math.MaxInt64-1 {
		n, err := tx.Exec(ctx, `UPDATE agent_task SET user_id=$3, goal=$4, constraints=$5::jsonb,
			plan=(CASE WHEN plan ? 'document_omissions'
				THEN (($6::jsonb - 'document_omissions') || jsonb_build_object('document_omissions',plan->'document_omissions'))
				ELSE ($6::jsonb - 'document_omissions') END), plan_digest=$7, plan_confirmed=$8,
			state=$9, version=$10, current_step=$11, wake=$12::jsonb, wake_kind=$13, wake_key=$14, wake_due_at=$15, wake_stale_after=$16,
			paused_state=$17, paused_wake=$18::jsonb, worker_lease=$19, model_session=$20, failure_code=$21, failure_detail=$22,
			ledger=$23::jsonb, created_at=$24, updated_at=$25, expires_at=$26, last_wake_event=$28::jsonb,task_lineage=$29::jsonb
			WHERE tenant_id=$1 AND task_id=$2 AND version=$27
			AND COALESCE(plan->'document_references','[]'::jsonb)=COALESCE($6::jsonb->'document_references','[]'::jsonb)
			AND COALESCE(plan->'answering_agent','null'::jsonb)=COALESCE($6::jsonb->'answering_agent','null'::jsonb)`, f.updateArgs(s.tenant, int64(expectedVersion))...)
		if err != nil {
			return fmt.Errorf("agentrunstore: save: %w", err)
		}
		if n == 1 {
			if err := appendEvents(ctx, tx, s.tenant, task.ID, events); err != nil {
				return err
			}
			return commit(ctx, tx)
		}
	}
	var current int64
	err = tx.QueryRow(ctx, `SELECT version FROM agent_task WHERE tenant_id=$1 AND task_id=$2`, s.tenant, task.ID).Scan(&current)
	switch {
	case errors.Is(err, dbport.ErrNoRows):
		return fmt.Errorf("%w: %s", agentrun.ErrNotFound, task.ID)
	case err != nil:
		return fmt.Errorf("agentrunstore: read version: %w", err)
	case uint64(current) != expectedVersion:
		return fmt.Errorf("%w: task %s is version %d, expected %d", agentrun.ErrConflict, task.ID, current, expectedVersion)
	default:
		return fmt.Errorf("%w: next version must be %d", agentrun.ErrInvalid, expectedVersion+1)
	}
}

// ListEvents returns one tenant's task events in their assigned append order.
func (s *TenantStore) ListEvents(ctx context.Context, id string) ([]agentrun.TaskEvent, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_task WHERE tenant_id=$1 AND task_id=$2)`, s.tenant, id).Scan(&exists); err != nil {
		return nil, fmt.Errorf("agentrunstore: check task for events: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("%w: %s", agentrun.ErrNotFound, id)
	}
	rows, err := tx.Query(ctx, `SELECT event_sequence, event_type, plan_revision, plan_digest, plan_snapshot, step_id, step_type, side_effect_tier,
		approval_digest, evidence_ref, evidence_digest, outcome, actor_id, occurred_at
		FROM agent_task_event WHERE tenant_id=$1 AND task_id=$2 ORDER BY event_sequence`, s.tenant, id)
	if err != nil {
		return nil, fmt.Errorf("agentrunstore: list task events: %w", err)
	}
	defer rows.Close()
	out := make([]agentrun.TaskEvent, 0)
	for rows.Next() {
		var event agentrun.TaskEvent
		var revision, tier *int64
		var planDigest, stepID, stepType, approvalDigest, evidenceRef, evidenceDigest *string
		var planSnapshot []byte
		if err := rows.Scan(&event.Sequence, &event.Type, &revision, &planDigest, &planSnapshot, &stepID, &stepType, &tier,
			&approvalDigest, &evidenceRef, &evidenceDigest, &event.Outcome, &event.ActorID, &event.OccurredAt); err != nil {
			return nil, fmt.Errorf("agentrunstore: scan task event: %w", err)
		}
		if revision != nil {
			event.PlanRevision = uint64(*revision)
		}
		if planDigest != nil {
			event.PlanDigest = *planDigest
		}
		if planSnapshot != nil {
			var snapshot agentrun.HistoricalPlanSnapshot
			if err := json.Unmarshal(planSnapshot, &snapshot); err != nil {
				return nil, fmt.Errorf("agentrunstore: decode plan snapshot: %w", err)
			}
			if err := snapshot.Verify(); err != nil || snapshot.Revision != event.PlanRevision || snapshot.Digest != event.PlanDigest {
				return nil, fmt.Errorf("agentrunstore: historical plan snapshot failed verification: %w", agentrun.ErrInvalid)
			}
			event.PlanSnapshot = &snapshot
		}
		if stepID != nil {
			event.StepID = *stepID
		}
		if stepType != nil {
			event.StepType = agentrun.StepType(*stepType)
		}
		if tier != nil {
			event.Tier = agentrun.Tier(*tier)
		}
		if approvalDigest != nil {
			event.ApprovalDigest = *approvalDigest
		}
		if evidenceRef != nil {
			event.EvidenceRef = *evidenceRef
		}
		if evidenceDigest != nil {
			event.EvidenceDigest = *evidenceDigest
		}
		event.TaskID = id
		event.OccurredAt = event.OccurredAt.UTC()
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agentrunstore: read task events: %w", err)
	}
	rows.Close()
	return out, commit(ctx, tx)
}

func appendEvents(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, taskID string, events []agentrun.TaskEvent) error {
	var sequence int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(event_sequence),0) FROM agent_task_event WHERE tenant_id=$1 AND task_id=$2`, tenant, taskID).Scan(&sequence); err != nil {
		return fmt.Errorf("agentrunstore: read task event sequence: %w", err)
	}
	for _, event := range events {
		if event.TaskID != taskID || event.Sequence != 0 {
			return fmt.Errorf("%w: event task or store-assigned sequence is invalid", agentrun.ErrInvalid)
		}
		if err := event.Validate(); err != nil {
			return err
		}
		sequence++
		var revision, tier any
		var planDigest, planSnapshot, stepID, stepType, approvalDigest, evidenceRef, evidenceDigest any
		if event.PlanRevision > 0 {
			revision = int64(event.PlanRevision)
			planDigest = event.PlanDigest
		}
		if event.PlanSnapshot != nil {
			encoded, err := json.Marshal(event.PlanSnapshot)
			if err != nil {
				return fmt.Errorf("agentrunstore: encode plan snapshot: %w", err)
			}
			planSnapshot = string(encoded)
		}
		if event.StepID != "" {
			stepID, stepType, tier = event.StepID, string(event.StepType), int16(event.Tier)
		}
		if event.ApprovalDigest != "" {
			approvalDigest = event.ApprovalDigest
		}
		if event.EvidenceRef != "" {
			evidenceRef = event.EvidenceRef
		}
		if event.EvidenceDigest != "" {
			evidenceDigest = event.EvidenceDigest
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agent_task_event (tenant_id, task_id, event_sequence, event_type, plan_revision, plan_digest, plan_snapshot,
			step_id, step_type, side_effect_tier, approval_digest, evidence_ref, evidence_digest, outcome, actor_id, occurred_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
			tenant, taskID, sequence, string(event.Type), revision, planDigest, planSnapshot, stepID, stepType, tier, approvalDigest,
			evidenceRef, evidenceDigest, event.Outcome, event.ActorID, event.OccurredAt.UTC()); err != nil {
			return fmt.Errorf("agentrunstore: append task event: %w", err)
		}
	}
	return nil
}

// ClaimWake records the event in the receipt inbox and, when the event matches
// the parked wake condition, resumes the task. The task row is locked for the
// whole claim, so a duplicate delivered concurrently waits and then reports
// Duplicate instead of racing the first delivery.
func (s *TenantStore) ClaimWake(ctx context.Context, id string, event agentrun.WakeEvent) (agentrun.WakeResult, error) {
	if strings.TrimSpace(event.ID) == "" || !validWakeKind(event.Kind) || strings.TrimSpace(event.Key) == "" || event.OccurredAt.IsZero() {
		return agentrun.WakeResult{}, fmt.Errorf("%w: event id, kind and key are required", agentrun.ErrInvalidWake)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return agentrun.WakeResult{}, err
	}
	defer tx.Rollback(ctx)
	task, err := scanTask(tx.QueryRow(ctx, `SELECT `+taskColumns+` FROM agent_task WHERE tenant_id=$1 AND task_id=$2 FOR UPDATE`, s.tenant, id))
	if errors.Is(err, dbport.ErrNoRows) {
		return agentrun.WakeResult{}, fmt.Errorf("%w: %s", agentrun.ErrNotFound, id)
	}
	if err != nil {
		return agentrun.WakeResult{}, err
	}
	accepted := wakeMatches(task, event)
	outcome := "IGNORED"
	if accepted {
		outcome = "ACCEPTED"
	}
	n, err := tx.Exec(ctx, `INSERT INTO agent_task_wake_receipt (tenant_id, task_id, event_id, kind, wake_key, correlation, payload_ref, occurred_at, outcome)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (tenant_id, task_id, event_id) DO NOTHING`,
		s.tenant, id, event.ID, string(event.Kind), event.Key, event.Correlation, event.PayloadRef, event.OccurredAt.UTC(), outcome)
	if err != nil {
		return agentrun.WakeResult{}, fmt.Errorf("agentrunstore: record wake receipt: %w", err)
	}
	if n == 0 {
		return agentrun.WakeResult{Duplicate: true, Task: task}, commit(ctx, tx)
	}
	if !terminalState(task.State) && !task.ExpiresAt.After(event.OccurredAt) {
		task.State, task.Wake, task.WorkerLease, task.ModelSession = agentrun.StateExpired, nil, "", ""
		task.FailureCode, task.FailureDetail = "TASK_EXPIRED", "maximum task lifetime elapsed"
		task.Version++
		task.UpdatedAt = event.OccurredAt.UTC()
		if _, err := tx.Exec(ctx, `UPDATE agent_task SET state=$3,wake=NULL,wake_kind=NULL,wake_key=NULL,
			wake_due_at=NULL,wake_stale_after=NULL,worker_lease='',model_session='',failure_code=$4,failure_detail=$5,
			version=$6,updated_at=$7 WHERE tenant_id=$1 AND task_id=$2`, s.tenant, id, string(task.State), task.FailureCode, task.FailureDetail, int64(task.Version), task.UpdatedAt); err != nil {
			return agentrun.WakeResult{}, fmt.Errorf("agentrunstore: expire task at wake: %w", err)
		}
		return agentrun.WakeResult{Ignored: true, Task: task}, commit(ctx, tx)
	}
	if !accepted {
		return agentrun.WakeResult{Ignored: true, Task: task}, commit(ctx, tx)
	}
	acceptedEvent := event
	acceptedEvent.OccurredAt = acceptedEvent.OccurredAt.UTC()
	task.LastWake = &acceptedEvent
	task.Wake = nil
	task.State = agentrun.StateRunning
	task.Version++
	task.UpdatedAt = event.OccurredAt.UTC()
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = task.CreatedAt
	}
	f, err := s.encode(task)
	if err != nil {
		return agentrun.WakeResult{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE agent_task SET wake=NULL, wake_kind=NULL, wake_key=NULL, wake_due_at=NULL, wake_stale_after=NULL,
		state=$3, version=$4, updated_at=$5,last_wake_event=$6::jsonb WHERE tenant_id=$1 AND task_id=$2`, s.tenant, id, f.state, f.version, f.updatedAt, jsonArg(f.lastWake)); err != nil {
		return agentrun.WakeResult{}, fmt.Errorf("agentrunstore: resume task: %w", err)
	}
	return agentrun.WakeResult{Accepted: true, Task: task}, commit(ctx, tx)
}

// List returns every task of this tenant ordered by id (byte order, like
// MemoryStore).
func (s *TenantStore) List(ctx context.Context) ([]agentrun.AgentTask, error) {
	return s.query(ctx, `SELECT `+taskColumns+` FROM agent_task WHERE tenant_id=$1 ORDER BY task_id COLLATE "C"`, s.tenant)
}

// DueWaits returns this tenant's tasks that a scheduler must act on at now: a
// WAITING timer whose due time has passed, a parked wait whose stale_after has
// passed, and any open task past its expiry. It reads through the partial
// indexes on the wake and expiry columns, ordered by id.
func (s *TenantStore) DueWaits(ctx context.Context, now time.Time) ([]agentrun.AgentTask, error) {
	return s.query(ctx, `SELECT `+taskColumns+` FROM agent_task WHERE tenant_id=$1 AND (
		(wake_kind='TIMER' AND state='WAITING' AND wake_due_at <= $2)
		OR (wake_stale_after IS NOT NULL AND state IN ('WAITING','AWAITING_APPROVAL') AND wake_stale_after <= $2)
		OR (state NOT IN ('COMPLETED','FAILED','CANCELLED','EXPIRED') AND expires_at <= $2))
		ORDER BY task_id COLLATE "C"`, s.tenant, now.UTC())
}

func (s *TenantStore) query(ctx context.Context, sql string, args ...any) ([]agentrun.AgentTask, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("agentrunstore: query: %w", err)
	}
	defer rows.Close()
	out := make([]agentrun.AgentTask, 0)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agentrunstore: query rows: %w", err)
	}
	rows.Close()
	return out, commit(ctx, tx)
}

func commit(ctx context.Context, tx dbport.Tx) error {
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("agentrunstore: commit: %w", err)
	}
	return nil
}

// wakeMatches mirrors MemoryStore.ClaimWake's accept rule.
func wakeMatches(task agentrun.AgentTask, event agentrun.WakeEvent) bool {
	if !task.ExpiresAt.After(event.OccurredAt) || (event.Kind == agentrun.WakeApproval && task.CurrentStep < len(task.Plan.Steps) && task.Plan.Steps[task.CurrentStep].State == agentrun.StepAwaitingApproval) {
		return false
	}
	w := task.Wake
	if task.State == agentrun.StatePaused || w == nil || w.Kind != event.Kind || w.Key != event.Key {
		return false
	}
	switch task.State {
	case agentrun.StateCompleted, agentrun.StateFailed, agentrun.StateCancelled, agentrun.StateExpired:
		return false
	}
	if event.Kind == agentrun.WakeTimer && event.OccurredAt.Before(w.DueAt) {
		return false
	}
	return w.Correlation == "" || w.Correlation == event.Correlation
}

func terminalState(state agentrun.TaskState) bool {
	return state == agentrun.StateCompleted || state == agentrun.StateFailed || state == agentrun.StateCancelled || state == agentrun.StateExpired
}

func validWakeKind(k agentrun.WakeKind) bool {
	switch k {
	case agentrun.WakeApproval, agentrun.WakeSignal, agentrun.WakeTimer, agentrun.WakeUserReply, agentrun.WakePolling:
		return true
	}
	return false
}

func validState(s agentrun.TaskState) bool {
	switch s {
	case agentrun.StateDrafting, agentrun.StateAwaitingPlanConfirmation, agentrun.StateRunning, agentrun.StateWaiting,
		agentrun.StateAwaitingApproval, agentrun.StatePaused, agentrun.StateCompleted, agentrun.StateFailed,
		agentrun.StateCancelled, agentrun.StateExpired:
		return true
	}
	return false
}

// fields is the column image of one task.
type fields struct {
	id, tenantRef, userID, goal                     string
	constraints, plan, ledger                       []byte
	planDigest                                      string
	planConfirmed                                   bool
	state                                           string
	version                                         int64
	currentStep                                     int
	wake                                            []byte
	wakeKind, wakeKey                               *string
	wakeDue, wakeStale                              *time.Time
	pausedState                                     string
	pausedWake                                      []byte
	lastWake                                        []byte
	lineage                                         []byte
	workerLease, modelSession, failCode, failDetail string
	createdAt, updatedAt, expiresAt                 time.Time
}

func (s *TenantStore) encode(task agentrun.AgentTask) (fields, error) {
	if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.UserID) == "" {
		return fields{}, fmt.Errorf("%w: task and user ids are required", agentrun.ErrInvalid)
	}
	if task.TenantID != s.tenantRef {
		return fields{}, fmt.Errorf("%w: task %q names tenant %q, scope is %q", ErrTenantMismatch, task.ID, task.TenantID, s.tenantRef)
	}
	if !validState(task.State) || (task.PausedState != "" && !validState(task.PausedState)) {
		return fields{}, fmt.Errorf("%w: unknown task state", agentrun.ErrInvalid)
	}
	if task.Version == 0 || task.Version > math.MaxInt64 || task.CurrentStep < 0 {
		return fields{}, fmt.Errorf("%w: version must be positive and the step cursor non-negative", agentrun.ErrInvalid)
	}
	for _, w := range []*agentrun.WakeCondition{task.Wake, task.PausedWake} {
		if w != nil && (!validWakeKind(w.Kind) || strings.TrimSpace(w.Key) == "") {
			return fields{}, fmt.Errorf("%w: wake condition needs a kind and key", agentrun.ErrInvalid)
		}
	}
	plan := task.Plan
	if agentdocref.Validate(plan.DocumentReferences, agentdocref.MaxRequestReferences) != nil || agentrun.ValidateTaskDocumentOmissions(plan.DocumentReferences, plan.DocumentOmissions) != nil {
		return fields{}, agentrun.ErrDocumentReferenceInvalid
	}
	if plan.AnsweringAgent != nil && plan.AnsweringAgent.Validate() != nil {
		return fields{}, agentrun.ErrTaskAgentInvalid
	}
	plan.ConfirmedAt = plan.ConfirmedAt.UTC()
	f := fields{id: task.ID, tenantRef: task.TenantID, userID: task.UserID, goal: task.Goal, planDigest: plan.Digest, planConfirmed: plan.Confirmed,
		state: string(task.State), version: int64(task.Version), currentStep: task.CurrentStep, pausedState: string(task.PausedState),
		workerLease: task.WorkerLease, modelSession: task.ModelSession, failCode: task.FailureCode, failDetail: task.FailureDetail,
		createdAt: task.CreatedAt.UTC(), updatedAt: task.UpdatedAt.UTC(), expiresAt: task.ExpiresAt.UTC()}
	var err error
	if f.lineage, err = encodeTaskLineage(task); err != nil {
		return fields{}, err
	}
	if f.constraints, err = json.Marshal(task.Constraints); err != nil {
		return fields{}, err
	}
	if f.plan, err = json.Marshal(plan); err != nil {
		return fields{}, err
	}
	if f.ledger, err = json.Marshal(task.Ledger); err != nil {
		return fields{}, err
	}
	if task.Wake != nil {
		w := utcWake(*task.Wake)
		if f.wake, err = json.Marshal(w); err != nil {
			return fields{}, err
		}
		kind, key := string(w.Kind), w.Key
		f.wakeKind, f.wakeKey = &kind, &key
		if !w.DueAt.IsZero() {
			f.wakeDue = &w.DueAt
		}
		if !w.StaleAfter.IsZero() {
			f.wakeStale = &w.StaleAfter
		}
	}
	if task.PausedWake != nil {
		if f.pausedWake, err = json.Marshal(utcWake(*task.PausedWake)); err != nil {
			return fields{}, err
		}
	}
	if task.LastWake != nil {
		event := *task.LastWake
		event.OccurredAt = event.OccurredAt.UTC()
		if f.lastWake, err = json.Marshal(event); err != nil {
			return fields{}, err
		}
	}
	return f, nil
}

func utcWake(w agentrun.WakeCondition) agentrun.WakeCondition {
	w.DueAt, w.StaleAfter = w.DueAt.UTC(), w.StaleAfter.UTC()
	return w
}

// jsonArg passes a nil byte slice as SQL NULL rather than an empty document.
func jsonArg(b []byte) any {
	if b == nil {
		return nil
	}
	return string(b)
}

func (f fields) args(tenant uuid.UUID) []any {
	return []any{tenant, f.id, f.tenantRef, f.userID, f.goal, string(f.constraints), string(f.plan), f.planDigest, f.planConfirmed,
		f.state, f.version, f.currentStep, jsonArg(f.wake), f.wakeKind, f.wakeKey, f.wakeDue, f.wakeStale, f.pausedState, jsonArg(f.pausedWake),
		f.workerLease, f.modelSession, f.failCode, f.failDetail, string(f.ledger), f.createdAt, f.updatedAt, f.expiresAt, jsonArg(f.lastWake), jsonArg(f.lineage)}
}

func (f fields) updateArgs(tenant uuid.UUID, expected int64) []any {
	return []any{tenant, f.id, f.userID, f.goal, string(f.constraints), string(f.plan), f.planDigest, f.planConfirmed,
		f.state, f.version, f.currentStep, jsonArg(f.wake), f.wakeKind, f.wakeKey, f.wakeDue, f.wakeStale, f.pausedState, jsonArg(f.pausedWake),
		f.workerLease, f.modelSession, f.failCode, f.failDetail, string(f.ledger), f.createdAt, f.updatedAt, f.expiresAt, expected, jsonArg(f.lastWake), jsonArg(f.lineage)}
}

type scanner interface{ Scan(...any) error }

func scanTask(row scanner) (agentrun.AgentTask, error) {
	var (
		t                                  agentrun.AgentTask
		state, paused                      string
		version                            int64
		constraints, plan, ledger          []byte
		wake, pausedWake                   []byte
		lastWake                           []byte
		lineage                            []byte
		created, updated, expires          time.Time
		id, tenantRef, userID, goal        string
		lease, session, failCode, failText string
	)
	if err := row.Scan(&id, &tenantRef, &userID, &goal, &constraints, &plan, &state, &version, &t.CurrentStep,
		&wake, &paused, &pausedWake, &lease, &session, &failCode, &failText, &ledger, &created, &updated, &expires, &lastWake, &lineage); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return agentrun.AgentTask{}, err
		}
		return agentrun.AgentTask{}, fmt.Errorf("agentrunstore: scan task: %w", err)
	}
	t.ID, t.TenantID, t.UserID, t.Goal = id, tenantRef, userID, goal
	t.State, t.PausedState, t.Version = agentrun.TaskState(state), agentrun.TaskState(paused), uint64(version)
	t.WorkerLease, t.ModelSession, t.FailureCode, t.FailureDetail = lease, session, failCode, failText
	t.CreatedAt, t.UpdatedAt, t.ExpiresAt = created.UTC(), updated.UTC(), expires.UTC()
	if err := decodeTaskLineage(lineage, &t); err != nil {
		return agentrun.AgentTask{}, err
	}
	if err := json.Unmarshal(constraints, &t.Constraints); err != nil {
		return agentrun.AgentTask{}, fmt.Errorf("agentrunstore: decode constraints: %w", err)
	}
	if err := json.Unmarshal(plan, &t.Plan); err != nil {
		return agentrun.AgentTask{}, fmt.Errorf("agentrunstore: decode plan: %w", err)
	}
	if agentdocref.Validate(t.Plan.DocumentReferences, agentdocref.MaxRequestReferences) != nil || agentrun.ValidateTaskDocumentOmissions(t.Plan.DocumentReferences, t.Plan.DocumentOmissions) != nil {
		return agentrun.AgentTask{}, agentrun.ErrDocumentReferenceInvalid
	}
	if t.Plan.AnsweringAgent != nil && t.Plan.AnsweringAgent.Validate() != nil {
		return agentrun.AgentTask{}, agentrun.ErrTaskAgentInvalid
	}
	if err := json.Unmarshal(ledger, &t.Ledger); err != nil {
		return agentrun.AgentTask{}, fmt.Errorf("agentrunstore: decode ledger: %w", err)
	}
	if lastWake != nil {
		if err := json.Unmarshal(lastWake, &t.LastWake); err != nil {
			return agentrun.AgentTask{}, fmt.Errorf("agentrunstore: decode accepted wake: %w", err)
		}
	}
	for _, d := range []struct {
		raw []byte
		to  **agentrun.WakeCondition
	}{{wake, &t.Wake}, {pausedWake, &t.PausedWake}} {
		if d.raw == nil {
			continue
		}
		w := new(agentrun.WakeCondition)
		if err := json.Unmarshal(d.raw, w); err != nil {
			return agentrun.AgentTask{}, fmt.Errorf("agentrunstore: decode wake: %w", err)
		}
		*d.to = w
	}
	return t, nil
}
