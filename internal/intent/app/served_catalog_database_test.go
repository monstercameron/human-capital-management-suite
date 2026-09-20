package app

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/bandfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionladder"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// The served catalogs read the tenant's own rows.
//
// The pay bands and the promotion ladder used to be Go maps this process
// built at start-up: a band seeded into compensation_band was never read, and
// the ladder a client was offered belonged to the binary rather than to the
// tenant. These tests drive the composed read paths -- the same
// [promotionLadderSource] the journey engine is given and the same
// internal/data/bandfacts catalog [NewCell] binds -- against a real
// PostgreSQL server, and prove the answers come from the rows.

var (
	servedCatalogSeededAt = time.Date(2026, time.September, 1, 13, 45, 0, 0, time.UTC)
	servedCatalogReadAt   = time.Date(2026, time.September, 19, 0, 0, 0, 0, time.UTC)
)

func servedCatalogTenant(t *testing.T, db *pgtest.DB) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'served catalog tenant', 'ACTIVE', timestamptz '2020-01-01T00:00:00Z')`,
		tenantID, "served-catalog-"+tenantID.String()[:8])

	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	if _, err := demoworkforce.SeedPayBands(ctx, tx, tenantID, servedCatalogSeededAt); err != nil {
		t.Fatalf("SeedPayBands: %v", err)
	}
	if _, err := demoworkforce.SeedPromotionLadder(ctx, tx, tenantID, servedCatalogSeededAt); err != nil {
		t.Fatalf("SeedPromotionLadder: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return tenantID
}

func servedCatalogLadder(db *pgtest.DB, tenantID uuid.UUID) *promotionLadderSource {
	return &promotionLadderSource{
		reader: promotionladder.Reader{DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return tenantID }},
		now:    func() time.Time { return servedCatalogReadAt },
	}
}

func servedCatalogBands(db *pgtest.DB, tenantID uuid.UUID) bandfacts.Catalog {
	// No fallback: a lookup that is answered proves it was answered by a row.
	return bandfacts.Catalog{
		DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return tenantID },
		CatalogVersion: demoworkforce.PayBandPolicyVersion, Blocking: true, Source: "hcmnext.compensation",
	}
}

// TestServedPromotionCatalogReadsTheLadderFromTheDatabase proves the served
// promotion-path catalog publishes the tenant's rows and not the compiled-in
// ladder: an edge recorded only in the database is offered, and the options a
// client is shown are the ones the ladder gate admits.
func TestServedPromotionCatalogReadsTheLadderFromTheDatabase(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := servedCatalogTenant(t, db)
	source := servedCatalogLadder(db, tenantID)

	authored := demoworkforce.PromotionPaths()
	edges, err := source.edges(ctx, "harborcare-demo")
	if err != nil {
		t.Fatalf("read the published ladder: %v", err)
	}
	if len(edges) != len(authored) {
		t.Fatalf("the database published %d edges, want the %d that were seeded", len(edges), len(authored))
	}

	// A row nobody compiled in. If the served catalog were still answering
	// from the Go map this edge could not appear.
	db.Exec(t, `
		INSERT INTO promotion_path_edge (
			row_id, tenant_id, path_id, revision, org_unit, kind, ordinal,
			source_profile_id, source_job_code, source_grade,
			target_profile_id, target_job_code, target_grade, target_title,
			minimum_base_increase, maximum_base_increase,
			lifecycle, policy_version, effective_from, known_from, recorded_at)
		VALUES ($1, $2, 'test:database-only-edge', '1', 'care-coordination', 'UPWARD', 9,
			'profile/CARE-CC2', 'CARE-CC2', 'P2',
			'profile/EXEC-CEO', 'EXEC-CEO', 'E7', 'Chief Executive Officer',
			0.0500, 0.5000, 'PUBLISHED', 'test.ladder/1',
			timestamptz '2000-01-01T00:00:00Z', timestamptz '2026-01-01T00:00:00Z', timestamptz '2026-09-01T13:45:00Z')`,
		uuid.New(), tenantID)

	withExtra, err := source.edges(ctx, "harborcare-demo")
	if err != nil {
		t.Fatalf("read the published ladder again: %v", err)
	}
	if len(withExtra) != len(authored)+1 {
		t.Fatalf("the served catalog published %d edges after a row was added, want %d", len(withExtra), len(authored)+1)
	}
	found := false
	for _, edge := range withExtra {
		if edge.SourceJobCode == "CARE-CC2" && edge.TargetJobCode == "EXEC-CEO" {
			found = true
		}
	}
	if !found {
		t.Fatal("the served catalog did not publish the database-only edge; it is still reading the compiled-in ladder")
	}

	// The options a client is offered and the gate that admits a proposal
	// read the same rows, so a listed target is an accepted one.
	options, err := workforceOptionsFrom(withExtra)
	if err != nil {
		t.Fatalf("workforceOptionsFrom: %v", err)
	}
	baseline := journeyBaselineFacts{currentBase: "112000.00", currency: "USD"}
	offered := 0
	for _, option := range options.PromotionPaths {
		if option.SourceJobCode != "CARE-CC2" {
			continue
		}
		offered++
		in := workspace.ProposalInput{
			TargetJobCode: option.TargetJobCode, TargetGrade: option.TargetGrade,
			ProposedBase: proposedBaseWithin(t, baseline.currentBase, option),
		}
		if err := validatePublishedPromotionPathFrom(withExtra,
			journeyCurrent{jobCode: "CARE-CC2", grade: "P2", orgUnit: "care-coordination"}, in, baseline); err != nil {
			t.Errorf("published target %s/%s was refused by the ladder gate: %v", option.TargetJobCode, option.TargetGrade, err)
		}
	}
	if offered < 2 {
		t.Fatalf("CARE-CC2 was offered %d published targets, want the several its ladder publishes", offered)
	}
}

// TestServedBandCatalogResolvesEveryLadderTargetFromTheDatabase proves the
// other half: the band a promotion target is priced against is the tenant's
// own compensation_band row, the target's band minimum is above the source's
// (so a promotion is always a raise), and a base the ladder admits exists
// inside the target's stored band for every published edge.
func TestServedBandCatalogResolvesEveryLadderTargetFromTheDatabase(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := servedCatalogTenant(t, db)
	bands := servedCatalogBands(db, tenantID)
	edges, err := servedCatalogLadder(db, tenantID).edges(ctx, "harborcare-demo")
	if err != nil {
		t.Fatalf("read the published ladder: %v", err)
	}
	if len(edges) == 0 {
		t.Fatal("no published ladder to price")
	}
	zone := demoworkforce.PayZones()[0]
	asOf, err := values.NewLocalDate(2026, time.September, 19)
	if err != nil {
		t.Fatalf("as-of date: %v", err)
	}
	lookup := func(jobCode, grade string) rewards.BandRecord {
		t.Helper()
		record, lookupErr := bands.LookupBand(ctx, rewards.BandQuery{
			Tenant: "harborcare-demo", JobCode: jobCode, Grade: grade, PayZone: zone, Currency: "USD", AsOf: asOf,
		})
		if lookupErr != nil {
			t.Fatalf("no stored band governs %s/%s/%s: %v", jobCode, grade, zone, lookupErr)
		}
		return record
	}

	for _, edge := range edges {
		source := lookup(edge.SourceJobCode, edge.SourceGrade)
		target := lookup(edge.TargetJobCode, edge.TargetGrade)
		if source.CatalogVersion != demoworkforce.PayBandPolicyVersion {
			t.Fatalf("band for %s pins catalog %q", edge.SourceJobCode, source.CatalogVersion)
		}
		if ratOf(t, target.Band.Minimum.Amount().String()).Cmp(ratOf(t, source.Band.Minimum.Amount().String())) <= 0 {
			t.Errorf("%s -> %s does not raise the band minimum: %s vs %s",
				edge.SourceJobCode, edge.TargetJobCode, target.Band.Minimum.Amount().String(), source.Band.Minimum.Amount().String())
		}
		// A proposal has to exist: the ladder admits [1+min, 1+max] x the
		// worker's midpoint pay, and the target band admits [minimum,
		// maximum]. An edge whose two windows do not meet is an option the
		// preflight would always refuse.
		pay := ratOf(t, source.Band.Midpoint.Amount().String())
		ladderLow := new(big.Rat).Mul(pay, onePlus(t, edge.MinimumBaseIncrease))
		ladderHigh := new(big.Rat).Mul(pay, onePlus(t, edge.MaximumBaseIncrease))
		bandLow, bandHigh := ratOf(t, target.Band.Minimum.Amount().String()), ratOf(t, target.Band.Maximum.Amount().String())
		low, high := maxRat(ladderLow, bandLow), minRat(ladderHigh, bandHigh)
		if low.Cmp(high) > 0 {
			t.Errorf("%s -> %s admits no proposable base: the ladder allows [%s, %s] and the stored band [%s, %s]",
				edge.SourceJobCode, edge.TargetJobCode,
				ladderLow.FloatString(2), ladderHigh.FloatString(2), bandLow.FloatString(2), bandHigh.FloatString(2))
		}
	}
}

// TestServedAggregateCatalogOpensVacanciesForEveryPublishedTarget proves the
// catalog the bootstrap records keeps up with a ladder that publishes several
// targets per job: every published target has a job row and OPEN positions in
// the organization unit its edge applies to, so any of them can be proposed
// and committed.
func TestServedAggregateCatalogOpensVacanciesForEveryPublishedTarget(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := servedCatalogTenant(t, db)
	edges, err := servedCatalogLadder(db, tenantID).edges(ctx, "harborcare-demo")
	if err != nil {
		t.Fatalf("read the published ladder: %v", err)
	}
	catalog, err := PromotionAggregateCatalogFrom(servedCatalogSeededAt, edges)
	if err != nil {
		t.Fatalf("PromotionAggregateCatalogFrom: %v", err)
	}
	vacancies := map[string]bool{}
	for _, vacancy := range catalog.Vacancies {
		vacancies[vacancy.OrgUnit+"|"+vacancy.JobCode+"|"+vacancy.Grade] = true
	}
	jobs := map[string]bool{}
	for _, job := range catalog.Jobs {
		jobs[job.Code+"|"+job.Grade] = true
	}
	// A seat opens in the unit its JOB belongs to, not in the unit the
	// promotion is proposed from: a Director of Product vacancy inside
	// Engineering Platform is the defect this assertion refuses.
	perSource := map[string]int{}
	for _, edge := range edges {
		perSource[edge.OrgUnit+"|"+edge.SourceJobCode]++
		home := demoworkforce.JobHomeUnit(edge.TargetJobCode)
		if home == "" {
			t.Errorf("published target %s is placed in no organization unit", edge.TargetJobCode)
			continue
		}
		if !vacancies[home+"|"+edge.TargetJobCode+"|"+edge.TargetGrade] {
			t.Errorf("published target %s/%s has no OPEN vacancy in %s, the unit it belongs to", edge.TargetJobCode, edge.TargetGrade, home)
		}
		if edge.OrgUnit != home && vacancies[edge.OrgUnit+"|"+edge.TargetJobCode+"|"+edge.TargetGrade] {
			t.Errorf("%s opens a %s vacancy in %s, which is the source's unit and not the job's",
				edge.SourceJobCode, edge.TargetJobCode, edge.OrgUnit)
		}
		if !jobs[edge.TargetJobCode+"|"+edge.TargetGrade] || !jobs[edge.SourceJobCode+"|"+edge.SourceGrade] {
			t.Errorf("edge %s -> %s is missing a catalog job row", edge.SourceJobCode, edge.TargetJobCode)
		}
	}
	// Every vacancy the catalog opens for a job this company publishes sits
	// in that job's own unit; nothing is placed somewhere it has nothing to
	// do with.
	for _, vacancy := range catalog.Vacancies {
		home := demoworkforce.JobHomeUnit(vacancy.JobCode)
		if home != "" && vacancy.OrgUnit != home {
			t.Errorf("the catalog opens a %s (%s) vacancy in %s, but that job belongs to %s",
				vacancy.JobCode, vacancy.Grade, vacancy.OrgUnit, home)
		}
	}
	multi := 0
	for _, count := range perSource {
		if count > 1 {
			multi++
		}
	}
	if multi == 0 {
		t.Fatal("no source job published more than one target; this test would prove nothing about several targets")
	}
	if demoworkforce.VacanciesPerTarget < 1 {
		t.Fatal("the catalog opens no position per target")
	}
	t.Logf("the catalog opens %d vacancy scopes for %d published edges, %d positions in all",
		len(catalog.Vacancies), len(edges), len(catalog.Vacancies)*demoworkforce.VacanciesPerTarget)
}

func ratOf(t *testing.T, decimal string) *big.Rat {
	t.Helper()
	value, ok := new(big.Rat).SetString(decimal)
	if !ok {
		t.Fatalf("%q is not a decimal", decimal)
	}
	return value
}

func onePlus(t *testing.T, fraction string) *big.Rat {
	t.Helper()
	return new(big.Rat).Add(big.NewRat(1, 1), ratOf(t, fraction))
}

func maxRat(a, b *big.Rat) *big.Rat {
	if a.Cmp(b) >= 0 {
		return a
	}
	return b
}

func minRat(a, b *big.Rat) *big.Rat {
	if a.Cmp(b) <= 0 {
		return a
	}
	return b
}
