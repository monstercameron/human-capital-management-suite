package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// ErrConfig reports a Scheduler that cannot be built from the supplied
// [Config]. It is returned by [New] before anything is started, so a
// misconfigured replica fails at composition rather than at its first tick.
var ErrConfig = errors.New("scheduler: invalid configuration")

// Defaults a Config may leave unset.
const (
	// DefaultBatchSize bounds how many units of ready work one tick claims
	// per (tenant, queue). It is the admission bound: a replica never holds
	// more instance leases at once than this.
	DefaultBatchSize = 16
	// DefaultBulkBatchShare bounds low-priority and REPLAY units within one
	// (tenant, queue) tick. A live pilot row therefore retains capacity even
	// when a tenant has a large background backlog.
	DefaultBulkBatchShare = 2
	// PilotPriority is the conventional high-priority value used by the live
	// Promotion pilot fixtures. Lower values are more urgent.
	PilotPriority = 10
	// BulkPriority is the first priority classified as background/bulk by this
	// scheduler. Ready-work priority remains a caller-owned criticality signal;
	// the scheduler only applies this lower-priority admission ceiling.
	BulkPriority = 100
	// DefaultQueueTTL is how long a queue lease is good for before another
	// replica may take it over.
	DefaultQueueTTL = 30 * time.Second
	// DefaultInstanceTTL is how long a claim's WORKFLOW_INSTANCE lease is
	// good for. It bounds how long one unit of dispatched work is
	// unrecoverable after the replica holding it dies.
	DefaultInstanceTTL = 5 * time.Minute
	// DefaultPollInterval is how long [Scheduler.Run] sleeps after a tick
	// that found nothing to do.
	DefaultPollInterval = 2 * time.Second
)

// Beginner opens the transactions a tick runs in. A pool or a dedicated
// connection satisfies it; it is [execute.Beginner]'s exact shape, restated
// here so this package's own contract does not read as a workflow one.
type Beginner interface {
	Begin(ctx context.Context) (dbport.Tx, error)
}

// Logger is the narrow structured-logging port this package writes to. It is
// satisfied directly by *log/slog.Logger and by
// internal/platform/bootstrap.Logger.
type Logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
}

// discardLogger is what a Config that names no Logger gets.
type discardLogger struct{}

func (discardLogger) Info(string, ...any)  {}
func (discardLogger) Error(string, ...any) {}

// Config is everything a scheduler replica needs. Every field is supplied by
// the composition root; this package resolves nothing from the environment and
// reads no clock of its own.
type Config struct {
	// DB opens each tick's transactions.
	DB Beginner

	// Claims names the (tenant, queue) pairs this replica serves, one acquire
	// request each: its TenantID, its Resource (a QUEUE) and this replica's
	// Holder identity. Now and TTL are filled in per tick and may be left
	// zero. Sharding a fleet is a matter of giving replicas different Claims.
	Claims []lease.AcquireRequest

	// Leases, Timers are the durable owners this package drives. Both are
	// usable as their zero value; Timers.Attempts may be set so a fired timer
	// resolves the node attempt it wakes from durable node executions.
	Leases lease.Manager
	Timers timer.Scheduler

	// Misfire is the declared policy for a timer that came due late.
	// internal/workflow/timer refuses a fire with no policy, on purpose:
	// there is no safe default for "this promise is overdue".
	Misfire schedule.MisfireConfig

	// Dispatcher runs each claimed unit of work. Nil makes this replica
	// publish-only: it still takes leases, recovers abandoned work and fires
	// due timers into workflow_ready_work, but claims nothing -- which is
	// exactly the timer-firing role
	// definitions/architecture/process-roles.yaml describes for the scheduler
	// command, and is safe because a row that is never claimed is never lost.
	Dispatcher Dispatcher

	// Clock is this replica's own time source. Nil means time.Now.
	Clock func() time.Time

	// BatchSize, QueueTTL, InstanceTTL bound one tick. Zero means the
	// Default* constants above. BulkBatchShare is the maximum number of
	// low-priority or REPLAY rows this claim admits in one tick.
	BatchSize      int
	BulkBatchShare int
	QueueTTL       time.Duration
	InstanceTTL    time.Duration

	// Logger receives one line per lease transition, timer settle, claim and
	// settle. Nil discards them.
	Logger Logger

	// Recorder is the workflow-engine telemetry recorder attached to every
	// tick's context that does not already carry one: each claim served and
	// every lease, timer and runtime operation it reaches emits a span and a
	// structured log line. Nil attaches none.
	Recorder observe.Recorder

	Roles      RoleConfig
	SignalRole SignalRole
}

