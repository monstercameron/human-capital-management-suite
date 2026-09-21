package bandfacts

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/payband"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

const (
	testCatalogVersion = "test.bands/1"
	jobCode            = "ENG-SWE3"
	grade              = "P4"
	payZone            = "US-EAST"
)

var (
	bandEffectiveFrom = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	bandRecordedAt    = time.Date(2026, time.September, 1, 13, 45, 0, 0, time.UTC)
)

func newTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2020-01-01T00:00:00Z')`,
		id, key+"-"+id.String()[:8], "bandfacts "+key)
	return id
}

func money(t *testing.T, amount string) values.Money {
	t.Helper()
	m, err := values.NewMoney(amount, "USD", 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatalf("%q is not exact money: %v", amount, err)
	}
	return m
}

// putBand records one band for a tenant, at a business start date the caller
// chooses, so a test can prove a later revision is what a lookup reads.
func putBand(t *testing.T, db *pgtest.DB, tenant, entity uuid.UUID, from time.Time, minimum, midpoint, maximum string) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	band, err := aggregates.NewCompensationBand(tenant, entity, from, nil, bandRecordedAt,
		jobCode, grade, payZone, money(t, minimum), money(t, midpoint), money(t, maximum))
	if err != nil {
		t.Fatalf("NewCompensationBand: %v", err)
	}
	if _, err := (aggregates.CompensationStore{}).PutCompensationBand(ctx, tx, band); err != nil {
		t.Fatalf("PutCompensationBand: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func query(asOfYear int) rewards.BandQuery {
	asOf, _ := values.NewLocalDate(asOfYear, time.June, 1)
	return rewards.BandQuery{
		Tenant: "harborcare-demo", JobCode: jobCode, Grade: grade, PayZone: payZone,
		Currency: "USD", AsOf: asOf,
	}
}

// refusingCatalog is a fallback that never answers, so a test can prove a
// lookup was served by the database rather than by the fallback.
type refusingCatalog struct{ calls int }

func (c *refusingCatalog) LookupBand(context.Context, rewards.BandQuery) (rewards.BandRecord, error) {
	c.calls++
	return rewards.BandRecord{}, rewards.ErrBandNotFound
}

// TestLookupBandReadsTheStoredRow proves the served catalog answers from the
// tenant's own compensation_band row: the fallback is never consulted, and
// the bounds returned are the stored ones.
func TestLookupBandReadsTheStoredRow(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenant := newTenant(t, db, "stored")
	entity := uuid.New()
	putBand(t, db, tenant, entity, bandEffectiveFrom, "126400.0000", "158000.0000", "189600.0000")

	fallback := &refusingCatalog{}
	catalog := Catalog{
		DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return tenant },
		CatalogVersion: testCatalogVersion, Blocking: true, Source: "hcmnext.compensation", Fallback: fallback,
	}
	record, err := catalog.LookupBand(ctx, query(2026))
	if err != nil {
		t.Fatalf("LookupBand: %v", err)
	}
	if fallback.calls != 0 {
		t.Fatalf("the fallback answered %d times; the lookup did not read the database", fallback.calls)
	}
	if record.Band.ID != entity.String() {
		t.Fatalf("band id = %q, want the stored row's identity %q", record.Band.ID, entity)
	}
	if record.CatalogVersion != testCatalogVersion || record.Band.Version != testCatalogVersion {
		t.Fatalf("band pins catalog %q/%q, want %q", record.CatalogVersion, record.Band.Version, testCatalogVersion)
	}
	if !record.Blocking {
		t.Fatal("the catalog published a blocking band as advisory")
	}
	if record.Band.Scope != (payband.Scope{JobCode: jobCode, Grade: grade, PayZone: payZone}) {
		t.Fatalf("band scope = %+v", record.Band.Scope)
	}
	if record.Band.Midpoint.Amount().Cmp(money(t, "158000.0000").Amount()) != 0 {
		t.Fatalf("band midpoint = %s, want the stored 158000.0000", record.Band.Midpoint.String())
	}
	if record.Authority.Kind != evidence.AuthorityLocal || record.Provenance.EvidenceRef == "" {
		t.Fatalf("band record is not citable: %+v", record)
	}
	if err := record.Validate(); err != nil {
		t.Fatalf("the returned record does not validate: %v", err)
	}
}

