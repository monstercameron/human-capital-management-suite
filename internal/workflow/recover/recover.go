package recover

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Options are the ports one [Recoverer] is built from. Every one of them is
// a port rather than a concrete collaborator, which is what lets the
// MUTATION case remove exactly one guard -- the fence verifier, or the
// idempotency key -- and leave the rest of the recovery running unchanged.
type Options struct {
	// DB opens the three transactions one recovery is made of.
	DB Beginner
	// Clock is the caller's own instant. This package reads no wall clock.
	Clock Clock

	// Holder is the recovering worker's own workload identity, which becomes
	// the new lease holder. WF-RUN-002 refuses a bare hostname.
	Holder lease.Identity
	// LeaseTTL is how long the recovery's own claim is good for.
	LeaseTTL time.Duration
	// Leases acquires, verifies and releases the node execution lease.
	Leases lease.Manager

	// Idempotency is TX-006's durable store. It is what tells a replay from a
	// missing effect, and what makes the effect exactly-once.
	Idempotency idempotency.Store
	// Retention is the caller-declared lifecycle policy for the guarded
	// effect. This package invents no default: a retention that expires
	// before the dead worker could still come back would reopen exactly the
	// duplicate this whole ticket closes.
	Retention idempotency.RetentionPolicy

	// Effects performs, or explains, the node's business effect.
	Effects Effect
	// Sink persists the continuation records the recovered advancement
	// derives, in the advancement's own transaction.
	Sink runtime.ContinuationSink
	// Verifier checks the presented fence before the effect is dispatched and
	// again before the advancement reads any workflow state.
	Verifier runtime.FenceVerifier

	// Failpoints crashes the recovery deterministically at a declared
	// boundary. Nil means [NoFailpoint].
	Failpoints Failpoint
	// TraceID is stamped on the advancement this recovery performs.
	TraceID string
}

// Recoverer recovers node executions whose worker died. It holds no state
// and starts nothing: no goroutine, no ticker, no reaper (see the package
// comment), and every instant it uses comes from [Options.Clock].
type Recoverer struct{ opts Options }

// New validates the wiring and returns a [Recoverer].
func New(opts Options) (Recoverer, error) {
	switch {
	case opts.DB == nil:
		return Recoverer{}, invalid("a database Beginner is required")
	case opts.Clock == nil:
		return Recoverer{}, invalid("a Clock is required; this package reads no wall clock")
	case opts.Idempotency == nil:
		return Recoverer{}, invalid("an idempotency Store is required; it is what makes the effect exactly-once")
	case opts.Effects == nil:
		return Recoverer{}, invalid("an Effect is required")
	case opts.Sink == nil:
		return Recoverer{}, invalid("a ContinuationSink is required")
	case opts.Verifier == nil:
		return Recoverer{}, invalid("a FenceVerifier is required; an unfenced recovery is the RED case")
	case opts.LeaseTTL <= 0:
		return Recoverer{}, invalid("lease ttl must be positive")
	}
	// The holder identity is validated by the lease package's own rule rather
	// than by a second copy of it here: HolderID and ParseHolder are exact
	// inverses, so round-tripping is the check.
	if _, err := lease.ParseHolder(opts.Holder.HolderID()); err != nil {
		e := invalid("recovering worker identity is not a workload identity")
		e.err = err
		return Recoverer{}, e
	}
	if err := opts.Retention.Validate(); err != nil {
		e := invalid("idempotency retention policy is not honorable")
		e.err = err
		return Recoverer{}, e
	}
	if opts.Failpoints == nil {
		opts.Failpoints = NoFailpoint{}
	}
	return Recoverer{opts: opts}, nil
}

// Receipt is everything one [Recoverer.Recover] call did.
type Receipt struct {
	// Assessment is what the durable rows said before anything was written.
	Assessment Assessment
	// Recovered reports that a new attempt was scheduled and advanced. It is
	// false for a node that needed nothing.
	Recovered bool

	// Attempt is the attempt number this recovery scheduled.
	Attempt int
	// Fence is the fence the recovery minted and wrote under.
	Fence runtime.Fence
	// Lease is the lease transition evidence the takeover produced
	// (TAKEN_OVER when a lapsed holder was retired, ACQUIRED otherwise).
	Lease lease.Evidence

	// Effect is what the dispatch settled on, including whether the committed
	// result was replayed rather than re-run.
	Effect EffectOutcome
	// Advance is the runtime advancement receipt.
	Advance runtime.AdvanceReceipt

	// CrashedAt is the boundary an injected [Failpoint] fired at, when one
	// did. The returned error classifies as [ErrCrashed] in that case.
	CrashedAt Phase
}

