package workforce_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// fixedInstant is the clock every fixture stamps. This package reads no wall
// clock, so a test that wants a time has to say which one.
var fixedInstant = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// insertTenant registers one active tenant as the migration/admin role.
func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

// appConn opens a fresh connection on db's schema and assumes the
// least-privilege hcmnext_app role, the only way a test observes the row level
// security policy migration 00023 declares.
func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

// inTenantTxErr runs fn inside its own tenant-scoped transaction and commits
// it, returning fn's own failure so a refused write can be inspected. A
// failing fn rolls back, so a refusal leaves nothing behind.
func inTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

// newRow builds a complete, insertable worker row for tenant.
func newRow(tenant uuid.UUID, key string) workforce.WorkerRow {
	id := uuid.New()
	return workforce.WorkerRow{
		TenantID:      tenant,
		WorkerID:      id,
		WorkerKey:     key,
		LegalName:     "Ada Lovelace",
		PreferredName: "Ada",
		// Migration 00181 made worker numbers unique per tenant, so every
		// fixture row derives its number from its key.
		WorkerNumber:            "W-" + key,
		WorkerType:              "employee",
		LifecycleStatus:         "active",
		EmploymentID:            "emp_" + key,
		AssignmentID:            "asg_" + key,
		JobCode:                 "OPS-HRBP2",
		JobTitle:                "Senior People Partner",
		Grade:                   "P2",
		OrgUnit:                 "people-ops",
		PositionID:              "POS-HRBP-900",
		Location:                "Boston, MA",
		PayZone:                 "US-EAST",
		FTE:                     "1.0000",
		ManagerRelationshipRef:  "rel_mgr_" + key,
		ProfilePhotoOriginalRef: "profile-originals/" + key + ".png",
		ProfilePhotoProxyRef:    "/workspace/assets/person-" + key + "-small.jpg",
		HireDate:                "2021-04-05",
		EffectiveFrom:           "2021-04-05",
		BasePay:                 "90000.00",
		Currency:                "USD",
		PayBasis:                "ANNUAL_SALARY",
		BonusTarget:             "0.0500",
		RevisionStream:          "people.worker." + id.String(),
		RevisionSequence:        1,
		KnownAt:                 fixedInstant,
		RecordedAt:              fixedInstant,
		CreatedBy:               "principal:manager-1",
		Source:                  workforce.SourceCreated,
	}
}

// TestStoreCreateAndGetRoundTripEveryColumn proves an inserted worker comes
// back byte for byte -- including the three numeric columns and the two date
// columns, which are the ones a float or a timezone would quietly change.
func TestStoreCreateAndGetRoundTripEveryColumn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "workforce-roundtrip")
	conn := appConn(t, db)
	store := workforce.Store{}
	in := newRow(tenant, "ada-lovelace-1")

	var stored workforce.WorkerRow
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		stored, err = store.Create(ctx, tx, in)
		return err
	})
	if stored != in {
		t.Fatalf("Create returned a different row:\n got %+v\nwant %+v", stored, in)
	}

	for _, ref := range []string{in.WorkerKey, in.WorkerID.String()} {
		var (
			got   workforce.WorkerRow
			found bool
		)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			got, found, err = store.Get(ctx, tx, tenant, ref)
			return err
		})
		if !found {
			t.Fatalf("Get(%q) found nothing", ref)
		}
		if got != in {
			t.Errorf("Get(%q) = %+v, want %+v", ref, got, in)
		}
	}

	var found bool
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		_, found, err = store.Get(ctx, tx, tenant, "nobody-at-all")
		return err
	})
	if found {
		t.Error("Get of an unknown reference reported a worker")
	}
}

// TestStoreCreateRefusesADuplicateKey proves a repeated worker_key is the
// typed ErrDuplicate rather than a raw constraint violation, and that the
// first row is untouched.
func TestStoreCreateRefusesADuplicateKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "workforce-duplicate")
	conn := appConn(t, db)
	store := workforce.Store{}

	first := newRow(tenant, "ada-lovelace-1")
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.Create(ctx, tx, first)
		return err
	})

	second := newRow(tenant, "ada-lovelace-1")
	second.LegalName = "Someone Else"
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, createErr := store.Create(ctx, tx, second)
		return createErr
	})
	if !errors.Is(err, workforce.ErrDuplicate) {
		t.Fatalf("Create(duplicate key) = %v, want ErrDuplicate", err)
	}

	var listed []workforce.WorkerRow
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var listErr error
		listed, listErr = store.List(ctx, tx, tenant)
		return listErr
	})
	if len(listed) != 1 || listed[0].LegalName != first.LegalName {
		t.Fatalf("the refused duplicate changed the population: %+v", listed)
	}
}

// TestStoreCreateRefusesAnIncompleteRow proves the row contract is enforced
// before the statement runs: journey_worker is append-only, so a row that is
// wrong on the way in could never be corrected.
func TestStoreCreateRefusesAnIncompleteRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "workforce-invalid")
	conn := appConn(t, db)
	store := workforce.Store{}

	for name, mutate := range map[string]func(*workforce.WorkerRow){
		"no legal name":     func(r *workforce.WorkerRow) { r.LegalName = "" },
		"no job code":       func(r *workforce.WorkerRow) { r.JobCode = "" },
		"bad currency":      func(r *workforce.WorkerRow) { r.Currency = "usd" },
		"bad hire date":     func(r *workforce.WorkerRow) { r.HireDate = "05/04/2021" },
		"zero revision":     func(r *workforce.WorkerRow) { r.RevisionSequence = 0 },
		"knowledge inverts": func(r *workforce.WorkerRow) { r.KnownAt = r.RecordedAt.Add(time.Hour) },
	} {
		t.Run(name, func(t *testing.T) {
			row := newRow(tenant, "invalid-"+uuid.NewString())
			mutate(&row)
			err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
				_, createErr := store.Create(ctx, tx, row)
				return createErr
			})
			if !errors.Is(err, workforce.ErrInvalidRow) {
				t.Fatalf("Create(%s) = %v, want ErrInvalidRow", name, err)
			}
		})
	}
}

