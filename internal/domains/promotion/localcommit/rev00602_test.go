package localcommit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/budget"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// REV-006-02 fixtures: one seat with exactly one open head, its memory
// reader, and the fence inputs for two contending proposals.

var (
	rev00602Tenant = values.TenantId("11111111-1111-4111-8111-111111111111")
	rev00602Now    = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
)

func rev00602Date(t *testing.T, s string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func rev00602KnownAt(t *testing.T, s string) values.KnownAt {
	t.Helper()
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	k, err := values.NewKnownAt(values.NewInstant(at))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func rev00602Seat(t *testing.T, id string) position.PositionRevision {
	t.Helper()
	interval, err := values.NewLocalDateInterval(
		rev00602Date(t, "2026-01-01"), rev00602Date(t, "2026-12-31"),
		values.CalendarRef{Ref: "position.test.calendar", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := values.NewSequenceRevision("position.revision.seat-1", 3)
	if err != nil {
		t.Fatal(err)
	}
	fte, err := values.NewDecimal("1.0000", 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	ref := values.EntityRef{Tenant: rev00602Tenant, Kind: position.KindPosition, Id: id}
	if err := ref.Validate(); err != nil {
		t.Fatal(err)
	}
	recorded, err := values.NewRecordedAt(values.NewInstant(time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	return position.PositionRevision{
		Position: ref, Revision: revision, Effective: interval, Lifecycle: position.LifecycleOpen,
		JobCode: "ENG-MGR", OrgUnit: "eng-platform", LegalEntity: "HarborCare US Inc.",
		Capacity:   position.CapacityPolicy{CapacityFTE: fte, CapacityHeads: 1},
		Authority:  evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "hcmnext.position", PolicyRef: "position.source_authority/2026.1"},
		Provenance: evidence.Provenance{Source: "hcmnext.position", EvidenceRef: "evd_seat_1_r3", RecordedAt: recorded},
	}
}

type rev00602Reader struct {
	seats map[string]position.PositionRevision
	err   error
}

func rev00602ReaderFor(seats ...position.PositionRevision) rev00602Reader {
	r := rev00602Reader{seats: map[string]position.PositionRevision{}}
	for _, seat := range seats {
		r.seats[seat.Position.Id] = seat
	}
	return r
}

func (r rev00602Reader) PositionRevisionAt(_ context.Context, q position.PositionQuery) (position.PositionRevision, bool, error) {
	if r.err != nil {
		return position.PositionRevision{}, false, r.err
	}
	if err := q.Validate(); err != nil {
		return position.PositionRevision{}, false, err
	}
	rev, ok := r.seats[q.Position.Id]
	if !ok {
		return position.PositionRevision{}, false, nil
	}
	return rev, true, nil
}

func rev00602Money(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func rev00602PositionRequest(t *testing.T, seat position.PositionRevision, revision, digest string, expires time.Time) position.PositionReservationRequest {
	t.Helper()
	asOf := position.AsOf{EffectiveOn: rev00602Date(t, "2026-06-01"), KnownAt: rev00602KnownAt(t, "2026-05-15T00:00:00Z")}
	return position.PositionReservationRequest{
		Tenant: seat.Position.Tenant, Position: seat.Position, AsOf: asOf,
		ProposalRevisionID: revision, ProposalDigest: digest, EffectiveDate: asOf.EffectiveOn,
		FTE: seat.Capacity.CapacityFTE, Heads: 1,
		DesiredJobCode: seat.JobCode, DesiredOrgUnit: seat.OrgUnit, DesiredLegalEntity: seat.LegalEntity,
		MinRevision:     seat.Revision,
		AuthorityDigest: "sha256:authority", IdempotencyKey: digest, ExpiresAt: expires,
	}
}

func rev00602BudgetRequest(t *testing.T, digest string, expires time.Time) budget.CompensationReservationRequest {
	t.Helper()
	return budget.CompensationReservationRequest{
		TenantID: string(rev00602Tenant), BudgetID: "pool-1", ProposalDigest: digest,
		Amount: rev00602Money(t, "5000.00"), Currency: "USD",
		AuthorityDigest: "sha256:authority", IdempotencyKey: digest, ExpiresAt: expires,
	}
}

func rev00602Authority(t *testing.T) budget.CompensationBudgetAuthority {
	t.Helper()
	return budget.CompensationBudgetAuthority{
		BudgetID: "pool-1", Currency: "USD",
		Available: rev00602Money(t, "100000.00"), AuthorityDigest: "sha256:authority",
	}
}

func rev00602Fence() *PromotionReservationFence {
	fence := NewPromotionReservationFence()
	fence.Now = func() time.Time { return rev00602Now }
	return fence
}

func rev00602Expiry() time.Time { return time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC) }

// TestTodo_REV_006_02 proves the GREEN clause: the admitted proposal holds a
// position Reserve and a budget reservation keyed to its digest before the
// commit is attempted, the commit refuses without a live hold for that exact
// digest, an identical re-admission replays to the same holds, and a
// half-acquired proposal never pins a head its raise cannot pay for.
func TestTodo_REV_006_02(t *testing.T) {
	ctx := context.Background()
	seat := rev00602Seat(t, "44444444-4444-4444-8444-444444444444")
	reader := rev00602ReaderFor(seat)

	// The compensation half first, on its own fence: an underfunded
	// proposal must not pin the head, and the freed head admits the next
	// proposal.
	compFence := rev00602Fence()
	broke := rev00602Authority(t)
	broke.Available = rev00602Money(t, "1.00")
	if _, err := compFence.Acquire(ctx, reader,
		rev00602PositionRequest(t, seat, "proposal-C", "sha256:proposal-c", rev00602Expiry()),
		rev00602BudgetRequest(t, "sha256:proposal-c", rev00602Expiry()), broke, rev00602Now); !errors.Is(err, budget.ErrInsufficientBudget) {
		t.Fatalf("underfunded admission = %v, want %v", err, budget.ErrInsufficientBudget)
	}
	// The compensated head is free again: a new proposal for the same seat
	// acquires it. (Re-acquiring C itself would replay the released hold,
	// the stores' own idempotency; D proves the slot is unpinned.)
	if _, err := compFence.Acquire(ctx, reader,
		rev00602PositionRequest(t, seat, "proposal-D", "sha256:proposal-d", rev00602Expiry()),
		rev00602BudgetRequest(t, "sha256:proposal-d", rev00602Expiry()), rev00602Authority(t), rev00602Now); err != nil {
		t.Fatalf("compensated head was not freed: %v", err)
	}

	fence := rev00602Fence()

	hold, err := fence.Acquire(ctx, reader,
		rev00602PositionRequest(t, seat, "proposal-A", "sha256:proposal-a", rev00602Expiry()),
		rev00602BudgetRequest(t, "sha256:proposal-a", rev00602Expiry()), rev00602Authority(t), rev00602Now)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if hold.Position.State != position.PositionReservationHeld || hold.Position.ProposalDigest != "sha256:proposal-a" {
		t.Fatalf("position hold = %+v", hold.Position)
	}
	if hold.Budget.State != budget.Held || hold.Budget.ProposalDigest != "sha256:proposal-a" {
		t.Fatalf("budget hold = %+v", hold.Budget)
	}

	committer := &Committer{Store: NewStore(), Fence: fence}
	receipt, err := committer.Commit(ctx, testPreparedPlanFor(t, "proposal-A", "sha256:proposal-a", "plan-A", "idem-A"))
	if err != nil {
		t.Fatalf("Commit with live holds: %v", err)
	}
	if receipt.PlanID != "plan-A" || receipt.Replayed {
		t.Fatalf("receipt = %+v", receipt)
	}

	unfenced := &Committer{Store: NewStore(), Fence: fence}
	if _, err := unfenced.Commit(ctx, testPreparedPlanFor(t, "proposal-B", "sha256:proposal-b", "plan-B", "idem-B")); !errors.Is(err, ErrReservationRequired) {
		t.Fatalf("commit without holds = %v, want %v", err, ErrReservationRequired)
	}

	replay, err := fence.Acquire(ctx, reader,
		rev00602PositionRequest(t, seat, "proposal-A", "sha256:proposal-a", rev00602Expiry()),
		rev00602BudgetRequest(t, "sha256:proposal-a", rev00602Expiry()), rev00602Authority(t), rev00602Now)
	if err != nil {
		t.Fatalf("identical re-admission: %v", err)
	}
	if replay.Position.ReservationID != hold.Position.ReservationID || replay.Budget.ReservationID != hold.Budget.ReservationID {
		t.Fatalf("replay minted new holds: %+v", replay)
	}
}

// TestTodo_REV_006_02_Race starts two promotions for the last open headcount
// on one seat: exactly one commits and the other receives the typed
// reservation conflict rather than a duplicate successful commit.
func TestTodo_REV_006_02_Race(t *testing.T) {
	ctx := context.Background()
	seat := rev00602Seat(t, "44444444-4444-4444-8444-444444444444")
	reader := rev00602ReaderFor(seat)
	fence := rev00602Fence()
	committer := &Committer{Store: NewStore(), Fence: fence}

	type contender struct {
		plan   PreparedPlan
		posReq position.PositionReservationRequest
		budReq budget.CompensationReservationRequest
		auth   budget.CompensationBudgetAuthority
	}
	sides := []contender{
		{
			plan:   testPreparedPlanFor(t, "proposal-A", "sha256:proposal-a", "plan-A", "idem-A"),
			posReq: rev00602PositionRequest(t, seat, "proposal-A", "sha256:proposal-a", rev00602Expiry()),
			budReq: rev00602BudgetRequest(t, "sha256:proposal-a", rev00602Expiry()),
			auth:   rev00602Authority(t),
		},
		{
			plan:   testPreparedPlanFor(t, "proposal-B", "sha256:proposal-b", "plan-B", "idem-B"),
			posReq: rev00602PositionRequest(t, seat, "proposal-B", "sha256:proposal-b", rev00602Expiry()),
			budReq: rev00602BudgetRequest(t, "sha256:proposal-b", rev00602Expiry()),
			auth:   rev00602Authority(t),
		},
	}
	var group sync.WaitGroup
	receipts := make([]Receipt, len(sides))
	errs := make([]error, len(sides))
	for i := range sides {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			if _, err := fence.Acquire(ctx, reader, sides[i].posReq, sides[i].budReq, sides[i].auth, rev00602Now); err != nil {
				errs[i] = err
				return
			}
			receipts[i], errs[i] = committer.Commit(ctx, sides[i].plan)
		}(i)
	}
	group.Wait()

	var committed, conflicted int
	loser := -1
	for i := range sides {
		switch {
		case errs[i] == nil:
			committed++
			if receipts[i].PlanID == "" || receipts[i].Replayed {
				t.Fatalf("side %d receipt = %+v", i, receipts[i])
			}
		case errors.Is(errs[i], position.ErrCompetingReservation):
			conflicted++
			loser = i
		default:
			t.Fatalf("side %d error = %v, want a commit or %v", i, errs[i], position.ErrCompetingReservation)
		}
	}
	if committed != 1 || conflicted != 1 || loser < 0 {
		t.Fatalf("committed=%d conflicted=%d, want exactly one commit and one typed conflict", committed, conflicted)
	}

	// The loser holds nothing: driving its plan at the committer directly
	// is refused instead of reaching a duplicate commit.
	if _, err := committer.Commit(ctx, sides[loser].plan); !errors.Is(err, ErrReservationRequired) {
		t.Fatalf("loser commit = %v, want %v", err, ErrReservationRequired)
	}
}

// TestTodo_REV_006_02_Security refuses forged, cross-boundary, expired and
// released holds: only a live hold for the claim's exact digest, tenant and
// revision satisfies the fence, and a replay with changed material conflicts
// without unwinding the live holds.
func TestTodo_REV_006_02_Security(t *testing.T) {
	ctx := context.Background()
	seat := rev00602Seat(t, "44444444-4444-4444-8444-444444444444")
	shortSeat := rev00602Seat(t, "55555555-5555-4555-8555-555555555555")
	reader := rev00602ReaderFor(seat, shortSeat)
	fence := rev00602Fence()
	committer := &Committer{Store: NewStore(), Fence: fence}

	if _, err := fence.Acquire(ctx, reader,
		rev00602PositionRequest(t, seat, "proposal-A", "sha256:proposal-a", rev00602Expiry()),
		rev00602BudgetRequest(t, "sha256:proposal-a", rev00602Expiry()), rev00602Authority(t), rev00602Now); err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// A digest the fence never acquired.
	if _, err := committer.Commit(ctx, testPreparedPlanFor(t, "proposal-X", "sha256:forged", "plan-X", "idem-X")); !errors.Is(err, ErrReservationUnknown) {
		t.Fatalf("forged digest commit = %v, want %v", err, ErrReservationUnknown)
	}

	// The right digest claimed for another revision.
	otherRevision := testPreparedPlanFor(t, "proposal-A2", "sha256:proposal-a", "plan-A2", "idem-A2")
	if _, err := committer.Commit(ctx, otherRevision); !errors.Is(err, ErrReservationBinding) {
		t.Fatalf("cross-revision commit = %v, want %v", err, ErrReservationBinding)
	}

	// The right digest claimed for another tenant.
	foreign := ReservationClaim{Tenant: values.TenantId("22222222-2222-4222-8222-222222222222"), ProposalRevisionID: "proposal-A", ProposalDigest: "sha256:proposal-a"}
	if err := fence.AssertHeld(ctx, foreign); !errors.Is(err, ErrReservationBinding) {
		t.Fatalf("cross-tenant claim = %v, want %v", err, ErrReservationBinding)
	}

	// A replay with changed material conflicts and leaves the live holds.
	changed := rev00602BudgetRequest(t, "sha256:proposal-a", rev00602Expiry())
	changed.Amount = rev00602Money(t, "6000.00")
	if _, err := fence.Acquire(ctx, reader,
		rev00602PositionRequest(t, seat, "proposal-A", "sha256:proposal-a", rev00602Expiry()),
		changed, rev00602Authority(t), rev00602Now); !errors.Is(err, budget.ErrReservationConflict) {
		t.Fatalf("changed-material replay = %v, want %v", err, budget.ErrReservationConflict)
	}
	if _, err := committer.Commit(ctx, testPreparedPlanFor(t, "proposal-A", "sha256:proposal-a", "plan-A", "idem-A")); err != nil {
		t.Fatalf("live holds were unwound by the conflict: %v", err)
	}

	// An expired hold refuses, on its own seat so the live hold above is
	// undisturbed.
	short := rev00602Now.Add(time.Hour)
	if _, err := fence.Acquire(ctx, reader,
		rev00602PositionRequest(t, shortSeat, "proposal-E", "sha256:proposal-e", short),
		rev00602BudgetRequest(t, "sha256:proposal-e", short), rev00602Authority(t), rev00602Now); err != nil {
		t.Fatalf("short acquire: %v", err)
	}
	fence.Now = func() time.Time { return short.Add(time.Minute) }
	if _, err := committer.Commit(ctx, testPreparedPlanFor(t, "proposal-E", "sha256:proposal-e", "plan-E", "idem-E")); !errors.Is(err, ErrReservationNotHeld) {
		t.Fatalf("expired commit = %v, want %v", err, ErrReservationNotHeld)
	}
	fence.Now = func() time.Time { return rev00602Now }

	// A released hold refuses.
	if err := fence.Release("sha256:proposal-a", rev00602Now); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := committer.Commit(ctx, testPreparedPlanFor(t, "proposal-A", "sha256:proposal-a", "plan-A", "idem-A")); !errors.Is(err, ErrReservationUnknown) {
		t.Fatalf("released commit = %v, want %v", err, ErrReservationUnknown)
	}
	if err := fence.Release("sha256:proposal-a", rev00602Now); !errors.Is(err, ErrReservationUnknown) {
		t.Fatalf("double release = %v, want %v", err, ErrReservationUnknown)
	}
}
