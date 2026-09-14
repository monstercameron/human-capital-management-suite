package lease

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// Executor is the database capability this package needs: a transaction the
// caller opened and has already scoped with internal/data/tenancy.WithTenant.
// It is exactly [runtimestate.Executor], restated so a caller of this package
// does not have to name the storage package to call it.
type Executor = runtimestate.Executor

// Fence is what a holder presents on every write it makes while holding a
// lease. It is a value: copy it, carry it into a request, hand it across a
// package boundary. Its safety comes from Token, which is monotonic across the
// resource's whole lease history -- a returning holder that lost the resource
// still carries the token it was given, and comparison is what refuses it.
type Fence struct {
	TenantID uuid.UUID
	Resource Resource
	LeaseID  uuid.UUID
	Holder   Identity
	Token    uint64
}

// Validate reports whether the fence is well-formed. It runs before any
// statement, so a malformed fence is refused without touching the database.
func (f Fence) Validate() error {
	if f.TenantID == uuid.Nil {
		return invalid(f.Resource, f.Holder.HolderID(), "tenant id must not be the nil UUID")
	}
	if err := f.Resource.validate(); err != nil {
		return err
	}
	if f.LeaseID == uuid.Nil {
		return invalid(f.Resource, f.Holder.HolderID(), "fence names no lease id")
	}
	if err := f.Holder.validate(f.Resource); err != nil {
		return err
	}
	if f.Token == 0 {
		return invalid(f.Resource, f.Holder.HolderID(), "fence token must be at least 1; zero is never a minted token")
	}
	return nil
}

// Grant is a live claim on a resource: the fence to present on every write it
// authorizes, the window it is good for, and the evidence of the transition
// that produced it.
type Grant struct {
	Fence      Fence
	AcquiredAt time.Time
	ExpiresAt  time.Time
	Version    uint64
	Evidence   Evidence
}

// AcquireRequest asks for the lease on one resource.
type AcquireRequest struct {
	TenantID uuid.UUID
	Resource Resource
	Holder   Identity

	// Now is the caller's own clock reading. This package reads no clock: it
	// is what decides whether an existing lease has lapsed and may be taken.
	Now time.Time
	// TTL is how long the claim is good for. Renew extends it; nothing
	// extends it automatically.
	TTL time.Duration
	// LeaseID, when set, is the identity of the new lease row. It is here for
	// a caller that wants a derived, reproducible id; left zero, a fresh one
	// is minted. It is never the fence: the fence is the token.
	LeaseID uuid.UUID
}

func (r AcquireRequest) validate() error {
	if r.TenantID == uuid.Nil {
		return invalid(r.Resource, r.Holder.HolderID(), "tenant id must not be the nil UUID")
	}
	if err := r.Resource.validate(); err != nil {
		return err
	}
	if err := r.Holder.validate(r.Resource); err != nil {
		return err
	}
	if r.Now.IsZero() {
		return invalid(r.Resource, r.Holder.HolderID(),
			"acquiring a lease needs the caller's own clock reading; this package reads no clock")
	}
	if r.TTL <= 0 {
		return invalid(r.Resource, r.Holder.HolderID(), "lease ttl must be positive")
	}
	return nil
}

// Manager acquires, renews, releases, expires and verifies leases over
// migration 00026's workflow_lease through [runtimestate.LeaseStore].
//
// It holds no state and starts nothing: every method takes the caller's
// transaction and the caller's instant. There is no reaper (see the package
// comment) and no heartbeat goroutine.
type Manager struct{ store runtimestate.LeaseStore }

