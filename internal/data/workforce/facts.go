package workforce

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DefaultCalendar is the versioned business calendar a created worker's
// effective interval is dated under.
//
// It is stated here rather than read from internal/domains/fixtures because
// this package does not import that one -- the same boundary
// internal/data/seed and internal/data/aggregates hold, for the same reason: a
// data-plane package that reached into the domain test corpus would make the
// corpus a production dependency. The value is the P1A design-partner calendar
// the corpus is also dated under, which is what makes a created worker's
// interval comparable with a corpus worker's.
var DefaultCalendar = values.CalendarRef{Ref: "harborcare.us.business", Version: "2026.1"}

// Facts answers [people.WorkerFacts] from journey_worker.
//
// It opens and rolls back its own read transaction rather than taking a
// caller's executor, because a governed worker read has no caller-visible
// transaction to join: it is invoked through the capability gateway, several
// layers above anything that holds one. The transaction exists at all because
// journey_worker is row-level-security protected, so the tenant has to be
// established on the connection (internal/data/tenancy.WithTenant) before the
// SELECT runs.
type Facts struct {
	// DB opens the read transactions. Nil makes every read report "this
	// worker does not exist", which is the correct answer for a cell composed
	// with no execution database: there is no created population at all.
	DB dbport.Beginner
	// TenantUUID maps the query's tenant key onto the uuid the journey_worker
	// rows carry. Nil is treated the same way as a nil DB.
	TenantUUID func(values.TenantId) uuid.UUID
	// Calendar dates the effective interval. The zero value means
	// [DefaultCalendar].
	Calendar values.CalendarRef

	store Store
}

var _ people.WorkerFacts = Facts{}

// NewFacts builds the created-population reader.
func NewFacts(db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID) Facts {
	return Facts{DB: db, TenantUUID: tenantUUID, Calendar: DefaultCalendar}
}

// WorkerFactsAt implements [people.WorkerFacts].
//
// The projection is exactly q.Fields and never wider: a field the caller was
// not authorized for is a field this reader is never asked about, and
// answering one anyway would defeat the whole point of the port taking a
// projection at all. A field this row leaves unset -- every column
// migrations/00316 added is optional -- is disclosed as absent rather than
// omitted, so a caller can tell "nobody asserts a legal entity for this
// worker" apart from "you were not shown it".
func (f Facts) WorkerFactsAt(ctx context.Context, q people.FactQuery) (people.FactSet, error) {
	if err := q.Validate(); err != nil {
		return people.FactSet{}, err
	}
	if f.DB == nil || f.TenantUUID == nil {
		return people.FactSet{Worker: q.Worker, Exists: false}, nil
	}
	tenantID := f.TenantUUID(q.Tenant)
	if tenantID == uuid.Nil {
		return people.FactSet{Worker: q.Worker, Exists: false}, nil
	}

	row, found, err := f.read(ctx, tenantID, q.Worker.Id)
	if err != nil {
		return people.FactSet{}, fmt.Errorf("%w: %w", people.ErrReaderFailed, err)
	}
	if !found {
		return people.FactSet{Worker: q.Worker, Exists: false}, nil
	}
	return f.project(q, row)
}

