package recover

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// The sweep is WF-RUN-003's production redelivery path for a driver that died
// in the middle of a synchronous drain.
//
// Every served run holds a WORKFLOW_INSTANCE lease around Execute, Resume,
// ResumeTimer and CompleteApproval (WF-RUN-036). A driver that dies mid-drain
// releases nothing, so it leaves exactly one durable signature behind: the
// instance's lease is still HELD but has lapsed, at least one node's latest
// attempt is READY or RUNNING, and no workflow_ready_work row still claims an
// unfinished node attempt of the instance (the scheduler's own claim recovery
// owns those).
// [FindOrphans] reads that signature and nothing else -- no heartbeat table,
// no process registry.
//
// [Sweeper.Sweep] claims each orphan by taking the lapsed lease over through
// [lease.Manager.Acquire], which serializes concurrent sweepers on the
// resource's advisory lock and mints a fence token one past the dead
// driver's. A claim is kept only when the acquire really was a takeover of a
// lapsed holder and the orphan signature still holds inside the claiming
// transaction; a second sweeper therefore gets lease.ErrHeld while the first
// is live, and a fresh ACQUIRED (the first one already finished and released)
// is rolled back rather than redelivered twice. The claimed instance is handed
// to a [Redeliverer] with the new fence on its context ([execute.WithFence]),
// so every advancement the redelivery performs is fenced against the dead
// driver coming back, and the lease is released only after the redelivery
// returns. A redelivery that fails leaves the sweeper's own lease to lapse,
// which is the retry: the next sweep after the TTL finds the same signature.

// Sweep persistence boundaries. They are not part of [Phases] -- those are
// [Recoverer.Recover]'s own four -- but a [Failpoint] crashes at them the same
// way, so a sweeper's own death is as deterministic to test as a driver's.
const (
	// SweepPhaseAfterClaimCommit is after the takeover committed and before
	// the redelivery ran: the sweeper holds a live lease and nothing moved.
	SweepPhaseAfterClaimCommit Phase = "SWEEP_AFTER_CLAIM_COMMIT"
	// SweepPhaseAfterRedelivery is after the redelivery returned and before
	// the sweeper released the lease.
	SweepPhaseAfterRedelivery Phase = "SWEEP_AFTER_REDELIVERY"
)

// SweepPhases returns the sweep's two boundaries in the order a sweep crosses
// them.
func SweepPhases() []Phase {
	return []Phase{SweepPhaseAfterClaimCommit, SweepPhaseAfterRedelivery}
}

// OpenNode is one node whose latest attempt a dead driver left READY or
// RUNNING.
type OpenNode struct {
	NodeID  string
	Attempt int
	Status  runtime.NodeStatus
}

// Orphan is one instance whose driver died mid-drain, as [FindOrphans] read
// it.
type Orphan struct {
	TenantID        uuid.UUID
	InstanceID      uuid.UUID
	InstanceVersion int64
	Status          runtime.InstanceStatus
	// HolderID, Token and ExpiresAt describe the lapsed lease the dead driver
	// left HELD.
	HolderID  string
	Token     uint64
	ExpiresAt time.Time
	// Nodes are the open nodes, sorted by node id.
	Nodes []OpenNode
}

// Redelivery is one claimed orphan handed to a [Redeliverer].
type Redelivery struct {
	Orphan Orphan
	// Fence is the WORKFLOW_INSTANCE fence the sweep took over. The context
	// the redeliverer receives already carries it ([execute.WithFence]).
	Fence runtime.Fence
	// Lease is the takeover evidence the claim produced.
	Lease lease.Evidence
}

// Redeliverer drains one claimed orphan's READY frontier again. A composition
// root implements it over the served driver ([execute.Driver.RedeliverReady]),
// reconstructing the instance's pinned start request from durable rows.
type Redeliverer interface {
	Redeliver(ctx context.Context, req Redelivery) error
}

// RedelivererFunc adapts a function to [Redeliverer].
type RedelivererFunc func(ctx context.Context, req Redelivery) error