// Acquire takes the lease on a resource and mints the next fence token.
//
// A live claim in someone else's hands is [ErrHeld]. A claim that has lapsed
// by the caller's own Now is taken over: the lapsed row is retired as EXPIRED
// and the new holder's token is one past the highest ever issued for the
// resource, so the previous holder's later write is refusable by comparison
// rather than by hoping it noticed.
//
// Concurrent acquirers of the same resource serialize on a transaction-scoped
// advisory lock, so exactly one of them is a holder and every other one gets a
// typed [ErrHeld] rather than a unique-constraint failure it would have to
// interpret. The advisory lock lives and dies with the caller's transaction.
func (m Manager) Acquire(ctx context.Context, ex Executor, req AcquireRequest) (ret0 Grant, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.lease.acquire", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := req.validate(); err != nil {
		return Grant{}, err
	}
	if err := lockResource(ctx, ex, req.TenantID, req.Resource); err != nil {
		return Grant{}, err
	}

	prior, priorErr := m.store.Current(ctx, ex, req.TenantID, req.Resource.Kind, req.Resource.ID)
	switch {
	case priorErr == nil:
	case errors.Is(priorErr, runtimestate.ErrNotFound):
		prior = runtimestate.Lease{}
	default:
		return Grant{}, wrapStorage(req.Resource, req.Holder.HolderID(), priorErr, "read the resource's live lease")
	}

	leaseID := req.LeaseID
	if leaseID == uuid.Nil {
		leaseID = uuid.New()
	}
	held, err := m.store.Acquire(ctx, ex, runtimestate.Lease{
		TenantID: req.TenantID, LeaseID: leaseID,
		ResourceKind: req.Resource.Kind, ResourceID: req.Resource.ID,
		HolderID:    req.Holder.HolderID(),
		AcquiredAt:  req.Now,
		ExpiresAt:   req.Now.Add(req.TTL),
		HeartbeatAt: req.Now,
	}, req.Now)
	if err != nil {
		return Grant{}, m.acquireError(req, prior, err)
	}

	// A live lease was found and the store took it anyway, which means the
	// caller's own Now had passed its window: that is a takeover, and it is
	// recorded as one because "somebody else's claim was expired to make room
	// for this" is a materially different operational fact from a first claim.
	kind := TransitionAcquired
	reason := ""
	if prior.FenceToken != 0 {
		kind = TransitionTakenOver
		reason = "previous holder " + prior.HolderID + " lapsed at " + prior.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	// The token is one past the highest ever issued for the resource, so the
	// token it superseded is exactly one below it -- including after a clean
	// release, where no live lease was there to read.
	priorToken := uint64(0)
	if held.FenceToken > 1 {
		priorToken = held.FenceToken - 1
	}
	fence := Fence{
		TenantID: req.TenantID, Resource: req.Resource, LeaseID: held.LeaseID,
		Holder: req.Holder, Token: held.FenceToken,
	}
	return Grant{
		Fence: fence, AcquiredAt: held.AcquiredAt, ExpiresAt: held.ExpiresAt, Version: held.Version,
		Evidence: newEvidence(kind, req.TenantID, held.LeaseID, req.Resource, fence.Holder.HolderID(),
			held.FenceToken, priorToken, held.AcquiredAt, reason),
	}, nil
}

// acquireError classifies a refused acquire. A live holder is the store's own
// ErrLeaseHeld; a losing racer that got past the advisory lock anyway (a
// caller that acquired outside this package, for instance) collides with one
// of migration 00026's two lease uniqueness rules, and that collision means
// the same thing.
func (m Manager) acquireError(req AcquireRequest, prior runtimestate.Lease, err error) error {
	holder := prior.HolderID
	switch {
	case errors.Is(err, runtimestate.ErrLeaseHeld):
		e := refuse(CodeLeaseHeld, ErrHeld, req.Resource, req.Holder.HolderID(),
			"resource is held by %s until %s", holder, prior.ExpiresAt.UTC().Format(time.RFC3339Nano))
		e.err = err
		return e
	case strings.Contains(err.Error(), "workflow_lease_one_holder"),
		strings.Contains(err.Error(), "workflow_lease_fence_unique"):
		e := refuse(CodeLeaseHeld, ErrHeld, req.Resource, req.Holder.HolderID(),
			"a concurrent acquirer took the resource first")
		e.err = err
		return e
	case errors.Is(err, runtimestate.ErrInvalid):
		e := invalid(req.Resource, req.Holder.HolderID(), "the durable store refused the lease row")
		e.err = err
		return e
	default:
		return wrapStorage(req.Resource, req.Holder.HolderID(), err, "acquire the lease")
	}
}

// Renew extends the holder's own claim. It verifies the presented fence
// first, so a holder whose lease was taken over while it was away is refused
// rather than silently given a fresh window on a resource it no longer holds.
func (m Manager) Renew(ctx context.Context, ex Executor, fence Fence, now time.Time, ttl time.Duration) (ret0 Grant, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.lease.renew", fence)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := fence.Validate(); err != nil {
		return Grant{}, err
	}
	if now.IsZero() {
		return Grant{}, invalid(fence.Resource, fence.Holder.HolderID(),
			"renewing a lease needs the caller's own clock reading")
	}
	if ttl <= 0 {
		return Grant{}, invalid(fence.Resource, fence.Holder.HolderID(), "lease ttl must be positive")
	}
	current, err := m.verify(ctx, ex, fence, now)
	if err != nil {
		return Grant{}, err
	}
	expires := now.Add(ttl)
	if err := m.store.Heartbeat(ctx, ex, fence.TenantID, fence.LeaseID, fence.Token, now, expires); err != nil {
		return Grant{}, m.settleError(fence, err, "renew the lease")
	}
	return Grant{
		Fence: fence, AcquiredAt: current.AcquiredAt, ExpiresAt: expires.UTC(), Version: current.Version + 1,
		Evidence: newEvidence(TransitionRenewed, fence.TenantID, fence.LeaseID, fence.Resource,
			fence.Holder.HolderID(), fence.Token, fence.Token, now, ""),
	}, nil
}