// read loads one created worker inside its own tenant-scoped transaction. The
// transaction is always rolled back: this is a read, and a read that committed
// would be claiming to have changed something.
func (f Facts) read(ctx context.Context, tenantID uuid.UUID, workerID string) (WorkerRow, bool, error) {
	tx, err := f.DB.Begin(ctx)
	if err != nil {
		return WorkerRow{}, false, fmt.Errorf("workforce: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return WorkerRow{}, false, fmt.Errorf("workforce: scope tenant: %w", err)
	}
	return f.store.Get(ctx, tx, tenantID, workerID)
}

// project turns one stored row into the fact set the port promises.
//
// It mirrors fixtures.MemoryWorkerFacts.WorkerFactsAt step for step -- one
// revision token from the row's stream position, one open-ended effective
// interval from effective_from, the row's own known-at, one source authority
// and one provenance shared by every fact, and the revision as the set's read
// watermark -- because people.ExplainWorkerState must not be able to tell a
// created worker from a corpus one by the shape of the evidence it carries.
func (f Facts) project(q people.FactQuery, row WorkerRow) (people.FactSet, error) {
	revision, err := values.NewSequenceRevision(row.RevisionStream, row.RevisionSequence)
	if err != nil {
		return people.FactSet{}, err
	}
	effectiveFrom, err := values.ParseLocalDate(row.EffectiveFrom)
	if err != nil {
		return people.FactSet{}, err
	}
	calendar := f.Calendar
	if calendar.Ref == "" {
		calendar = DefaultCalendar
	}
	interval, err := values.NewOpenLocalDateInterval(effectiveFrom, calendar)
	if err != nil {
		return people.FactSet{}, err
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(row.KnownAt))
	if err != nil {
		return people.FactSet{}, err
	}
	recordedAt, err := values.NewRecordedAt(values.NewInstant(row.RecordedAt))
	if err != nil {
		return people.FactSet{}, err
	}

	authority := evidence.SourceAuthority{
		Kind:      evidence.AuthorityLocal,
		System:    SourceSystem,
		PolicyRef: AuthorityPolicy,
	}
	provenance := evidence.Provenance{
		Source:      SourceSystem,
		EvidenceRef: EvidenceRef(row),
		RecordedAt:  recordedAt,
	}

	all := FieldValues(row)
	facts := make([]people.Fact, 0, len(q.Fields))
	for _, field := range q.Fields {
		raw, present := all[field]
		value := values.Value(raw)
		if !present || raw == "" {
			value = values.Absent[string]()
		}
		facts = append(facts, people.Fact{
			Field:      field,
			Value:      value,
			Effective:  interval,
			KnownAt:    knownAt,
			Revision:   revision,
			Authority:  authority,
			Provenance: provenance,
		})
	}
	return people.FactSet{
		Worker:    q.Worker,
		Exists:    true,
		Facts:     facts,
		Watermark: revision,
	}, nil
}

// EvidenceRef is the immutable artifact reference a created worker's facts
// cite. It is derived from the row's own identity and revision position rather
// than minted per read, so two reads of the same worker cite the same
// evidence.
func EvidenceRef(row WorkerRow) string {
	return fmt.Sprintf("evd_journey_worker_%s_r%d", row.WorkerID, row.RevisionSequence)
}

// FieldValues is the created worker's field values keyed by
// [people.FieldID], the same mapping fixtures.MemoryWorkerFacts builds over
// the corpus record.
//
// people.FieldLegalEntity is answered from the row's company, the employing
// legal entity migrations/00316 gave this table a column for. The column is
// optional, so a row that names no company still projects the fact as absent
// rather than as an empty string pretending to be an answer -- which is the
// distinction [Facts.project] draws from an empty value here.
//
// The other five facts that migration added -- employment and time type,
// business unit, cost center, work arrangement -- have no people.FieldID and
// deliberately get none. [people.FieldID] is the closed projection the
// promotion authorization contract is written against; widening it would
// re-open that contract for facts no governed read asks for. They travel to
// the object page on the worker listing row instead.
func FieldValues(row WorkerRow) map[people.FieldID]string {
	return map[people.FieldID]string{
		people.FieldWorkerNumber:    row.WorkerNumber,
		people.FieldLifecycleStatus: row.LifecycleStatus,
		people.FieldLegalName:       row.LegalName,
		people.FieldPreferredName:   row.PreferredName,
		people.FieldEmploymentID:    row.EmploymentID,
		people.FieldWorkerType:      row.WorkerType,
		people.FieldLegalEntity:     row.Company,
		people.FieldHireDate:        row.HireDate,
		// journey_worker carries one lifecycle token; a created worker's
		// employment status is that same token, because a created worker
		// whose employment had already ended is not a worker this surface
		// can create.
		people.FieldEmploymentStatus: row.LifecycleStatus,
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
}

// layeredWorkerFacts answers from the corpus first and the created population
// second.
type layeredWorkerFacts struct {
	primary  people.WorkerFacts
	fallback Facts
}

var _ people.WorkerFacts = layeredWorkerFacts{}

// NewLayeredWorkerFacts composes the corpus reader and the created-population
// reader into the single governed worker read a cell wires.
//
// The order is not arbitrary. The corpus is the authoritative population for
// every worker it knows, so it answers first and its answer is final; only
// when it reports Exists = false -- "there is no such worker here", which is
// an answer and not a fault -- is the created population asked. A created
// worker can therefore never shadow a corpus worker, and a corpus read never
// pays for a database round trip.
//
// primary may be nil, which makes the created population the only one. db and
// tenantUUID may be nil, which makes the corpus the only one; that is the
// shape of a cell composed with no execution database, and it behaves exactly
// as it did before this package existed.
func NewLayeredWorkerFacts(
	primary people.WorkerFacts, db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID,
) people.WorkerFacts {
	return layeredWorkerFacts{primary: primary, fallback: NewFacts(db, tenantUUID)}
}

// WorkerFactsAt implements [people.WorkerFacts].
func (l layeredWorkerFacts) WorkerFactsAt(ctx context.Context, q people.FactQuery) (people.FactSet, error) {
	if l.primary != nil {
		set, err := l.primary.WorkerFactsAt(ctx, q)
		if err != nil {
			return people.FactSet{}, err
		}
		if set.Exists {
			return set, nil
		}
	}
	return l.fallback.WorkerFactsAt(ctx, q)
}

// Lookup resolves one created worker by its key or its entity id, reporting
// whether this tenant carries it.
//
// It is the identity half of this package's read surface, separate from
// [Facts.WorkerFactsAt] because resolving "which worker did somebody mean" is
// a different question from "what may this caller see about them". The
// composed cell uses it to turn a reference typed on a form into the entity
// reference every governed read then names, so a created worker is resolved in
// exactly one place rather than in each surface that accepts a reference.
//
// Absence is a false, not an error, for the same reason [Store.Get]'s is: the
// worker may be a corpus worker, and the caller has to be able to ask.
func (f Facts) Lookup(ctx context.Context, tenant values.TenantId, ref string) (WorkerRow, bool, error) {
	if f.DB == nil || f.TenantUUID == nil || ref == "" {
		return WorkerRow{}, false, nil
	}
	tenantID := f.TenantUUID(tenant)
	if tenantID == uuid.Nil {
		return WorkerRow{}, false, nil
	}
	return f.read(ctx, tenantID, ref)
}

// Populated reports whether this tenant has a durable population of its own.
//
// A cell that cannot read one answers false, which is the same answer it
// gives for a tenant that genuinely has nobody: both mean "the release's
// fixed corpus is this cell's population", which is exactly what the caller
// asks this to decide.
func (f Facts) Populated(ctx context.Context, tenant values.TenantId) (bool, error) {
	if f.DB == nil || f.TenantUUID == nil {
		return false, nil
	}
	tenantID := f.TenantUUID(tenant)
	if tenantID == uuid.Nil {
		return false, nil
	}
	tx, err := f.DB.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("workforce: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return false, fmt.Errorf("workforce: scope tenant: %w", err)
	}
	return f.store.Populated(ctx, tx, tenantID)
}
