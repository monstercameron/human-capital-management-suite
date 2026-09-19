package compfacts_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/compfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/payband"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func seedCompfactsTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenantID, key, "compfacts test tenant "+key)
	return tenantID
}

// seededPackage creates a compensation package with one base-pay component
// for workerID and returns the component's entity id and digest.
func seededPackage(t *testing.T, db *pgtest.DB, tenantID, workerID uuid.UUID, frequency, amount string) (uuid.UUID, string) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Recorded in June, effective since January: the June coordinate below
	// reads current facts whose knowledge order holds.
	recorded := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	// The package's worker_ref foreign-keys to aggregate_entity: register
	// the synthetic worker id the way the aggregates package's own tests
	// do rather than building a full worker row this adapter never reads.
	db.Exec(t, `
		INSERT INTO aggregate_entity (tenant_id, entity_id, kind, canonical_id)
		VALUES ($1, $2, 'worker', $3)`,
		tenantID, workerID, "eid:v1:worker:"+workerID.String())
	packageID := uuid.New()
	pkg, err := aggregates.NewCompensationPackage(tenantID, packageID, workerID, nil, nil, from, nil, recorded, "USD")
	if err != nil {
		t.Fatalf("NewCompensationPackage: %v", err)
	}
	if _, err := (aggregates.CompensationStore{}).PutCompensationPackage(ctx, tx, pkg); err != nil {
		t.Fatalf("PutCompensationPackage: %v", err)
	}
	pay, err := values.NewMoney(amount, "USD", 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	componentID := uuid.New()
	component, err := aggregates.NewCompensationComponent(tenantID, componentID, packageID, from, nil, recorded, "BASE_PAY", pay, frequency)
	if err != nil {
		t.Fatalf("NewCompensationComponent: %v", err)
	}
	rowID, err := (aggregates.CompensationStore{}).PutCompensationComponent(ctx, tx, component)
	if err != nil {
		t.Fatalf("PutCompensationComponent: %v", err)
	}
	_ = rowID
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	stored, err := readComponent(t, db, tenantID, componentID)
	if err != nil {
		t.Fatalf("read back component: %v", err)
	}
	return componentID, stored.Digest
}

func readComponent(t *testing.T, db *pgtest.DB, tenantID, componentID uuid.UUID) (aggregates.CompensationComponent, error) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	return (aggregates.CompensationStore{}).CurrentCompensationComponent(ctx, tx, tenantID, componentID, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
}

// stubWorkerFacts answers the placement projection from fixed strings.
type stubWorkerFacts struct {
	job, grade, zone string
	exists           bool
}

func (s stubWorkerFacts) WorkerFactsAt(_ context.Context, q people.FactQuery) (people.FactSet, error) {
	if !s.exists {
		return people.FactSet{Worker: q.Worker}, nil
	}
	return people.FactSet{
		Worker: q.Worker,
		Exists: true,
		Facts: []people.Fact{
			{Field: people.FieldJobCode, Value: values.Value(s.job)},
			{Field: people.FieldGrade, Value: values.Value(s.grade)},
			{Field: people.FieldPayZone, Value: values.Value(s.zone)},
		},
	}, nil
}

// stubBandCatalog answers one band code for any query.
type stubBandCatalog struct {
	code string
}

func (s stubBandCatalog) LookupBand(_ context.Context, _ rewards.BandQuery) (rewards.BandRecord, error) {
	if s.code == "" {
		return rewards.BandRecord{}, rewards.ErrBandNotFound
	}
	return rewards.BandRecord{Band: payband.Band{ID: s.code}}, nil
}

func compfactsAsOf(t *testing.T) values.Instant {
	t.Helper()
	return values.NewInstant(time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))
}

func compfactsReader(db *pgtest.DB, tenantID uuid.UUID) compfacts.Reader {
	return compfacts.Reader{
		DB:            db.Conn,
		TenantUUID:    func(values.TenantId) uuid.UUID { return tenantID },
		Worker:        stubWorkerFacts{job: "ENG-MGR", grade: "M1", zone: "USEAST", exists: true},
		Bands:         stubBandCatalog{code: "BAND-OPS-P2-USEAST"},
		PolicyVersion: "hcmnext.rewards.simulate_compensation/v1",
	}
}