// Release gives the resource up. The fence is verified first: a superseded
// holder must not be able to release the lease its successor now holds.
func (m Manager) Release(ctx context.Context, ex Executor, fence Fence, now time.Time) (ret0 Evidence, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.lease.release", fence)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := fence.Validate(); err != nil {
		return Evidence{}, err
	}
	if now.IsZero() {
		return Evidence{}, invalid(fence.Resource, fence.Holder.HolderID(),
			"releasing a lease needs the caller's own clock reading")
	}
	// A holder may release a lease whose window has already passed -- that is
	// tidying up after itself, not writing under a dead fence -- so the
	// expiry check the fenced-write path applies is deliberately skipped here
	// by verifying against the zero instant.
	if _, err := m.verify(ctx, ex, fence, time.Time{}); err != nil {
		return Evidence{}, err
	}
	if err := m.store.Release(ctx, ex, fence.TenantID, fence.LeaseID, fence.Token, now); err != nil {
		return Evidence{}, m.settleError(fence, err, "release the lease")
	}
	return newEvidence(TransitionReleased, fence.TenantID, fence.LeaseID, fence.Resource,
		fence.Holder.HolderID(), fence.Token, fence.Token, now, ""), nil
}

// Expire retires a lease the caller has observed to have lapsed.
//
// This is the whole of WF-RUN-002's expiry story, and it is deliberately
// caller-driven: the WF-RUN-000 gate blocks a background reaper, so expiry is
// something a caller notices against its own clock reading and acts on in its
// own transaction. A lease whose window has not passed by now is [ErrLeaseLive]
// -- this package refuses to declare a live holder dead.
func (m Manager) Expire(ctx context.Context, ex Executor, tenantID uuid.UUID, res Resource, now time.Time) (ret0 Evidence, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.lease.expire", observe.Attrs{observe.KeyTenant: tenantID.String()}, res)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if tenantID == uuid.Nil {
		return Evidence{}, invalid(res, "", "tenant id must not be the nil UUID")
	}
	if err := res.validate(); err != nil {
		return Evidence{}, err
	}
	if now.IsZero() {
		return Evidence{}, invalid(res, "", "expiring a lease needs the caller's own clock reading")
	}
	current, err := m.store.CurrentForUpdate(ctx, ex, tenantID, res.Kind, res.ID)
	if err != nil {
		if errors.Is(err, runtimestate.ErrNotFound) {
			return Evidence{}, refuse(CodeLeaseLost, ErrLeaseLost, res, "", "no live lease to expire")
		}
		return Evidence{}, wrapStorage(res, "", err, "read the resource's live lease")
	}
	if current.ExpiresAt.After(now) {
		return Evidence{}, refuse(CodeLeaseLive, ErrLeaseLive, res, current.HolderID,
			"lease is live until %s and now is %s", current.ExpiresAt.UTC().Format(time.RFC3339Nano),
			now.UTC().Format(time.RFC3339Nano))
	}
	if err := m.store.Expire(ctx, ex, tenantID, current.LeaseID, current.FenceToken, now); err != nil {
		return Evidence{}, wrapStorage(res, current.HolderID, err, "expire the lapsed lease")
	}
	return newEvidence(TransitionExpired, tenantID, current.LeaseID, res, current.HolderID,
		current.FenceToken, current.FenceToken, now,
		"observed lapsed: expired at "+current.ExpiresAt.UTC().Format(time.RFC3339Nano)), nil
}

// Observation is what one caller-driven look at a resource's lease found.
// Expired is computed against the instant the caller supplied, never against
// an ambient clock.
type Observation struct {
	Held      bool
	Expired   bool
	HolderID  string
	LeaseID   uuid.UUID
	Token     uint64
	ExpiresAt time.Time
}

