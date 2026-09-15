package reconcile

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// PostgresStore is the [Store] backed by migration 00040's effect_reconciliation_job
// table.
//
// It holds no state of its own: every method takes the caller's transaction,
// which must already be scoped by internal/data/tenancy.WithTenant so the
// table's tenant_isolation row-level-security policy applies. That is what
// makes a restart lose nothing -- the row IS the state, and a freshly
// constructed PostgresStore reading it back sees exactly what the last
// advance left.
type PostgresStore struct{}

var _ Store = PostgresStore{}

const jobColumns = `
	tenant_id, job_id, effect_ref, effect_id, policy_ref,
	intended_ref, canonical_ref, required_freshness, observation_attempts,
	next_check_at, deadline_at, owner_ref, sla_ref, repair_policy,
	job_state, job_version, created_at, updated_at`

// Create implements [Store].
func (PostgresStore) Create(ctx context.Context, ex Executor, job Job) (Job, bool, error) {
	affected, err := ex.Exec(ctx, `
		INSERT INTO effect_reconciliation_job (`+jobColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		ON CONFLICT (tenant_id, effect_ref, policy_ref) DO NOTHING`,
		job.TenantID, job.JobID, job.EffectRef, job.EffectID, job.PolicyRef,
		job.IntendedRef, job.CanonicalRef, string(job.RequiredFreshness), job.ObservationAttempts,
		job.NextCheckAt.UTC(), job.Deadline.UTC(), job.Owner, job.SLARef, job.RepairPolicy,
		string(job.Status), int64(job.Version), job.CreatedAt.UTC(), job.UpdatedAt.UTC())
	if err != nil {
		return Job{}, false, wrapErr(CodeStorageFailed, ErrStorage, job.TenantID, job.JobID, err,
			"insert reconciliation job for effect %s", job.EffectRef)
	}
	if affected == 1 {
		return job, false, nil
	}

	existing, loadErr := PostgresStore{}.loadByNaturalKey(ctx, ex, job.TenantID, job.EffectRef, job.PolicyRef)
	if loadErr != nil {
		return Job{}, false, wrapErr(CodeStorageFailed, ErrStorage, job.TenantID, job.JobID, loadErr,
			"read the reconciliation job that already exists for effect %s", job.EffectRef)
	}
	return existing, true, nil
}

// Load implements [Store].
func (PostgresStore) Load(ctx context.Context, ex Executor, tenantID, jobID uuid.UUID) (Job, error) {
	return scanJobRow(ex.QueryRow(ctx, `
		SELECT `+jobColumns+`
		FROM effect_reconciliation_job
		WHERE tenant_id = $1 AND job_id = $2`, tenantID, jobID), tenantID, jobID)
}

// LoadForUpdate implements [Store].
func (PostgresStore) LoadForUpdate(ctx context.Context, ex Executor, tenantID, jobID uuid.UUID) (Job, error) {
	return scanJobRow(ex.QueryRow(ctx, `
		SELECT `+jobColumns+`
		FROM effect_reconciliation_job
		WHERE tenant_id = $1 AND job_id = $2
		FOR UPDATE`, tenantID, jobID), tenantID, jobID)
}

func (PostgresStore) loadByNaturalKey(ctx context.Context, ex Executor, tenantID uuid.UUID, effectRef, policyRef string) (Job, error) {
	return scanJobRow(ex.QueryRow(ctx, `
		SELECT `+jobColumns+`
		FROM effect_reconciliation_job
		WHERE tenant_id = $1 AND effect_ref = $2 AND policy_ref = $3`, tenantID, effectRef, policyRef),
		tenantID, uuid.Nil)
}

// ListForEffect returns every reconciliation job watching one committed
// effect -- one per comparison policy -- ordered by policy. It is the workflow
// execution inspector's read (WF-RUN-019): a node's recorded effect reference
// is resolved to the observation and reconciliation state behind it. An
// effect nothing watches is an empty list, not an error.
func (PostgresStore) ListForEffect(ctx context.Context, ex Executor, tenantID uuid.UUID, effectRef string) ([]Job, error) {
	rows, err := ex.Query(ctx, `
		SELECT `+jobColumns+`
		FROM effect_reconciliation_job
		WHERE tenant_id = $1 AND effect_ref = $2
		ORDER BY policy_ref, job_id`, tenantID, effectRef)
	if err != nil {
		return nil, wrapErr(CodeStorageFailed, ErrStorage, tenantID, uuid.Nil, err,
			"list reconciliation jobs for effect %s", effectRef)
	}
	defer rows.Close()

	out := []Job{}
	for rows.Next() {
		job, scanErr := scanJob(rows)
		if scanErr != nil {
			return nil, wrapErr(CodeStorageFailed, ErrStorage, tenantID, uuid.Nil, scanErr, "scan reconciliation job row")
		}
		out = append(out, job)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapErr(CodeStorageFailed, ErrStorage, tenantID, uuid.Nil, err,
			"list reconciliation jobs for effect %s", effectRef)
	}
	return out, nil
}

// Due implements [Store].
func (PostgresStore) Due(ctx context.Context, ex Executor, tenantID uuid.UUID, asOf time.Time, limit int) ([]Job, error) {
	if limit < 1 {
		limit = 100
	}
	rows, err := ex.Query(ctx, `
		SELECT `+jobColumns+`
		FROM effect_reconciliation_job
		WHERE tenant_id = $1
		  AND job_state IN ('PENDING', 'OBSERVING', 'UNKNOWN')
		  AND next_check_at <= $2
		ORDER BY next_check_at, job_id
		LIMIT $3`, tenantID, asOf.UTC(), limit)
	if err != nil {
		return nil, wrapErr(CodeStorageFailed, ErrStorage, tenantID, uuid.Nil, err, "list due reconciliation jobs")
	}
	defer rows.Close()

	var out []Job
	for rows.Next() {
		job, scanErr := scanJob(rows)
		if scanErr != nil {
			return nil, wrapErr(CodeStorageFailed, ErrStorage, tenantID, uuid.Nil, scanErr, "scan reconciliation job row")
		}
		out = append(out, job)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapErr(CodeStorageFailed, ErrStorage, tenantID, uuid.Nil, err, "list due reconciliation jobs")
	}
	return out, nil
}

// Advance implements [Store].
func (PostgresStore) Advance(ctx context.Context, ex Executor, next Job, expectedVersion uint64) error {
	affected, err := ex.Exec(ctx, `
		UPDATE effect_reconciliation_job SET
			canonical_ref = $4,
			observation_attempts = $5,
			next_check_at = $6,
			deadline_at = $7,
			owner_ref = $8,
			sla_ref = $9,
			job_state = $10,
			job_version = job_version + 1,
			updated_at = $11
		WHERE tenant_id = $1 AND job_id = $2 AND job_version = $3`,
		next.TenantID, next.JobID, int64(expectedVersion),
		next.CanonicalRef, next.ObservationAttempts, next.NextCheckAt.UTC(), next.Deadline.UTC(),
		next.Owner, next.SLARef, string(next.Status), next.UpdatedAt.UTC())
	if err != nil {
		return wrapErr(CodeStorageFailed, ErrStorage, next.TenantID, next.JobID, err, "advance reconciliation job")
	}
	if affected == 0 {
		return refuse(CodeVersionConflict, ErrVersionConflict, next.TenantID, next.JobID,
			"job is no longer at version %d", expectedVersion)
	}
	return nil
}

func scanJobRow(row dbport.Row, tenantID, jobID uuid.UUID) (Job, error) {
	job, err := scanJob(row)
	if err != nil {
		if isNoRows(err) {
			return Job{}, refuse(CodeNotFound, ErrNotFound, tenantID, jobID, "no such job")
		}
		return Job{}, wrapErr(CodeStorageFailed, ErrStorage, tenantID, jobID, err, "read reconciliation job")
	}
	return job, nil
}

// rowScanner is the common surface of [dbport.Row] and [dbport.Rows] that
// scanJob needs, so one scan function serves a single-row query and a
// multi-row cursor alike.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(row rowScanner) (Job, error) {
	var (
		job       Job
		freshness string
		attempts  int64
		owner     string
		sla       string
		repair    string
		state     string
		version   int64
		nextCheck time.Time
		deadline  time.Time
		createdAt time.Time
		updatedAt time.Time
	)
	if err := row.Scan(
		&job.TenantID, &job.JobID, &job.EffectRef, &job.EffectID, &job.PolicyRef,
		&job.IntendedRef, &job.CanonicalRef, &freshness, &attempts,
		&nextCheck, &deadline, &owner, &sla, &repair,
		&state, &version, &createdAt, &updatedAt,
	); err != nil {
		return Job{}, err
	}
	job.RequiredFreshness = observe.Freshness(freshness)
	job.ObservationAttempts = int(attempts)
	job.NextCheckAt = nextCheck.UTC()
	job.Deadline = deadline.UTC()
	job.Owner = owner
	job.SLARef = sla
	job.RepairPolicy = repair
	job.Status = Status(state)
	job.Version = uint64(version)
	job.CreatedAt = createdAt.UTC()
	job.UpdatedAt = updatedAt.UTC()
	return job, nil
}

func isNoRows(err error) bool { return errors.Is(err, dbport.ErrNoRows) }
