package aggregates_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_DB_008_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	store := aggregates.PeopleStore{}
	personID := uuid.New()
	person, err := aggregates.NewPerson(tenant, personID, date(t, "2024-01-01"), nil, instant(t, "2024-01-01T00:00:00Z"), "ACTIVE", "Integration Person", "")
	if err != nil {
		t.Fatal(err)
	}
	inTx(t, db, func(tx dbport.Tx) error { _, e := store.PutPerson(ctx, tx, person); return e })
	claimID := uuid.New()
	claim, err := aggregates.NewIdentityClaim(tenant, claimID, &personID, date(t, "2024-01-01"), nil, instant(t, "2024-01-02T00:00:00Z"), "EMAIL", "integration", "sha256:integration", "integration", "VERIFIED")
	if err != nil {
		t.Fatal(err)
	}
	inTx(t, db, func(tx dbport.Tx) error { _, e := store.PutIdentityClaim(ctx, tx, claim); return e })
	got, err := store.CurrentIdentityClaim(ctx, db.Conn, tenant, claimID, date(t, "2024-06-01"))
	if err != nil {
		t.Fatal(err)
	}
	if got.PersonRef == nil || *got.PersonRef != personID || got.Assurance != "VERIFIED" {
		t.Fatalf("identity claim=%+v, person link or assurance lost", got)
	}
}

func TestTodo_DB_008_Property(t *testing.T) {
	t.Parallel()
	tenant, person := uuid.New(), uuid.New()
	open, err := aggregates.NewPerson(tenant, person, date(t, "2024-01-01"), nil, instant(t, "2024-01-02T00:00:00Z"), "ACTIVE", "Boundary", "")
	if err != nil {
		t.Fatal(err)
	}
	end := date(t, "2024-02-01")
	bounded, err := aggregates.NewPerson(tenant, person, date(t, "2024-01-01"), &end, instant(t, "2024-01-02T00:00:00Z"), "ACTIVE", "Boundary", "")
	if err != nil {
		t.Fatal(err)
	}
	if open.EffectiveTo != nil || bounded.EffectiveTo == nil || !bounded.EffectiveTo.Equal(end) {
		t.Fatalf("effective interval encoding open=%v bounded=%v", open.EffectiveTo, bounded.EffectiveTo)
	}
	if open.Digest == bounded.Digest {
		t.Fatal("changing the effective interval did not change the person digest")
	}
}

func TestTodo_DB_008_Race(t *testing.T) {
	t.Parallel()
	const workers = 24
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, e := aggregates.NewPerson(uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"), uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"), date(t, "2024-01-01"), nil, instant(t, "2024-01-01T00:00:00Z"), "ACTIVE", "Stable", "")
			if e == nil && (p.Digest == "" || p.CanonicalID == "") {
				e = fmt.Errorf("constructor omitted canonical envelope")
			}
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
}

func TestTodo_DB_009_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	org := aggregates.OrganizationStore{}
	entityID, unitID := uuid.New(), uuid.New()
	entity, err := aggregates.NewLegalEntity(tenant, entityID, date(t, "2020-01-01"), nil, instant(t, "2020-01-01T00:00:00Z"), "Integration LLC", "ACTIVE")
	if err != nil {
		t.Fatal(err)
	}
	inTx(t, db, func(tx dbport.Tx) error { _, e := org.PutLegalEntity(ctx, tx, entity); return e })
	unit, err := aggregates.NewOrganizationUnit(tenant, unitID, date(t, "2024-01-01"), nil, instant(t, "2024-01-01T00:00:00Z"), "DEPARTMENT", "integration", "Integration", &entityID, nil, "ACTIVE")
	if err != nil {
		t.Fatal(err)
	}
	inTx(t, db, func(tx dbport.Tx) error { _, e := org.PutOrganizationUnit(ctx, tx, unit); return e })
	got, err := org.CurrentOrganizationUnit(ctx, db.Conn, tenant, unitID, date(t, "2024-06-01"))
	if err != nil {
		t.Fatal(err)
	}
	if got.LegalEntityRef == nil || *got.LegalEntityRef != entityID || got.Code != "integration" {
		t.Fatalf("organization unit lost legal entity relation: %+v", got)
	}
}

