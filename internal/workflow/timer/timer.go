package timer

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"
)

// Executor is the database capability this package needs: a transaction the
// caller opened and has already scoped with internal/data/tenancy.WithTenant.
type Executor = runtimestate.Executor

// Kind names the shape of wait a step declared. It is this package's
// vocabulary; [Kind.durable] maps it onto migration 00026's own timer kinds,
// which are coarser on purpose (WAIT and UNTIL are both "this node becomes
// ready at fires_at" and persist as DELAY; the finer wake shape -- fixed
// instant, local date, local date and time -- is pinned by the requirement
// digest the timer is keyed on).
type Kind string

// The declared wait shapes.
const (
	// KindWait is a relative wait: this node resumes after a duration the
	// wake condition resolved to an instant.
	KindWait Kind = "WAIT"
	// KindUntil is an absolute wait: this node resumes at a named business
	// instant.
	KindUntil Kind = "UNTIL"
	// KindDeadline is a bound on something else: at this instant, the node's
	// declared late route is taken.
	KindDeadline Kind = "DEADLINE"
)

func (k Kind) durable() (string, bool) {
	switch k {
	case KindWait, KindUntil:
		return runtimestate.TimerDelay, true
	case KindDeadline:
		return runtimestate.TimerDeadline, true
	default:
		return "", false
	}
}

// timerNamespace and readyNamespace are the UUIDv5 namespaces this package
// derives its row identities in. Deriving rather than minting is what makes a
// replayed advancement address the row it already wrote instead of creating a
// second one.
var (
	timerNamespace = uuid.MustParse("6f2a0d1e-4a2b-5c3d-9e7f-0a1b2c3d4e5f")
	readyNamespace = uuid.MustParse("7c5b1e2f-5b3c-5d4e-af80-1b2c3d4e5f60")
)

// TimerID is the derived identity of the timer promising one requirement for
// one node of one instance. Two callers that computed the same wake
// requirement address the same row.
func TimerID(tenantID, instanceID uuid.UUID, nodeID, requirementDigest string) uuid.UUID {
	return uuid.NewSHA1(timerNamespace, []byte(tenantID.String()+"|"+instanceID.String()+"|"+nodeID+"|"+requirementDigest))
}

// TimerIDForAttempt is [TimerID] qualified by the node activation it wakes.
// A node the plan routes back to (a re-approval returning through the same
// WAIT) is a new attempt with its own promise; the first activation keeps the
// unqualified identity so every row already written stays addressable.
func TimerIDForAttempt(tenantID, instanceID uuid.UUID, nodeID, requirementDigest string, attempt int) uuid.UUID {
	if attempt <= 1 {
		return TimerID(tenantID, instanceID, nodeID, requirementDigest)
	}
	return uuid.NewSHA1(timerNamespace, []byte(tenantID.String()+"|"+instanceID.String()+"|"+nodeID+"|"+TimerKey(requirementDigest, attempt)))
}

// attemptKeySuffix separates the wake-requirement digest from the activation
// qualifier in a timer key. A digest is hex and never contains it.
const attemptKeySuffix = "|attempt:"

// TimerKey is the durable timer_key for a wake requirement on one activation
// of a node. workflow_timer is unique per (instance, node, key), so a WAIT the
// plan re-enters must carry its activation in the key or its second promise
// would collapse onto the first, already-fired row. The first activation keeps
// the bare digest so every row already written stays valid.
func TimerKey(requirementDigest string, attempt int) string {
	if attempt <= 1 {
		return requirementDigest
	}
	return requirementDigest + attemptKeySuffix + itoa(attempt)
}

// KeyRequirementDigest returns the wake-requirement digest a timer key was
// built from, with any activation qualifier removed.
func KeyRequirementDigest(key string) string {
	if i := strings.Index(key, attemptKeySuffix); i >= 0 {
		return key[:i]
	}
	return key
}

