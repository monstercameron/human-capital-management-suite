package agentrunstate

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var (
	ErrTenant   = errors.New("agentrunstate: tenant does not own this run")
	ErrConflict = errors.New("agentrunstate: revision conflict")
	ErrNotFound = errors.New("agentrunstate: run not found")
)

// TenantRunner is the agent-store transaction seam.
type TenantRunner interface {
	RunTenantTx(context.Context, uuid.UUID, func(dbport.Tx) error) error
}

// Store opens tenant-scoped views of durable run state.
type Store struct {
	db         TenantRunner
	tenantUUID func(string) uuid.UUID
}

// New constructs a repository over the isolated agent database.
func New(db TenantRunner, tenantUUID func(string) uuid.UUID) (*Store, error) {
	if db == nil || tenantUUID == nil {
		return nil, errors.New("agentrunstate: database and tenant mapper required")
	}
	return &Store{db: db, tenantUUID: tenantUUID}, nil
}

// ForTenant returns the runstate port scoped to one tenant.
func (s *Store) ForTenant(tenant string) (*TenantStore, error) {
	if s == nil || s.db == nil || s.tenantUUID == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(tenant) != tenant {
		return nil, errors.New("agentrunstate: valid tenant required")
	}
	id := s.tenantUUID(tenant)
	if id == uuid.Nil {
		return nil, errors.New("agentrunstate: unknown tenant")
	}
	return &TenantStore{db: s.db, tenantID: id, tenantRef: tenant}, nil
}

// TenantStore is a database adapter whose transactions always set tenant RLS.
type TenantStore struct {
	db        TenantRunner
	tenantID  uuid.UUID
	tenantRef string
}

var _ runstate.Store = (*TenantStore)(nil)

