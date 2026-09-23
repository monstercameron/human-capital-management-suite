package localcommit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/budget"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrReservationRequired refuses a commit that cannot prove a live capacity
// hold for its exact proposal digest. The committer never trusts a
// caller-supplied capacity boolean: the fence below is the only evidence a
// position head and a compensation-pool slice were fenced for this proposal
// before the commit was attempted.
var ErrReservationRequired = errors.New("promotion local commit: no live position and budget reservation for this proposal")

// ReservationClaim names the proposal a commit must hold fenced capacity for.
// Tenant scopes the lookup; the digest pins the exact approved material, so a
// hold cut for another revision of the same proposal never satisfies it.
type ReservationClaim struct {
	Tenant             values.TenantId
	ProposalRevisionID string
	ProposalDigest     string
}

// ReservationFence is the commit-time port over the position and budget
// holds. AssertHeld reports whether a live, unexpired hold exists for the
// claim's exact proposal digest in both capacities. Implementations own their
// clocks and stores; the committer only consumes the verdict.
type ReservationFence interface {
	AssertHeld(ctx context.Context, claim ReservationClaim) error
}

// assertReservations fails closed: a committer with no fence, or a fence
// that does not vouch for this exact digest, refuses the commit before any
// participant is staged.
func (c *Committer) assertReservations(ctx context.Context, prepared PreparedPlan) error {
	if c.Fence == nil {
		return fmt.Errorf("%w: committer has no reservation fence", ErrReservationRequired)
	}
	claim := ReservationClaim{
		Tenant:             prepared.Plan.Tenant,
		ProposalRevisionID: prepared.Plan.ProposalRevisionID,
		ProposalDigest:     prepared.Plan.ProposalDigest,
	}
	if err := c.Fence.AssertHeld(ctx, claim); err != nil {
		return fmt.Errorf("%w: proposal %q: %w", ErrReservationRequired, prepared.Plan.ProposalDigest, err)
	}
	return nil
}

var (
	// ErrFenceUnconfigured fails closed: a fence composed without its
	// stores acquires and vouches for nothing.
	ErrFenceUnconfigured = errors.New("promotion local commit: reservation fence is not composed")
	// ErrReservationUnknown answers a commit-time claim for a proposal
	// digest this fence never acquired.
	ErrReservationUnknown = errors.New("promotion local commit: no reservation for this proposal digest")
	// ErrReservationNotHeld answers a claim whose recorded hold is no
	// longer HELD or is past its expiry: capacity was fenced and then
	// lost, so the commit is refused rather than overbooking.
	ErrReservationNotHeld = errors.New("promotion local commit: reservation is not held")
	// ErrReservationBinding answers a claim whose tenant or revision does
	// not match the recorded hold: a hold cut for another proposal never
	// satisfies this one.
	ErrReservationBinding = errors.New("promotion local commit: reservation is bound to another proposal")
)

// promotionHoldIDs are the store identities one Acquire recorded, keyed by
// the proposal digest the commit claim presents.
type promotionHoldIDs struct {
	tenant             values.TenantId
	proposalRevisionID string
	positionID         string
	positionFence      uint64
	budgetID           string
	budgetFence        uint64
}

// PromotionReservationFence fronts the pure position and budget holds behind
// one digest-keyed fence. Admission acquires the holds before the commit is
// attempted; the committer asserts them at commit time through
// [ReservationFence], refusing without a live, unexpired hold for the exact
// proposal digest. The fence owns its stores (composition-root rule: state
// lives on the value that owns it) and its digest index. Use
// NewPromotionReservationFence; the zero value refuses everything.
type PromotionReservationFence struct {
	positions *position.PositionReservationStore
	budgets   *budget.ReservationStore
	// Now is the clock seam. Nil reads the process clock.
	Now func() time.Time

	mu   sync.Mutex
	held map[string]promotionHoldIDs
}

// NewPromotionReservationFence composes a fence over fresh in-memory stores.
func NewPromotionReservationFence() *PromotionReservationFence {
	return &PromotionReservationFence{
		positions: position.NewPositionReservationStore(),
		budgets:   budget.NewReservationStore(),
		held:      make(map[string]promotionHoldIDs),
	}
}

func (f *PromotionReservationFence) now() time.Time {
	if f == nil || f.Now == nil {
		return time.Now().UTC()
	}
	return f.Now().UTC()
}