// Recover recovers one node execution whose worker died.
//
// It is three transactions with four failpoint boundaries between them (see
// the package comment), and it is safe to call again after any of them: the
// next call re-derives what is left to do from the durable rows alone. A
// node that needs nothing -- already finished, or still held by a live
// worker -- is reported in the receipt rather than forced.
func (r Recoverer) Recover(ctx context.Context, req Request) (ret0 Receipt, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.recover.recover", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := req.validate(); err != nil {
		return Receipt{}, err
	}
	req = req.withDefaults()
	now := r.opts.Clock.Now()
	if now.IsZero() {
		return Receipt{}, invalid("the clock port reported the zero instant")
	}
	instanceID, nodeID := req.InstanceID.String(), req.NodeID

	// --- TX1: take over the lapsed lease and schedule the next attempt -----
	took, out, err := r.takeOver(ctx, req, now)
	if err != nil || !out.Recovered {
		return out, err
	}
	if err := r.crash(ctx, PhaseAfterNodeStateCommitBeforeDispatch, instanceID, nodeID); err != nil {
		out.CrashedAt = PhaseAfterNodeStateCommitBeforeDispatch
		return out, err
	}

	// --- TX2: dispatch the effect, or replay the committed result ----------
	tx2, err := r.begin(ctx, req.TenantID)
	if err != nil {
		return out, err
	}
	effect, err := r.DispatchEffect(ctx, tx2, req, took.attempt, took.fence, now)
	if err != nil {
		_ = tx2.Rollback(ctx)
		return out, err
	}
	if err := tx2.Commit(ctx); err != nil {
		_ = tx2.Rollback(ctx)
		return out, wrap(CodeStorageFailed, ErrStorage, instanceID, nodeID, err, "commit the effect dispatch")
	}
	out.Effect = effect
	if err := r.crash(ctx, PhaseAfterDispatchBeforeResultCommit, instanceID, nodeID); err != nil {
		out.CrashedAt = PhaseAfterDispatchBeforeResultCommit
		return out, err
	}

	// --- TX3: advance under the same fence, then give the lease back -------
	advanced, err := r.commitResult(ctx, req, took, effect, now)
	if err != nil {
		return out, err
	}
	out.Advance = advanced
	if err := r.crash(ctx, PhaseAfterResultCommit, instanceID, nodeID); err != nil {
		out.CrashedAt = PhaseAfterResultCommit
		return out, err
	}
	return out, nil
}

// takeover is what TX1 established.
type takeover struct {
	grant   lease.Grant
	fence   runtime.Fence
	version int64
	attempt int
}

// takeOver is TX1: assess, take the lapsed lease, retire the dead attempt and
// schedule the next one, all in one transaction. A crash at
// [PhaseBeforeNodeStateCommit] rolls every one of those back together, which
// is what makes that boundary observable as "nothing happened".
func (r Recoverer) takeOver(ctx context.Context, req Request, now time.Time) (takeover, Receipt, error) {
	instanceID, nodeID := req.InstanceID.String(), req.NodeID
	var out Receipt

	tx, err := r.begin(ctx, req.TenantID)
	if err != nil {
		return takeover{}, out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	assess, err := r.Inspect(ctx, tx, req, now)
	if err != nil {
		return takeover{}, out, err
	}
	out.Assessment = assess
	switch assess.Disposition {
	case DispositionLeaseLive:
		return takeover{}, out, refuse(CodeLeaseLive, ErrLeaseLive, instanceID, nodeID,
			"%s still holds the node execution until %s and the caller's instant is %s",
			assess.HolderID, assess.ExpiresAt.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano))
	case DispositionNothingToRecover:
		return takeover{}, out, nil
	}

	grant, err := r.opts.Leases.Acquire(ctx, tx, lease.AcquireRequest{
		TenantID: req.TenantID, Resource: req.Resource(), Holder: r.opts.Holder,
		Now: now, TTL: r.opts.LeaseTTL,
	})
	if err != nil {
		if errors.Is(err, lease.ErrHeld) {
			return takeover{}, out, wrap(CodeLeaseLive, ErrLeaseLive, instanceID, nodeID, err,
				"a concurrent holder took the node execution first")
		}
		return takeover{}, out, wrap(CodeStorageFailed, ErrStorage, instanceID, nodeID, err,
			"take over the lapsed node execution lease")
	}
	out.Lease = grant.Evidence
	fence := grant.Fence.RuntimeFence(now)
	out.Fence = fence

	store := runtime.Store{}
	version := assess.InstanceVersion
	for _, status := range retirePath(assess.DeadAttemptStatus) {
		t := runtime.NodeTransition{
			TenantID: req.TenantID, InstanceID: req.InstanceID, NodeID: req.NodeID,
			Attempt: assess.DeadAttempt, ExpectedInstanceVersion: version, Status: status,
		}
		if status == runtime.NodeFailed {
			// The error class is the fact the lease established, not a guess:
			// this attempt is failed because the worker holding it stopped
			// renewing, and that is worth reading off the row later.
			t.ErrorClass = ErrorClassWorkerDied
		}
		_, bumped, terr := store.RecordNodeTransition(ctx, tx, t)
		if terr != nil {
			return takeover{}, out, wrap(CodeStorageFailed, ErrStorage, instanceID, nodeID, terr,
				"retire the dead attempt %d as %s", assess.DeadAttempt, status)
		}
		version = bumped
	}

	next := runtime.NewNodeExecution(req.TenantID, req.InstanceID, req.NodeID,
		assess.NextAttempt, assess.StepType, runtime.NodeReady)
	next.RecordedAt = now
	_, version, err = store.RecordNodeExecution(ctx, tx, next, version)
	if err != nil {
		return takeover{}, out, wrap(CodeStorageFailed, ErrStorage, instanceID, nodeID, err,
			"schedule attempt %d", assess.NextAttempt)
	}

	if err := r.crash(ctx, PhaseBeforeNodeStateCommit, instanceID, nodeID); err != nil {
		out.CrashedAt = PhaseBeforeNodeStateCommit
		return takeover{}, out, err
	}
	if err := tx.Commit(ctx); err != nil {
		return takeover{}, out, wrap(CodeStorageFailed, ErrStorage, instanceID, nodeID, err,
			"commit the takeover and the scheduled attempt")
	}

	out.Recovered = true
	out.Attempt = assess.NextAttempt
	return takeover{grant: grant, fence: fence, version: version, attempt: assess.NextAttempt}, out, nil
}

