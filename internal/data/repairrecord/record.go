package repairrecord

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Executor is the minimal database capability this package needs. A
// [dbport.Tx] and a [dbport.Conn] both satisfy it. It is taken per call rather
// than held, because the caller owns the tenant-scoped transaction the
// row-level-security policy is evaluated against.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

// Stage is the closed vocabulary of repair-record stages. See the package doc
// for why the claim is written before the effect rather than after it.
type Stage string

// Stages.
const (
	StageClaimed  Stage = "CLAIMED"
	StageExecuted Stage = "EXECUTED"
	StageSettled  Stage = "SETTLED"
)

// Valid reports whether s is a declared stage.
func (s Stage) Valid() bool {
	switch s {
	case StageClaimed, StageExecuted, StageSettled:
		return true
	default:
		return false
	}
}

// ErrInvalid means a required input was empty, zero, or outside a closed
// vocabulary. It is returned before any statement runs.
var ErrInvalid = errors.New("repairrecord: invalid input")

// Record is one append-only row: everything the executor needs to decide, on a
// cell that has just started, whether a repair fence already reached the
// effect boundary and what it concluded.
type Record struct {
	TenantID uuid.UUID
	// FenceKey is the repair's own separately fenced identity, stable across
	// restarts because it is derived from the immutable plan.
	FenceKey string
	Stage    Stage
	FenceID  string
	// PlanDigest pins the exact plan this attempt revalidated.
	PlanDigest string
	// OriginalSemanticKey is the parent transaction's provider idempotency
	// identity, reused by the redrive; it is evidence, never the fence.
	OriginalSemanticKey string
	FailedEffectKey     string
	// Status is the typed revalidation answer this stage recorded.
	Status string
	// Executed reports whether the corrective effect was accepted.
	Executed bool
	// ConsistencyState is CONSISTENT, DEGRADED or UNKNOWN.
	ConsistencyState string

	EffectRef            string
	EffectResultRef      string
	ObservationState     string
	ObservationDigest    string
	ObservationComplete  bool
	ReconciliationStatus string
	ReconciliationRoute  string

	RecordedAt time.Time
}

func (r Record) validate() error {
	if r.TenantID == uuid.Nil {
		return fmt.Errorf("%w: tenant id is nil", ErrInvalid)
	}
	if !r.Stage.Valid() {
		return fmt.Errorf("%w: stage %q is not CLAIMED, EXECUTED or SETTLED", ErrInvalid, r.Stage)
	}
	for _, field := range []struct{ name, value string }{
		{"fence_key", r.FenceKey}, {"fence_id", r.FenceID}, {"plan_digest", r.PlanDigest},
		{"original_semantic_key", r.OriginalSemanticKey}, {"failed_effect_key", r.FailedEffectKey},
		{"status", r.Status},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%w: %s is empty", ErrInvalid, field.name)
		}
	}
	switch r.ConsistencyState {
	case "CONSISTENT", "DEGRADED", "UNKNOWN":
	default:
		return fmt.Errorf("%w: consistency state %q is not declared", ErrInvalid, r.ConsistencyState)
	}
	if r.RecordedAt.IsZero() {
		return fmt.Errorf("%w: recorded_at is unset", ErrInvalid)
	}
	return nil
}