// TestReaderResolvesARealSeededPackage proves the adapter reads a genuine
// compensation row -- base pay, currency, basis, frequency, band reference
// and row-bound revision and provenance -- rather than fabricating any of
// it, and that the set passes the port's own validation.
func TestReaderResolvesARealSeededPackage(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedCompfactsTenant(t, db, "compfacts-primary")
	tenant := values.TenantId("compfacts-primary")
	workerID := uuid.New()
	worker := values.EntityRef{Tenant: tenant, Kind: people.KindWorker, Id: workerID.String()}
	if _, err := uuid.Parse(worker.Id); err != nil {
		t.Fatalf("worker id: %v", err)
	}
	_, digest := seededPackage(t, db, tenantID, workerID, "MONTHLY", "8500.0000")

	set, err := compfactsReader(db, tenantID).CompensationFactsAt(context.Background(), rewards.CompensationFactsQuery{
		Tenant: tenant, Worker: worker, AsOf: compfactsAsOf(t),
	})
	if err != nil {
		t.Fatalf("CompensationFactsAt: %v", err)
	}
	if !set.Exists {
		t.Fatal("Exists = false, want true for the seeded package")
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("set.Validate: %v", err)
	}
	if got := set.Fact.BasePay.String(); got != "8500.0000 USD" {
		t.Fatalf("BasePay = %s, want 8500.0000 USD", got)
	}
	if set.Fact.PayBasis != rewards.PayBasisMonthlySalary {
		t.Fatalf("PayBasis = %v, want monthly salary for a MONTHLY row", set.Fact.PayBasis)
	}
	if set.Fact.PayBandRef != "BAND-OPS-P2-USEAST" {
		t.Fatalf("PayBandRef = %q, want the catalog band code", set.Fact.PayBandRef)
	}
	if set.Fact.Currency != "USD" || set.Fact.Frequency != "MONTHLY" {
		t.Fatalf("currency/frequency = %s/%s, want USD/MONTHLY", set.Fact.Currency, set.Fact.Frequency)
	}
	if len(set.Fact.Components) != 1 || set.Fact.Components[0].Kind != "BASE_PAY" {
		t.Fatalf("Components = %+v, want the single BASE_PAY row", set.Fact.Components)
	}
	if set.Fact.Provenance.EvidenceRef != digest {
		t.Fatalf("Provenance.EvidenceRef = %q, want the row digest %q", set.Fact.Provenance.EvidenceRef, digest)
	}
	if set.PolicyVersion != "hcmnext.rewards.simulate_compensation/v1" {
		t.Fatalf("PolicyVersion = %q, want the served capability version", set.PolicyVersion)
	}
}

// TestReaderWithholdsUnmappableBasis proves a package whose frequency names
// no rewards pay basis (biweekly has no member in the closed basis
// vocabulary) is reported absent rather than disclosed with a guessed basis.
func TestReaderWithholdsUnmappableBasis(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedCompfactsTenant(t, db, "compfacts-basis")
	tenant := values.TenantId("compfacts-basis")
	workerID := uuid.New()
	worker := values.EntityRef{Tenant: tenant, Kind: people.KindWorker, Id: workerID.String()}
	seededPackage(t, db, tenantID, workerID, "BIWEEKLY", "3250.0000")

	set, err := compfactsReader(db, tenantID).CompensationFactsAt(context.Background(), rewards.CompensationFactsQuery{
		Tenant: tenant, Worker: worker, AsOf: compfactsAsOf(t),
	})
	if err != nil {
		t.Fatalf("CompensationFactsAt: %v", err)
	}
	if set.Exists {
		t.Fatal("Exists = true for a biweekly row, want absent: the basis vocabulary cannot name it")
	}
}

// TestReaderAbsentWithoutPackage proves a worker with no package reads as
// absent, not as an error.
func TestReaderAbsentWithoutPackage(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedCompfactsTenant(t, db, "compfacts-empty")
	tenant := values.TenantId("compfacts-empty")
	worker := values.EntityRef{Tenant: tenant, Kind: people.KindWorker, Id: uuid.NewString()}

	set, err := compfactsReader(db, tenantID).CompensationFactsAt(context.Background(), rewards.CompensationFactsQuery{
		Tenant: tenant, Worker: worker, AsOf: compfactsAsOf(t),
	})
	if err != nil {
		t.Fatalf("CompensationFactsAt: %v", err)
	}
	if set.Exists {
		t.Fatal("Exists = true with no package rows, want absent")
	}
}