// nilTenant is the zero identifier value. It is read off a zero request
// rather than written as uuid.Nil because this package never names
// github.com/google/uuid; see the package comment.
var nilTenant = lease.AcquireRequest{}.TenantID

// Scheduler is one replica's dispatch loop.
//
// It holds the queue grants it has taken so a later tick renews them rather
// than fighting itself for a lease it already owns. That state is the only
// thing it keeps in memory, and losing it costs nothing: a restarted replica
// simply waits for its own lapsed lease and takes it again.
//
// A Scheduler is not safe for concurrent use: [Scheduler.Run] calls
// [Scheduler.Tick] serially, and two replicas are two Schedulers (usually two
// processes), never two goroutines sharing one.
type Scheduler struct {
	cfg   Config
	held  map[string]lease.Grant
	clock func() time.Time
}

// New validates a Config and returns the replica it describes.
func New(cfg Config) (*Scheduler, error) {
	cfg.Roles = cfg.Roles.withDefaults()
	if err := cfg.Roles.Validate(); err != nil {
		return nil, err
	}
	if cfg.DB == nil {
		return nil, fmt.Errorf("%w: no database Beginner", ErrConfig)
	}
	if len(cfg.Claims) == 0 {
		return nil, fmt.Errorf("%w: a replica serves at least one (tenant, queue) claim", ErrConfig)
	}
	seen := map[string]bool{}
	for i, claim := range cfg.Claims {
		if claim.TenantID == nilTenant {
			return nil, fmt.Errorf("%w: claim %d names no tenant", ErrConfig, i)
		}
		if claim.Resource.Kind != lease.ResourceQueue {
			return nil, fmt.Errorf("%w: claim %d leases %q; a scheduler claim is a QUEUE",
				ErrConfig, i, claim.Resource.Kind)
		}
		if claim.Resource.ID == "" {
			return nil, fmt.Errorf("%w: claim %d names no queue", ErrConfig, i)
		}
		if claim.Holder.WorkloadRef == "" || claim.Holder.InstanceRef == "" {
			return nil, fmt.Errorf("%w: claim %d names no workload identity", ErrConfig, i)
		}
		key := claim.TenantID.String() + "|" + claim.Resource.String()
		if seen[key] {
			return nil, fmt.Errorf("%w: claim %d repeats %s", ErrConfig, i, claim.Resource)
		}
		seen[key] = true
	}
	if cfg.Misfire.Policy == schedule.MisfireUnspecified {
		return nil, fmt.Errorf("%w: no misfire policy; an overdue promise has no safe default", ErrConfig)
	}
	if err := cfg.Misfire.Validate(); err != nil {
		return nil, fmt.Errorf("%w: misfire policy: %w", ErrConfig, err)
	}
	if cfg.BatchSize < 0 || cfg.BulkBatchShare < 0 || cfg.QueueTTL < 0 || cfg.InstanceTTL < 0 {
		return nil, fmt.Errorf("%w: batch size and lease windows are not negative", ErrConfig)
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = DefaultBatchSize
	}
	if cfg.BulkBatchShare == 0 {
		cfg.BulkBatchShare = DefaultBulkBatchShare
	}
	if cfg.QueueTTL == 0 {
		cfg.QueueTTL = DefaultQueueTTL
	}
	if cfg.InstanceTTL == 0 {
		cfg.InstanceTTL = DefaultInstanceTTL
	}
	if cfg.Logger == nil {
		cfg.Logger = discardLogger{}
	}
	clock := cfg.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &Scheduler{cfg: cfg, held: map[string]lease.Grant{}, clock: clock}, nil
}

