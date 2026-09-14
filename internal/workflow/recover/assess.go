package recover

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Request names the one node execution a recovery is about.
//
// Capability, EffectScope and IdempotencyKey may all be left empty, in which
// case they are derived from the identity of the node itself ([EffectScopeFor],
// [AttemptKey]). Deriving them is the point: the worker that died and the
// worker that recovers must arrive at the same TX-006 coordinates without
// having exchanged anything, and they can only do that if the coordinates are
// a function of the node rather than of the worker or the attempt.
type Request struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	NodeID     string

	// Plan is the exact compiled plan the instance is pinned to. The
	// advancement refuses a plan whose digest disagrees with the instance's
	// own compiled_plan_hash, so a recovery cannot quietly migrate a version.
	Plan *workflow.CompiledWorkflow

	// Capability is the TX-006 capability dimension. Empty means the plan's
	// own workflow id.
	Capability string
	// EffectScope is the TX-006 semantic effect scope. Empty means
	// [EffectScopeFor] of the node.
	EffectScope string
	// IdempotencyKey is the TX-006 key. Empty means [AttemptKey]. Whatever it
	// is, it must not vary with the attempt number: see the package comment.
	IdempotencyKey string

	// CorrelationID ties the recovery to the run it is repairing.
	CorrelationID string
}

const (
	// effectScopePrefix is the prefix every node effect scope carries, so an
	// operator reading an idempotency_record row can tell a workflow node
	// effect from any other guarded effect at a glance.
	effectScopePrefix = "workflow-node:"
	// attemptKeyPrefix is the prefix of the derived idempotency key.
	attemptKeyPrefix = "wf-node-effect:"
)

// EffectScopeFor is the semantic effect scope one node's business effect is
// guarded under. It names the node and nothing else -- not the attempt, and
// not the worker.
func EffectScopeFor(nodeID string) string { return effectScopePrefix + nodeID }

// AttemptKey is the attempt-independent idempotency key one node's business
// effect is guarded under.
//
// It is a function of the instance and the node alone. That is the whole
// safety property WF-RUN-003 rests on: the dead worker's attempt and every
// recovery attempt after it present the same key, so the first effect to
// commit is the only one that ever runs, and every later presentation is
// either a replay (same digest) or an IDEMPOTENCY_CONFLICT (different
// digest).
func AttemptKey(instanceID uuid.UUID, nodeID string) string {
	return attemptKeyPrefix + instanceID.String() + ":" + nodeID
}

// Scope is the TX-006 uniqueness scope this request's effect is guarded
// under, after defaults are applied.
func (r Request) Scope() idempotency.Scope {
	d := r.withDefaults()
	return idempotency.Scope{
		Tenant: d.TenantID, Capability: d.Capability,
		EffectScope: d.EffectScope, Key: d.IdempotencyKey,
	}
}

const effectDigestProfile = "hcmnext.workflow.recover.NodeEffect/v1"

type effectDigestIdentity struct {
	TenantID    string `json:"tenant_id"`
	InstanceID  string `json:"instance_id"`
	NodeID      string `json:"node_id"`
	PlanDigest  string `json:"plan_digest"`
	Capability  string `json:"capability"`
	EffectScope string `json:"effect_scope"`
	Key         string `json:"key"`
}

// EffectDigest is the canonical request digest the effect is reserved under.
//
// It covers the semantic identity of the effect and deliberately not the
// attempt, the worker, the fence or the instant: two attempts at the same
// node of the same instance under the same plan are the same request, which
// is what makes the second one a replay instead of a conflict.
func (r Request) EffectDigest() string {
	d := r.withDefaults()
	planDigest := ""
	if d.Plan != nil {
		planDigest = d.Plan.Digest()
	}
	return canonicalDigest(effectDigestProfile, effectDigestIdentity{
		TenantID: d.TenantID.String(), InstanceID: d.InstanceID.String(), NodeID: d.NodeID,
		PlanDigest: planDigest, Capability: d.Capability,
		EffectScope: d.EffectScope, Key: d.IdempotencyKey,
	})
}

// withDefaults fills the derived TX-006 coordinates. It never overwrites a
// value the caller stated.
func (r Request) withDefaults() Request {
	if r.Capability == "" && r.Plan != nil {
		r.Capability = r.Plan.WorkflowID
	}
	if r.EffectScope == "" {
		r.EffectScope = EffectScopeFor(r.NodeID)
	}
	if r.IdempotencyKey == "" {
		r.IdempotencyKey = AttemptKey(r.InstanceID, r.NodeID)
	}
	return r
}