// Observe reports the resource's live lease as of the caller's own instant.
// It writes nothing: it is how a caller learns that a lease has lapsed and
// that [Manager.Expire] or [Manager.Acquire] is now the right call.
func (m Manager) Observe(ctx context.Context, ex Executor, tenantID uuid.UUID, res Resource, now time.Time) (Observation, error) {
	if tenantID == uuid.Nil {
		return Observation{}, invalid(res, "", "tenant id must not be the nil UUID")
	}
	if err := res.validate(); err != nil {
		return Observation{}, err
	}
	if now.IsZero() {
		return Observation{}, invalid(res, "", "observing a lease needs the caller's own clock reading")
	}
	current, err := m.store.Current(ctx, ex, tenantID, res.Kind, res.ID)
	if err != nil {
		if errors.Is(err, runtimestate.ErrNotFound) {
			return Observation{}, nil
		}
		return Observation{}, wrapStorage(res, "", err, "read the resource's live lease")
	}
	return Observation{
		Held: true, Expired: !current.ExpiresAt.After(now),
		HolderID: current.HolderID, LeaseID: current.LeaseID,
		Token: current.FenceToken, ExpiresAt: current.ExpiresAt,
	}, nil
}

// Held is an accepted fence: the live lease the presented token still names.
type Held struct {
	Fence      Fence
	AcquiredAt time.Time
	ExpiresAt  time.Time
	Version    uint64
	Evidence   Evidence
}

// Verify accepts or refuses one presented fence, and is the check every
// fenced write runs before it writes anything.
//
// It reads the live lease under a row lock, which is what makes the answer
// hold for the rest of the caller's transaction: a concurrent takeover has to
// retire that same row and therefore blocks until the verifying transaction
// finishes. Without the lock the answer would only be true at the instant it
// was read, which is precisely the hole fencing exists to close.
//
// The refusals are:
//
//   - [ErrLeaseLost] ([CodeLeaseLost]) -- nobody holds the resource any more,
//     or the holder's own window has passed by the supplied instant.
//   - [ErrFenceStale] ([CodeFenceStale]) -- the presented token is behind the
//     resource's current one.
//   - [ErrFenceStale] ([CodeFenceForeign]) -- the presented fence names a
//     lease or a holder that is not this resource's current one at all.
//
// A zero now skips the expiry check and compares tokens only. That is for the
// holder tidying up after itself ([Manager.Release]); a write path always
// supplies its instant.
func (m Manager) Verify(ctx context.Context, ex Executor, fence Fence, now time.Time) (ret0 Held, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.lease.verify", fence)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := fence.Validate(); err != nil {
		return Held{}, err
	}
	current, err := m.verify(ctx, ex, fence, now)
	if err != nil {
		return Held{}, err
	}
	at := now
	if at.IsZero() {
		at = current.HeartbeatAt
	}
	return Held{
		Fence: fence, AcquiredAt: current.AcquiredAt, ExpiresAt: current.ExpiresAt, Version: current.Version,
		Evidence: newEvidence(TransitionVerified, fence.TenantID, fence.LeaseID, fence.Resource,
			fence.Holder.HolderID(), fence.Token, fence.Token, at, ""),
	}, nil
}

// verify is Verify without the evidence record, shared by every method that
// has to establish the caller really is the holder before it writes.
func (m Manager) verify(ctx context.Context, ex Executor, fence Fence, now time.Time) (runtimestate.Lease, error) {
	holder := fence.Holder.HolderID()
	current, err := m.store.CurrentForUpdate(ctx, ex, fence.TenantID, fence.Resource.Kind, fence.Resource.ID)
	if err != nil {
		if errors.Is(err, runtimestate.ErrNotFound) {
			return runtimestate.Lease{}, refuse(CodeLeaseLost, ErrLeaseLost, fence.Resource, holder,
				"no live lease on the resource; fence token %d names a claim that has been settled", fence.Token)
		}
		return runtimestate.Lease{}, wrapStorage(fence.Resource, holder, err, "read the resource's live lease")
	}
	if current.FenceToken > fence.Token {
		return runtimestate.Lease{}, refuse(CodeFenceStale, ErrFenceStale, fence.Resource, holder,
			"presented fence token %d is behind the resource's current token %d, held by %s",
			fence.Token, current.FenceToken, current.HolderID)
	}
	if current.FenceToken != fence.Token || current.LeaseID != fence.LeaseID || current.HolderID != holder {
		return runtimestate.Lease{}, refuse(CodeFenceForeign, ErrFenceStale, fence.Resource, holder,
			"presented fence (lease %s, holder %s, token %d) does not name the live lease (lease %s, holder %s, token %d)",
			fence.LeaseID, holder, fence.Token, current.LeaseID, current.HolderID, current.FenceToken)
	}
	if !now.IsZero() && !current.ExpiresAt.After(now) {
		return runtimestate.Lease{}, refuse(CodeLeaseLost, ErrLeaseLost, fence.Resource, holder,
			"lease expired at %s and the caller's instant is %s; a holder past its own window may not write",
			current.ExpiresAt.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano))
	}
	return current, nil
}