func TestTodo_DB_009_Mutation(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	org := aggregates.OrganizationStore{}
	id := uuid.New()
	e, err := aggregates.NewLegalEntity(tenant, id, date(t, "2024-01-01"), nil, instant(t, "2024-01-01T00:00:00Z"), "Immutable LLC", "ACTIVE")
	if err != nil {
		t.Fatal(err)
	}
	inTx(t, db, func(tx dbport.Tx) error { _, x := org.PutLegalEntity(context.Background(), tx, e); return x })
	if err := db.ExecErr(`UPDATE legal_entity SET registered_name='forged' WHERE entity_id=$1`, id); err == nil {
		t.Fatal("direct legal entity mutation succeeded")
	}
	got, err := org.CurrentLegalEntity(context.Background(), db.Conn, tenant, id, date(t, "2024-06-01"))
	if err != nil {
		t.Fatal(err)
	}
	if got.RegisteredName != "Immutable LLC" {
		t.Fatalf("name after refused mutation=%q", got.RegisteredName)
	}
}

func TestTodo_DB_009_Property(t *testing.T) {
	t.Parallel()
	tenant, position := uuid.New(), uuid.New()
	for _, tc := range []struct {
		fte string
		ok  bool
	}{{"0", true}, {"0.0001", true}, {"1.0000", true}, {"1.0001", true}} {
		t.Run(tc.fte, func(t *testing.T) {
			got, err := aggregates.NewPositionOccupancy(tenant, uuid.New(), position, nil, nil, date(t, "2024-01-01"), nil, instant(t, "2024-01-01T00:00:00Z"), tc.fte, true)
			if (err == nil) != tc.ok {
				t.Fatalf("allocation %s err=%v, want valid=%v", tc.fte, err, tc.ok)
			}
			if got.AllocationFTE == "" {
				t.Fatal("canonical FTE is empty")
			}
		})
	}
}

func TestTodo_DB_009_Race(t *testing.T) {
	t.Parallel()
	const n = 32
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	tenant, position := uuid.New(), uuid.New()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, e := aggregates.NewPositionOccupancy(tenant, uuid.New(), position, nil, nil, date(t, "2024-01-01"), nil, instant(t, "2024-01-01T00:00:00Z"), "0.2500", false)
			if e == nil && o.AllocationFTE != "0.2500" {
				e = fmt.Errorf("normalized allocation=%q", o.AllocationFTE)
			}
			errCh <- e
		}()
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		if e != nil {
			t.Error(e)
		}
	}
}

func BenchmarkTodo_DB_009(b *testing.B) {
	tenant, position := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"), uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := aggregates.NewPositionOccupancy(tenant, uuid.New(), position, nil, nil, at, nil, at, "0.2500", false); err != nil {
			b.Fatal(err)
		}
	}
}

func TestTodo_DB_010_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	store := aggregates.CompensationStore{}
	worker := uuid.New()
	registerStandinEntity(t, db, tenant, worker, "worker")
	packageID, componentID := uuid.New(), uuid.New()
	p, err := aggregates.NewCompensationPackage(tenant, packageID, worker, nil, nil, date(t, "2024-01-01"), nil, instant(t, "2024-01-01T00:00:00Z"), "USD")
	if err != nil {
		t.Fatal(err)
	}
	inTx(t, db, func(tx dbport.Tx) error { _, e := store.PutCompensationPackage(ctx, tx, p); return e })
	amount, err := values.NewMoney("123456.78", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	c, err := aggregates.NewCompensationComponent(tenant, componentID, packageID, date(t, "2024-01-01"), nil, instant(t, "2024-01-01T00:00:00Z"), "BASE_PAY", amount, "ANNUAL")
	if err != nil {
		t.Fatal(err)
	}
	inTx(t, db, func(tx dbport.Tx) error { _, e := store.PutCompensationComponent(ctx, tx, c); return e })
	got, err := store.CurrentCompensationComponent(ctx, db.Conn, tenant, componentID, date(t, "2024-06-01"))
	if err != nil {
		t.Fatal(err)
	}
	actual, err := values.NewDecimal(got.Amount, 4, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("parse stored exact amount %q: %v", got.Amount, err)
	}
	want, _ := values.NewDecimal("123456.78", 4, values.RoundingHalfEven)
	if !actual.Equal(want) || got.Currency != "USD" {
		t.Fatalf("stored compensation=(%s,%s), want exact (123456.78,USD)", got.Amount, got.Currency)
	}
}