// Append writes one stage row for one tenant-scoped repair fence and reports
// whether this call is the one that wrote it.
//
// It is the claim decision itself, not a check before one: the single INSERT
// below conflicts on the table's own primary key, so of two concurrent callers
// racing to claim the same fence, PostgreSQL decides which one lands and the
// loser is told claimed=false without ever having observed a state it then
// acted on. A caller told false must read [Load] and follow the history it
// finds; it must never treat false as permission to proceed.
func Append(ctx context.Context, ex Executor, record Record) (claimed bool, err error) {
	if ex == nil {
		return false, fmt.Errorf("%w: executor is nil", ErrInvalid)
	}
	if err := record.validate(); err != nil {
		return false, err
	}
	var stage string
	scanErr := ex.QueryRow(ctx, `
		INSERT INTO workflow_repair_execution_record
			(tenant_id, fence_key, stage, fence_id, plan_digest, original_semantic_key,
			 failed_effect_key, status, executed, consistency_state, effect_ref, effect_result_ref,
			 observation_state, observation_digest, observation_complete,
			 reconciliation_status, reconciliation_route, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		ON CONFLICT (tenant_id, fence_key, stage) DO NOTHING
		RETURNING stage`,
		record.TenantID, strings.TrimSpace(record.FenceKey), string(record.Stage),
		strings.TrimSpace(record.FenceID), strings.TrimSpace(record.PlanDigest),
		strings.TrimSpace(record.OriginalSemanticKey), strings.TrimSpace(record.FailedEffectKey),
		record.Status, record.Executed, record.ConsistencyState,
		record.EffectRef, record.EffectResultRef,
		record.ObservationState, record.ObservationDigest, record.ObservationComplete,
		record.ReconciliationStatus, record.ReconciliationRoute, record.RecordedAt.UTC(),
	).Scan(&stage)
	if scanErr != nil {
		if errors.Is(scanErr, dbport.ErrNoRows) {
			// The conflict path ran: a row for this (tenant, fence, stage)
			// already exists and nothing was written. This is the only mutating
			// statement the function issues, so there is no partial effect to
			// unwind.
			return false, nil
		}
		return false, fmt.Errorf("repairrecord: append %s: %w", record.Stage, scanErr)
	}
	return true, nil
}

// Load returns every stage row recorded for one tenant-scoped repair fence,
// ordered CLAIMED, EXECUTED, SETTLED. An empty result means no attempt has
// ever reached the effect boundary under this fence.
func Load(ctx context.Context, ex Executor, tenantID uuid.UUID, fenceKey string) ([]Record, error) {
	if ex == nil {
		return nil, fmt.Errorf("%w: executor is nil", ErrInvalid)
	}
	if tenantID == uuid.Nil {
		return nil, fmt.Errorf("%w: tenant id is nil", ErrInvalid)
	}
	if strings.TrimSpace(fenceKey) == "" {
		return nil, fmt.Errorf("%w: fence_key is empty", ErrInvalid)
	}
	rows, err := ex.Query(ctx, `
		SELECT fence_key, stage, fence_id, plan_digest, original_semantic_key, failed_effect_key,
		       status, executed, consistency_state, effect_ref, effect_result_ref,
		       observation_state, observation_digest, observation_complete,
		       reconciliation_status, reconciliation_route, recorded_at
		FROM workflow_repair_execution_record
		WHERE tenant_id = $1 AND fence_key = $2
		ORDER BY CASE stage WHEN 'CLAIMED' THEN 0 WHEN 'EXECUTED' THEN 1 ELSE 2 END`,
		tenantID, strings.TrimSpace(fenceKey))
	if err != nil {
		return nil, fmt.Errorf("repairrecord: load: %w", err)
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		record := Record{TenantID: tenantID}
		var stage string
		if err := rows.Scan(&record.FenceKey, &stage, &record.FenceID, &record.PlanDigest,
			&record.OriginalSemanticKey, &record.FailedEffectKey, &record.Status, &record.Executed,
			&record.ConsistencyState, &record.EffectRef, &record.EffectResultRef,
			&record.ObservationState, &record.ObservationDigest, &record.ObservationComplete,
			&record.ReconciliationStatus, &record.ReconciliationRoute, &record.RecordedAt); err != nil {
			return nil, fmt.Errorf("repairrecord: scan: %w", err)
		}
		record.Stage = Stage(stage)
		record.RecordedAt = record.RecordedAt.UTC()
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repairrecord: load: %w", err)
	}
	return out, nil
}
