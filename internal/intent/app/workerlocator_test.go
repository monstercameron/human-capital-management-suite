package app

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestCorpusWorkerLocatorResolvesKeysAndIdentifiers proves the corpus locator
// answers both spellings a reference arrives in, and refuses a string that is
// neither -- which is what stops a typo from being proposed as a promotion for
// nobody.
func TestCorpusWorkerLocatorResolvesKeysAndIdentifiers(t *testing.T) {
	ctx := context.Background()
	ref, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("fixtures.WorkerRef: %v", err)
	}

	for name, input := range map[string]string{
		"by corpus key": "omar-reyes",
		"by entity id":  ref.Id,
	} {
		t.Run(name, func(t *testing.T) {
			got, ok, err := corpusWorkerLocator(ctx, fixtures.Tenant, input)
			if err != nil || !ok {
				t.Fatalf("corpusWorkerLocator(%q) = %v, ok=%v, err=%v", input, got, ok, err)
			}
			if got.Ref.Id != ref.Id {
				t.Errorf("resolved id = %q, want %q", got.Ref.Id, ref.Id)
			}
			if got.Key != "omar-reyes" {
				t.Errorf("resolved key = %q, want the corpus key", got.Key)
			}
			if got.Created != nil {
				t.Error("a corpus worker carries a created row")
			}
			if got.Ref.Validate() != nil {
				t.Errorf("resolved reference %+v is not well formed", got.Ref)
			}
		})
	}

	for name, input := range map[string]string{
		"empty":                 "",
		"blank":                 "   ",
		"a created worker key":  "lena-01a0694b",
		"not an identifier":     "who?",
		"an uppercase uuid":     "11111111-1111-4111-8111-11111111111A",
		"a number that is not":  "12345",
		"a name somebody typed": "Omar Reyes",
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			if _, ok, err := corpusWorkerLocator(ctx, fixtures.Tenant, input); ok || err != nil {
				t.Fatalf("corpusWorkerLocator(%q) resolved (ok=%v err=%v)", input, ok, err)
			}
		})
	}
}

// TestCorpusWorkerLocatorAcceptsAWellFormedUnknownIdentifier pins the
// deliberate permissive case: a caller holding a raw entity id still resolves,
// and it is the governed read -- not resolution -- that reports the worker is
// not disclosed. Removing this would turn a disclosure decision into a
// resolution decision, which is exactly the confusion the read model exists to
// prevent.
func TestCorpusWorkerLocatorAcceptsAWellFormedUnknownIdentifier(t *testing.T) {
	unknown := uuid.NewString()
	got, ok, err := corpusWorkerLocator(context.Background(), fixtures.Tenant, unknown)
	if err != nil || !ok {
		t.Fatalf("corpusWorkerLocator(unknown id) = ok=%v err=%v", ok, err)
	}
	if got.Ref.Id != unknown || got.Key != unknown {
		t.Fatalf("resolved %+v, want the identifier itself", got)
	}
	if got.Ref.Kind != people.KindWorker || got.Ref.Tenant != fixtures.Tenant {
		t.Fatalf("resolved reference = %+v, want a worker in the named tenant", got.Ref)
	}
}

// TestNewWorkerLocatorWithoutADatabaseIsTheCorpusLocator proves the
// composition condition: a cell with no execution database has no created
// population, and resolution behaves exactly as it did before one existed.
func TestNewWorkerLocatorWithoutADatabaseIsTheCorpusLocator(t *testing.T) {
	ctx := context.Background()
	for name, locate := range map[string]WorkerLocator{
		"no database":   newWorkerLocator(nil, func(values.TenantId) uuid.UUID { return uuid.New() }),
		"no tenant map": newWorkerLocator(nil, nil),
	} {
		t.Run(name, func(t *testing.T) {
			got, ok, err := locate(ctx, fixtures.Tenant, "omar-reyes")
			if err != nil || !ok {
				t.Fatalf("locate(corpus key) = ok=%v err=%v", ok, err)
			}
			if got.Key != "omar-reyes" || got.Created != nil {
				t.Fatalf("resolved %+v, want the corpus worker", got)
			}
			if _, ok, _ := locate(ctx, fixtures.Tenant, "lena-01a0694b"); ok {
				t.Fatal("a created worker key resolved on a cell with no created population")
			}
		})
	}
}

// TestLocateCorpusWorkerExactHasNoPermissiveFallback proves the split the
// layered locator depends on: the exact pass must not claim a reference the
// created population might own, or a created worker's key would be answered as
// an unknown entity id before the database was ever asked.
func TestLocateCorpusWorkerExactHasNoPermissiveFallback(t *testing.T) {
	unknown := uuid.NewString()
	if _, ok, err := locateCorpusWorkerExact(fixtures.Tenant, unknown); ok || err != nil {
		t.Fatalf("locateCorpusWorkerExact(unknown id) resolved (ok=%v err=%v)", ok, err)
	}
	if _, ok, err := locateCorpusWorkerExact(fixtures.Tenant, "lena-01a0694b"); ok || err != nil {
		t.Fatalf("locateCorpusWorkerExact(created key) resolved (ok=%v err=%v)", ok, err)
	}
	got, ok, err := locateCorpusWorkerExact(fixtures.Tenant, "omar-reyes")
	if err != nil || !ok || got.Key != "omar-reyes" {
		t.Fatalf("locateCorpusWorkerExact(corpus key) = %+v ok=%v err=%v", got, ok, err)
	}
}