// Redeliver implements [Redeliverer].
func (f RedelivererFunc) Redeliver(ctx context.Context, req Redelivery) (retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.recover.redeliver", orphanAttrs(req.Orphan))
	defer func() { observe.DoneWith(obsOp, retErr) }()
	return f(ctx, req)
}

// SweeperOptions are the ports one [Sweeper] is built from.
type SweeperOptions struct {
	DB Beginner
	// Holder is the sweeping workload's identity; it becomes the holder of
	// every lease the sweep takes over.
	Holder lease.Identity
	// LeaseTTL bounds one redelivery. It must outlast the drain it covers: a
	// redelivery still running when it lapses is fenced out by the next sweep.
	LeaseTTL time.Duration
	Leases   lease.Manager
	// Redeliver drains each claimed orphan.
	Redeliver Redeliverer
	// Limit bounds the orphans one sweep reads per tenant. Zero means
	// [DefaultSweepLimit].
	Limit int
	// Failpoints crashes the sweep at a declared boundary. Nil never crashes.
	Failpoints Failpoint
}

// DefaultSweepLimit is the per-tenant orphan bound when SweeperOptions.Limit
// is zero.
const DefaultSweepLimit = 16

// Sweeper finds orphaned instances and redelivers each exactly once. It holds
// no state and starts nothing; a caller runs [Sweeper.Sweep] on its own
// schedule with its own clock reading.
type Sweeper struct{ opts SweeperOptions }

// NewSweeper validates the wiring.
func NewSweeper(opts SweeperOptions) (Sweeper, error) {
	switch {
	case opts.DB == nil:
		return Sweeper{}, invalid("a database Beginner is required")
	case opts.Redeliver == nil:
		return Sweeper{}, invalid("a Redeliverer is required")
	case opts.LeaseTTL <= 0:
		return Sweeper{}, invalid("lease ttl must be positive")
	case opts.Limit < 0:
		return Sweeper{}, invalid("sweep limit must not be negative")
	}
	if _, err := lease.ParseHolder(opts.Holder.HolderID()); err != nil {
		e := invalid("sweeping workload identity is not a workload identity")
		e.err = err
		return Sweeper{}, e
	}
	if opts.Limit == 0 {
		opts.Limit = DefaultSweepLimit
	}
	if opts.Failpoints == nil {
		opts.Failpoints = NoFailpoint{}
	}
	return Sweeper{opts: opts}, nil
}

// SweepOutcome is what the sweep did with one orphan.
type SweepOutcome string

const (
	// SweepRedelivered: claimed, redelivered and released.
	SweepRedelivered SweepOutcome = "REDELIVERED"
	// SweepContended: another holder took the instance first.
	SweepContended SweepOutcome = "CONTENDED"
	// SweepSettled: the signature no longer held once the claim was taken
	// (the instance moved, or its previous holder released cleanly).
	SweepSettled SweepOutcome = "SETTLED"
	// SweepFailed: the redelivery (or the release after it) failed; the
	// sweeper's own lease is left to lapse so a later sweep retries.
	SweepFailed SweepOutcome = "FAILED"
)

// SweepItem is one orphan's result.
type SweepItem struct {
	Orphan  Orphan
	Outcome SweepOutcome
	Fence   runtime.Fence
	Err     error
}

// SweepReceipt is everything one [Sweeper.Sweep] did.
type SweepReceipt struct {
	At    time.Time
	Items []SweepItem
	// CrashedAt is the boundary an injected failpoint fired at, if any.
	CrashedAt Phase
}

// Count returns how many items settled with outcome.
func (r SweepReceipt) Count(outcome SweepOutcome) int {
	n := 0
	for _, item := range r.Items {
		if item.Outcome == outcome {
			n++
		}
	}
	return n
}

