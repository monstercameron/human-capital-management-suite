package workforce_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// factsTenant is the tenant key the fact queries below are scoped to. The
// mapping onto a uuid is the seam CellConfig.TenantUUID fills in production;
// here it is a closure over one seeded tenant, which is the same shape.
const factsTenant values.TenantId = "workforce-facts-tenant"

// factQuery builds a well-formed query for one worker over every field the
// created population can answer.
func factQuery(worker values.EntityRef) people.FactQuery {
	known, err := values.NewKnownAt(values.NewInstant(fixedInstant))
	if err != nil {
		panic(err)
	}
	effective, err := values.ParseLocalDate("2026-06-01")
	if err != nil {
		panic(err)
	}
	return people.FactQuery{
		Tenant: worker.Tenant,
		Worker: worker,
		AsOf:   people.AsOf{EffectiveOn: effective, KnownAt: known},
		Fields: people.AllFields(),
	}
}

// stubFacts is a people.WorkerFacts that answers about exactly one worker. It
// stands in for the corpus reader so the layering order can be proven without
// dragging the fixture corpus into a data-plane test.
type stubFacts struct {
	id        string
	value     string
	err       error
	callCount *int
}

func (s stubFacts) WorkerFactsAt(_ context.Context, q people.FactQuery) (people.FactSet, error) {
	if s.callCount != nil {
		*s.callCount++
	}
	if s.err != nil {
		return people.FactSet{}, s.err
	}
	if q.Worker.Id != s.id {
		return people.FactSet{Worker: q.Worker, Exists: false}, nil
	}
	revision, err := values.NewSequenceRevision("stub.worker", 1)
	if err != nil {
		return people.FactSet{}, err
	}
	return people.FactSet{
		Worker: q.Worker, Exists: true, Watermark: revision,
		Facts: []people.Fact{{Field: people.FieldLegalName, Value: values.Value(s.value)}},
	}, nil
}

// appBeginner is the dbport.Beginner a Facts reader is composed over: a
// connection that has assumed the least-privilege role, so every read it opens
// is subject to migration 00023's row level security policy exactly as a
// composed cell's pool is.
func appBeginner(t *testing.T, db *pgtest.DB) dbport.Beginner {
	t.Helper()
	return appConn(t, db)
}

// seedWorker stands up a tenant, inserts one created worker and returns the
// row plus the tenant-uuid mapping a Facts reader is composed with.
func seedWorker(t *testing.T, key string) (*pgtest.DB, uuid.UUID, workforce.WorkerRow) {
	t.Helper()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, key)
	conn := appConn(t, db)
	row := newRow(tenant, key)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := workforce.Store{}.Create(context.Background(), tx, row)
		return err
	})
	return db, tenant, row
}

func tenantMap(tenant uuid.UUID) func(values.TenantId) uuid.UUID {
	return func(values.TenantId) uuid.UUID { return tenant }
}

