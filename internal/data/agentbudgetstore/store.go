// Package agentbudgetstore makes the agentbudget ledger durable. Store
// implements agentbudget.Persister: the ledger calls it under its own lock
// after every state transition, and each call commits one PostgreSQL
// transaction bound to the task's tenant (row-level security tenant_isolation).
// Load returns a tenant's stored state for agentbudget.Ledger.Restore.
//
// What is durable: task specs, task revisions, settled usage, the pause card, the attempt and
// failure counters, per-user-day and per-tenant-month settled usage, and an
// append-only journal of open, settle, fail, extension and pause decisions.
// In-flight reservations are not stored: after a restart they are gone (the
// restart releases them) while the attempt counter written when the
// reservation was admitted still counts the interrupted try.
//
// The ledger is the single writer of a tenant's rows; the store writes the
// absolute counters the ledger hands it.
package agentbudgetstore

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrInvalid marks a store that is not wired or a transition it cannot map.
	ErrInvalid = errors.New("agentbudgetstore: invalid request")
	// ErrTaskExists is returned when OPEN_TASK finds the task already stored.
	ErrTaskExists = errors.New("agentbudgetstore: task already stored")
	// ErrTaskMissing is returned when a later transition finds no stored task.
	ErrTaskMissing = errors.New("agentbudgetstore: task is not stored")
)

// writeTimeout bounds one durable write. The ledger holds its lock while the
// write runs, so a stalled database must fail the transition, not hang it.
const writeTimeout = 10 * time.Second

// DB is the transaction opener the store needs.
type DB interface{ dbport.Beginner }

// Store persists agentbudget transitions.
type Store struct {
	db         DB
	tenantUUID func(values.TenantId) uuid.UUID
}

var _ agentbudget.Persister = (*Store)(nil)

// New builds a Store. tenantUUID maps a tenant key to its canonical UUID and
// returns uuid.Nil for an unknown tenant.
func New(db DB, tenantUUID func(values.TenantId) uuid.UUID) (*Store, error) {
	if db == nil || tenantUUID == nil {
		return nil, fmt.Errorf("%w: database and canonical tenant mapper are required", ErrInvalid)
	}
	return &Store{db: db, tenantUUID: tenantUUID}, nil
}

