package runtime

// Durable QuarantinedWork (WF-RUN-007) over
// migrations/00297_workflow_quarantined_work.sql. [QuarantineStore] carries
// the same admit/lookup contract as the in-memory [QuarantineLedger] --
// idempotent by key, a broken seal refused, a missing key refused rather than
// reported as success -- but the record is tenant scoped, survives a restart
// and is written through the caller's own transaction, so a driver commits
// the record together with the instance route it justifies.

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// Durable quarantine refusal codes.
const (
	// CodeQuarantineNotFound reports a lookup of an idempotency key that holds
	// no quarantined work for the tenant, which is also what a cross-tenant
	// read looks like.
	CodeQuarantineNotFound = "QUARANTINE_NOT_FOUND"
	// CodeQuarantineSealBroken reports a record whose content no longer
	// digests to its seal, whether submitted or read back.
	CodeQuarantineSealBroken = "QUARANTINE_SEAL_BROKEN"
)

// QuarantineStore persists [QuarantinedWork]. It holds no state; every method
// takes its [Executor] explicitly, and the caller's transaction must already
// carry the tenant scope (internal/data/tenancy.WithTenant).
type QuarantineStore struct{}

const quarantineColumns = `node_id, workflow_id, attempts, last_error, ambiguous, idempotency_key,
	owner, sla_nanos, next_action, repair_route, route, record_digest`

// File admits one sealed record for instanceID idempotently: the first record
// filed under an idempotency key is kept, and every later filing of that key
// -- a retry, a concurrent driver -- returns the stored record unchanged
// instead of duplicating or overwriting it.
func (QuarantineStore) File(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID, work QuarantinedWork, recordedAt time.Time) (ret0 QuarantinedWork, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.file_quarantined_work", tenantID, instanceID, work)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	id := instanceID.String()
	switch {
	case tenantID == uuid.Nil || instanceID == uuid.Nil:
		return QuarantinedWork{}, refuse(CodeInvalidRecord, id, work.NodeID, "tenant and instance ids must not be the nil UUID")
	case recordedAt.IsZero():
		return QuarantinedWork{}, refuse(CodeInvalidRecord, id, work.NodeID, "recorded_at must be supplied; this package never reads a wall clock")
	case work.Route != WorkflowBlocked && work.Route != WorkflowRepairRequired && work.Route != WorkflowQuarantined:
		return QuarantinedWork{}, refuse(CodeInvalidRecord, id, work.NodeID, "route %q is not a quarantine route", work.Route)
	case work.IdempotencyKey == "":
		return QuarantinedWork{}, refuse(CodeInvalidRecord, id, work.NodeID, "quarantined work names no idempotency key")
	}
	if err := work.Verify(); err != nil {
		return QuarantinedWork{}, wrap(CodeQuarantineSealBroken, id, work.NodeID, err, "refuse unsealed quarantined work")
	}
	if _, err := ex.Exec(ctx, `INSERT INTO workflow_quarantined_work (tenant_id, instance_id, recorded_at, `+quarantineColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`,
		tenantID, instanceID, recordedAt.UTC(), work.NodeID, work.WorkflowID, work.Attempts, work.LastError, work.Ambiguous,
		work.IdempotencyKey, work.Owner, int64(work.SLA), work.NextAction, work.RepairRoute, work.Route, work.Digest); err != nil {
		return QuarantinedWork{}, wrap(CodeStorageFailed, id, work.NodeID, err, "file quarantined work")
	}
	return loadQuarantinedWork(ctx, ex, tenantID, work.IdempotencyKey)
}

// Get returns the record retained under idempotencyKey. A missing key is
// refused with [CodeQuarantineNotFound], never reported as success, and a
// stored record whose seal no longer verifies is refused with
// [CodeQuarantineSealBroken].
func (QuarantineStore) Get(ctx context.Context, ex Executor, tenantID uuid.UUID, idempotencyKey string) (ret0 QuarantinedWork, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.get_quarantined_work", tenantID)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	return loadQuarantinedWork(ctx, ex, tenantID, idempotencyKey)
}

// ListForInstance returns every record retained for one instance in filing
// order, each seal verified. An instance with none returns an empty slice.
func (QuarantineStore) ListForInstance(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) (ret0 []QuarantinedWork, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.list_quarantined_work", tenantID, instanceID)
	defer func() { observe.DoneWith(obsOp, retErr) }()
	rows, err := ex.Query(ctx, `SELECT `+quarantineColumns+` FROM workflow_quarantined_work
		WHERE tenant_id = $1 AND instance_id = $2 ORDER BY recorded_at, idempotency_key`, tenantID, instanceID)
	if err != nil {
		return nil, wrap(CodeStorageFailed, instanceID.String(), "", err, "list quarantined work")
	}
	defer rows.Close()
	out := []QuarantinedWork{}
	for rows.Next() {
		work, err := scanQuarantinedWork(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, work)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap(CodeStorageFailed, instanceID.String(), "", err, "list quarantined work")
	}
	return out, nil
}

func loadQuarantinedWork(ctx context.Context, ex Executor, tenantID uuid.UUID, idempotencyKey string) (QuarantinedWork, error) {
	work, err := scanQuarantinedWork(ex.QueryRow(ctx, `SELECT `+quarantineColumns+` FROM workflow_quarantined_work
		WHERE tenant_id = $1 AND idempotency_key = $2`, tenantID, idempotencyKey))
	if errors.Is(err, dbport.ErrNoRows) {
		return QuarantinedWork{}, refuse(CodeQuarantineNotFound, "", "", "no quarantined work under idempotency key %q", idempotencyKey)
	}
	return work, err
}

func scanQuarantinedWork(row dbport.Row) (QuarantinedWork, error) {
	var work QuarantinedWork
	var slaNanos int64
	if err := row.Scan(&work.NodeID, &work.WorkflowID, &work.Attempts, &work.LastError, &work.Ambiguous,
		&work.IdempotencyKey, &work.Owner, &slaNanos, &work.NextAction, &work.RepairRoute, &work.Route, &work.Digest); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return QuarantinedWork{}, err
		}
		return QuarantinedWork{}, wrap(CodeStorageFailed, "", "", err, "read quarantined work")
	}
	work.SLA = time.Duration(slaNanos)
	if err := work.Verify(); err != nil {
		return QuarantinedWork{}, wrap(CodeQuarantineSealBroken, "", work.NodeID, err,
			"stored quarantined work %q", work.IdempotencyKey)
	}
	return work, nil
}