func (f *PromotionReservationFence) configured() bool {
	return f != nil && f.positions != nil && f.budgets != nil
}

// PromotionReservationHold is the replay-safe evidence one Acquire recorded:
// digests, quantities and lifecycle facts only.
type PromotionReservationHold struct {
	Position position.PositionReservationEvidence
	Budget   budget.ReservationEvidence
}

// Acquire holds the position head and the budget slice for one proposal
// before its commit is attempted. Both holds key to the proposal digest, so
// a second proposal for the same capacity loses with the stores' typed
// conflict instead of reaching a duplicate commit. A budget failure releases
// the position hold it just took, so a half-acquired proposal never pins a
// head its raise cannot pay for.
func (f *PromotionReservationFence) Acquire(
	ctx context.Context, reader position.PositionFacts,
	pos position.PositionReservationRequest,
	bud budget.CompensationReservationRequest, auth budget.CompensationBudgetAuthority,
	now time.Time,
) (PromotionReservationHold, error) {
	if !f.configured() {
		return PromotionReservationHold{}, ErrFenceUnconfigured
	}
	f.Sweep()
	now = now.UTC()
	// A digest already indexed is a replay, not a first touch: a failure
	// below must propagate without unwinding the live holds the earlier
	// call recorded. Only a first touch compensates the sibling it may
	// just have created.
	f.mu.Lock()
	_, replay := f.held[pos.ProposalDigest]
	f.mu.Unlock()
	posHeld, err := f.positions.Reserve(ctx, reader, pos, now)
	if err != nil {
		return PromotionReservationHold{}, err
	}
	budHeld, err := f.budgets.Reserve(bud, auth, now)
	if err != nil {
		if !replay {
			if releaseErr := releaseQuietPosition(f.positions, posHeld.ID, posHeld.Fence, now); releaseErr != nil {
				return PromotionReservationHold{}, errors.Join(err, releaseErr)
			}
		}
		return PromotionReservationHold{}, err
	}
	f.mu.Lock()
	if f.held == nil {
		f.held = make(map[string]promotionHoldIDs)
	}
	f.held[pos.ProposalDigest] = promotionHoldIDs{
		tenant: pos.Tenant, proposalRevisionID: pos.ProposalRevisionID,
		positionID: posHeld.ID, positionFence: posHeld.Fence,
		budgetID: budHeld.ID, budgetFence: budHeld.Fence,
	}
	f.mu.Unlock()
	posEvidence, ok := f.positions.Evidence(posHeld.ID)
	if !ok {
		return PromotionReservationHold{}, ErrReservationUnknown
	}
	budEvidence, ok := f.budgets.Evidence(budHeld.ID)
	if !ok {
		return PromotionReservationHold{}, ErrReservationUnknown
	}
	return PromotionReservationHold{Position: posEvidence, Budget: budEvidence}, nil
}

// AcquirePosition holds only the position head, for paths whose budget half
// is held elsewhere (the served promotion holds its raise durable at
// candidate materialization and the terminal writer verifies it HELD, bound
// and unexpired). It records no budget identity, so AssertHeld refuses a
// position-only digest until the budget half is recorded too; use Acquire
// where both pure holds apply.
func (f *PromotionReservationFence) AcquirePosition(
	ctx context.Context, reader position.PositionFacts, pos position.PositionReservationRequest, now time.Time,
) (position.PositionReservationEvidence, error) {
	if !f.configured() {
		return position.PositionReservationEvidence{}, ErrFenceUnconfigured
	}
	f.Sweep()
	held, err := f.positions.Reserve(ctx, reader, pos, now.UTC())
	if err != nil {
		return position.PositionReservationEvidence{}, err
	}
	f.mu.Lock()
	if f.held == nil {
		f.held = make(map[string]promotionHoldIDs)
	}
	f.held[pos.ProposalDigest] = promotionHoldIDs{
		tenant: pos.Tenant, proposalRevisionID: pos.ProposalRevisionID,
		positionID: held.ID, positionFence: held.Fence,
	}
	f.mu.Unlock()
	evidence, ok := f.positions.Evidence(held.ID)
	if !ok {
		return position.PositionReservationEvidence{}, ErrReservationUnknown
	}
	return evidence, nil
}