// TestReaderIsolatesTenants proves one tenant's worker id never discloses
// another tenant's package.
func TestReaderIsolatesTenants(t *testing.T) {
	db := pgtest.New(t)
	tenantAID := seedCompfactsTenant(t, db, "compfacts-tenant-a")
	tenantBID := seedCompfactsTenant(t, db, "compfacts-tenant-b")
	workerID := uuid.New()
	seededPackage(t, db, tenantAID, workerID, "MONTHLY", "8500.0000")

	other := values.EntityRef{Tenant: values.TenantId("compfacts-tenant-b"), Kind: people.KindWorker, Id: workerID.String()}
	set, err := compfactsReader(db, tenantBID).CompensationFactsAt(context.Background(), rewards.CompensationFactsQuery{
		Tenant: values.TenantId("compfacts-tenant-b"), Worker: other, AsOf: compfactsAsOf(t),
	})
	if err != nil {
		t.Fatalf("CompensationFactsAt: %v", err)
	}
	if set.Exists {
		t.Fatal("Exists = true across tenants for the same worker id, want isolated")
	}
}

// TestReaderRefusesWithoutConfiguration proves an unwired reader fails
// closed instead of reading.
func TestReaderRefusesWithoutConfiguration(t *testing.T) {
	if _, err := (compfacts.Reader{}).CompensationFactsAt(context.Background(), rewards.CompensationFactsQuery{}); err == nil {
		t.Fatal("unconfigured reader returned nil error, want fail-closed")
	}
}

// errUnit4Down is the downstream outage the failure tests inject.
var errUnit4Down = errors.New("unit4: downstream projection unavailable")

// failingWorkerFacts stands in for a placement projection that is down.
type failingWorkerFacts struct{ err error }

func (s failingWorkerFacts) WorkerFactsAt(context.Context, people.FactQuery) (people.FactSet, error) {
	return people.FactSet{}, s.err
}

// failingBandCatalog stands in for a band catalog that is down.
type failingBandCatalog struct{ err error }

func (s failingBandCatalog) LookupBand(context.Context, rewards.BandQuery) (rewards.BandRecord, error) {
	return rewards.BandRecord{}, s.err
}

// seedMonthlyPackage stores a disclosable monthly package for a fresh
// worker and returns its query worker ref.
func seedMonthlyPackage(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, tenant values.TenantId) values.EntityRef {
	t.Helper()
	workerID := uuid.New()
	worker := values.EntityRef{Tenant: tenant, Kind: people.KindWorker, Id: workerID.String()}
	seededPackage(t, db, tenantID, workerID, "MONTHLY", "8500.0000")
	return worker
}

// TestTodo_Unit4_PlacementFailureIsAnError proves a placement projection
// failure surfaces as an error, not as a silent absent fact: "unknown" and
// "the read failed" decide differently downstream.
func TestTodo_Unit4_PlacementFailureIsAnError(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedCompfactsTenant(t, db, "compfacts-placement-err")
	tenant := values.TenantId("compfacts-placement-err")
	worker := seedMonthlyPackage(t, db, tenantID, tenant)

	reader := compfactsReader(db, tenantID)
	reader.Worker = failingWorkerFacts{err: errUnit4Down}
	if _, err := reader.CompensationFactsAt(context.Background(), rewards.CompensationFactsQuery{
		Tenant: tenant, Worker: worker, AsOf: compfactsAsOf(t),
	}); err == nil {
		t.Fatal("placement projection down: want error, not a silent absent fact")
	}
}

// TestTodo_Unit4_BandLookupFailureIsAnError proves a band catalog failure
// that is not "no such band" surfaces as an error rather than masquerading
// as an absent band.
func TestTodo_Unit4_BandLookupFailureIsAnError(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedCompfactsTenant(t, db, "compfacts-band-err")
	tenant := values.TenantId("compfacts-band-err")
	worker := seedMonthlyPackage(t, db, tenantID, tenant)

	reader := compfactsReader(db, tenantID)
	reader.Bands = failingBandCatalog{err: errUnit4Down}
	if _, err := reader.CompensationFactsAt(context.Background(), rewards.CompensationFactsQuery{
		Tenant: tenant, Worker: worker, AsOf: compfactsAsOf(t),
	}); err == nil {
		t.Fatal("band catalog down: want error, not an absent band")
	}
}
