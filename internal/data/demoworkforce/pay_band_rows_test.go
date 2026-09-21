package demoworkforce

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var payBandSeedRecordedAt = time.Date(2026, time.September, 1, 13, 45, 0, 0, time.UTC)

// TestSeedPayBandsLandsIsTenantScopedAndReplays proves the published salary
// ranges reach compensation_band under deterministic identities, that a
// second tenant sees none of them, and that a second run writes nothing
// rather than superseding every live row with an identical successor.
func TestSeedPayBandsLandsIsTenantScopedAndReplays(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantA := seedAggregateTenant(t, db)
	tenantB := seedAggregateTenant(t, db)

	specs, err := PayBandSpecs()
	if err != nil {
		t.Fatalf("PayBandSpecs: %v", err)
	}
	var summary PayBandSummary
	inAggregateTx(t, db, tenantA, func(tx dbport.Tx) error {
		var seedErr error
		summary, seedErr = SeedPayBands(ctx, tx, tenantA, payBandSeedRecordedAt)
		return seedErr
	})
	if summary.Planned != len(specs) || summary.Inserted != len(specs) || summary.Skipped != 0 {
		t.Fatalf("seed summary = %+v, want %d planned and inserted with no skips", summary, len(specs))
	}

	var rows int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM compensation_band WHERE tenant_id = $1 AND superseded_at IS NULL`, tenantA).Scan(&rows); err != nil {
		t.Fatalf("count seeded bands: %v", err)
	}
	if rows != len(specs) {
		t.Fatalf("compensation_band holds %d live rows, want %d", rows, len(specs))
	}
	var otherRows int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM compensation_band WHERE tenant_id = $1`, tenantB).Scan(&otherRows); err != nil {
		t.Fatalf("count the other tenant's bands: %v", err)
	}
	if otherRows != 0 {
		t.Fatalf("compensation_band holds %d rows for the untouched tenant, want 0", otherRows)
	}

	// The stored bounds are the published ones, read back through the row's
	// own deterministic identity rather than by scanning for something close.
	sample := specs[0]
	var minimum, midpoint, maximum, currency string
	if err := db.QueryRow(ctx, `
		SELECT minimum::text, midpoint::text, maximum::text, currency
		FROM compensation_band WHERE tenant_id = $1 AND entity_id = $2 AND superseded_at IS NULL`,
		tenantA, PayBandID(sample.ID)).Scan(&minimum, &midpoint, &maximum, &currency); err != nil {
		t.Fatalf("read back band %s: %v", sample.ID, err)
	}
	if currency != "USD" {
		t.Fatalf("band %s stores currency %q, want USD", sample.ID, currency)
	}
	for _, bound := range []struct {
		name   string
		stored string
		want   values.Money
	}{{"minimum", minimum, sample.Minimum}, {"midpoint", midpoint, sample.Midpoint}, {"maximum", maximum, sample.Maximum}} {
		got, parseErr := values.NewMoney(bound.stored, currency, 4, values.RoundingExactRequired)
		if parseErr != nil {
			t.Fatalf("band %s %s %q is not exact money: %v", sample.ID, bound.name, bound.stored, parseErr)
		}
		if got.Amount().Cmp(bound.want.Amount()) != 0 {
			t.Fatalf("band %s %s stored %s, want %s", sample.ID, bound.name, bound.stored, bound.want.String())
		}
	}

	var replay PayBandSummary
	inAggregateTx(t, db, tenantA, func(tx dbport.Tx) error {
		var seedErr error
		replay, seedErr = SeedPayBands(ctx, tx, tenantA, payBandSeedRecordedAt)
		return seedErr
	})
	if replay.Inserted != 0 || replay.Skipped != len(specs) {
		t.Fatalf("replay summary = %+v, want zero inserts and %d skips", replay, len(specs))
	}
	var afterReplay int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM compensation_band WHERE tenant_id = $1`, tenantA).Scan(&afterReplay); err != nil {
		t.Fatalf("count bands after the replay: %v", err)
	}
	if afterReplay != len(specs) {
		t.Fatalf("the replay left %d rows, want the original %d", afterReplay, len(specs))
	}
}

// TestSeededPayBandsContainEverySeededSalary proves the consistency the
// product's own screens depend on: every planned worker's recorded base pay
// sits inside the stored band for their job code, grade and pay zone. A
// worker priced outside their own band is the kind of contradiction a reader
// spots immediately and cannot explain.
func TestSeededPayBandsContainEverySeededSalary(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenant := seedAggregateTenant(t, db)
	inAggregateTx(t, db, tenant, func(tx dbport.Tx) error {
		_, seedErr := SeedPayBands(ctx, tx, tenant, payBandSeedRecordedAt)
		return seedErr
	})

	employees, err := Plan(tenant)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	checked := 0
	for _, employee := range employees {
		row := employee.Row
		var minimum, midpoint, maximum string
		if err := db.QueryRow(ctx, `
			SELECT minimum::text, midpoint::text, maximum::text FROM compensation_band
			WHERE tenant_id = $1 AND job_code = $2 AND grade = $3 AND pay_zone = $4 AND currency = $5
			  AND superseded_at IS NULL`,
			tenant, row.JobCode, row.Grade, row.PayZone, row.Currency).Scan(&minimum, &midpoint, &maximum); err != nil {
			t.Fatalf("%s (%s/%s/%s) has no stored band: %v", row.WorkerKey, row.JobCode, row.Grade, row.PayZone, err)
		}
		pay := mustMoney(t, row.BasePay, row.Currency)
		low, high, mid := mustMoney(t, minimum, row.Currency), mustMoney(t, maximum, row.Currency), mustMoney(t, midpoint, row.Currency)
		if pay.Amount().Cmp(low.Amount()) < 0 || pay.Amount().Cmp(high.Amount()) > 0 {
			t.Errorf("%s is paid %s, outside their stored band [%s, %s]", row.WorkerKey, row.BasePay, minimum, maximum)
		}
		if pay.Amount().Cmp(mid.Amount()) != 0 {
			t.Errorf("%s is paid %s, which is not their band midpoint %s", row.WorkerKey, row.BasePay, midpoint)
		}
		checked++
	}
	if checked != NewWorkerCount {
		t.Fatalf("checked %d workers against their bands, want %d", checked, NewWorkerCount)
	}
}

// TestSeedPayBandsRefusesAnIncompleteCall proves the seeder refuses a call it
// could not record correctly rather than reporting a silent success.
func TestSeedPayBandsRefusesAnIncompleteCall(t *testing.T) {
	ctx := context.Background()
	if _, err := SeedPayBands(ctx, nil, uuid.New(), payBandSeedRecordedAt); err == nil {
		t.Error("seeding without a transaction was accepted")
	}
}

func mustMoney(t *testing.T, amount, currency string) values.Money {
	t.Helper()
	money, err := values.NewMoney(amount, currency, 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatalf("%q is not exact money: %v", amount, err)
	}
	return money
}