func (s *Store) begin(ctx context.Context, tenantKey string) (dbport.Tx, uuid.UUID, error) {
	if s == nil || s.db == nil || strings.TrimSpace(tenantKey) == "" {
		return nil, uuid.Nil, fmt.Errorf("%w: tenant is required", ErrInvalid)
	}
	tenant := s.tenantUUID(values.TenantId(tenantKey))
	if tenant == uuid.Nil {
		return nil, uuid.Nil, fmt.Errorf("%w: unknown tenant %q", ErrInvalid, tenantKey)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("agentbudgetstore: begin: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return nil, uuid.Nil, err
	}
	return tx, tenant, nil
}

// Persist implements agentbudget.Persister.
func (s *Store) Persist(t agentbudget.Transition) error {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	task := t.Task
	tx, tenant, err := s.begin(ctx, task.Spec.TenantID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	at := t.At.UTC()
	if at.IsZero() {
		at = time.Now().UTC()
	}
	img, err := encodeTask(task)
	if err != nil {
		return err
	}
	if t.Kind == agentbudget.TransitionOpenTask {
		n, err := tx.Exec(ctx, `INSERT INTO agent_budget_task (tenant_id, task_id, tenant_ref, user_id, revision, limit_steps, limit_tokens, limit_wall_ns, limit_spend,
			used_steps, used_tokens, used_wall_ns, used_spend, pause_reason, pause_card, attempts, failures, created_at, updated_at, parent_task_id, root_task_id, delegation_depth)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16::jsonb,$17::jsonb,$18,$19,$20,$21,$22) ON CONFLICT (tenant_id, task_id) DO NOTHING`,
			append([]any{tenant, task.Spec.ID, task.Spec.TenantID, task.Spec.UserID, task.Revision}, append(img.args(), at, at, nullLineage(task.Spec.ParentTaskID), nullLineage(task.Spec.RootTaskID), task.Spec.Depth)...)...)
		if err != nil {
			return fmt.Errorf("agentbudgetstore: open task: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("%w: %s", ErrTaskExists, task.Spec.ID)
		}
	} else {
		if t.Kind == agentbudget.TransitionExtension {
			if replayed, err := s.persistExtensionReplay(ctx, tx, tenant, t, at); err != nil {
				return err
			} else if replayed {
				if err := tx.Commit(ctx); err != nil {
					return fmt.Errorf("agentbudgetstore: replay commit: %w", err)
				}
				return nil
			}
		}
		n, err := tx.Exec(ctx, `UPDATE agent_budget_task SET revision=$3, limit_steps=$4, limit_tokens=$5, limit_wall_ns=$6, limit_spend=$7,
			used_steps=$8, used_tokens=$9, used_wall_ns=$10, used_spend=$11, pause_reason=$12, pause_card=$13::jsonb,
			attempts=$14::jsonb, failures=$15::jsonb, updated_at=$16 WHERE tenant_id=$1 AND task_id=$2 AND revision=$17`,
			append([]any{tenant, task.Spec.ID, task.Revision}, append(img.args(), at, t.ExpectedRevision)...)...)
		if err != nil {
			return fmt.Errorf("agentbudgetstore: update task: %w", err)
		}
		if n == 0 {
			if t.ExpectedRevision > 0 {
				return fmt.Errorf("%w: task %s expected revision %d", agentbudget.ErrStaleRevision, task.Spec.ID, t.ExpectedRevision)
			}
			return fmt.Errorf("%w: %s", ErrTaskMissing, task.Spec.ID)
		}
	}
	for _, related := range t.RelatedTasks {
		if related.Spec.TenantID != task.Spec.TenantID || related.Revision == 0 {
			return fmt.Errorf("%w: related task lineage", ErrInvalid)
		}
		relatedImg, err := encodeTask(related)
		if err != nil {
			return err
		}
		n, err := tx.Exec(ctx, `UPDATE agent_budget_task SET revision=$3, limit_steps=$4, limit_tokens=$5, limit_wall_ns=$6, limit_spend=$7,
			used_steps=$8, used_tokens=$9, used_wall_ns=$10, used_spend=$11, pause_reason=$12, pause_card=$13::jsonb,
			attempts=$14::jsonb, failures=$15::jsonb, updated_at=$16 WHERE tenant_id=$1 AND task_id=$2 AND revision=$17`,
			append([]any{tenant, related.Spec.ID, related.Revision}, append(relatedImg.args(), at, related.Revision-1)...)...)
		if err != nil {
			return fmt.Errorf("agentbudgetstore: update related task: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("%w: related task %s", ErrTaskMissing, related.Spec.ID)
		}
	}
	for _, p := range t.Periods {
		if p.Scope != agentbudget.ScopeUser && p.Scope != agentbudget.ScopeTenant {
			return fmt.Errorf("%w: period scope %q", ErrInvalid, p.Scope)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agent_budget_period (tenant_id, scope, period_key, period_start, used_steps, used_tokens, used_wall_ns, used_spend, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (tenant_id, scope, period_key) DO UPDATE SET used_steps=EXCLUDED.used_steps, used_tokens=EXCLUDED.used_tokens,
				used_wall_ns=EXCLUDED.used_wall_ns, used_spend=EXCLUDED.used_spend, updated_at=EXCLUDED.updated_at`,
			tenant, string(p.Scope), p.Key, p.Start.UTC(), p.Used.Steps, p.Used.Tokens, int64(p.Used.WallClock), p.Used.SpendMicros, at); err != nil {
			return fmt.Errorf("agentbudgetstore: write period: %w", err)
		}
	}
	if event, ok := journalEntry(t, task); ok {
		if _, err := tx.Exec(ctx, `INSERT INTO agent_budget_event (tenant_id, task_id, kind, reservation_id, step_id, steps, tokens, wall_ns, spend, pause_reason, occurred_at, request_id, expected_revision, result_revision)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
			tenant, task.Spec.ID, string(event.kind), t.ReservationID, t.StepID, event.amount.Steps, event.amount.Tokens, int64(event.amount.WallClock), event.amount.SpendMicros, event.pause, at, t.RequestID, t.ExpectedRevision, t.ResultRevision); err != nil {
			return fmt.Errorf("agentbudgetstore: journal: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("agentbudgetstore: commit: %w", err)
	}
	return nil
}

type journal struct {
	kind   agentbudget.TransitionKind
	amount agentbudget.Limits
	pause  string
}

func (s *Store) persistExtensionReplay(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, t agentbudget.Transition, at time.Time) (bool, error) {
	additional, err := json.Marshal(t.Extension)
	if err != nil {
		return false, err
	}
	limits, err := json.Marshal(t.Result.Limit)
	if err != nil {
		return false, err
	}
	metadata, err := json.Marshal(t.Result)
	if err != nil {
		return false, err
	}
	n, err := tx.Exec(ctx, `INSERT INTO agent_budget_extension_replay
		(tenant_id, task_id, request_id, expected_revision, result_revision, additional, result_limits, additional_metadata, created_at)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8::jsonb,$9)
		ON CONFLICT (tenant_id, task_id, request_id) DO NOTHING`,
		tenant, t.Task.Spec.ID, t.RequestID, t.ExpectedRevision, t.ResultRevision, string(additional), string(limits), string(metadata), at)
	if err != nil {
		return false, fmt.Errorf("agentbudgetstore: extension replay: %w", err)
	}
	if n > 0 {
		return false, nil
	}
	var expected, resultRevision uint64
	var storedAdditional, storedLimits, storedMetadata []byte
	err = tx.QueryRow(ctx, `SELECT expected_revision, result_revision, additional, result_limits, additional_metadata
		FROM agent_budget_extension_replay WHERE tenant_id=$1 AND task_id=$2 AND request_id=$3`, tenant, t.Task.Spec.ID, t.RequestID).
		Scan(&expected, &resultRevision, &storedAdditional, &storedLimits, &storedMetadata)
	if err != nil {
		return false, fmt.Errorf("agentbudgetstore: load extension replay: %w", err)
	}
	var additionalLimits agentbudget.Limits
	if err := json.Unmarshal(storedAdditional, &additionalLimits); err != nil {
		return false, fmt.Errorf("agentbudgetstore: decode extension replay: %w", err)
	}
	var storedResultLimit agentbudget.Limits
	if err := json.Unmarshal(storedLimits, &storedResultLimit); err != nil {
		return false, fmt.Errorf("agentbudgetstore: decode extension replay limits: %w", err)
	}
	var result agentbudget.ExtensionResult
	if err := json.Unmarshal(storedMetadata, &result); err != nil {
		return false, fmt.Errorf("agentbudgetstore: decode extension result: %w", err)
	}
	if expected != t.ExpectedRevision || resultRevision != t.ResultRevision || additionalLimits != t.Extension || storedResultLimit != t.Result.Limit || result.Limit != t.Result.Limit {
		return false, agentbudget.ErrExtensionConflict
	}
	return true, nil
}

// journalEntry maps a transition to its append-only record. RESERVE is not
// journaled (an admission with no durable outcome yet); a FAIL that raised a
// pause records the reason.
func journalEntry(t agentbudget.Transition, task agentbudget.DurableTask) (journal, bool) {
	pause := ""
	if task.Paused != nil {
		pause = string(task.Paused.Reason)
	}
	switch t.Kind {
	case agentbudget.TransitionOpenTask:
		return journal{kind: t.Kind, amount: task.Spec.Limit}, true
	case agentbudget.TransitionSettle:
		return journal{kind: t.Kind, amount: t.Actual}, true
	case agentbudget.TransitionFail:
		return journal{kind: t.Kind, pause: pause}, true
	case agentbudget.TransitionExtension:
		return journal{kind: t.Kind, amount: t.Extension}, true
	case agentbudget.TransitionPause:
		return journal{kind: t.Kind, pause: pause}, true
	}
	return journal{}, false
}

// taskImage is the column image shared by insert and update: limits, used,
// pause reason and card, attempts, failures.
type taskImage struct {
	limit, used     agentbudget.Limits
	pauseReason     string
	pauseCard       any
	attempts, fails string
}

func (i taskImage) args() []any {
	return []any{i.limit.Steps, i.limit.Tokens, int64(i.limit.WallClock), i.limit.SpendMicros,
		i.used.Steps, i.used.Tokens, int64(i.used.WallClock), i.used.SpendMicros,
		i.pauseReason, i.pauseCard, i.attempts, i.fails}
}

type pauseJSON struct {
	Reason agentbudget.PauseReason   `json:"reason"`
	Scope  agentbudget.Scope         `json:"scope"`
	TaskID string                    `json:"task_id"`
	Card   agentbudget.ExtensionCard `json:"card"`
}

func nullLineage(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func encodeTask(t agentbudget.DurableTask) (taskImage, error) {
	img := taskImage{limit: t.Spec.Limit, used: t.Used}
	if t.Paused != nil {
		raw, err := json.Marshal(pauseJSON{Reason: t.Paused.Reason, Scope: t.Paused.Scope, TaskID: t.Paused.TaskID, Card: t.Paused.Card})
		if err != nil {
			return taskImage{}, err
		}
		img.pauseReason, img.pauseCard = string(t.Paused.Reason), string(raw)
		if img.pauseReason == "" {
			return taskImage{}, fmt.Errorf("%w: a pause needs a reason", ErrInvalid)
		}
	}
	var err error
	if img.attempts, err = encodeCounts(t.Attempts); err != nil {
		return taskImage{}, err
	}
	if img.fails, err = encodeCounts(t.Failures); err != nil {
		return taskImage{}, err
	}
	return img, nil
}

// encodeCounts hex-encodes the ledger's raw keys: a failure key contains a
// NUL byte, which jsonb text cannot hold.
func encodeCounts(m map[string]int) (string, error) {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[hex.EncodeToString([]byte(k))] = v
	}
	raw, err := json.Marshal(out)
	return string(raw), err
}

func decodeCounts(raw []byte) (map[string]int, error) {
	var enc map[string]int
	if err := json.Unmarshal(raw, &enc); err != nil {
		return nil, err
	}
	out := make(map[string]int, len(enc))
	for k, v := range enc {
		key, err := hex.DecodeString(k)
		if err != nil {
			return nil, err
		}
		out[string(key)] = v
	}
	return out, nil
}

// Load returns one tenant's durable state for agentbudget.Ledger.Restore: every
// stored task, and the period counters of now's month (the current day and
// month; older periods can no longer admit or bill anything).
func (s *Store) Load(ctx context.Context, tenant values.TenantId, now time.Time) (agentbudget.RestoredState, error) {
	tx, id, err := s.begin(ctx, string(tenant))
	if err != nil {
		return agentbudget.RestoredState{}, err
	}
	defer tx.Rollback(ctx)
	var state agentbudget.RestoredState
	rows, err := tx.Query(ctx, `SELECT task_id, tenant_ref, user_id, revision, limit_steps, limit_tokens, limit_wall_ns, limit_spend,
		used_steps, used_tokens, used_wall_ns, used_spend, pause_card, attempts, failures, parent_task_id, root_task_id, delegation_depth
		FROM agent_budget_task WHERE tenant_id=$1 ORDER BY task_id COLLATE "C"`, id)
	if err != nil {
		return state, fmt.Errorf("agentbudgetstore: load tasks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var t agentbudget.DurableTask
		var limitWall, usedWall int64
		var card, attempts, failures []byte
		var parentID, rootID *string
		var depth int
		if err := rows.Scan(&t.Spec.ID, &t.Spec.TenantID, &t.Spec.UserID, &t.Revision, &t.Spec.Limit.Steps, &t.Spec.Limit.Tokens, &limitWall, &t.Spec.Limit.SpendMicros,
			&t.Used.Steps, &t.Used.Tokens, &usedWall, &t.Used.SpendMicros, &card, &attempts, &failures, &parentID, &rootID, &depth); err != nil {
			return state, fmt.Errorf("agentbudgetstore: scan task: %w", err)
		}
		t.Spec.Limit.WallClock, t.Used.WallClock = time.Duration(limitWall), time.Duration(usedWall)
		if parentID != nil {
			t.Spec.ParentTaskID = *parentID
		}
		if rootID != nil {
			t.Spec.RootTaskID = *rootID
		}
		t.Spec.Depth = depth
		if card != nil {
			var p pauseJSON
			if err := json.Unmarshal(card, &p); err != nil {
				return state, fmt.Errorf("agentbudgetstore: decode pause card: %w", err)
			}
			t.Paused = &agentbudget.PauseError{Reason: p.Reason, Scope: p.Scope, TaskID: p.TaskID, Card: p.Card}
		}
		if t.Attempts, err = decodeCounts(attempts); err != nil {
			return state, fmt.Errorf("agentbudgetstore: decode attempts: %w", err)
		}
		if t.Failures, err = decodeCounts(failures); err != nil {
			return state, fmt.Errorf("agentbudgetstore: decode failures: %w", err)
		}
		state.Tasks = append(state.Tasks, t)
	}
	if err := rows.Err(); err != nil {
		return state, fmt.Errorf("agentbudgetstore: load tasks: %w", err)
	}
	rows.Close()
	monthStart := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	prows, err := tx.Query(ctx, `SELECT scope, period_key, period_start, used_steps, used_tokens, used_wall_ns, used_spend
		FROM agent_budget_period WHERE tenant_id=$1 AND period_start >= $2 ORDER BY scope, period_key COLLATE "C"`, id, monthStart)
	if err != nil {
		return state, fmt.Errorf("agentbudgetstore: load periods: %w", err)
	}
	defer prows.Close()
	for prows.Next() {
		var p agentbudget.DurablePeriod
		var scope string
		var start time.Time
		var wall int64
		if err := prows.Scan(&scope, &p.Key, &start, &p.Used.Steps, &p.Used.Tokens, &wall, &p.Used.SpendMicros); err != nil {
			return state, fmt.Errorf("agentbudgetstore: scan period: %w", err)
		}
		p.Scope, p.Start, p.Used.WallClock = agentbudget.Scope(scope), start.UTC(), time.Duration(wall)
		state.Periods = append(state.Periods, p)
	}
	if err := prows.Err(); err != nil {
		return state, fmt.Errorf("agentbudgetstore: load periods: %w", err)
	}
	prows.Close()
	rerows, err := tx.Query(ctx, `SELECT task_id, request_id, expected_revision, result_revision, additional, result_limits, additional_metadata
		FROM agent_budget_extension_replay WHERE tenant_id=$1 ORDER BY task_id COLLATE "C", request_id COLLATE "C"`, id)
	if err != nil {
		return state, fmt.Errorf("agentbudgetstore: load extension replays: %w", err)
	}
	defer rerows.Close()
	for rerows.Next() {
		var replay agentbudget.DurableExtensionReplay
		var additional, resultLimits, metadata []byte
		if err := rerows.Scan(&replay.TaskID, &replay.RequestID, &replay.ExpectedRevision, &replay.ResultRevision, &additional, &resultLimits, &metadata); err != nil {
			return state, fmt.Errorf("agentbudgetstore: scan extension replay: %w", err)
		}
		if err := json.Unmarshal(additional, &replay.Additional); err != nil {
			return state, fmt.Errorf("agentbudgetstore: decode extension replay additional: %w", err)
		}
		var canonicalLimit agentbudget.Limits
		if err := json.Unmarshal(resultLimits, &canonicalLimit); err != nil {
			return state, fmt.Errorf("agentbudgetstore: decode extension replay limits: %w", err)
		}
		if err := json.Unmarshal(metadata, &replay.Result); err != nil {
			return state, fmt.Errorf("agentbudgetstore: decode extension replay result: %w", err)
		}
		// result_limits is the canonical compact replay payload. Metadata is
		// supplementary audit information and must agree when it contains a
		// result limit.
		metadataResult := replay.Result
		if metadataResult.Limit != (agentbudget.Limits{}) && metadataResult.Limit != canonicalLimit {
			return state, fmt.Errorf("agentbudgetstore: extension replay result limit mismatch for %s", replay.RequestID)
		}
		replay.Result = metadataResult
		replay.Result.Limit = canonicalLimit
		state.Replays = append(state.Replays, replay)
	}
	if err := rerows.Err(); err != nil {
		return state, fmt.Errorf("agentbudgetstore: load extension replays: %w", err)
	}
	rerows.Close()
	if err := tx.Commit(ctx); err != nil {
		return state, fmt.Errorf("agentbudgetstore: commit: %w", err)
	}
	return state, nil
}