// TestStoreListIsNewestFirst proves the list order the journey's worker list
// renders: newest created at the top, ties broken by key so two identical
// reads agree.
func TestStoreListIsNewestFirst(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "workforce-order")
	conn := appConn(t, db)
	store := workforce.Store{}

	for i, key := range []string{"first-1", "second-2", "third-3"} {
		row := newRow(tenant, key)
		row.KnownAt = fixedInstant.Add(time.Duration(i) * time.Minute)
		row.RecordedAt = row.KnownAt
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			_, err := store.Create(ctx, tx, row)
			return err
		})
	}

	var listed []workforce.WorkerRow
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		listed, err = store.List(ctx, tx, tenant)
		return err
	})
	want := []string{"third-3", "second-2", "first-1"}
	if len(listed) != len(want) {
		t.Fatalf("listed %d workers, want %d", len(listed), len(want))
	}
	for i, key := range want {
		if listed[i].WorkerKey != key {
			t.Errorf("listed[%d] = %s, want %s", i, listed[i].WorkerKey, key)
		}
	}
}

// TestStoreIsTenantIsolated proves migration 00023's row level security
// policy: one tenant's connection can neither read nor write another tenant's
// workers, and an unscoped transaction sees nothing at all.
func TestStoreIsTenantIsolated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	alpha := insertTenant(t, db, "workforce-alpha")
	beta := insertTenant(t, db, "workforce-beta")
	conn := appConn(t, db)
	store := workforce.Store{}

	inTenantTx(t, conn, alpha, func(tx dbport.Tx) error {
		_, err := store.Create(ctx, tx, newRow(alpha, "alpha-worker-1"))
		return err
	})
	inTenantTx(t, conn, beta, func(tx dbport.Tx) error {
		_, err := store.Create(ctx, tx, newRow(beta, "beta-worker-1"))
		return err
	})

	for _, tc := range []struct {
		tenant uuid.UUID
		want   string
	}{{alpha, "alpha-worker-1"}, {beta, "beta-worker-1"}} {
		var listed []workforce.WorkerRow
		inTenantTx(t, conn, tc.tenant, func(tx dbport.Tx) error {
			var err error
			listed, err = store.List(ctx, tx, tc.tenant)
			return err
		})
		if len(listed) != 1 || listed[0].WorkerKey != tc.want {
			t.Fatalf("tenant %s sees %+v, want only %s", tc.tenant, listed, tc.want)
		}
	}

	// Even naming the other tenant's id explicitly discloses nothing: the
	// policy predicate is the session setting, not the WHERE clause.
	var found bool
	inTenantTx(t, conn, alpha, func(tx dbport.Tx) error {
		var err error
		_, found, err = store.Get(ctx, tx, beta, "beta-worker-1")
		return err
	})
	if found {
		t.Fatal("a tenant-scoped read disclosed another tenant's worker")
	}

	// An unscoped transaction fails closed rather than seeing everything.
	unscoped := appConn(t, db)
	tx, err := unscoped.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := store.List(ctx, tx, alpha)
	if err != nil {
		t.Fatalf("List without a tenant scope: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("an unscoped read returned %d workers, want 0", len(rows))
	}
}

// TestStoreRowsAreAppendOnly proves the forbid_mutation trigger and the
// SELECT/INSERT-only grant: an employee is a fact, not an editable row.
func TestStoreRowsAreAppendOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "workforce-append-only")
	conn := appConn(t, db)
	store := workforce.Store{}
	row := newRow(tenant, "immutable-1")
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.Create(ctx, tx, row)
		return err
	})

	for name, sql := range map[string]string{
		"update": `UPDATE journey_worker SET legal_name = 'Rewritten' WHERE worker_id = $1`,
		"delete": `DELETE FROM journey_worker WHERE worker_id = $1`,
	} {
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, execErr := tx.Exec(ctx, sql, row.WorkerID)
			return execErr
		})
		if err == nil {
			t.Fatalf("%s of a created worker was accepted", name)
		}
	}
}

// TestStorePopulatedAnswersOneBitPerTenant proves the population probe the
// journey's worker resolution decides on: false for a tenant that has created
// nobody, true once it has, and never true because some other tenant has.
func TestStorePopulatedAnswersOneBitPerTenant(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "workforce-populated-a")
	other := insertTenant(t, db, "workforce-populated-b")
	conn := appConn(t, db)
	ctx := context.Background()

	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		populated, err := (workforce.Store{}).Populated(ctx, tx, tenant)
		if err != nil {
			return err
		}
		if populated {
			t.Error("a tenant that has created nobody reports a population")
		}
		return nil
	})

	inTenantTx(t, conn, other, func(tx dbport.Tx) error {
		_, err := (workforce.Store{}).Create(ctx, tx, newRow(other, "populated-elsewhere"))
		return err
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		populated, err := (workforce.Store{}).Populated(ctx, tx, tenant)
		if err != nil {
			return err
		}
		if populated {
			t.Error("another tenant's worker made this tenant look populated")
		}
		return nil
	})

	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := (workforce.Store{}).Create(ctx, tx, newRow(tenant, "populated-here"))
		return err
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		populated, err := (workforce.Store{}).Populated(ctx, tx, tenant)
		if err != nil {
			return err
		}
		if !populated {
			t.Error("a tenant with a created worker reports no population")
		}
		return nil
	})
}