// ErrorClassWorkerDied is the error class a retired attempt carries. It says
// what the lease established -- the holder stopped renewing -- and nothing
// more; it is never a guess about why the step itself failed.
const ErrorClassWorkerDied = "WORKER_LEASE_EXPIRED"

// DispatchEffect performs one attempt's business effect exactly once, under
// the presented fence and the attempt's TX-006 coordinates, or replays the
// result a previous attempt already committed.
//
// It is exported because it is the same call a live worker makes: a worker
// and the recoverer that follows it must agree on the scope, the key and the
// digest, and the way to guarantee that is for both to derive them here
// rather than each in its own way. tx is the caller's transaction; this
// method neither begins nor finishes it.
//
// The ordering is the point. The fence is verified before the guard is even
// reached, so a superseded holder never reserves, never dispatches and never
// touches the idempotency table at all. If the fence is somehow accepted
// anyway, the guard is what is left: the same key under the same digest
// replays, and the same key under a different digest is refused with
// TX-006's IDEMPOTENCY_CONFLICT.
func (r Recoverer) DispatchEffect(
	ctx context.Context, tx dbport.Tx, req Request, attempt int, fence runtime.Fence, now time.Time,
) (EffectOutcome, error) {
	if err := req.validate(); err != nil {
		return EffectOutcome{}, err
	}
	if attempt < 1 {
		return EffectOutcome{}, invalid("attempt must be at least 1")
	}
	if now.IsZero() {
		return EffectOutcome{}, invalid("dispatching an effect needs the caller's own clock reading")
	}
	req = req.withDefaults()
	instanceID, nodeID := req.InstanceID.String(), req.NodeID

	if err := r.opts.Verifier.VerifyFence(ctx, tx, req.TenantID, fence); err != nil {
		return EffectOutcome{}, wrap(CodeFenceRefused, ErrFenceRefused, instanceID, nodeID, err,
			"fence token %d on %s %s presented by %s was refused before anything was dispatched",
			fence.Token, fence.ResourceKind, fence.ResourceID, fence.HolderID)
	}

	scope, digest := req.Scope(), req.EffectDigest()
	effReq := EffectRequest{
		TenantID: req.TenantID, InstanceID: req.InstanceID, NodeID: req.NodeID, Attempt: attempt,
		Scope: scope, Digest: digest, Fence: fence,
		CorrelationID: req.CorrelationID, RecordedAt: now,
	}

	var (
		performed bool
		produced  frontier.NodeOutcome
	)
	rec, err := idempotency.Guard(ctx, tx, r.opts.Idempotency, scope, digest, r.opts.Retention, now,
		func(ctx context.Context, tx dbport.Tx) (idempotency.ResultIdentity, error) {
			res, perr := r.opts.Effects.Perform(ctx, tx, effReq)
			if perr != nil {
				return idempotency.ResultIdentity{}, perr
			}
			performed = true
			produced = res.Outcome
			return res.Identity, nil
		})
	if err != nil {
		return EffectOutcome{}, wrap(CodeEffectFailed, ErrEffect, instanceID, nodeID, err,
			"dispatch the node effect under idempotency key %q", scope.Key)
	}

	out := EffectOutcome{Identity: rec.Identity, Replayed: !performed}
	if performed {
		out.Outcome = produced
	} else {
		replayed, rerr := r.opts.Effects.Replay(ctx, tx, effReq, rec.Identity)
		if rerr != nil {
			return EffectOutcome{}, wrap(CodeEffectFailed, ErrEffect, instanceID, nodeID, rerr,
				"replay the committed result stored under idempotency key %q", scope.Key)
		}
		out.Outcome = replayed
	}
	if out.Outcome.NodeID == "" {
		out.Outcome.NodeID = req.NodeID
	}
	if out.Outcome.NodeID != req.NodeID {
		return EffectOutcome{}, refuse(CodeEffectFailed, ErrEffect, instanceID, nodeID,
			"the effect reported an outcome for node %q while recovering %q", out.Outcome.NodeID, req.NodeID)
	}
	return out, nil
}