// ReadyWorkID is the derived identity of the ready-work row one node attempt
// gets. It matches workflow_ready_work's own (instance, node, attempt)
// uniqueness, so a replayed settle finds its own row rather than colliding
// with a stranger's.
func ReadyWorkID(tenantID, instanceID uuid.UUID, nodeID string, attempt int) uuid.UUID {
	return uuid.NewSHA1(readyNamespace, []byte(tenantID.String()+"|"+instanceID.String()+"|"+nodeID+"|"+itoa(attempt)))
}

// AttemptReader reports an instance's node executions so a fired timer
// enqueues ready work for the attempt that is actually waiting.
// internal/workflow/runtime's Store satisfies it as it stands.
//
// It is optional: a [Scheduler] with no reader settles into attempt 1, which
// is correct for every plan this phase compiles (a WAIT node's second attempt
// only exists once WF-RUN-006's retry policy ships, and that ticket is not
// built yet). Supplying the reader makes the attempt a durable fact rather
// than an assumption.
type AttemptReader interface {
	LoadNodeExecutions(ctx context.Context, ex runtime.Executor, tenantID, instanceID uuid.UUID) ([]runtime.NodeExecution, error)
}

// Scheduler creates, cancels and settles durable timers.
//
// It holds no state and starts nothing. Every method takes the caller's
// transaction and the caller's instant; there is no ticker, no goroutine and
// no ambient clock, per the WF-RUN-000 gate.
type Scheduler struct {
	// Attempts, when set, resolves the node attempt a fired timer wakes. Nil
	// settles into attempt 1 (see [AttemptReader]).
	Attempts AttemptReader

	timers runtimestate.TimerStore
	ready  runtimestate.ReadyWorkStore
	leases lease.Manager
}

// Request asks for one durable timer.
type Request struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	NodeID     string
	Kind       Kind

	// Requirement is internal/workflow/steps/wait's pure, already-computed
	// wake requirement. Its digest becomes the timer's durable key, which is
	// how the zone, tzdb release, business calendar and reference-update
	// policy behind the instant are pinned without copying them into columns.
	Requirement wait.TimerRequirement

	CreatedAt time.Time

	// Attempt is the activation of NodeID this timer wakes. Zero or one is
	// the first activation; a later activation gets its own timer identity
	// and key so a WAIT the plan re-enters is promised again rather than
	// replayed as the already-fired first promise.
	Attempt int

	// Causal is optional diagnostic and correlation context inherited from
	// the continuation that requested this durable promise.
	Causal *runtime.CausalMetadata
}

func (r Request) validate() error {
	loc := location{instanceID: r.InstanceID, nodeID: r.NodeID}
	switch {
	case r.TenantID == uuid.Nil:
		return invalid(loc, "tenant id must not be the nil UUID")
	case r.InstanceID == uuid.Nil:
		return invalid(loc, "instance id must not be the nil UUID")
	case r.NodeID == "":
		return invalid(loc, "a timer names the node it wakes")
	case r.CreatedAt.IsZero():
		return invalid(loc, "creating a timer needs the caller's own clock reading")
	case r.Requirement.Digest == "":
		return invalid(loc, "wake requirement carries no digest; it was not produced by wait.ComputeTimerRequirement")
	}
	if _, ok := r.Kind.durable(); !ok {
		return invalid(loc, "timer kind %q is not one of WAIT, UNTIL, DEADLINE", r.Kind)
	}
	if r.Requirement.ReviewRequired {
		return refuse(CodeReviewRequired, ErrReviewRequired, loc,
			"wake requirement needs review (%s); there is no instant to promise", r.Requirement.ReviewReason)
	}
	if !r.Requirement.FireAt.IsSet() {
		return invalid(loc, "wake requirement resolved to no instant")
	}
	return nil
}

// Timer is one durable promise as this package reports it.
type Timer struct {
	TenantID   uuid.UUID
	TimerID    uuid.UUID
	InstanceID uuid.UUID
	NodeID     string

	// Key is the wake requirement's content digest, which is also the durable
	// timer_key.
	Key     string
	Kind    string
	State   string
	FiresAt time.Time
	Version uint64

	CreatedAt time.Time
	Causal    *runtime.CausalMetadata
}

