package agentmodel

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/schemaflux/schemafluxtest"
)

// r4SettleRefusing is a reservation whose settlement is refused, as the ledger
// does when the observed usage is above what was reserved.
type r4SettleRefusing struct{ fails int }

func (r *r4SettleRefusing) Settle(agentbudget.Usage) error { return agentbudget.ErrReservationExceeded }
func (r *r4SettleRefusing) Fail() error                    { r.fails++; return nil }

type r4Budget struct{ reservation *r4SettleRefusing }

func (b r4Budget) Reserve(context.Context, agentbudget.Request) (Reservation, error) {
	return b.reservation, nil
}

// A refused settlement must not leave its reservation held: a held reservation
// counts against the task, the person's day and the tenant's month.
func TestR4_RefusedSettlementReleasesItsReservation(t *testing.T) {
	provider := schemafluxtest.New().Reply(`{"decision":"approve"}`)
	defer schemafluxtest.Install(t, provider)()
	gateway, _, _, _ := gatewayFixture(t)
	reservation := &r4SettleRefusing{}
	gateway.budget = r4Budget{reservation: reservation}
	_, err := Generate[modelAnswer](context.Background(), gateway, request())
	if !errors.Is(err, ErrBudgetFailed) || reservation.fails != 1 {
		t.Fatalf("err = %v, releases = %d, want a budget failure that released the reservation once", err, reservation.fails)
	}
}
