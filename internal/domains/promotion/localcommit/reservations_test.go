package localcommit

import (
	"context"
	"errors"
	"testing"
)

// allowReservations vouches for every claim. Existing committer tests use it
// to keep proving idempotency, crash atomicity and replay serialization; the
// fence's own verdicts are covered below and by the REV-006-02 matrix.
type allowReservations struct{}

func (allowReservations) AssertHeld(context.Context, ReservationClaim) error { return nil }

// denyReservations refuses every claim with a scripted cause.
type denyReservations struct{ err error }

func (d denyReservations) AssertHeld(context.Context, ReservationClaim) error { return d.err }

// recordReservations captures the claim the committer presented.
type recordReservations struct{ got *ReservationClaim }

func (r recordReservations) AssertHeld(_ context.Context, claim ReservationClaim) error {
	*r.got = claim
	return nil
}

var errTestReservationDenied = errors.New("test: reservation denied")

func TestCommitterRefusesWithoutFence(t *testing.T) {
	c := &Committer{Store: NewStore()}
	if _, err := c.Commit(t.Context(), testPreparedPlan(t)); !errors.Is(err, ErrReservationRequired) {
		t.Fatalf("error = %v, want %v", err, ErrReservationRequired)
	}
}

func TestCommitterRefusesWhenFenceDenies(t *testing.T) {
	c := &Committer{Store: NewStore(), Fence: denyReservations{err: errTestReservationDenied}}
	if _, err := c.Commit(t.Context(), testPreparedPlan(t)); !errors.Is(err, ErrReservationRequired) || !errors.Is(err, errTestReservationDenied) {
		t.Fatalf("error = %v, want both %v and the fence cause", err, ErrReservationRequired)
	}
}

func TestCommitterFenceClaimBindsExactDigest(t *testing.T) {
	var got ReservationClaim
	c := &Committer{Store: NewStore(), Fence: recordReservations{got: &got}}
	plan := testPreparedPlan(t)
	if _, err := c.Commit(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	if got.Tenant != plan.Plan.Tenant || got.ProposalRevisionID != plan.Plan.ProposalRevisionID || got.ProposalDigest != plan.Plan.ProposalDigest {
		t.Fatalf("claim = %+v, want the plan's tenant, revision and digest", got)
	}
}