// TickResult is what one tick did, counted across every claim it served. It is
// returned rather than only logged so a test -- and an operator's own
// instrumentation -- can assert on it without reading log lines.
type TickResult struct {
	// At is the instant the tick ran against. Every decision in the tick used
	// exactly this reading; the scheduler reads its clock once per tick.
	At time.Time
	// Leased and Refused count the queue claims this replica holds after the
	// tick and the ones another replica holds.
	Leased  int
	Refused int
	// Fired, Skipped and Deferred count the durable timers this tick settled,
	// by the misfire decision that settled them.
	Fired    int
	Skipped  int
	Deferred int
	// Recovered counts DISPATCHED rows returned to READY because the replica
	// that claimed them no longer holds the instance.
	Recovered int
	// Selected, Claimed and Contended count admissible eligible rows seen,
	// rows this replica moved to DISPATCHED, and rows another replica already
	// held the instance lease for.
	Selected  int
	Claimed   int
	Contended int
	// Completed, Retried and Abandoned count how the claimed rows settled.
	Completed int
	Retried   int
	Abandoned int
	// Signals counts signal receipts evaluated by the enabled signal role.
	Signals int
}

// Idle reports a tick that found nothing to do, which is what
// [Scheduler.Run] sleeps on.
func (r TickResult) Idle() bool {
	return r.Fired == 0 && r.Skipped == 0 && r.Recovered == 0 && r.Claimed == 0 && r.Signals == 0
}

func (r *TickResult) add(other TickResult) {
	r.Leased += other.Leased
	r.Refused += other.Refused
	r.Fired += other.Fired
	r.Skipped += other.Skipped
	r.Deferred += other.Deferred
	r.Recovered += other.Recovered
	r.Selected += other.Selected
	r.Claimed += other.Claimed
	r.Contended += other.Contended
	r.Completed += other.Completed
	r.Retried += other.Retried
	r.Abandoned += other.Abandoned
	r.Signals += other.Signals
}

// Run ticks until ctx is canceled, sleeping poll between ticks that found
// nothing to do. A non-positive poll means [DefaultPollInterval].
//
// It always returns nil: the loop's only exit is its context being done, which
// is a shutdown, not a failure. A tick that fails is logged and retried,
// because every failure a tick can suffer (a lost connection, a lease taken
// over, a contended row) is recoverable from durable state on the next one.
func (s *Scheduler) Run(ctx context.Context, poll time.Duration) error {
	if poll <= 0 {
		poll = DefaultPollInterval
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		result, err := s.Tick(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.cfg.Logger.Error("scheduler.tick_failed", "error", err.Error())
		}
		if !result.Idle() {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(poll):
		}
	}
}

// Tick runs one full cycle over every configured claim.
//
// A failure serving one claim is logged and the tick continues with the next:
// one tenant's unreachable state must not stop another tenant's work. The
// returned error is the last such failure, so a caller that wants to react to
// them still can, while the counts describe everything that did succeed.
func (s *Scheduler) Tick(ctx context.Context) (TickResult, error) {
	now := s.clock().UTC()
	out := TickResult{At: now}
	var lastErr error
	if observe.RecorderFrom(ctx) == nil {
		ctx = observe.WithRecorder(ctx, s.cfg.Recorder)
	}
	for _, claim := range s.cfg.Claims {
		claimCtx, op := observe.Begin(ctx, "workflow.scheduler.serve", claim)
		one, err := s.serve(claimCtx, claim, now)
		observe.Done(op, err)
		out.add(one)
		if err != nil {
			lastErr = err
			s.cfg.Logger.Error("scheduler.claim_failed",
				"tenant", claim.TenantID.String(), "queue", claim.Resource.ID, "error", err.Error())
		}
	}
	return out, lastErr
}

