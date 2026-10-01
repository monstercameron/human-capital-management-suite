package aggregates_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestTodo_AGENT_015_ActiveEmploymentsForWorkerReturnsAllCurrentAuthorities(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	worker := uuid.New()
	legalA, legalB := uuid.New(), uuid.New()
	registerStandinEntity(t, db, tenant, worker, "worker")
	org := aggregates.OrganizationStore{}
	for _, id := range []uuid.UUID{legalA, legalB} {
		legal, err := aggregates.NewLegalEntity(tenant, id, date(t, "2020-01-01"), nil, instant(t, "2020-01-01T00:00:00Z"), "Example Legal Entity", "ACTIVE")
		if err != nil {
			t.Fatal(err)
		}
		inTx(t, db, func(tx dbport.Tx) error {
			_, err := org.PutLegalEntity(ctx, tx, legal)
			return err
		})
	}
	people := aggregates.PeopleStore{}
	for n, legalID := range []uuid.UUID{legalA, legalB} {
		employment, err := aggregates.NewEmployment(tenant, uuid.New(), worker, legalID, date(t, "2024-01-01"), nil, instant(t, "2024-01-01T00:00:00Z"), "EMPLOYEE", "ACTIVE", nil)
		if err != nil {
			t.Fatal(err)
		}
		inTx(t, db, func(tx dbport.Tx) error {
			_, err := people.PutEmployment(ctx, tx, employment)
			return err
		})
		if n == 0 {
			got, err := people.ActiveEmploymentsForWorker(ctx, db.Conn, tenant, worker, instant(t, "2026-01-01T00:00:00Z"))
			if err != nil || len(got) != 1 || got[0].LegalEntityRef != legalA {
				t.Fatalf("single active employment = %+v, %v", got, err)
			}
		}
	}
	got, err := people.ActiveEmploymentsForWorker(ctx, db.Conn, tenant, worker, instant(t, "2026-01-01T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].EntityID.String() >= got[1].EntityID.String() {
		t.Fatalf("current employments = %+v, want both rows in deterministic order", got)
	}
	missing, err := people.ActiveEmploymentsForWorker(ctx, db.Conn, tenant, uuid.New(), instant(t, "2026-01-01T00:00:00Z"))
	if err != nil || len(missing) != 0 {
		t.Fatalf("missing worker employments = %+v, %v, want empty", missing, err)
	}
}