func (r Request) validate() error {
	switch {
	case r.TenantID == uuid.Nil:
		return invalid("tenant id must not be the nil UUID")
	case r.InstanceID == uuid.Nil:
		return invalid("instance id must not be the nil UUID")
	case strings.TrimSpace(r.NodeID) == "":
		return invalid("recovery names no node id")
	case r.Plan == nil:
		return invalid("no compiled plan supplied; a recovery advances against the plan the instance is pinned to")
	}
	if _, ok := r.Plan.Node(r.NodeID); !ok {
		return invalid("node %q is not declared by the pinned plan", r.NodeID)
	}
	return nil
}

// Resource is the lease resource one node execution is claimed as. Recovery
// contends on the node execution, not on the whole instance, because a
// worker dies holding one node.
func (r Request) Resource() lease.Resource {
	return lease.Resource{
		Kind: lease.ResourceNodeExecution,
		ID:   r.InstanceID.String() + "/" + r.NodeID,
	}
}

// Disposition is what one [Recoverer.Inspect] concluded a node needs.
type Disposition string

// The declared dispositions. There is no fifth: every node is either not
// this package's business (finished, or held by a live worker) or is one of
// WF-RUN-003's two recovery shapes.
const (
	// DispositionNothingToRecover reports a node whose latest attempt has
	// already finished. A crash after the result commit lands here, which is
	// how "no lost work" is observable rather than assumed.
	DispositionNothingToRecover Disposition = "NOTHING_TO_RECOVER"
	// DispositionLeaseLive reports a node whose lease has not lapsed by the
	// caller's instant. Recovering it would be declaring a live worker dead.
	DispositionLeaseLive Disposition = "LEASE_LIVE"
	// DispositionReplayResult reports a dead attempt whose effect is already
	// recorded COMPLETED under the attempt's idempotency key. The new attempt
	// reads the stored result and advances without re-running anything.
	DispositionReplayResult Disposition = "REPLAY_COMMITTED_RESULT"
	// DispositionExecuteEffect reports a dead attempt with nothing recorded.
	// The new attempt performs the effect, under the same key.
	DispositionExecuteEffect Disposition = "EXECUTE_MISSING_EFFECT"
)

// Recoverable reports whether the disposition asks for any work at all.
func (d Disposition) Recoverable() bool {
	return d == DispositionReplayResult || d == DispositionExecuteEffect
}

// Assessment is everything one look at the durable rows concluded. It is
// derived entirely from committed state -- the instance row, the node
// execution rows, the workflow_lease row and the idempotency_record row --
// and from the instant the [Clock] port supplied. Nothing in it comes from a
// worker's memory, which is what makes a recovery restartable from a cold
// process.
type Assessment struct {
	Disposition Disposition

	// InstanceVersion and InstanceStatus are the instance as read.
	InstanceVersion int64
	InstanceStatus  runtime.InstanceStatus

	// DeadAttempt is the attempt the dead worker was on and DeadAttemptStatus
	// is the status it was left in. NextAttempt is the attempt this recovery
	// would schedule: always DeadAttempt + 1, never a reuse.
	DeadAttempt       int
	DeadAttemptStatus runtime.NodeStatus
	NextAttempt       int
	StepType          workflow.StepType

	// LeaseHeld, LeaseExpired, HolderID and Token describe the workflow_lease
	// row as of the supplied instant. A resource nobody holds is
	// LeaseHeld=false, which is recoverable: a lease that was already expired
	// away leaves the node just as orphaned as one that lapsed.
	LeaseHeld    bool
	LeaseExpired bool
	HolderID     string
	Token        uint64
	ExpiresAt    time.Time

	// EffectRecorded reports a COMPLETED idempotency record under the
	// attempt's scope, and EffectIdentity is what it stored. Together they
	// are the only thing that decides between the two dispositions.
	EffectRecorded bool
	EffectIdentity idempotency.ResultIdentity
	// EffectDigestRecorded is the request digest the stored record is bound
	// to. A record under a different digest is reported here rather than
	// silently treated as this request's own.
	EffectDigestRecorded string
}