// settleError classifies a refused heartbeat, release or expire on a fence
// this package already verified. A stale fence at that point means a
// concurrent settle landed between the verify and the write.
func (m Manager) settleError(fence Fence, err error, what string) error {
	holder := fence.Holder.HolderID()
	if errors.Is(err, runtimestate.ErrFenceStale) {
		e := refuse(CodeFenceStale, ErrFenceStale, fence.Resource, holder,
			"the lease no longer holds fence token %d", fence.Token)
		e.err = err
		return e
	}
	if errors.Is(err, runtimestate.ErrInvalid) {
		e := invalid(fence.Resource, holder, "the durable store refused the settle")
		e.err = err
		return e
	}
	return wrapStorage(fence.Resource, holder, err, "%s", what)
}

// History reconstructs the evidence of every transition one resource's leases
// went through, oldest fence token first, from the durable rows alone.
//
// Each settled lease row yields two records -- the ACQUIRED (or TAKEN_OVER)
// that opened it and the RELEASED, EXPIRED or REVOKED that closed it -- so an
// [Evidence] value a caller kept from a live call can be compared against
// what the database itself says happened.
func (m Manager) History(ctx context.Context, ex Executor, tenantID uuid.UUID, res Resource) ([]Evidence, error) {
	if tenantID == uuid.Nil {
		return nil, invalid(res, "", "tenant id must not be the nil UUID")
	}
	if err := res.validate(); err != nil {
		return nil, err
	}
	rows, err := m.store.History(ctx, ex, tenantID, res.Kind, res.ID)
	if err != nil {
		return nil, wrapStorage(res, "", err, "read the resource's lease history")
	}
	out := make([]Evidence, 0, len(rows))
	var prior uint64
	for _, row := range rows {
		// Every opening is reported as ACQUIRED rather than sometimes as
		// TAKEN_OVER: the rows record that a lease was taken and that an
		// earlier one expired, but not whether this acquire is what expired
		// it (a caller-observed Expire produces the same two rows). Live
		// evidence from Acquire knows and says so; reconstructed evidence
		// only claims what the table actually proves.
		out = append(out, newEvidence(TransitionAcquired, tenantID, row.LeaseID, res, row.HolderID,
			row.FenceToken, prior, row.AcquiredAt, ""))
		if closed, ok := closingTransition(row.State); ok {
			out = append(out, newEvidence(closed, tenantID, row.LeaseID, res, row.HolderID,
				row.FenceToken, row.FenceToken, closingInstant(row), ""))
		}
		prior = row.FenceToken
	}
	return out, nil
}

// closingTransition maps a settled lease row's state to the transition that
// closed it. A HELD row is still open and closes nothing.
func closingTransition(state string) (TransitionKind, bool) {
	switch state {
	case runtimestate.LeaseReleased:
		return TransitionReleased, true
	case runtimestate.LeaseExpired:
		return TransitionExpired, true
	case runtimestate.LeaseRevoked:
		return TransitionRevoked, true
	default:
		return "", false
	}
}

// closingInstant is when a settled row was settled. runtimestate.Lease does
// not surface released_at, so the row's expiry instant stands in for an
// EXPIRED row and its heartbeat for the rest: both are facts the row carries,
// and neither is invented.
func closingInstant(row runtimestate.Lease) time.Time {
	if row.State == runtimestate.LeaseExpired {
		return row.ExpiresAt
	}
	return row.HeartbeatAt
}

// lockResource serializes concurrent acquirers of one resource on a
// transaction-scoped PostgreSQL advisory lock. The lock is released when the
// caller's transaction ends, whichever way it ends, and it is taken only by
// [Manager.Acquire] -- a verifying or renewing holder takes the row lock
// instead, so the two paths never wait on each other in a cycle.
func lockResource(ctx context.Context, ex Executor, tenantID uuid.UUID, res Resource) error {
	key := tenantID.String() + "|" + res.Kind + "|" + res.ID
	if _, err := ex.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
		return wrapStorage(res, "", err, "serialize concurrent acquirers")
	}
	return nil
}

// itoa renders a fence token without pulling strconv into a message helper.
func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
