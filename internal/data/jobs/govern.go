package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ErrPaused reports a dispatch gated by a governed pause. The run row is
// untouched: pausing gates future Begin/Claim decisions, it never rewrites
// state behind the compare-and-swap stores.
var ErrPaused = errors.New("jobs: run is paused")

type govKey struct {
	tenant uuid.UUID
	run    uuid.UUID
}

// cancelCustodian is the holder identity CancelRun claims PENDING
// partitions under before cancelling them, satisfying the schema's rule
// that every non-PENDING partition names its holder.
const cancelCustodian = "governor:cancel"

// Governor performs the governed safe-point operations of JOB-004 over
// the JOB-001 stores: pause gates dispatch, cancel settles a run plus
// every open partition, redrive opens a new attempt on the same run
// identity, and housekeeping reports lineage and holds without deleting
// anything. Every operation is tenant scoped through the caller's
// transaction: a run invisible to the tenant reports [ErrNotFound] and
// changes nothing. The pause registry is executor-local and safe for
// concurrent use; all durable transitions go through the CAS stores.
type Governor struct {
	mu     sync.Mutex
	paused map[govKey]struct{}
}

// NewGovernor returns a Governor with an empty pause registry.
func NewGovernor() *Governor {
	return &Governor{paused: map[govKey]struct{}{}}
}

func terminalRunState(state string) bool {
	return state == RunCompleted || state == RunFailed || state == RunCancelled
}

func checkGovScope(tenant, runID uuid.UUID) error {
	if tenant == uuid.Nil {
		return invalid("tenant_id", "governed operations are tenant scoped")
	}
	if runID == uuid.Nil {
		return invalid("run_id", "a governed operation names its run")
	}
	return nil
}

// PauseRun gates dispatch for a non-terminal run. Pausing is idempotent.
// A terminal run has no safe point left to hold, so it reports
// [ErrIllegalTransition]; an invisible run reports [ErrNotFound].
func (g *Governor) PauseRun(ctx context.Context, ex Executor, tenant, runID uuid.UUID) error {
	if err := checkGovScope(tenant, runID); err != nil {
		return err
	}
	run, err := (RunStore{}).Load(ctx, ex, tenant, runID)
	if err != nil {
		return err
	}
	if terminalRunState(run.State) {
		return fmt.Errorf("%w: cannot pause %s run %s", ErrIllegalTransition, run.State, runID)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.paused[govKey{tenant: tenant, run: runID}] = struct{}{}
	return nil
}

// ResumeRun lifts a pause. It is idempotent: resuming a run that is not
// paused still verifies the run is visible to the tenant and reports
// [ErrNotFound] otherwise.
func (g *Governor) ResumeRun(ctx context.Context, ex Executor, tenant, runID uuid.UUID) error {
	if err := checkGovScope(tenant, runID); err != nil {
		return err
	}
	if _, err := (RunStore{}).Load(ctx, ex, tenant, runID); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.paused, govKey{tenant: tenant, run: runID})
	return nil
}

// IsPaused reports the executor-local pause state. It never touches the
// store.
func (g *Governor) IsPaused(tenant, runID uuid.UUID) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.paused[govKey{tenant: tenant, run: runID}]
	return ok
}

// CheckDispatchable is the gate dispatchers consult before Begin or
// Claim: a paused run reports [ErrPaused], a terminal run reports
// [ErrIllegalTransition], an invisible run reports [ErrNotFound].
func (g *Governor) CheckDispatchable(ctx context.Context, ex Executor, tenant, runID uuid.UUID) error {
	if err := checkGovScope(tenant, runID); err != nil {
		return err
	}
	g.mu.Lock()
	_, paused := g.paused[govKey{tenant: tenant, run: runID}]
	g.mu.Unlock()
	if paused {
		return fmt.Errorf("%w: run %s", ErrPaused, runID)
	}
	run, err := (RunStore{}).Load(ctx, ex, tenant, runID)
	if err != nil {
		return err
	}
	if terminalRunState(run.State) {
		return fmt.Errorf("%w: cannot dispatch %s run %s", ErrIllegalTransition, run.State, runID)
	}
	return nil
}