// Scheduled is the result of [Scheduler.Schedule].
type Scheduled struct {
	Timer Timer
	// Replay reports that this exact promise was already on the table -- a
	// replayed advancement -- and that nothing was written this time.
	Replay   bool
	Evidence Evidence
}

// Schedule writes one durable timer for a WAIT step.
//
// The row's identity is derived from the tenant, instance, node and the wake
// requirement's digest, so a replayed advancement addresses the promise it
// already made: the second call reports Replay and writes nothing, which is
// what makes "one wait produces one wakeup" true under retry rather than
// hoped for.
//
// A requirement that needs review is refused ([ErrReviewRequired]): a wake
// condition that could not be resolved to an instant is not a promise anything
// can keep, and internal/workflow/steps/wait routes such a node to its
// declared failure route instead.
func (s Scheduler) Schedule(ctx context.Context, ex Executor, req Request) (ret0 Scheduled, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.timer.schedule", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := req.validate(); err != nil {
		return Scheduled{}, err
	}
	kind, _ := req.Kind.durable()
	firesAt := req.Requirement.FireAt.Time().UTC()
	id := TimerIDForAttempt(req.TenantID, req.InstanceID, req.NodeID, req.Requirement.Digest, req.Attempt)
	key := TimerKey(req.Requirement.Digest, req.Attempt)
	loc := location{timerID: id, instanceID: req.InstanceID, nodeID: req.NodeID}

	err := s.timers.Set(ctx, ex, runtimestate.Timer{
		TenantID: req.TenantID, TimerID: id, InstanceID: req.InstanceID, NodeID: req.NodeID,
		Key: key, Kind: kind, FiresAt: firesAt, CreatedAt: req.CreatedAt, Causal: causalToRow(req.Causal),
	})
	switch {
	case err == nil:
	case errors.Is(err, runtimestate.ErrDuplicate):
		existing, loadErr := s.Load(ctx, ex, req.TenantID, id)
		if loadErr != nil {
			return Scheduled{}, loadErr
		}
		return Scheduled{
			Timer: existing, Replay: true,
			Evidence: newEvidence(EventReplayed, existing, req.CreatedAt, "", 0, uuid.Nil,
				"a timer for this exact wake requirement was already promised"),
		}, nil
	case errors.Is(err, runtimestate.ErrInvalid):
		return Scheduled{}, wrapErr(CodeInvalid, ErrInvalid, loc, err, "the durable store refused the timer row")
	default:
		return Scheduled{}, wrapErr(CodeStorageFailed, ErrStorage, loc, err, "write the timer row")
	}

	created := Timer{
		TenantID: req.TenantID, TimerID: id, InstanceID: req.InstanceID, NodeID: req.NodeID,
		Key: key, Kind: kind, State: runtimestate.TimerPending,
		FiresAt: firesAt, Version: 1, CreatedAt: req.CreatedAt.UTC(), Causal: req.Causal,
	}
	return Scheduled{
		Timer:    created,
		Evidence: newEvidence(EventScheduled, created, req.CreatedAt, "", 0, uuid.Nil, ""),
	}, nil
}

// Load returns one timer.
func (s Scheduler) Load(ctx context.Context, ex Executor, tenantID, timerID uuid.UUID) (Timer, error) {
	row, err := s.timers.Load(ctx, ex, tenantID, timerID)
	if err != nil {
		loc := location{timerID: timerID}
		if errors.Is(err, runtimestate.ErrNotFound) {
			return Timer{}, wrapErr(CodeNotFound, ErrNotFound, loc, err, "no such timer")
		}
		return Timer{}, wrapErr(CodeStorageFailed, ErrStorage, loc, err, "read the timer row")
	}
	return fromRow(row), nil
}

// Pending returns every still-unsettled timer of one instance, soonest first.
func (s Scheduler) Pending(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) ([]Timer, error) {
	rows, err := s.timers.PendingForInstance(ctx, ex, tenantID, instanceID)
	if err != nil {
		return nil, wrapErr(CodeStorageFailed, ErrStorage, location{instanceID: instanceID}, err,
			"read the instance's pending timers")
	}
	out := make([]Timer, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromRow(row))
	}
	return out, nil
}