// TestFactsProjectEveryFieldTheCorpusReaderProjects is the read half's
// contract: a created worker's fact set carries the same coordinates a corpus
// worker's does, so people.ExplainWorkerState cannot tell them apart.
func TestFactsProjectEveryFieldTheCorpusReaderProjects(t *testing.T) {
	t.Parallel()
	db, tenant, row := seedWorker(t, "facts-projection")
	facts := workforce.NewFacts(appBeginner(t, db), tenantMap(tenant))

	worker := values.EntityRef{Tenant: factsTenant, Kind: people.KindWorker, Id: row.WorkerID.String()}
	set, err := facts.WorkerFactsAt(context.Background(), factQuery(worker))
	if err != nil {
		t.Fatalf("WorkerFactsAt: %v", err)
	}
	if !set.Exists {
		t.Fatal("a created worker is reported as non-existent")
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("the projected fact set is not disclosable: %v", err)
	}
	if len(set.Facts) != len(people.AllFields()) {
		t.Fatalf("projected %d facts, want the %d requested", len(set.Facts), len(people.AllFields()))
	}
	if !set.Watermark.IsSpecified() {
		t.Error("the fact set carries no read watermark")
	}

	want := map[people.FieldID]string{
		people.FieldLegalName:        row.LegalName,
		people.FieldPreferredName:    row.PreferredName,
		people.FieldWorkerNumber:     row.WorkerNumber,
		people.FieldLifecycleStatus:  row.LifecycleStatus,
		people.FieldEmploymentStatus: row.LifecycleStatus,
		people.FieldWorkerType:       row.WorkerType,
		people.FieldHireDate:         row.HireDate,
		people.FieldEmploymentID:     row.EmploymentID,
		people.FieldAssignmentID:     row.AssignmentID,
		people.FieldJobCode:          row.JobCode,
		people.FieldGrade:            row.Grade,
		people.FieldOrgUnit:          row.OrgUnit,
		people.FieldPositionID:       row.PositionID,
		people.FieldLocation:         row.Location,
		people.FieldPayZone:          row.PayZone,
		people.FieldFTE:              row.FTE,
		people.FieldManagerRelation:  row.ManagerRelationshipRef,
	}
	for field, expected := range want {
		fact, ok := set.Lookup(field)
		if !ok {
			t.Errorf("no fact for %s", field)
			continue
		}
		got, present := fact.Value.Get()
		if !present || got != expected {
			t.Errorf("%s = %q (present=%v), want %q", field, got, present, expected)
		}
		if fact.Provenance.EvidenceRef != workforce.EvidenceRef(row) {
			t.Errorf("%s cites evidence %q, want %q", field, fact.Provenance.EvidenceRef, workforce.EvidenceRef(row))
		}
	}

	// The one field journey_worker has no column for is absent, not empty:
	// "nobody asserts this" and "you were shown nothing" are different answers.
	entity, ok := set.Lookup(people.FieldLegalEntity)
	if !ok {
		t.Fatal("legal entity was dropped from the projection rather than disclosed as absent")
	}
	if _, present := entity.Value.Get(); present {
		t.Errorf("legal entity = %v, want absent", entity.Value)
	}
}

// TestFactsNarrowToTheRequestedProjection proves the port's own rule: the
// reader answers exactly what it was asked for and never widens.
func TestFactsNarrowToTheRequestedProjection(t *testing.T) {
	t.Parallel()
	db, tenant, row := seedWorker(t, "facts-narrow")
	facts := workforce.NewFacts(appBeginner(t, db), tenantMap(tenant))

	worker := values.EntityRef{Tenant: factsTenant, Kind: people.KindWorker, Id: row.WorkerID.String()}
	q := factQuery(worker)
	q.Fields = []people.FieldID{people.FieldJobCode, people.FieldGrade}
	set, err := facts.WorkerFactsAt(context.Background(), q)
	if err != nil {
		t.Fatalf("WorkerFactsAt: %v", err)
	}
	if len(set.Facts) != 2 {
		t.Fatalf("projected %d facts for a two-field query: %+v", len(set.Facts), set.Facts)
	}
	if _, ok := set.Lookup(people.FieldPayZone); ok {
		t.Error("the reader disclosed a field the caller did not request")
	}
}

// TestFactsReportAbsenceRatherThanFailing proves the port's other rule: a
// worker that is not in this population is an answer, not a fault -- which is
// exactly what lets the layered reader ask the corpus first.
func TestFactsReportAbsenceRatherThanFailing(t *testing.T) {
	t.Parallel()
	db, tenant, _ := seedWorker(t, "facts-absence")
	facts := workforce.NewFacts(appBeginner(t, db), tenantMap(tenant))

	worker := values.EntityRef{Tenant: factsTenant, Kind: people.KindWorker, Id: uuid.NewString()}
	set, err := facts.WorkerFactsAt(context.Background(), factQuery(worker))
	if err != nil {
		t.Fatalf("WorkerFactsAt(unknown worker): %v", err)
	}
	if set.Exists || len(set.Facts) != 0 {
		t.Fatalf("an unknown worker answered %+v, want Exists=false with no facts", set)
	}
}