// TestWorkspaceWorkerStillResolvesTheCorpus guards the one caller left on the
// corpus-only helper: the stored-intent summary projection, which has no
// context or database and whose worker id is overwritten from the intent's own
// EMPLOYMENT subject anyway.
func TestWorkspaceWorkerStillResolvesTheCorpus(t *testing.T) {
	got, ok := workspaceWorker("omar-reyes")
	if !ok {
		t.Fatal("workspaceWorker no longer resolves a corpus key")
	}
	if got.Tenant != fixtures.Tenant || got.Kind != people.KindWorker {
		t.Fatalf("workspaceWorker resolved %+v", got)
	}
	if _, ok := workspaceWorker("lena-01a0694b"); ok {
		t.Fatal("workspaceWorker claimed a created worker key it cannot resolve")
	}
}

// TestLayeredLocatorServesTheCorpusOnlyToATenantWithNoPopulation is
// PROMOUX-015's resolution rule, the other half of ListWorkers' own: a tenant
// with people of its own resolves only those people, while a tenant that has
// created nobody still resolves the release's fixed corpus.
//
// The permissive identifier case survives both ways round, because a caller
// holding a raw worker id must still resolve whatever the tenant's population
// looks like.
func TestLayeredLocatorServesTheCorpusOnlyToATenantWithNoPopulation(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'worker locator tenant', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenantID, "worker-locator-"+tenantID.String()[:8])
	locate := newWorkerLocator(db.Conn, func(values.TenantId) uuid.UUID { return tenantID })

	corpusRef, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("fixtures.WorkerRef: %v", err)
	}
	unknownID := uuid.NewString()

	// An empty tenant: the corpus is this cell's population.
	got, ok, err := locate(ctx, fixtures.Tenant, "omar-reyes")
	if err != nil || !ok || got.Key != "omar-reyes" {
		t.Fatalf("an empty tenant resolved the corpus key as %+v (ok=%v err=%v)", got, ok, err)
	}
	if got, ok, err := locate(ctx, fixtures.Tenant, corpusRef.Id); err != nil || !ok || got.Key != "omar-reyes" {
		t.Fatalf("an empty tenant resolved the corpus id as %+v (ok=%v err=%v)", got, ok, err)
	}

	// Give the tenant one worker of its own.
	created := workerLocatorRow(tenantID)
	locatorInsert(t, db, tenantID, created)

	if got, ok, err := locate(ctx, fixtures.Tenant, created.WorkerKey); err != nil || !ok || got.Created == nil {
		t.Fatalf("the tenant's own worker resolved as %+v (ok=%v err=%v)", got, ok, err)
	}
	displayRef := journeyWorkerKey(created.PreferredName, created.LegalName, shortID(created.WorkerID))
	if got, ok, err := locate(ctx, fixtures.Tenant, displayRef); err != nil || !ok || got.Created == nil || got.Ref.Id != created.WorkerID.String() {
		t.Fatalf("the tenant's display slug %q did not resolve to its opaque identity: %+v (ok=%v err=%v)", displayRef, got, ok, err)
	}
	if got, ok, err := locate(ctx, fixtures.Tenant, "omar-reyes"); ok || err != nil {
		t.Fatalf("a populated tenant resolved the corpus key %+v (ok=%v err=%v); the directory does not offer that person",
			got, ok, err)
	}
	// A corpus entity id is still a well-formed worker identifier, so the
	// permissive case answers it -- but as a bare reference this cell has not
	// been told about, never as the corpus worker's own key.
	got, ok, err = locate(ctx, fixtures.Tenant, corpusRef.Id)
	if err != nil || !ok {
		t.Fatalf("a populated tenant refused a well-formed identifier (ok=%v err=%v)", ok, err)
	}
	if got.Key == "omar-reyes" {
		t.Errorf("a populated tenant resolved %s to the corpus worker's key", corpusRef.Id)
	}
	if got, ok, err := locate(ctx, fixtures.Tenant, unknownID); err != nil || !ok || got.Key != unknownID {
		t.Fatalf("a populated tenant resolved an unknown identifier as %+v (ok=%v err=%v)", got, ok, err)
	}
	if _, ok, err := locate(ctx, fixtures.Tenant, "not a reference at all"); ok || err != nil {
		t.Fatalf("a populated tenant resolved a typo (ok=%v err=%v)", ok, err)
	}
}

// workerLocatorRow is one durable worker for the tenant under test.
func workerLocatorRow(tenantID uuid.UUID) workforce.WorkerRow {
	recorded := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	id := uuid.New()
	return workforce.WorkerRow{
		TenantID: tenantID, WorkerID: id, WorkerKey: id.String(),
		LegalName: "Locator Worker", PreferredName: "Locator", WorkerNumber: "W-LOC-1",
		WorkerType: "employee", LifecycleStatus: "active", EmploymentID: "emp_loc", AssignmentID: "asg_loc",
		JobCode: "OPS-HRBP2", JobTitle: "People Partner", Grade: "P2", OrgUnit: "people-ops",
		PositionID: "POS-LOC-1", Location: "Boston, MA", PayZone: "US-EAST", FTE: "1.0000",
		HireDate: "2021-04-05", EffectiveFrom: "2021-04-05", BasePay: "90000.00", Currency: "USD",
		PayBasis: "ANNUAL_SALARY", BonusTarget: "0.0500", ManagerRelationshipRef: "rel_mgr_loc",
		RevisionStream: "people.worker." + id.String(), RevisionSequence: 1,
		KnownAt: recorded, RecordedAt: recorded, CreatedBy: "test", Source: workforce.SourceCreated,
	}
}

func locatorInsert(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, row workforce.WorkerRow) {
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
	if _, err := (workforce.Store{}).Create(ctx, tx, row); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}