// History returns every timer one instance ever scheduled -- pending, fired
// and cancelled -- oldest first. It is the execution inspector's read
// (WF-RUN-019): a retry backoff that already fired explains the attempt that
// followed it, so a pending-only list would hide the retry history.
func (s Scheduler) History(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) (ret0 []Timer, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.timer.history",
		observe.Attrs{observe.KeyTenant: tenantID.String(), observe.KeyInstance: instanceID.String()})
	defer func() { observe.DoneWith(obsOp, retErr, len(ret0)) }()
	rows, err := s.timers.ForInstance(ctx, ex, tenantID, instanceID)
	if err != nil {
		return nil, wrapErr(CodeStorageFailed, ErrStorage, location{instanceID: instanceID}, err,
			"read the instance's timer history")
	}
	out := make([]Timer, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromRow(row))
	}
	return out, nil
}

// Due returns every pending timer across the tenant whose instant is at or
// before asOf, soonest first, bounded by limit. It writes nothing: it is what
// a caller reads to decide what to hand [Scheduler.Fire].
func (s Scheduler) Due(ctx context.Context, ex Executor, tenantID uuid.UUID, asOf time.Time, limit int) (ret0 []Timer, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.timer.due", observe.Attrs{observe.KeyTenant: tenantID.String()})
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	rows, err := s.timers.Due(ctx, ex, tenantID, asOf, limit)
	if err != nil {
		return nil, wrapErr(CodeStorageFailed, ErrStorage, location{}, err, "read the tenant's due timers")
	}
	out := make([]Timer, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromRow(row))
	}
	return out, nil
}

// Cancel settles one pending timer as cancelled without waking anything.
//
// It is a compare-and-swap on the timer's own version, so cancelling a timer a
// concurrent [Scheduler.Fire] already settled is [ErrAlreadySettled] rather
// than a silent overwrite.
func (s Scheduler) Cancel(ctx context.Context, ex Executor, tenantID, timerID uuid.UUID, at time.Time, reason string) (ret0 Evidence, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.timer.cancel", observe.Attrs{observe.KeyTenant: tenantID.String()}, observe.Attrs{observe.KeyTimer: timerID.String()})
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if at.IsZero() {
		return Evidence{}, invalid(location{timerID: timerID}, "cancelling a timer needs the caller's own clock reading")
	}
	row, err := s.Load(ctx, ex, tenantID, timerID)
	if err != nil {
		return Evidence{}, err
	}
	if err := s.settle(ctx, ex, row, runtimestate.TimerCancelled, at); err != nil {
		return Evidence{}, err
	}
	return newEvidence(EventCancelled, row, at, "", 0, uuid.Nil, reason), nil
}

// CancelInstance cancels every timer an instance still has outstanding.
//
// This is WF-RUN-004's cancellation clause: an instance that reached a
// terminal must not be woken later by a promise it made on the way there. The
// caller invokes it in the same transaction that completes the instance; a
// timer another caller settled first is skipped rather than fought over, so
// the call is safe to repeat.
func (s Scheduler) CancelInstance(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID, at time.Time, reason string) (ret0 []Evidence, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.timer.cancel_instance", observe.Attrs{observe.KeyTenant: tenantID.String()}, observe.Attrs{observe.KeyInstance: instanceID.String()})
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if at.IsZero() {
		return nil, invalid(location{instanceID: instanceID}, "cancelling timers needs the caller's own clock reading")
	}
	pending, err := s.Pending(ctx, ex, tenantID, instanceID)
	if err != nil {
		return nil, err
	}
	out := make([]Evidence, 0, len(pending))
	for _, row := range pending {
		if err := s.settle(ctx, ex, row, runtimestate.TimerCancelled, at); err != nil {
			if errors.Is(err, ErrAlreadySettled) {
				continue
			}
			return nil, err
		}
		out = append(out, newEvidence(EventCancelled, row, at, "", 0, uuid.Nil, reason))
	}
	return out, nil
}