// Create durably inserts one run and its admission checkpoint atomically.
func (s *TenantStore) Create(ctx context.Context, run runstate.Run) error {
	if err := s.validate(run); err != nil {
		return err
	}
	return s.db.RunTenantTx(ctx, s.tenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO agent_run_execution
		(tenant_id,run_id,admission_id,request_digest,agent_id,agent_version,agent_digest,context_digest,deadline,state,revision,fence,
			 lease_owner,lease_until,cancel_requested,expire_requested,failure_requested,terminal_code,created_at,updated_at,retryable,wait_kind,wait_ref)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NULL,NULL,$13,$14,$15,$16,$17,$18,$19,$20,$21)`,
			s.tenantID, run.ID, run.AdmissionID, run.RequestDigest, run.AgentID, run.AgentVersion, run.AgentDigest, run.ContextDigest,
			run.Deadline.UTC(), string(run.State), int64(run.Version), int64(run.Fence), run.CancelRequested, run.ExpireRequested, run.FailureRequested, nullable(run.TerminalCode), run.CreatedAt.UTC(), run.UpdatedAt.UTC(), run.Retryable, string(run.WaitKind), run.WaitRef)
		if err != nil {
			return fmt.Errorf("agentrunstate: create run: %w", err)
		}
		return insertCheckpoints(ctx, tx, s.tenantID, run.ID, run.Checkpoints)
	})
}

// Get loads a run snapshot and its ordered durable evidence.
func (s *TenantStore) Get(ctx context.Context, id string) (runstate.Run, error) {
	var run runstate.Run
	err := s.db.RunTenantTx(ctx, s.tenantID, func(tx dbport.Tx) error {
		var state string
		var version, fence int64
		var owner *string
		var until *time.Time
		var terminalCode *string
		err := tx.QueryRow(ctx, `SELECT run_id,admission_id,request_digest,agent_id,agent_version,agent_digest,context_digest,deadline,
			state,revision,fence,lease_owner,lease_until,cancel_requested,expire_requested,failure_requested,terminal_code,created_at,updated_at,retryable,wait_kind,wait_ref,
			(SELECT principal_chain->>'mode' FROM agent_run_request a WHERE a.tenant_id=$1 AND a.request_id=agent_run_execution.admission_id),
			(SELECT CASE WHEN principal_chain->>'mode'='SPONSORED' THEN principal_chain->>'sponsor_id' ELSE principal_chain->>'invoker_id' END
			 FROM agent_run_request a WHERE a.tenant_id=$1 AND a.request_id=agent_run_execution.admission_id)
			FROM agent_run_execution WHERE tenant_id=$1 AND run_id=$2`, s.tenantID, id).
			Scan(&run.ID, &run.AdmissionID, &run.RequestDigest, &run.AgentID, &run.AgentVersion, &run.AgentDigest, &run.ContextDigest, &run.Deadline,
				&state, &version, &fence, &owner, &until, &run.CancelRequested, &run.ExpireRequested, &run.FailureRequested, &terminalCode, &run.CreatedAt, &run.UpdatedAt, &run.Retryable, &run.WaitKind, &run.WaitRef, &run.PrincipalMode, &run.ActorID)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("agentrunstate: read run: %w", err)
		}
		run.TenantID, run.State, run.Version, run.Fence = s.tenantRef, runstate.State(state), uint64(version), uint64(fence)
		if owner != nil && until != nil {
			run.Lease = &runstate.Lease{Owner: *owner, Fence: run.Fence, Until: until.UTC()}
		}
		if terminalCode != nil {
			run.TerminalCode = *terminalCode
		}
		if err := readCheckpoints(ctx, tx, s.tenantID, id, &run); err != nil {
			return err
		}
		return readEffects(ctx, tx, s.tenantID, id, &run)
	})
	return run, err
}

// Save compare-and-swaps the snapshot, appending checkpoints and reconciling
// effects in the same tenant transaction.
func (s *TenantStore) Save(ctx context.Context, run runstate.Run, expected uint64) error {
	if err := s.validate(run); err != nil {
		return err
	}
	if expected >= math.MaxInt64 || run.Version != expected+1 || run.Fence > math.MaxInt64 {
		return ErrConflict
	}
	return s.db.RunTenantTx(ctx, s.tenantID, func(tx dbport.Tx) error {
		var current, checkpointCount int64
		if err := tx.QueryRow(ctx, `SELECT revision,(SELECT count(*) FROM agent_run_checkpoint WHERE tenant_id=$1 AND run_id=$2)
			FROM agent_run_execution WHERE tenant_id=$1 AND run_id=$2 FOR UPDATE`, s.tenantID, run.ID).Scan(&current, &checkpointCount); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("agentrunstate: lock run: %w", err)
		}
		if uint64(current) != expected {
			return ErrConflict
		}
		var persisted runstate.Run
		if err := readCheckpoints(ctx, tx, s.tenantID, run.ID, &persisted); err != nil {
			return err
		}
		if int64(len(run.Checkpoints)) < checkpointCount {
			return fmt.Errorf("%w: checkpoint history truncated", ErrConflict)
		}
		for i, checkpoint := range persisted.Checkpoints {
			candidate := run.Checkpoints[i]
			if candidate.Sequence != checkpoint.Sequence || candidate.Phase != checkpoint.Phase || candidate.Attempt != checkpoint.Attempt || candidate.Ref != checkpoint.Ref || candidate.Digest != checkpoint.Digest || !candidate.At.Equal(checkpoint.At) {
				return fmt.Errorf("%w: committed checkpoint history changed", ErrConflict)
			}
		}
		if err := readEffects(ctx, tx, s.tenantID, run.ID, &persisted); err != nil {
			return err
		}
		for _, effect := range persisted.Effects {
			if err := preserveEffect(effect, run.Effects); err != nil {
				return err
			}
		}
		leaseOwner, leaseUntil := leaseValues(run.Lease)
		n, err := tx.Exec(ctx, `UPDATE agent_run_execution SET state=$3,revision=$4,fence=$5,lease_owner=$6,lease_until=$7,
			cancel_requested=$8,expire_requested=$9,failure_requested=$10,terminal_code=$11,updated_at=$12,retryable=$14,wait_kind=$15,wait_ref=$16 WHERE tenant_id=$1 AND run_id=$2 AND revision=$13`,
			s.tenantID, run.ID, string(run.State), int64(run.Version), int64(run.Fence), leaseOwner, leaseUntil, run.CancelRequested, run.ExpireRequested, run.FailureRequested, nullable(run.TerminalCode), run.UpdatedAt.UTC(), current, run.Retryable, string(run.WaitKind), run.WaitRef)
		if err != nil {
			return fmt.Errorf("agentrunstate: update run: %w", err)
		}
		if n != 1 {
			return ErrConflict
		}
		if err := insertCheckpoints(ctx, tx, s.tenantID, run.ID, run.Checkpoints[checkpointCount:]); err != nil {
			return err
		}
		return syncEffects(ctx, tx, s.tenantID, run.ID, run.Effects)
	})
}

func preserveEffect(previous runstate.Effect, candidates []runstate.Effect) error {
	for _, candidate := range candidates {
		if candidate.ID != previous.ID {
			continue
		}
		if candidate.IdempotencyKey != previous.IdempotencyKey || candidate.ArgumentsDigest != previous.ArgumentsDigest || !candidate.StartedAt.Equal(previous.StartedAt) {
			return fmt.Errorf("%w: committed effect identity changed", ErrConflict)
		}
		if previous.Status != runstate.EffectUnknown && (candidate.Status != previous.Status || candidate.ResultRef != previous.ResultRef || candidate.ResultDigest != previous.ResultDigest || !candidate.ResolvedAt.Equal(previous.ResolvedAt)) {
			return fmt.Errorf("%w: resolved effect evidence changed", ErrConflict)
		}
		return nil
	}
	return fmt.Errorf("%w: committed effect evidence removed", ErrConflict)
}

func (s *TenantStore) validate(run runstate.Run) error {
	if s == nil || s.db == nil || s.tenantID == uuid.Nil || run.TenantID != s.tenantRef || run.ID == "" || run.AdmissionID != run.ID {
		return ErrTenant
	}
	return nil
}

func insertCheckpoints(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, runID string, items []runstate.Checkpoint) error {
	for _, item := range items {
		_, err := tx.Exec(ctx, `INSERT INTO agent_run_checkpoint (tenant_id,run_id,sequence,phase,attempt,ref,digest,occurred_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, tenant, runID, int64(item.Sequence), string(item.Phase), int32(item.Attempt), nullable(item.Ref), nullable(item.Digest), item.At.UTC())
		if err != nil {
			return fmt.Errorf("agentrunstate: append checkpoint: %w", err)
		}
	}
	return nil
}