// TestLookupBandPublishesBoundsAtTheServedMoneyScale is the regression for
// the defect that made every promotion against a stored band unproposable:
// the reader published the numeric(19, 4) column's own scale, and
// internal/engines/payband refuses to place a scale-2 amount in a scale-4
// band, so the served propose path answered "the operation could not be
// completed" for every demo worker. The bounds a lookup returns must declare
// the same scale the served amounts do, and must still place an amount.
func TestLookupBandPublishesBoundsAtTheServedMoneyScale(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenant := newTenant(t, db, "scale")
	putBand(t, db, tenant, uuid.New(), bandEffectiveFrom, "126400.0000", "158000.0000", "189600.0000")

	catalog := Catalog{
		DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return tenant },
		CatalogVersion: testCatalogVersion, Blocking: true,
	}
	record, err := catalog.LookupBand(ctx, query(2026))
	if err != nil {
		t.Fatalf("LookupBand: %v", err)
	}
	for _, bound := range []struct {
		name string
		got  values.Money
	}{{"minimum", record.Band.Minimum}, {"midpoint", record.Band.Midpoint}, {"maximum", record.Band.Maximum}} {
		if got := bound.got.Amount().Scale(); got != DefaultMoneyScale {
			t.Errorf("band %s declares scale %d, want the served money scale %d", bound.name, got, DefaultMoneyScale)
		}
	}
	// The engine is the real judge: an amount expressed the way every served
	// base pay is must place inside the band this reader published.
	pay, err := values.NewMoney("158000.00", "USD", DefaultMoneyScale, values.RoundingExactRequired)
	if err != nil {
		t.Fatalf("base pay: %v", err)
	}
	position, err := payband.Evaluate(record.Band, pay)
	if err != nil {
		t.Fatalf("payband.Evaluate refused the stored band: %v", err)
	}
	if position.Placement != payband.PlacementInBand {
		t.Fatalf("the midpoint placed as %s, want IN_BAND", position.Placement)
	}
}

// TestLookupBandReadsTheRevisionLiveAtTheQueryDate proves the read is
// effective-dated against the stored rows rather than answering from whatever
// was compiled in: a later revision is invisible before it takes effect and
// authoritative afterwards.
func TestLookupBandReadsTheRevisionLiveAtTheQueryDate(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenant := newTenant(t, db, "revised")
	putBand(t, db, tenant, uuid.New(), bandEffectiveFrom, "126400.0000", "158000.0000", "189600.0000")
	putBand(t, db, tenant, uuid.New(), time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC),
		"140000.0000", "175000.0000", "210000.0000")

	catalog := Catalog{
		DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return tenant },
		CatalogVersion: testCatalogVersion, Blocking: true,
	}
	before, err := catalog.LookupBand(ctx, query(2026))
	if err != nil {
		t.Fatalf("LookupBand before the revision: %v", err)
	}
	if before.Band.Midpoint.Amount().Cmp(money(t, "158000.0000").Amount()) != 0 {
		t.Fatalf("2026 midpoint = %s, want 158000.0000", before.Band.Midpoint.String())
	}
	after, err := catalog.LookupBand(ctx, query(2027))
	if err != nil {
		t.Fatalf("LookupBand after the revision: %v", err)
	}
	if after.Band.Midpoint.Amount().Cmp(money(t, "175000.0000").Amount()) != 0 {
		t.Fatalf("2027 midpoint = %s, want the revised 175000.0000", after.Band.Midpoint.String())
	}
}

// TestLookupBandIsTenantScopedAndFallsBack proves a tenant reads only its own
// bands, and that a scope this tenant records no band for is delegated to the
// declared fallback (or reported as not found when there is none).
func TestLookupBandIsTenantScopedAndFallsBack(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	owner := newTenant(t, db, "owner")
	other := newTenant(t, db, "other")
	putBand(t, db, owner, uuid.New(), bandEffectiveFrom, "126400.0000", "158000.0000", "189600.0000")

	fallback := &refusingCatalog{}
	otherTenant := Catalog{
		DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return other },
		CatalogVersion: testCatalogVersion, Fallback: fallback,
	}
	if _, err := otherTenant.LookupBand(ctx, query(2026)); !errors.Is(err, rewards.ErrBandNotFound) {
		t.Fatalf("a second tenant read somebody else's band: %v", err)
	}
	if fallback.calls != 1 {
		t.Fatalf("the fallback was consulted %d times, want once", fallback.calls)
	}

	noFallback := Catalog{
		DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return other },
		CatalogVersion: testCatalogVersion,
	}
	if _, err := noFallback.LookupBand(ctx, query(2026)); !errors.Is(err, rewards.ErrBandNotFound) {
		t.Fatalf("an unbacked scope did not report ErrBandNotFound: %v", err)
	}
}

// TestLookupBandRefusesMalformedCompositionAndQueries proves the catalog
// refuses rather than answering from an unpinned or unscoped composition.
func TestLookupBandRefusesMalformedCompositionAndQueries(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenant := newTenant(t, db, "refusals")

	unpinned := Catalog{DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return tenant }}
	if _, err := unpinned.LookupBand(ctx, query(2026)); err == nil {
		t.Error("a catalog with no version answered a lookup")
	}
	if _, err := unpinned.LookupBand(ctx, rewards.BandQuery{}); !errors.Is(err, rewards.ErrBandQueryInvalid) {
		t.Error("an empty query was accepted")
	}

	fallback := &refusingCatalog{}
	unbacked := Catalog{CatalogVersion: testCatalogVersion, Fallback: fallback}
	if _, err := unbacked.LookupBand(ctx, query(2026)); !errors.Is(err, rewards.ErrBandNotFound) {
		t.Errorf("a catalog with no database did not delegate: %v", err)
	}
	if fallback.calls != 1 {
		t.Fatalf("the fallback was consulted %d times, want once", fallback.calls)
	}
}