// serve runs one claim's whole cycle: try to hold the queue, settle due timers
// if this replica is the holder, recover abandoned work, then claim and
// dispatch.
//
// Only timer firing is gated on the queue lease, because
// internal/workflow/timer's Fire is a fenced write and one epoch holder is what
// makes "one promise, one wakeup" true. Recovery and claiming are not gated on
// it: they carry their own refusals (the instance lease and the row's own
// compare-and-swap version), so a replica that lost the queue race still helps
// drain the frontier instead of idling. That is what makes the fleet
// horizontally useful rather than one active replica and a row of spares.
func (s *Scheduler) serve(ctx context.Context, claim lease.AcquireRequest, now time.Time) (TickResult, error) {
	var out TickResult
	grant, held, err := s.holdQueue(ctx, claim, now)
	if err != nil {
		return out, err
	}
	if held {
		out.Leased = 1
		if s.cfg.Roles.TimerEnabled {
			fired, skipped, deferred, fireErr := s.fireTimers(ctx, claim, grant.Fence, now)
			out.Fired, out.Skipped, out.Deferred = fired, skipped, deferred
			s.cfg.Logger.Info("scheduler.timer_role_tick",
				"tenant", claim.TenantID.String(), "queue", claim.Resource.ID,
				"shard", s.cfg.Roles.TimerShard, "fired", fired,
				"skipped", skipped, "deferred", deferred,
				"fence_token", grant.Fence.Token)
			if fireErr != nil {
				return out, fireErr
			}
		}
	} else {
		out.Refused = 1
	}

	recovered, err := s.recover(ctx, claim, now)
	out.Recovered = recovered
	if err != nil {
		return out, err
	}
	if held && s.cfg.Roles.SignalEnabled && s.cfg.SignalRole != nil {
		count, signalErr := runSignalRole(ctx, s.cfg.SignalRole, claim, grant.Fence, now, s.cfg.Roles.SignalShard)
		out.Signals = count
		if signalErr != nil {
			return out, signalErr
		}
	}

	if s.cfg.Dispatcher == nil {
		return out, nil
	}
	claimed, err := s.claim(ctx, claim, now)
	out.Selected, out.Claimed, out.Contended = claimed.selected, len(claimed.work), claimed.contended
	if err != nil {
		return out, err
	}
	for _, work := range claimed.work {
		disposition := s.dispatch(ctx, work)
		switch disposition {
		case DispositionCompleted:
			out.Completed++
		case DispositionAbandoned:
			out.Abandoned++
		default:
			out.Retried++
		}
		if err := s.settle(ctx, claim, work, disposition, s.clock().UTC()); err != nil {
			return out, err
		}
	}
	return out, nil
}

func runSignalRole(ctx context.Context, role SignalRole, claim lease.AcquireRequest, fence lease.Fence, now time.Time, shard string) (int, error) {
	if fenced, ok := role.(FencedSignalRole); ok {
		return fenced.RunFencedSignalRole(ctx, claim, fence, now, shard)
	}
	return role.RunSignalRole(ctx, claim, now, shard)
}

// holdQueue takes or renews this replica's lease on one claim's queue. The
// second result is false when another replica holds it, which is not an error:
// it is the fleet working correctly.
func (s *Scheduler) holdQueue(ctx context.Context, claim lease.AcquireRequest, now time.Time) (lease.Grant, bool, error) {
	key := claim.TenantID.String() + "|" + claim.Resource.String()
	if prior, ok := s.held[key]; ok {
		grant, err := s.renew(ctx, claim, prior, now)
		switch {
		case err == nil:
			s.held[key] = grant
			return grant, true, nil
		case errors.Is(err, lease.ErrLeaseLost), errors.Is(err, lease.ErrFenceStale):
			// Somebody took the queue while this replica was away. Forget the
			// grant and fall through to a fresh acquire, which either takes it
			// back (their window has passed too) or is refused.
			delete(s.held, key)
			s.cfg.Logger.Info("scheduler.queue_lease_lost",
				"queue", claim.Resource.ID, "reason", lease.CodeOf(err))
		default:
			return lease.Grant{}, false, err
		}
	}

	req := claim
	req.Now, req.TTL = now, s.cfg.QueueTTL
	var grant lease.Grant
	err := s.inTenantTx(ctx, claim, func(tx dbport.Tx) error {
		var acquireErr error
		grant, acquireErr = s.cfg.Leases.Acquire(ctx, tx, req)
		return acquireErr
	})
	switch {
	case err == nil:
		s.held[key] = grant
		s.cfg.Logger.Info("scheduler.queue_leased",
			"queue", claim.Resource.ID, "fence_token", grant.Fence.Token,
			"expires_at", grant.ExpiresAt.Format(time.RFC3339Nano))
		return grant, true, nil
	case errors.Is(err, lease.ErrHeld):
		return lease.Grant{}, false, nil
	default:
		return lease.Grant{}, false, err
	}
}

func (s *Scheduler) renew(ctx context.Context, claim lease.AcquireRequest, prior lease.Grant, now time.Time) (lease.Grant, error) {
	var grant lease.Grant
	err := s.inTenantTx(ctx, claim, func(tx dbport.Tx) error {
		var renewErr error
		grant, renewErr = s.cfg.Leases.Renew(ctx, tx, prior.Fence, now, s.cfg.QueueTTL)
		return renewErr
	})
	return grant, err
}