func TestTodo_DB_010_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	store := aggregates.CompensationStore{}
	worker, packageID, componentID := uuid.New(), uuid.New(), uuid.New()
	registerStandinEntity(t, db, tenant, worker, "worker")
	p, _ := aggregates.NewCompensationPackage(tenant, packageID, worker, nil, nil, date(t, "2024-01-01"), nil, instant(t, "2024-01-01T00:00:00Z"), "USD")
	inTx(t, db, func(tx dbport.Tx) error { _, e := store.PutCompensationPackage(ctx, tx, p); return e })
	c, _ := aggregates.NewCompensationComponent(tenant, componentID, packageID, date(t, "2024-01-01"), nil, instant(t, "2024-01-01T00:00:00Z"), "BASE_PAY", usd(t, "100.00"), "ANNUAL")
	inTx(t, db, func(tx dbport.Tx) error { _, e := store.PutCompensationComponent(ctx, tx, c); return e })
	if err := db.ExecErr(`UPDATE compensation_component SET amount=999 WHERE entity_id=$1 AND superseded_at IS NULL`, componentID); err == nil {
		t.Fatal("direct compensation amount mutation succeeded")
	}
	got, err := store.CurrentCompensationComponent(ctx, db.Conn, tenant, componentID, date(t, "2024-06-01"))
	if err != nil {
		t.Fatal(err)
	}
	actual, err := values.NewDecimal(got.Amount, 4, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("parse amount after refused mutation %q: %v", got.Amount, err)
	}
	want, _ := values.NewDecimal("100", 4, values.RoundingHalfEven)
	if !actual.Equal(want) {
		t.Fatalf("amount after refused mutation=%s, want 100", actual.String())
	}
}

func TestTodo_DB_010_Property(t *testing.T) {
	t.Parallel()
	tenant, worker := uuid.New(), uuid.New()
	for _, amount := range []string{"0.01", "1000000000000.99", "-1.25", "999999999999999999.0001"} {
		t.Run(amount, func(t *testing.T) {
			m, err := values.NewMoney(amount, "USD", 4, values.RoundingHalfEven)
			if err != nil {
				t.Fatalf("valid exact money %q rejected: %v", amount, err)
			}
			p, err := aggregates.NewCompensationPackage(tenant, uuid.New(), worker, nil, nil, date(t, "2024-01-01"), nil, instant(t, "2024-01-01T00:00:00Z"), "USD")
			if err != nil {
				t.Fatal(err)
			}
			component, err := aggregates.NewCompensationComponent(tenant, uuid.New(), p.EntityID, date(t, "2024-01-01"), nil, instant(t, "2024-01-01T00:00:00Z"), "BASE_PAY", m, "ANNUAL")
			if err != nil {
				t.Fatal(err)
			}
			if component.Amount == "" {
				t.Fatal("exact decimal amount was lost")
			}
		})
	}
}

func TestTodo_DB_010_Race(t *testing.T) {
	t.Parallel()
	const n = 24
	tenant := uuid.New()
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			money, e := values.NewMoney("98765.4321", "USD", 4, values.RoundingHalfEven)
			if e == nil {
				_, e = aggregates.NewCompensationComponent(tenant, uuid.New(), uuid.New(), date(t, "2024-01-01"), nil, instant(t, "2024-01-01T00:00:00Z"), "BASE_PAY", money, "ANNUAL")
			}
			errCh <- e
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Error(err)
		}
	}
}