func readCheckpoints(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, id string, run *runstate.Run) error {
	rows, err := tx.Query(ctx, `SELECT sequence,phase,attempt,ref,digest,occurred_at FROM agent_run_checkpoint WHERE tenant_id=$1 AND run_id=$2 ORDER BY sequence`, tenant, id)
	if err != nil {
		return fmt.Errorf("agentrunstate: list checkpoints: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item runstate.Checkpoint
		var phase string
		var attempt int32
		var ref, dig *string
		if err := rows.Scan(&item.Sequence, &phase, &attempt, &ref, &dig, &item.At); err != nil {
			return err
		}
		item.Phase, item.Attempt = runstate.Phase(phase), uint32(attempt)
		if ref != nil {
			item.Ref = *ref
		}
		if dig != nil {
			item.Digest = *dig
		}
		item.At = item.At.UTC()
		run.Checkpoints = append(run.Checkpoints, item)
	}
	return rows.Err()
}

func readEffects(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, id string, run *runstate.Run) error {
	rows, err := tx.Query(ctx, `SELECT effect_id,idempotency_key,arguments_digest,status,result_ref,result_digest,started_at,resolved_at FROM agent_run_effect WHERE tenant_id=$1 AND run_id=$2 ORDER BY started_at,effect_id`, tenant, id)
	if err != nil {
		return fmt.Errorf("agentrunstate: list effects: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item runstate.Effect
		var status string
		var ref, dig *string
		var resolved *time.Time
		if err := rows.Scan(&item.ID, &item.IdempotencyKey, &item.ArgumentsDigest, &status, &ref, &dig, &item.StartedAt, &resolved); err != nil {
			return err
		}
		item.Status = runstate.EffectStatus(status)
		if ref != nil {
			item.ResultRef = *ref
		}
		if dig != nil {
			item.ResultDigest = *dig
		}
		if resolved != nil {
			item.ResolvedAt = resolved.UTC()
		}
		item.StartedAt = item.StartedAt.UTC()
		run.Effects = append(run.Effects, item)
	}
	return rows.Err()
}

func syncEffects(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, id string, effects []runstate.Effect) error {
	for _, effect := range effects {
		_, err := tx.Exec(ctx, `INSERT INTO agent_run_effect (tenant_id,run_id,effect_id,idempotency_key,arguments_digest,status,result_ref,result_digest,started_at,resolved_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (tenant_id,run_id,effect_id) DO UPDATE SET status=EXCLUDED.status,result_ref=EXCLUDED.result_ref,result_digest=EXCLUDED.result_digest,resolved_at=EXCLUDED.resolved_at
			WHERE agent_run_effect.status='UNKNOWN' AND EXCLUDED.status IN ('APPLIED','NOT_APPLIED')`,
			tenant, id, effect.ID, effect.IdempotencyKey, effect.ArgumentsDigest, string(effect.Status), nullable(effect.ResultRef), nullable(effect.ResultDigest), effect.StartedAt.UTC(), nullableTime(effect.ResolvedAt))
		if err != nil {
			return fmt.Errorf("agentrunstate: persist effect: %w", err)
		}
	}
	return nil
}

func leaseValues(lease *runstate.Lease) (any, any) {
	if lease == nil {
		return nil, nil
	}
	return lease.Owner, lease.Until.UTC()
}
func nullable(text string) any {
	if text == "" {
		return nil
	}
	return text
}
func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