// recover returns abandoned work to the pool.
//
// A DISPATCHED row whose instance lease is gone, or has lapsed by this
// replica's own reading, was claimed by a replica that is not coming back. The
// row goes to READY and the lapsed lease is retired, so the next selection
// admits it again. This is the whole of SVC-004's "restart loses no ready
// work": nothing about recovery consults process memory.
func (s *Scheduler) recover(ctx context.Context, claim lease.AcquireRequest, now time.Time) (int, error) {
	count := 0
	err := s.inTenantTx(ctx, claim, func(tx dbport.Tx) error {
		rows, err := abandoned(ctx, tx, claim, s.cfg.BatchSize)
		if err != nil {
			return err
		}
		for _, row := range rows {
			resource := instanceResource(row)
			observed, err := s.cfg.Leases.Observe(ctx, tx, claim.TenantID, resource, now)
			if err != nil {
				return err
			}
			if observed.Held && !observed.Expired {
				continue
			}
			if observed.Held {
				if _, err := s.cfg.Leases.Expire(ctx, tx, claim.TenantID, resource, now); err != nil {
					return err
				}
			}
			if err := (runtimestate.ReadyWorkStore{}).Transition(ctx, tx,
				row.TenantID, row.ReadyWorkID, row.Version, runtimestate.ReadyReady, time.Time{}); err != nil {
				return err
			}
			count++
			s.cfg.Logger.Info("scheduler.ready_work_recovered",
				"instance", row.InstanceID.String(), "node", row.NodeID, "attempt", row.Attempt)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// fireTimers settles every due promise under the queue fence. The fence, not
// the loop, is the safety property: a replica whose queue lease was taken over
// fires nothing, because internal/workflow/timer verifies it first.
func (s *Scheduler) fireTimers(ctx context.Context, claim lease.AcquireRequest, fence lease.Fence, now time.Time) (fired, skipped, deferred int, err error) {
	var result timer.FireResult
	err = s.inTenantTx(ctx, claim, func(tx dbport.Tx) error {
		var fireErr error
		result, fireErr = s.cfg.Timers.Fire(ctx, tx, timer.FireRequest{
			TenantID: claim.TenantID, Now: now, Fence: fence,
			Misfire: s.cfg.Misfire, Limit: s.cfg.BatchSize,
		})
		return fireErr
	})
	if err != nil {
		return 0, 0, 0, err
	}
	for _, settled := range result.Fired {
		s.cfg.Logger.Info("scheduler.timer_fired",
			"timer", settled.Timer.TimerID.String(), "instance", settled.Timer.InstanceID.String(),
			"node", settled.Timer.NodeID, "attempt", settled.Attempt, "replay", settled.ReadyWorkReplay)
	}
	return len(result.Fired), len(result.Skipped), len(result.Deferred), nil
}

// claimed is what one claiming transaction produced.
type claimed struct {
	work      []Work
	selected  int
	contended int
}

// claim selects the admissible, eligible ready work and takes it.
//
// Selection and claiming are one transaction on purpose: the row lock the
// selection takes is what a concurrent replica skips, and it only holds for as
// long as that transaction does. Each row is claimed by acquiring the
// instance's own lease and then moving the row under its compare-and-swap
// version -- two independent refusals, either of which is enough on its own to
// stop a duplicate publication.
func (s *Scheduler) claim(ctx context.Context, claim lease.AcquireRequest, now time.Time) (claimed, error) {
	var out claimed
	err := s.inTenantTx(ctx, claim, func(tx dbport.Tx) error {
		rows, err := eligible(ctx, tx, claim, now, s.cfg.BatchSize)
		if err != nil {
			return err
		}
		rows = admitBulkShare(rows, s.cfg.BatchSize, s.cfg.BulkBatchShare)
		out.selected = len(rows)
		for _, candidate := range rows {
			row := candidate.row
			grant, err := s.cfg.Leases.Acquire(ctx, tx, lease.AcquireRequest{
				TenantID: claim.TenantID, Resource: instanceResource(row),
				Holder: claim.Holder, Now: now, TTL: s.cfg.InstanceTTL,
			})
			if errors.Is(err, lease.ErrHeld) {
				out.contended++
				continue
			}
			if err != nil {
				return err
			}
			claimedRow, err := (runtimestate.ReadyWorkStore{}).Claim(ctx, tx,
				row.TenantID, row.ReadyWorkID, row.Version)
			if err != nil {
				return err
			}
			row = claimedRow
			out.work = append(out.work, Work{Row: row, Fence: grant.Fence})
			s.cfg.Logger.Info("scheduler.ready_work_claimed",
				"instance", row.InstanceID.String(), "node", row.NodeID, "attempt", row.Attempt,
				"priority", row.Priority, "fence_token", grant.Fence.Token)
		}
		return nil
	})
	if err != nil {
		return claimed{}, err
	}
	return out, nil
}

// admitBulkShare applies the scheduler's additive fairness bound after the
// database has selected its deterministic prefix. SQL ordering puts every
// EXECUTE row before a REPLAY row and priority puts live pilot work before
// low-priority bulk work, so dropping excess background rows cannot hide an
// urgent row from the next tick. The rows remain READY and are therefore
// durable, visible, and eligible on the following tick.
func admitBulkShare(rows []eligibleWork, batchSize, bulkShare int) []eligibleWork {
	if batchSize <= 0 || bulkShare < 0 {
		return nil
	}
	if len(rows) <= batchSize && bulkShare >= len(rows) {
		return rows
	}
	out := make([]eligibleWork, 0, minInt(len(rows), batchSize))
	background := 0
	for _, row := range rows {
		if row.mode == "REPLAY" || row.row.Priority >= BulkPriority {
			if background >= bulkShare {
				continue
			}
			background++
		}
		out = append(out, row)
		if len(out) == batchSize {
			break
		}
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// dispatch hands one claim to the dispatcher, outside any transaction of this
// package's. A dispatcher that fails or panics-free-returns an error is a
// retry: the row goes back to READY rather than being lost or double-run.
func (s *Scheduler) dispatch(ctx context.Context, work Work) Disposition {
	disposition, err := s.cfg.Dispatcher.Dispatch(ctx, work)
	if err != nil {
		s.cfg.Logger.Error("scheduler.dispatch_failed",
			"instance", work.Row.InstanceID.String(), "node", work.Row.NodeID, "error", err.Error())
		return DispositionRetry
	}
	if !disposition.Valid() {
		s.cfg.Logger.Error("scheduler.dispatch_undeclared_disposition",
			"instance", work.Row.InstanceID.String(), "node", work.Row.NodeID, "disposition", string(disposition))
		return DispositionRetry
	}
	return disposition
}

// settle writes the claimed row's outcome and gives the instance back.
//
// The release is in the same transaction as the row move, so an instance is
// never left leased by a replica that has already finished with it, and never
// released while its row still says DISPATCHED.
func (s *Scheduler) settle(ctx context.Context, claim lease.AcquireRequest, work Work,
	disposition Disposition, at time.Time,
) error {
	state, terminal, err := disposition.settleState()
	if err != nil {
		return err
	}
	completedAt := time.Time{}
	if terminal {
		completedAt = at
	}
	return s.inTenantTx(ctx, claim, func(tx dbport.Tx) error {
		if err := (runtimestate.ReadyWorkStore{}).Transition(ctx, tx,
			work.Row.TenantID, work.Row.ReadyWorkID, work.Row.Version, state, completedAt); err != nil {
			return err
		}
		if _, err := s.cfg.Leases.Release(ctx, tx, work.Fence, at); err != nil {
			return err
		}
		s.cfg.Logger.Info("scheduler.ready_work_settled",
			"instance", work.Row.InstanceID.String(), "node", work.Row.NodeID,
			"disposition", string(disposition), "state", state)
		return nil
	})
}

// inTenantTx runs fn in one transaction already scoped to the claim's tenant,
// committing it only when fn returns nil. Every statement this package issues
// goes through here, because migration 00026's tables are row-level-security
// protected and an unscoped statement sees nothing at all.
func (s *Scheduler) inTenantTx(ctx context.Context, claim lease.AcquireRequest, fn func(tx dbport.Tx) error) error {
	tx, err := s.cfg.DB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("scheduler: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, claim.TenantID); err != nil {
		return fmt.Errorf("scheduler: scope tenant: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("scheduler: commit: %w", err)
	}
	return nil
}