// CancelRun settles a run and every open partition through the
// compare-and-swap stores, returning the cancelled run and the number of
// partitions it settled. Checkpoints, attempts and identities are
// retained: cancel ends work, it never erases its lineage. A terminal run
// reports [ErrIllegalTransition] with zero effect.
func (g *Governor) CancelRun(ctx context.Context, ex Executor, tenant, runID uuid.UUID, expectedVersion uint64, at time.Time) (JobRun, int, error) {
	if err := checkGovScope(tenant, runID); err != nil {
		return JobRun{}, 0, err
	}
	if at.IsZero() {
		return JobRun{}, 0, invalid("completed_at", "timestamp is unset")
	}
	run, err := (RunStore{}).Cancel(ctx, ex, tenant, runID, expectedVersion, at)
	if err != nil {
		return JobRun{}, 0, err
	}
	parts, err := (PartitionStore{}).ListByRun(ctx, ex, tenant, runID)
	if err != nil {
		return run, 0, fmt.Errorf("jobs: list partitions of run %s: %w", runID, err)
	}
	settled := 0
	for _, part := range parts {
		version := part.Version
		switch part.State {
		case PartitionClaimed:
		case PartitionPending:
			// A PENDING partition names no holder, and the schema's
			// claimed_consistent CHECK requires every non-PENDING row
			// to name one, so PartitionStore.Cancel cannot settle it
			// directly. The governor first takes custody under its own
			// identity, then cancels the CLAIMED row: the claim and
			// the cancel are one governed custody step, visible in
			// the partition's attempt and version.
			custodied, err := (PartitionStore{}).ClaimPartition(ctx, ex, tenant, part.PartitionID, part.Version, cancelCustodian, at)
			if err != nil {
				return run, settled, fmt.Errorf("jobs: take custody of partition %s of run %s: %w", part.PartitionID, runID, err)
			}
			version = custodied.Version
		default:
			continue
		}
		if _, err := (PartitionStore{}).Cancel(ctx, ex, tenant, part.PartitionID, version, at); err != nil {
			return run, settled, fmt.Errorf("jobs: cancel partition %s of run %s: %w", part.PartitionID, runID, err)
		}
		settled++
	}
	g.mu.Lock()
	delete(g.paused, govKey{tenant: tenant, run: runID})
	g.mu.Unlock()
	return run, settled, nil
}

// RedriveRun opens a new attempt on a FAILED run's identity via
// [RunStore.Retry]. Any other state reports [ErrIllegalTransition]; the
// prior attempt's checkpoints and partition rows are untouched.
func (g *Governor) RedriveRun(ctx context.Context, ex Executor, tenant, runID uuid.UUID, expectedVersion uint64, at time.Time) (JobRun, error) {
	if err := checkGovScope(tenant, runID); err != nil {
		return JobRun{}, err
	}
	return (RunStore{}).Retry(ctx, ex, tenant, runID, expectedVersion, at)
}

// HousekeepingReport is the read-only outcome of a tenant-scoped reap
// scan: run lineage, partition and checkpoint totals, the open partition
// keys that remain holds or repair obligations, and the deleted row
// count, which reaping always reports as zero because it never deletes.
type HousekeepingReport struct {
	TenantID         uuid.UUID
	RunID            uuid.UUID
	RunState         string
	Attempt          int
	PartitionsTotal  int
	PartitionsOpen   int
	CheckpointsTotal int
	Holds            []string
	DeletedRows      int64
}

// HousekeepingScan reports what a reap would cover without covering
// anything: no statement it issues writes. Open means neither COMPLETED
// nor CANCELLED: those partitions keep lineage, holds and repair
// obligations alive.
func (g *Governor) HousekeepingScan(ctx context.Context, ex Executor, tenant, runID uuid.UUID) (HousekeepingReport, error) {
	if err := checkGovScope(tenant, runID); err != nil {
		return HousekeepingReport{}, err
	}
	run, err := (RunStore{}).Load(ctx, ex, tenant, runID)
	if err != nil {
		return HousekeepingReport{}, err
	}
	parts, err := (PartitionStore{}).ListByRun(ctx, ex, tenant, runID)
	if err != nil {
		return HousekeepingReport{}, fmt.Errorf("jobs: list partitions of run %s: %w", runID, err)
	}
	report := HousekeepingReport{
		TenantID: tenant,
		RunID:    runID,
		RunState: run.State,
		Attempt:  run.Attempt,
	}
	for _, part := range parts {
		report.PartitionsTotal++
		if part.State != PartitionCompleted && part.State != PartitionCancelled {
			report.PartitionsOpen++
			report.Holds = append(report.Holds, part.PartitionKey)
		}
		checkpoints, err := (CheckpointStore{}).List(ctx, ex, tenant, part.PartitionID)
		if err != nil {
			return HousekeepingReport{}, fmt.Errorf("jobs: list checkpoints of partition %s: %w", part.PartitionID, err)
		}
		report.CheckpointsTotal += len(checkpoints)
	}
	return report, nil
}