// TestFactsWithoutADatabaseAnswerAbsence proves the composition a cell with no
// execution database gets: there is no created population, so every worker is
// absent from it and nothing panics.
func TestFactsWithoutADatabaseAnswerAbsence(t *testing.T) {
	t.Parallel()
	worker := values.EntityRef{Tenant: factsTenant, Kind: people.KindWorker, Id: uuid.NewString()}
	for name, facts := range map[string]workforce.Facts{
		"no database":   workforce.NewFacts(nil, tenantMap(uuid.New())),
		"no tenant map": workforce.NewFacts(nil, nil),
	} {
		t.Run(name, func(t *testing.T) {
			set, err := facts.WorkerFactsAt(context.Background(), factQuery(worker))
			if err != nil {
				t.Fatalf("WorkerFactsAt: %v", err)
			}
			if set.Exists {
				t.Fatal("a reader with no database reported a worker")
			}
		})
	}
}

// TestFactsRefuseAnInvalidQuery proves the reader validates before it reads:
// a malformed query is the port's own refusal, not a database round trip.
func TestFactsRefuseAnInvalidQuery(t *testing.T) {
	t.Parallel()
	facts := workforce.NewFacts(nil, nil)
	worker := values.EntityRef{Tenant: factsTenant, Kind: people.KindWorker, Id: uuid.NewString()}
	q := factQuery(worker)
	q.Fields = nil
	if _, err := facts.WorkerFactsAt(context.Background(), q); !errors.Is(err, people.ErrNoFieldsRequested) {
		t.Fatalf("WorkerFactsAt(no fields) = %v, want ErrNoFieldsRequested", err)
	}
}

// TestLayeredWorkerFactsAnswerFromTheCorpusFirst is the layering contract: the
// corpus answer is final, the created population is only consulted when the
// corpus says the worker is not there, and a created worker can never shadow
// a corpus one.
func TestLayeredWorkerFactsAnswerFromTheCorpusFirst(t *testing.T) {
	t.Parallel()
	db, tenant, row := seedWorker(t, "facts-layered")
	pool := appBeginner(t, db)

	calls := 0
	primary := stubFacts{id: row.WorkerID.String(), value: "Corpus Wins", callCount: &calls}
	layered := workforce.NewLayeredWorkerFacts(primary, pool, tenantMap(tenant))

	// The corpus claims the very worker the created population also holds.
	worker := values.EntityRef{Tenant: factsTenant, Kind: people.KindWorker, Id: row.WorkerID.String()}
	q := factQuery(worker)
	q.Fields = []people.FieldID{people.FieldLegalName}
	set, err := layered.WorkerFactsAt(context.Background(), q)
	if err != nil {
		t.Fatalf("WorkerFactsAt: %v", err)
	}
	fact, ok := set.Lookup(people.FieldLegalName)
	if !ok {
		t.Fatal("no legal name in the layered answer")
	}
	if got, _ := fact.Value.Get(); got != "Corpus Wins" {
		t.Fatalf("legal name = %q, want the corpus answer", got)
	}

	// A worker the corpus does not know falls through to the created row.
	other := newRow(tenant, "facts-layered-second")
	inTenantTx(t, appConn(t, db), tenant, func(tx dbport.Tx) error {
		_, createErr := workforce.Store{}.Create(context.Background(), tx, other)
		return createErr
	})
	q.Worker.Id = other.WorkerID.String()
	q.Tenant = q.Worker.Tenant
	set, err = layered.WorkerFactsAt(context.Background(), q)
	if err != nil {
		t.Fatalf("WorkerFactsAt(created worker): %v", err)
	}
	fact, ok = set.Lookup(people.FieldLegalName)
	if !ok || !set.Exists {
		t.Fatalf("the created worker was not disclosed: %+v", set)
	}
	if got, _ := fact.Value.Get(); got != other.LegalName {
		t.Fatalf("legal name = %q, want %q", got, other.LegalName)
	}
	if calls != 2 {
		t.Errorf("the corpus reader was consulted %d times, want once per read", calls)
	}
}