// Sweep finds one tenant's orphans as of now and redelivers each one it can
// claim. A failure on one orphan does not stop the others; the first such
// failure is returned after every orphan was attempted. A crash failpoint
// abandons the sweep where it fired.
func (s Sweeper) Sweep(ctx context.Context, tenantID uuid.UUID, now time.Time) (ret0 SweepReceipt, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.recover.sweep", observe.Attrs{observe.KeyTenant: tenantID.String()})
	defer func() { observe.DoneWith(obsOp, retErr, ret0.Count(SweepRedelivered)) }()
	if tenantID == uuid.Nil {
		return SweepReceipt{}, invalid("tenant id must not be the nil UUID")
	}
	if now.IsZero() {
		return SweepReceipt{}, invalid("a sweep needs the caller's own clock reading")
	}
	out := SweepReceipt{At: now}
	var orphans []Orphan
	if err := s.inTenant(ctx, tenantID, func(tx Executor) error {
		var ferr error
		orphans, ferr = FindOrphans(ctx, tx, tenantID, now, s.opts.Limit)
		return ferr
	}); err != nil {
		return out, err
	}
	var firstErr error
	for _, orphan := range orphans {
		item, err := s.recoverOne(ctx, orphan, now)
		out.Items = append(out.Items, item)
		if err == nil {
			continue
		}
		if phase := PhaseOf(err); phase != "" {
			out.CrashedAt = phase
			return out, err
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return out, firstErr
}

// RunRecoveryRole runs one sweep for a scheduler claim's tenant and reports
// how many orphans it redelivered. It is the scheduler host's recovery role
// seam: the claim names the tenant a replica serves.
func (s Sweeper) RunRecoveryRole(ctx context.Context, claim lease.AcquireRequest, now time.Time) (ret0 int, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.recover.recovery_role", observe.Attrs{observe.KeyTenant: claim.TenantID.String()})
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	receipt, err := s.Sweep(ctx, claim.TenantID, now)
	return receipt.Count(SweepRedelivered), err
}

// recoverOne claims, redelivers and releases one orphan.
func (s Sweeper) recoverOne(ctx context.Context, orphan Orphan, now time.Time) (SweepItem, error) {
	item := SweepItem{Orphan: orphan}
	resource := lease.Resource{Kind: lease.ResourceWorkflowInstance, ID: orphan.InstanceID.String()}
	instanceID := orphan.InstanceID.String()

	var (
		grant   lease.Grant
		claimed Orphan
		settled bool
	)
	err := s.inTenant(ctx, orphan.TenantID, func(tx Executor) error {
		var aerr error
		grant, aerr = s.opts.Leases.Acquire(ctx, tx, lease.AcquireRequest{
			TenantID: orphan.TenantID, Resource: resource, Holder: s.opts.Holder, Now: now, TTL: s.opts.LeaseTTL,
		})
		if aerr != nil {
			return aerr
		}
		if grant.Evidence.Kind != lease.TransitionTakenOver {
			settled = true
			return errSettled
		}
		current, still, lerr := loadOrphan(ctx, tx, orphan.TenantID, orphan.InstanceID)
		if lerr != nil {
			return lerr
		}
		if !still || current.InstanceVersion != orphan.InstanceVersion {
			settled = true
			return errSettled
		}
		claimed = current
		claimed.HolderID, claimed.Token, claimed.ExpiresAt = orphan.HolderID, orphan.Token, orphan.ExpiresAt
		return nil
	})
	switch {
	case settled:
		item.Outcome = SweepSettled
		return item, nil
	case errors.Is(err, lease.ErrHeld):
		item.Outcome = SweepContended
		return item, nil
	case err != nil:
		item.Outcome, item.Err = SweepFailed, err
		return item, wrap(CodeStorageFailed, ErrStorage, instanceID, "", err, "claim the orphaned instance")
	}
	item.Orphan = claimed
	item.Fence = grant.Fence.RuntimeFence(now)
	if err := s.crash(ctx, SweepPhaseAfterClaimCommit, instanceID); err != nil {
		item.Outcome, item.Err = SweepFailed, err
		return item, err
	}

	redeliverCtx := execute.WithFence(ctx, grant.Fence.RuntimeFence(time.Time{}))
	if err := s.opts.Redeliver.Redeliver(redeliverCtx, Redelivery{Orphan: claimed, Fence: item.Fence, Lease: grant.Evidence}); err != nil {
		item.Outcome, item.Err = SweepFailed, err
		return item, wrap(CodeEffectFailed, ErrEffect, instanceID, "", err, "redeliver the orphaned instance")
	}
	if err := s.crash(ctx, SweepPhaseAfterRedelivery, instanceID); err != nil {
		item.Outcome, item.Err = SweepFailed, err
		return item, err
	}
	if err := s.inTenant(ctx, orphan.TenantID, func(tx Executor) error {
		_, rerr := s.opts.Leases.Release(ctx, tx, grant.Fence, now)
		return rerr
	}); err != nil {
		item.Outcome, item.Err = SweepFailed, err
		return item, wrap(CodeStorageFailed, ErrStorage, instanceID, "", err, "release the redelivered instance")
	}
	item.Outcome = SweepRedelivered
	return item, nil
}

// errSettled rolls a claim back whose orphan signature no longer holds.
var errSettled = errors.New("recover: orphan settled before its claim")

func (s Sweeper) crash(ctx context.Context, phase Phase, instanceID string) error {
	if err := s.opts.Failpoints.Check(ctx, phase); err != nil {
		e := wrap(CodeCrashInjected, ErrCrashed, instanceID, "", err, "the sweeper died at this boundary")
		e.Phase = phase
		return e
	}
	return nil
}

func (s Sweeper) inTenant(ctx context.Context, tenantID uuid.UUID, fn func(tx Executor) error) error {
	r := Recoverer{opts: Options{DB: s.opts.DB}}
	tx, err := r.begin(ctx, tenantID)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// orphanStatuses are the instance statuses a lost drain leaves behind.
var orphanStatuses = []string{
	string(runtime.InstanceCreated), string(runtime.InstanceRunning),
	string(runtime.InstanceWaiting), string(runtime.InstancePauseRequested),
}

// liveClaim matches a workflow_ready_work row r that still owns its instance's
// progress: READY or DISPATCHED, for a node attempt that has not already
// finished. A row whose attempt already finished is a stale claim -- the
// dispatcher that took it advanced the node and then died mid-drain -- and it
// must not hide the READY successors that dispatcher never ran.
const liveClaim = `
	r.ready_state IN ('READY', 'DISPATCHED')
	AND NOT EXISTS (
	    SELECT 1 FROM workflow_node_execution f
	    WHERE f.tenant_id = r.tenant_id AND f.instance_id = r.instance_id
	      AND f.node_id = r.node_id AND f.attempt = r.attempt
	      AND f.status IN ('SUCCEEDED', 'FAILED', 'SKIPPED', 'OVERRIDDEN', 'COMPENSATED', 'CANCELLED'))`

// orphanQuery selects the orphan signature: a live instance whose latest
// WORKFLOW_INSTANCE lease is HELD and lapsed by $2, with a node whose latest
// attempt is READY or RUNNING, and no live ready-work claim ([liveClaim]).
const orphanQuery = `
	SELECT i.instance_id, i.instance_version, i.runtime_status,
	       l.holder_id, l.fence_token, l.expires_at
	FROM workflow_instance i
	JOIN workflow_lease l
	  ON l.tenant_id = i.tenant_id
	 AND l.resource_kind = 'WORKFLOW_INSTANCE'
	 AND l.resource_id = i.instance_id::text
	 AND l.lease_state = 'HELD'
	 AND l.expires_at <= $2
	WHERE i.tenant_id = $1
	  AND i.runtime_status = ANY($3::text[])
	  AND EXISTS (
	      SELECT 1 FROM workflow_node_execution n
	      WHERE n.tenant_id = i.tenant_id AND n.instance_id = i.instance_id
	        AND n.status IN ('READY', 'RUNNING')
	        AND n.attempt = (
	            SELECT max(m.attempt) FROM workflow_node_execution m
	            WHERE m.tenant_id = n.tenant_id AND m.instance_id = n.instance_id AND m.node_id = n.node_id))
	  AND NOT EXISTS (
	      SELECT 1 FROM workflow_ready_work r
	      WHERE r.tenant_id = i.tenant_id AND r.instance_id = i.instance_id
	        AND ` + liveClaim + `)
	ORDER BY l.expires_at, i.instance_id
	LIMIT $4`

// FindOrphans reads one tenant's orphaned instances as of now, oldest lapse
// first, at most limit of them. It writes nothing.
func FindOrphans(ctx context.Context, ex Executor, tenantID uuid.UUID, now time.Time, limit int) (ret0 []Orphan, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.recover.find_orphans", observe.Attrs{observe.KeyTenant: tenantID.String()})
	defer func() { observe.DoneWith(obsOp, retErr, len(ret0)) }()
	if tenantID == uuid.Nil || now.IsZero() || limit < 1 {
		return nil, invalid("finding orphans needs a tenant, the caller's instant and a positive limit")
	}
	return queryOrphans(ctx, ex, tenantID, now, limit)
}

func loadOrphan(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) (Orphan, bool, error) {
	// The claim just took the lapsed lease over, so the signature is re-read
	// without the lease join: what must still hold is the instance and its
	// open nodes.
	store := runtime.Store{}
	inst, err := store.LoadInstance(ctx, ex, tenantID, instanceID)
	if err != nil {
		return Orphan{}, false, err
	}
	live := false
	for _, status := range orphanStatuses {
		if string(inst.RuntimeStatus) == status {
			live = true
		}
	}
	if !live {
		return Orphan{}, false, nil
	}
	nodes, err := openNodes(ctx, ex, tenantID, instanceID)
	if err != nil || len(nodes) == 0 {
		return Orphan{}, false, err
	}
	var claims int
	if err := ex.QueryRow(ctx, `
		SELECT count(*) FROM workflow_ready_work r
		WHERE r.tenant_id = $1 AND r.instance_id = $2 AND `+liveClaim,
		tenantID, instanceID).Scan(&claims); err != nil {
		return Orphan{}, false, fmt.Errorf("recover: read live ready work: %w", err)
	}
	if claims > 0 {
		return Orphan{}, false, nil
	}
	return Orphan{
		TenantID: tenantID, InstanceID: instanceID, InstanceVersion: inst.InstanceVersion,
		Status: inst.RuntimeStatus, Nodes: nodes,
	}, true, nil
}

func queryOrphans(ctx context.Context, ex Executor, tenantID uuid.UUID, now time.Time, limit int) ([]Orphan, error) {
	rows, err := ex.Query(ctx, orphanQuery, tenantID, now.UTC(), orphanStatuses, limit)
	if err != nil {
		return nil, fmt.Errorf("recover: select orphaned instances: %w", err)
	}
	var out []Orphan
	for rows.Next() {
		o := Orphan{TenantID: tenantID}
		var (
			status string
			token  int64
		)
		if err := rows.Scan(&o.InstanceID, &o.InstanceVersion, &status, &o.HolderID, &token, &o.ExpiresAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("recover: scan orphaned instance: %w", err)
		}
		o.Status, o.Token, o.ExpiresAt = runtime.InstanceStatus(status), uint64(token), o.ExpiresAt.UTC()
		out = append(out, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, fmt.Errorf("recover: iterate orphaned instances: %w", err)
	}
	for i := range out {
		nodes, err := openNodes(ctx, ex, tenantID, out[i].InstanceID)
		if err != nil {
			return nil, err
		}
		out[i].Nodes = nodes
	}
	return out, nil
}

// openNodes returns the nodes whose latest attempt is READY or RUNNING.
func openNodes(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) ([]OpenNode, error) {
	rows, err := (runtime.Store{}).LoadNodeExecutions(ctx, ex, tenantID, instanceID)
	if err != nil {
		return nil, err
	}
	latest := map[string]runtime.NodeExecution{}
	for _, row := range rows {
		if cur, ok := latest[row.NodeID]; !ok || row.Attempt > cur.Attempt {
			latest[row.NodeID] = row
		}
	}
	var out []OpenNode
	for id, row := range latest {
		if row.Status == runtime.NodeReady || row.Status == runtime.NodeRunning {
			out = append(out, OpenNode{NodeID: id, Attempt: row.Attempt, Status: row.Status})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out, nil
}

func orphanAttrs(o Orphan) observe.Attrs {
	return observe.Attrs{observe.KeyTenant: o.TenantID.String(), observe.KeyInstance: o.InstanceID.String()}
}