// commitResult is TX3: advance the node under the same fence and give the
// lease back, in one transaction. [runtime.AdvanceFenced] verifies the fence
// again before it reads any workflow state, so a holder superseded between
// TX2 and TX3 writes nothing and dispatches no continuation.
func (r Recoverer) commitResult(
	ctx context.Context, req Request, took takeover, effect EffectOutcome, now time.Time,
) (runtime.AdvanceReceipt, error) {
	instanceID, nodeID := req.InstanceID.String(), req.NodeID

	tx, err := r.begin(ctx, req.TenantID)
	if err != nil {
		return runtime.AdvanceReceipt{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	advanced, err := runtime.AdvanceFenced(ctx, tx, runtime.FencedAdvanceRequest{
		Fence: took.fence, Verifier: r.opts.Verifier,
		Request: runtime.AdvanceRequest{
			TenantID: req.TenantID, InstanceID: req.InstanceID,
			ExpectedInstanceVersion: took.version, Attempt: took.attempt,
			Plan: req.Plan, Outcome: effect.Outcome,
			Refs:       refsFor(effect.Identity),
			TraceID:    r.opts.TraceID,
			RecordedAt: now, Sink: r.opts.Sink,
		},
	})
	if err != nil {
		if runtime.CodeOf(err) == runtime.CodeFenceRefused {
			return runtime.AdvanceReceipt{}, wrap(CodeFenceRefused, ErrFenceRefused, instanceID, nodeID, err,
				"the recovering fence was refused before the advancement read anything")
		}
		return runtime.AdvanceReceipt{}, wrap(CodeStorageFailed, ErrStorage, instanceID, nodeID, err,
			"advance the recovered attempt %d", took.attempt)
	}
	if _, err := r.opts.Leases.Release(ctx, tx, took.grant.Fence, now); err != nil {
		return runtime.AdvanceReceipt{}, wrap(CodeStorageFailed, ErrStorage, instanceID, nodeID, err,
			"release the node execution lease")
	}
	if err := tx.Commit(ctx); err != nil {
		return runtime.AdvanceReceipt{}, wrap(CodeStorageFailed, ErrStorage, instanceID, nodeID, err,
			"commit the recovered advancement")
	}
	return advanced, nil
}

// refsFor records the stored result identity on the node execution row, so
// the row itself names the effect it is the result of. Only references the
// effect actually produced are written; an absent reference stays absent
// rather than becoming an empty string that later reads as a real value.
func refsFor(identity idempotency.ResultIdentity) runtime.GovernanceRefs {
	refs := runtime.GovernanceRefs{CapabilityExecutionID: identity.ResultRef}
	for _, ref := range []string{identity.EffectIdentity, identity.EventRef} {
		if ref != "" {
			refs.EffectRefs = append(refs.EffectRefs, ref)
		}
	}
	return refs
}

// begin opens one transaction already scoped to the tenant, the way
// internal/data/tenancy documents.
func (r Recoverer) begin(ctx context.Context, tenantID uuid.UUID) (dbport.Tx, error) {
	tx, err := r.opts.DB.Begin(ctx)
	if err != nil {
		return nil, wrap(CodeStorageFailed, ErrStorage, "", "", err, "begin a recovery transaction")
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, wrap(CodeStorageFailed, ErrStorage, "", "", err, "scope the recovery transaction to its tenant")
	}
	return tx, nil
}

// crash asks the configured failpoint whether the worker dies here, and
// turns a yes into this package's typed refusal carrying the boundary.
func (r Recoverer) crash(ctx context.Context, phase Phase, instanceID, nodeID string) error {
	if err := r.opts.Failpoints.Check(ctx, phase); err != nil {
		e := wrap(CodeCrashInjected, ErrCrashed, instanceID, nodeID, err,
			"the worker died at this persistence boundary")
		e.Phase = phase
		return e
	}
	return nil
}