// TestLayeredWorkerFactsPropagateACorpusFailure proves the layering never
// turns a read fault into a fallback: a corpus that failed did not say the
// worker is absent, and asking the created population anyway would answer a
// question nobody could still answer correctly.
func TestLayeredWorkerFactsPropagateACorpusFailure(t *testing.T) {
	t.Parallel()
	boom := errors.New("corpus unavailable")
	layered := workforce.NewLayeredWorkerFacts(stubFacts{err: boom}, nil, nil)
	worker := values.EntityRef{Tenant: factsTenant, Kind: people.KindWorker, Id: uuid.NewString()}
	if _, err := layered.WorkerFactsAt(context.Background(), factQuery(worker)); !errors.Is(err, boom) {
		t.Fatalf("WorkerFactsAt = %v, want the corpus failure", err)
	}
}

// TestLayeredWorkerFactsWithoutAPrimaryReadTheCreatedPopulation proves the
// degenerate composition still works, so a caller never has to supply a
// do-nothing corpus reader just to satisfy the constructor.
func TestLayeredWorkerFactsWithoutAPrimaryReadTheCreatedPopulation(t *testing.T) {
	t.Parallel()
	db, tenant, row := seedWorker(t, "facts-no-primary")
	layered := workforce.NewLayeredWorkerFacts(nil, appBeginner(t, db), tenantMap(tenant))
	worker := values.EntityRef{Tenant: factsTenant, Kind: people.KindWorker, Id: row.WorkerID.String()}
	set, err := layered.WorkerFactsAt(context.Background(), factQuery(worker))
	if err != nil {
		t.Fatalf("WorkerFactsAt: %v", err)
	}
	if !set.Exists {
		t.Fatal("the created worker was not disclosed")
	}
}

// TestDefaultCalendarIsTheDesignPartnerCalendar pins the calendar a created
// worker's effective interval is dated under. It is stated in this package
// rather than imported from the domain corpus, so a test has to say what it
// must equal.
func TestDefaultCalendarIsTheDesignPartnerCalendar(t *testing.T) {
	t.Parallel()
	if workforce.DefaultCalendar.Ref != "harborcare.us.business" || workforce.DefaultCalendar.Version != "2026.1" {
		t.Fatalf("DefaultCalendar = %+v, want the P1A design-partner calendar", workforce.DefaultCalendar)
	}
}

// TestFactsPopulatedReportsWhatTheCellCanSee proves the reader-level probe:
// a cell with no database or no tenant mapping answers false rather than
// failing, and a mapped tenant answers what its rows say.
func TestFactsPopulatedReportsWhatTheCellCanSee(t *testing.T) {
	ctx := context.Background()
	if populated, err := (workforce.Facts{}).Populated(ctx, factsTenant); err != nil || populated {
		t.Fatalf("a cell with no database = %t, %v; want false, nil", populated, err)
	}

	db := pgtest.New(t)
	tenant := insertTenant(t, db, "workforce-facts-populated")
	facts := workforce.NewFacts(db.Conn, func(values.TenantId) uuid.UUID { return tenant })
	if populated, err := facts.Populated(ctx, factsTenant); err != nil || populated {
		t.Fatalf("an empty tenant = %t, %v; want false, nil", populated, err)
	}

	conn := appConn(t, db)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := (workforce.Store{}).Create(ctx, tx, newRow(tenant, "facts-populated"))
		return err
	})
	if populated, err := facts.Populated(ctx, factsTenant); err != nil || !populated {
		t.Fatalf("a tenant with one created worker = %t, %v; want true, nil", populated, err)
	}

	unmapped := workforce.NewFacts(db.Conn, func(values.TenantId) uuid.UUID { return uuid.Nil })
	if populated, err := unmapped.Populated(ctx, factsTenant); err != nil || populated {
		t.Fatalf("a tenant with no physical mapping = %t, %v; want false, nil", populated, err)
	}
}