func (s Scheduler) settle(ctx context.Context, ex Executor, row Timer, next string, at time.Time) error {
	loc := location{timerID: row.TimerID, instanceID: row.InstanceID, nodeID: row.NodeID}
	var err error
	switch next {
	case runtimestate.TimerFired:
		err = s.timers.Fire(ctx, ex, row.TenantID, row.TimerID, row.Version, at)
	default:
		err = s.timers.Cancel(ctx, ex, row.TenantID, row.TimerID, row.Version, at)
	}
	switch {
	case err == nil:
		return nil
	case errors.Is(err, runtimestate.ErrIllegalTransition), errors.Is(err, runtimestate.ErrVersionConflict):
		return wrapErr(CodeAlreadySettled, ErrAlreadySettled, loc, err,
			"timer was already settled by another caller")
	case errors.Is(err, runtimestate.ErrNotFound):
		return wrapErr(CodeNotFound, ErrNotFound, loc, err, "no such timer")
	default:
		return wrapErr(CodeStorageFailed, ErrStorage, loc, err, "settle the timer as %s", next)
	}
}

// FireRequest asks for every due timer in one tenant to be settled under one
// holder's fence.
type FireRequest struct {
	TenantID uuid.UUID

	// Now is the caller's own clock reading. It decides which promises are
	// due and, through the misfire policy, what to do about the overdue ones.
	Now time.Time

	// Fence is the lease the caller holds. Every settle happens under it: it
	// is verified against the durable lease before any timer is touched, so a
	// worker whose lease was taken over fires nothing.
	Fence lease.Fence

	// Misfire is the declared policy for timers that came due late. A fire
	// with no policy is refused ([ErrMisfirePolicyRequired]).
	Misfire schedule.MisfireConfig

	// Limit bounds how many due timers one call settles. Zero reads one
	// page.
	Limit int

	// Only, when non-empty, restricts the call to these timer ids. It is how
	// a caller settles one known promise instead of draining the tenant.
	Only []uuid.UUID
}

// Settled is one timer this call disposed of.
type Settled struct {
	Timer    Timer
	Decision Decision

	// ReadyWorkID and EligibleAt are set when the settle woke work. A SKIP or
	// a REVIEW wakes nothing and leaves both zero.
	ReadyWorkID uuid.UUID
	EligibleAt  time.Time
	Attempt     int

	// ReadyWorkReplay reports that the ready-work row this settle would have
	// written already existed, so the wake was already enqueued.
	ReadyWorkReplay bool

	Evidence Evidence
}

// FireResult is everything one [Scheduler.Fire] call disposed of, grouped by
// what happened to it.
type FireResult struct {
	Now time.Time
	// Fired are the timers that woke work.
	Fired []Settled
	// Skipped are the timers a SKIP policy cancelled without waking anything.
	Skipped []Settled
	// Deferred are the timers a REVIEW policy left pending for a human.
	Deferred []Settled
}

// Fire settles every due timer exactly once, under the caller's lease fence.
//
// The sequence per timer is: apply the declared misfire policy, settle the
// timer row under its own version compare-and-swap, and -- for a decision that
// wakes work -- enqueue the node's ready-work row at the eligibility instant
// the policy chose. The order matters: the CAS is what makes a second
// concurrent Fire settle nothing, so exactly one caller ever reaches the
// enqueue for a given promise.
//
// Everything runs in the caller's transaction. Nothing here polls, sleeps or
// schedules a future call.
func (s Scheduler) Fire(ctx context.Context, ex Executor, req FireRequest) (ret0 FireResult, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.timer.fire", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if req.TenantID == uuid.Nil {
		return FireResult{}, invalid(location{}, "tenant id must not be the nil UUID")
	}
	if req.Now.IsZero() {
		return FireResult{}, invalid(location{}, "firing timers needs the caller's own clock reading")
	}
	if req.Misfire.Policy == schedule.MisfireUnspecified {
		return FireResult{}, refuse(CodeMisfirePolicyRequired, ErrMisfirePolicyRequired, location{},
			"a fire declares what to do with an overdue promise; there is no default")
	}
	if _, err := s.leases.Verify(ctx, ex, req.Fence, req.Now); err != nil {
		return FireResult{}, wrapErr(CodeFenceRefused, lease.ErrLease, location{}, err,
			"the fence presented for firing timers was refused")
	}

	due, err := s.Due(ctx, ex, req.TenantID, req.Now, req.Limit)
	if err != nil {
		return FireResult{}, err
	}
	if len(req.Only) > 0 {
		due = only(due, req.Only)
	}

	out := FireResult{Now: req.Now.UTC()}
	for _, row := range due {
		settled, err := s.fireOne(ctx, ex, req, row)
		if err != nil {
			return FireResult{}, err
		}
		switch settled.Decision {
		case DecisionSkipped:
			out.Skipped = append(out.Skipped, settled)
		case DecisionReview:
			out.Deferred = append(out.Deferred, settled)
		default:
			out.Fired = append(out.Fired, settled)
		}
	}
	return out, nil
}