// releaseQuietPosition releases a hold this call may just have created. A
// hold already gone (released, expired or never owned) already meets the
// postcondition and reports nil; only a genuine release failure propagates
// so the caller can join it to the error that triggered the compensation.
func releaseQuietPosition(store *position.PositionReservationStore, id string, fence uint64, now time.Time) error {
	if _, err := store.Release(id, fence, now); err != nil {
		if errors.Is(err, position.ErrReservationNotFound) ||
			errors.Is(err, position.ErrReservationTransition) ||
			errors.Is(err, position.ErrReservationFence) {
			return nil
		}
		return err
	}
	return nil
}

// AssertHeld implements [ReservationFence]: a live, unexpired HELD position
// hold and Held budget hold for the claim's exact digest, in the claim's
// tenant for the claim's revision. Anything else -- unknown digest, wrong
// tenant or revision, released or expired hold -- refuses with a typed
// error the committer wraps in [ErrReservationRequired].
func (f *PromotionReservationFence) AssertHeld(_ context.Context, claim ReservationClaim) error {
	if !f.configured() {
		return ErrFenceUnconfigured
	}
	if strings.TrimSpace(claim.ProposalDigest) == "" {
		return ErrReservationUnknown
	}
	f.mu.Lock()
	ids, ok := f.held[claim.ProposalDigest]
	f.mu.Unlock()
	if !ok {
		return ErrReservationUnknown
	}
	if ids.tenant != claim.Tenant || ids.proposalRevisionID != claim.ProposalRevisionID {
		return fmt.Errorf("%w: digest %q", ErrReservationBinding, claim.ProposalDigest)
	}
	now := f.now()
	if ids.positionID == "" {
		return ErrReservationUnknown
	}
	pos, ok := f.positions.Get(ids.positionID)
	if !ok || pos.State != position.PositionReservationHeld || !pos.Request.ExpiresAt.After(now) {
		return fmt.Errorf("%w: position hold for digest %q", ErrReservationNotHeld, claim.ProposalDigest)
	}
	if ids.budgetID == "" {
		return fmt.Errorf("%w: budget hold for digest %q", ErrReservationNotHeld, claim.ProposalDigest)
	}
	bud, ok := f.budgets.Get(ids.budgetID)
	if !ok || bud.State != budget.Held || !bud.Request.ExpiresAt.After(now) {
		return fmt.Errorf("%w: budget hold for digest %q", ErrReservationNotHeld, claim.ProposalDigest)
	}
	return nil
}

// Release frees both holds for a digest after its terminal outcome lands.
// An unknown digest is refused: releasing what was never held would hide a
// caller that lost track of its own capacity.
func (f *PromotionReservationFence) Release(digest string, now time.Time) error {
	if !f.configured() {
		return ErrFenceUnconfigured
	}
	f.mu.Lock()
	ids, ok := f.held[digest]
	f.mu.Unlock()
	if !ok {
		return ErrReservationUnknown
	}
	now = now.UTC()
	var errs []error
	if ids.positionID != "" {
		if _, err := f.positions.Release(ids.positionID, ids.positionFence, now); err != nil {
			errs = append(errs, err)
		}
	}
	if ids.budgetID != "" {
		if _, err := f.budgets.Release(ids.budgetID, ids.budgetFence, now); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	f.mu.Lock()
	delete(f.held, digest)
	f.mu.Unlock()
	return nil
}

// Sweep drops index entries whose holds already left HELD/Held, so the
// digest index cannot grow past the stores' own lifecycle. It runs at the
// head of every acquisition; callers with their own cadence may run it
// directly.
func (f *PromotionReservationFence) Sweep() {
	if !f.configured() {
		return
	}
	now := f.now()
	f.positions.Expire(now)
	f.budgets.Expire(now)
	f.mu.Lock()
	defer f.mu.Unlock()
	for digest, ids := range f.held {
		live := true
		if ids.positionID == "" {
			live = false
		} else if pos, ok := f.positions.Get(ids.positionID); !ok || pos.State != position.PositionReservationHeld {
			live = false
		}
		if live {
			if ids.budgetID == "" {
				live = false
			} else if bud, ok := f.budgets.Get(ids.budgetID); !ok || bud.State != budget.Held {
				live = false
			}
		}
		if !live {
			delete(f.held, digest)
		}
	}
}