// Inspect reads the durable rows and reports what recovery the node needs, as
// of now. It writes nothing at all: every statement it issues is a SELECT,
// which is what lets a cold process assess a node before deciding to take it.
func (r Recoverer) Inspect(ctx context.Context, ex Executor, req Request, now time.Time) (ret0 Assessment, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.recover.inspect", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := req.validate(); err != nil {
		return Assessment{}, err
	}
	if now.IsZero() {
		return Assessment{}, invalid("inspecting a node needs the caller's own clock reading")
	}
	req = req.withDefaults()
	instanceID, nodeID := req.InstanceID.String(), req.NodeID

	store := runtime.Store{}
	inst, err := store.LoadInstance(ctx, ex, req.TenantID, req.InstanceID)
	if err != nil {
		return Assessment{}, wrap(CodeStorageFailed, ErrStorage, instanceID, nodeID, err, "read the instance")
	}
	if inst.CompiledPlanHash != req.Plan.Digest() {
		return Assessment{}, refuse(CodeInvalid, ErrInvalid, instanceID, nodeID,
			"instance pins compiled plan %s; recovery was called with a plan digesting to %s",
			inst.CompiledPlanHash, req.Plan.Digest())
	}

	rows, err := store.LoadNodeExecutions(ctx, ex, req.TenantID, req.InstanceID)
	if err != nil {
		return Assessment{}, wrap(CodeStorageFailed, ErrStorage, instanceID, nodeID, err, "read the node executions")
	}
	latest, found := latestAttempt(rows, nodeID)
	if !found {
		return Assessment{}, refuse(CodeNotRecoverable, ErrNotRecoverable, instanceID, nodeID,
			"no attempt was ever recorded for this node; there is nothing a recovery could continue")
	}

	out := Assessment{
		InstanceVersion:   inst.InstanceVersion,
		InstanceStatus:    inst.RuntimeStatus,
		DeadAttempt:       latest.Attempt,
		DeadAttemptStatus: latest.Status,
		NextAttempt:       latest.Attempt + 1,
		StepType:          latest.StepType,
	}

	obs, err := r.opts.Leases.Observe(ctx, ex, req.TenantID, req.Resource(), now)
	if err != nil {
		return Assessment{}, wrap(CodeStorageFailed, ErrStorage, instanceID, nodeID, err, "observe the node execution lease")
	}
	out.LeaseHeld, out.LeaseExpired = obs.Held, obs.Expired
	out.HolderID, out.Token, out.ExpiresAt = obs.HolderID, obs.Token, obs.ExpiresAt

	// A finished attempt is not recoverable, and saying so before the lease is
	// even considered would be wrong the other way round: a worker that
	// committed its result and then died still holds a lease for a while, and
	// its node needs nothing.
	if latest.Status.Finished() {
		out.Disposition = DispositionNothingToRecover
		return out, nil
	}
	if obs.Held && !obs.Expired {
		out.Disposition = DispositionLeaseLive
		return out, nil
	}

	rec, hasRecord, err := r.opts.Idempotency.Lookup(ctx, ex, req.Scope())
	if err != nil {
		return Assessment{}, wrap(CodeStorageFailed, ErrStorage, instanceID, nodeID, err,
			"read the attempt's idempotency record")
	}
	if hasRecord {
		out.EffectDigestRecorded = rec.RequestDigest
		if rec.Status == idempotency.StatusCompleted {
			out.EffectRecorded = true
			out.EffectIdentity = rec.Identity
		}
	}
	if out.EffectRecorded {
		out.Disposition = DispositionReplayResult
	} else {
		out.Disposition = DispositionExecuteEffect
	}
	return out, nil
}

// latestAttempt returns the highest-numbered recorded attempt of one node.
func latestAttempt(rows []runtime.NodeExecution, nodeID string) (runtime.NodeExecution, bool) {
	var (
		out   runtime.NodeExecution
		found bool
	)
	for _, row := range rows {
		if row.NodeID != nodeID {
			continue
		}
		if !found || row.Attempt > out.Attempt {
			out, found = row, true
		}
	}
	return out, found
}

// retirePath is the sequence of legal node transitions that takes a dead
// attempt from where it was left to RETRYING, which is the only status the
// node state machine lets a further attempt follow.
//
// It is a lookup rather than a search so that the reasoning is readable: a
// RUNNING attempt failed (its worker died mid-run), a WAITING one failed the
// same way, and a READY one has to be marked RUNNING first because the
// machine has no READY -> FAILED edge. An already-RETRYING attempt is
// already retired, and a finished one is never reached (Inspect reports
// NOTHING_TO_RECOVER first).
func retirePath(current runtime.NodeStatus) []runtime.NodeStatus {
	switch current {
	case runtime.NodeRetrying:
		return nil
	case runtime.NodeFailed:
		return []runtime.NodeStatus{runtime.NodeRetrying}
	case runtime.NodeRunning, runtime.NodeWaiting:
		return []runtime.NodeStatus{runtime.NodeFailed, runtime.NodeRetrying}
	case runtime.NodeReady:
		return []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeFailed, runtime.NodeRetrying}
	default:
		return nil
	}
}

// canonicalDigest hashes a value under a profile, following the same
// profile-prefixed sha256 style internal/workflow/runtime, frontier and lease
// use.
func canonicalDigest(profile string, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// Every value this package digests is plain data it assembled itself,
		// so this is unreachable. If it ever happens, produce bytes that
		// cannot collide with a real digest rather than an empty one.
		b = []byte("unencodable:" + err.Error())
	}
	h := sha256.New()
	h.Write([]byte(profile))
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}