func (s Scheduler) fireOne(ctx context.Context, ex Executor, req FireRequest, row Timer) (Settled, error) {
	loc := location{timerID: row.TimerID, instanceID: row.InstanceID, nodeID: row.NodeID}
	decision, eligibleAt, err := Decide(row.FiresAt, req.Now, req.Misfire)
	if err != nil {
		return Settled{}, wrapErr(CodeInvalid, ErrInvalid, loc, err, "apply the declared misfire policy")
	}

	switch decision {
	case DecisionReview:
		// The row stays pending on purpose: a promise a human has to look at
		// is not settled by looking away from it.
		return Settled{
			Timer: row, Decision: decision,
			Evidence: newEvidence(EventDeferred, row, req.Now, string(decision), req.Fence.Token, uuid.Nil,
				"overdue beyond the declared grace; the misfire policy requires review"),
		}, nil
	case DecisionSkipped:
		if err := s.settle(ctx, ex, row, runtimestate.TimerCancelled, req.Now); err != nil {
			return Settled{}, err
		}
		return Settled{
			Timer: row, Decision: decision,
			Evidence: newEvidence(EventSkipped, row, req.Now, string(decision), req.Fence.Token, uuid.Nil,
				"overdue beyond the declared grace; the misfire policy skips it"),
		}, nil
	}

	if err := s.settle(ctx, ex, row, runtimestate.TimerFired, req.Now); err != nil {
		return Settled{}, err
	}
	attempt, err := s.attemptFor(ctx, ex, row)
	if err != nil {
		return Settled{}, err
	}
	readyID := ReadyWorkID(row.TenantID, row.InstanceID, row.NodeID, attempt)
	replay := false
	enqueueErr := s.ready.Enqueue(ctx, ex, runtimestate.ReadyWork{
		TenantID: row.TenantID, ReadyWorkID: readyID, InstanceID: row.InstanceID,
		NodeID: row.NodeID, Attempt: attempt,
		State: runtimestate.ReadyReady, EligibleAt: eligibleAt, EnqueuedAt: req.Now,
		Causal: causalToRow(row.Causal),
	})
	switch {
	case enqueueErr == nil:
	case errors.Is(enqueueErr, runtimestate.ErrDuplicate):
		// The derived id makes this the same row, not a competing one: the
		// wake was already enqueued for this attempt, so the promise is kept
		// and nothing is duplicated.
		replay = true
	default:
		return Settled{}, wrapErr(CodeReadyWorkConflict, ErrStorage, loc, enqueueErr,
			"enqueue the ready work this timer woke")
	}

	return Settled{
		Timer: row, Decision: decision, ReadyWorkID: readyID, EligibleAt: eligibleAt.UTC(),
		Attempt: attempt, ReadyWorkReplay: replay,
		Evidence: newEvidence(EventFired, row, req.Now, string(decision), req.Fence.Token, readyID, ""),
	}, nil
}

// attemptFor resolves which attempt of the waiting node the wake belongs to.
// With no [AttemptReader] configured it is attempt 1 -- see the port's own
// comment for why that is honest rather than a guess in this phase.
func (s Scheduler) attemptFor(ctx context.Context, ex Executor, row Timer) (int, error) {
	if s.Attempts == nil {
		return 1, nil
	}
	rows, err := s.Attempts.LoadNodeExecutions(ctx, ex, row.TenantID, row.InstanceID)
	if err != nil {
		return 0, wrapErr(CodeAttemptResolutionError, ErrStorage,
			location{timerID: row.TimerID, instanceID: row.InstanceID, nodeID: row.NodeID}, err,
			"resolve the attempt the timer wakes")
	}
	attempt := 1
	for _, ne := range rows {
		if ne.NodeID == row.NodeID && ne.Attempt > attempt {
			attempt = ne.Attempt
		}
	}
	return attempt, nil
}

// CheckRequirement refuses a caller whose freshly computed wake requirement
// does not match the promise on the table.
//
// This is WF-RUN-004's dataset clause made checkable: a republished tzdb or
// business calendar produces a different requirement digest, and a caller that
// recomputed the requirement can find out that the promise it is holding was
// made under different data instead of settling it anyway.
func (s Scheduler) CheckRequirement(row Timer, requirement wait.TimerRequirement) error {
	if requirement.Digest == "" {
		return invalid(location{timerID: row.TimerID, instanceID: row.InstanceID, nodeID: row.NodeID},
			"wake requirement carries no digest; it was not produced by wait.ComputeTimerRequirement")
	}
	if KeyRequirementDigest(row.Key) != requirement.Digest {
		return refuse(CodeRequirementDrift, ErrRequirementDrift,
			location{timerID: row.TimerID, instanceID: row.InstanceID, nodeID: row.NodeID},
			"timer promises requirement %s but the caller recomputed %s; the dataset or the wake condition changed",
			row.Key, requirement.Digest)
	}
	return nil
}

func fromRow(row runtimestate.Timer) Timer {
	return Timer{
		TenantID: row.TenantID, TimerID: row.TimerID, InstanceID: row.InstanceID, NodeID: row.NodeID,
		Key: row.Key, Kind: row.Kind, State: row.State, FiresAt: row.FiresAt, Version: row.Version,
		CreatedAt: row.CreatedAt,
		Causal:    causalFromRow(row.Causal),
	}
}

func causalToRow(in *runtime.CausalMetadata) *runtimestate.CausalMetadata {
	if in == nil {
		return nil
	}
	out := &runtimestate.CausalMetadata{CorrelationID: in.CorrelationID, CausationID: in.CausationID, LogicalOperationID: in.LogicalOperationID, AttemptID: in.AttemptID}
	if in.TraceLink != nil {
		out.TraceLink = &runtimestate.TraceLinkMetadata{TraceID: in.TraceLink.TraceID, SpanID: in.TraceLink.SpanID, TraceFlags: in.TraceLink.TraceFlags, TraceState: in.TraceLink.TraceState, ExpiresAt: in.TraceLink.ExpiresAt}
	}
	return out
}

func causalFromRow(in *runtimestate.CausalMetadata) *runtime.CausalMetadata {
	if in == nil {
		return nil
	}
	out := &runtime.CausalMetadata{CorrelationID: in.CorrelationID, CausationID: in.CausationID, LogicalOperationID: in.LogicalOperationID, AttemptID: in.AttemptID}
	if in.TraceLink != nil {
		out.TraceLink = &runtime.TraceLinkMetadata{TraceID: in.TraceLink.TraceID, SpanID: in.TraceLink.SpanID, TraceFlags: in.TraceLink.TraceFlags, TraceState: in.TraceLink.TraceState, ExpiresAt: in.TraceLink.ExpiresAt}
	}
	return out
}

func only(rows []Timer, ids []uuid.UUID) []Timer {
	want := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	out := make([]Timer, 0, len(ids))
	for _, row := range rows {
		if want[row.TimerID] {
			out = append(out, row)
		}
	}
	return out
}

// itoa renders an attempt without pulling strconv into an identity helper.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
