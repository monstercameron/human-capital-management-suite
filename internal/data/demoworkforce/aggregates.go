package demoworkforce

// WF-RUN-034: the served workforce's bitemporal aggregate projection.
//
// The Journey surface records employees in journey_worker (migration 00023),
// but a promotion commits through internal/data/promotioncommit, which reads
// and appends the DB-008/009/010 aggregate tables: worker, employment,
// assignment, job, job_position, position_occupancy, compensation_package,
// compensation_component, workforce_budget and budget_reservation. Before this
// file nothing in serve wrote those rows, so a served promotion could never
// resolve its commit command.
//
// ProjectWorker records one journey_worker row into those tables through the
// aggregates stores' own row builders (the same aggregates.New*/Put* path
// aggregates.LoadFixtures uses), under identities derived deterministically
// from the worker, so a replay finds the rows it wrote instead of creating a
// second, disjoint copy. SeedAggregateCatalog records the promotion catalog a
// served proposal selects from: one job per published ladder job/grade,
// vacant OPEN positions for every ladder target in its organization unit, and
// one compensation pool per organization unit.
//
// Nothing here invents a fact the workforce row does not state: the person,
// worker, employment, assignment, position, pay and manager all come from the
// row. Two derivations are named rather than hidden. A manager reference that
// resolves to no recorded worker (for example the board above the chief
// executive) is recorded as no manager, the top of the recorded chain, because
// the assignment's manager reference names a worker in this schema. And the
// catalog's vacancies and pools are demo catalog data, the same standing as
// the deterministic HarborCare staffing plan they are derived from.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// CatalogEffectiveFrom is the business instant every catalog row (legal
// entity, organization unit, job, vacancy, pool) created by this file takes
// effect from. It precedes every hire date the workforce carries, so a
// promotion's effective-dated reads always find the catalog it selects from.
var CatalogEffectiveFrom = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)

// harborCareEffective is when SeedOrganization's legal entity and units take
// effect.
var harborCareEffective = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// VacanciesPerTarget is how many OPEN positions the catalog records for each
// ladder target in an organization unit.
//
// One. Two identical requisitions for the same job in the same unit and the
// same location gave the propose form two rows a person cannot choose
// between -- same title, same unit, same office, nothing to tell them apart
// -- which reads as a duplicate-data defect rather than as two seats. A
// company that genuinely had two would distinguish them; this catalog does
// not, so it publishes one, and a second worker reaching for the same seat is
// told it has no capacity, which is the truth.
const VacanciesPerTarget = 1

// BudgetPoolType, BudgetPoolOwner and BudgetPoolPeriod describe the
// compensation pool every organization unit's promotions reserve against.
const (
	BudgetPoolType   = "COMPENSATION_POOL"
	BudgetPoolOwner  = "hcmnext.rewards.promotion_pool"
	BudgetPoolPeriod = "promotion-cycle"
)

// ErrNoVacancy is returned by [SelectVacancy] when no OPEN position for the
// target job has remaining capacity in the organization unit.
var ErrNoVacancy = errors.New("demoworkforce: no open position for the target job has capacity in the organization unit")

// ErrNoCatalogVacancy is returned by [SelectVacancy] when the catalog records
// no position at all for the target job in the organization unit.
var ErrNoCatalogVacancy = errors.New("demoworkforce: the catalog records no position for the target job in the organization unit")

// LegalEntityID is the deterministic legal entity identity for name.
func LegalEntityID(name string) uuid.UUID { return deterministicID("legal-entity", name) }

// OrganizationUnitID is the deterministic organization unit identity for code.
func OrganizationUnitID(code string) uuid.UUID { return deterministicID("organization-unit", code) }

// JobID is the deterministic job identity for a job code and grade.
func JobID(code, grade string) uuid.UUID { return deterministicID("job", code+"/"+grade) }

// FilledPositionID is the deterministic identity of a worker's recorded
// position code.
func FilledPositionID(code string) uuid.UUID { return deterministicID("job-position", code) }

// VacancyPositionID is the deterministic identity of the n-th (1-based) OPEN
// position for a ladder target in an organization unit.
func VacancyPositionID(orgUnit, jobCode, grade string, n int) uuid.UUID {
	return deterministicID("job-position-vacancy", fmt.Sprintf("%s/%s/%s/%d", orgUnit, jobCode, grade, n))
}

// BudgetID is the deterministic compensation pool identity for an
// organization unit.
func BudgetID(orgUnit string) uuid.UUID { return deterministicID("workforce-budget", orgUnit) }

// WorkerAggregateIDs are the aggregate identities one projected worker owns.
type WorkerAggregateIDs struct {
	Person, Worker, Employment, Assignment, Occupancy, Package, BasePay uuid.UUID
}

// AggregateIDsFor derives a journey_worker row's aggregate identities. The
// worker entity is the journey worker id itself, because a promotion's
// EMPLOYMENT subject names that id.
func AggregateIDsFor(row workforce.WorkerRow) WorkerAggregateIDs {
	w := row.WorkerID.String()
	return WorkerAggregateIDs{
		Person: deterministicID("person", w), Worker: row.WorkerID,
		Employment: deterministicID("employment", w+"/"+row.EmploymentID),
		Assignment: deterministicID("assignment", w+"/"+row.AssignmentID),
		Occupancy:  deterministicID("position-occupancy", w),
		Package:    deterministicID("compensation-package", w),
		BasePay:    deterministicID("compensation-base-pay", w),
	}
}

// ProjectWorker records row into the aggregate tables inside tx, which the
// caller owns and has already scoped to row.TenantID. legalEntityName names
// the employing legal entity when the tenant has none recorded yet. It is
// idempotent: a worker whose aggregate worker row already exists is left
// untouched and reported as not projected.
func ProjectWorker(ctx context.Context, tx dbport.Tx, row workforce.WorkerRow, legalEntityName string) (bool, error) {
	if tx == nil {
		return false, fmt.Errorf("demoworkforce: project worker: transaction is required")
	}
	if err := row.Validate(); err != nil {
		return false, fmt.Errorf("demoworkforce: project worker: %w", err)
	}
	ids := AggregateIDsFor(row)
	tenant := row.TenantID
	effective, err := time.Parse(workforce.DateLayout, row.EffectiveFrom)
	if err != nil {
		return false, fmt.Errorf("demoworkforce: project %s effective_from: %w", row.WorkerKey, err)
	}
	recorded := row.RecordedAt.UTC()
	people, org, comp := aggregates.PeopleStore{}, aggregates.OrganizationStore{}, aggregates.CompensationStore{}

	if _, err := people.CurrentWorker(ctx, tx, tenant, ids.Worker, effective); err == nil {
		return false, nil
	} else if !errors.Is(err, aggregates.ErrNotFound) {
		return false, fmt.Errorf("demoworkforce: read projected worker %s: %w", row.WorkerKey, err)
	}

	legalID, err := ensureLegalEntity(ctx, tx, tenant, legalEntityName, recorded)
	if err != nil {
		return false, err
	}
	orgID, err := ensureOrganizationUnit(ctx, tx, tenant, row.OrgUnit, legalID, recorded)
	if err != nil {
		return false, err
	}
	jobID, err := ensureJob(ctx, tx, tenant, row.JobCode, row.Grade, row.JobTitle, recorded)
	if err != nil {
		return false, err
	}

	person, err := aggregates.NewPerson(tenant, ids.Person, effective, nil, recorded, "ACTIVE", row.LegalName, row.PreferredName)
	if err != nil {
		return false, fmt.Errorf("demoworkforce: project %s person: %w", row.WorkerKey, err)
	}
	if _, err := people.PutPerson(ctx, tx, person); err != nil {
		return false, fmt.Errorf("demoworkforce: project %s person: %w", row.WorkerKey, err)
	}
	worker, err := aggregates.NewWorker(tenant, ids.Worker, ids.Person, effective, nil, recorded,
		row.WorkerNumber, aggregateWorkerType(row.WorkerType), aggregateLifecycle(row.LifecycleStatus))
	if err != nil {
		return false, fmt.Errorf("demoworkforce: project %s worker: %w", row.WorkerKey, err)
	}
	if _, err := people.PutWorker(ctx, tx, worker); err != nil {
		return false, fmt.Errorf("demoworkforce: project %s worker: %w", row.WorkerKey, err)
	}
	var hire *time.Time
	if parsed, parseErr := time.Parse(workforce.DateLayout, row.HireDate); parseErr == nil {
		hire = &parsed
	}
	employment, err := aggregates.NewEmployment(tenant, ids.Employment, ids.Worker, legalID, effective, nil, recorded,
		aggregateWorkerType(row.WorkerType), aggregateEmploymentStatus(row.LifecycleStatus), hire)
	if err != nil {
		return false, fmt.Errorf("demoworkforce: project %s employment: %w", row.WorkerKey, err)
	}
	if _, err := people.PutEmployment(ctx, tx, employment); err != nil {
		return false, fmt.Errorf("demoworkforce: project %s employment: %w", row.WorkerKey, err)
	}

	var positionRef *uuid.UUID
	if code := strings.TrimSpace(row.PositionID); code != "" {
		positionID, positionErr := ensurePosition(ctx, tx, tenant, FilledPositionID(code), jobID, orgID, legalID, code, row.Location, row.FTE, "FILLED", recorded)
		if positionErr != nil {
			return false, positionErr
		}
		positionRef = &positionID
	}
	manager, err := managerWorkerRef(ctx, tx, tenant, row.ManagerRelationshipRef)
	if err != nil {
		return false, err
	}
	assignment, err := aggregates.NewAssignment(tenant, ids.Assignment, ids.Employment, true, effective, nil, recorded,
		row.JobCode, row.Grade, &orgID, positionRef, row.Location, row.PayZone, row.FTE, manager)
	if err != nil {
		return false, fmt.Errorf("demoworkforce: project %s assignment: %w", row.WorkerKey, err)
	}
	if _, err := people.PutAssignment(ctx, tx, assignment); err != nil {
		return false, fmt.Errorf("demoworkforce: project %s assignment: %w", row.WorkerKey, err)
	}
	if positionRef != nil {
		occupancy, occErr := aggregates.NewPositionOccupancy(tenant, ids.Occupancy, *positionRef, &ids.Assignment, &ids.Worker,
			effective, nil, recorded, row.FTE, true)
		if occErr != nil {
			return false, fmt.Errorf("demoworkforce: project %s occupancy: %w", row.WorkerKey, occErr)
		}
		if _, err := org.PutPositionOccupancy(ctx, tx, occupancy); err != nil {
			return false, fmt.Errorf("demoworkforce: project %s occupancy: %w", row.WorkerKey, err)
		}
	}

	pkg, err := aggregates.NewCompensationPackage(tenant, ids.Package, ids.Worker, &ids.Employment, &ids.Assignment, effective, nil, recorded, row.Currency)
	if err != nil {
		return false, fmt.Errorf("demoworkforce: project %s compensation package: %w", row.WorkerKey, err)
	}
	if _, err := comp.PutCompensationPackage(ctx, tx, pkg); err != nil {
		return false, fmt.Errorf("demoworkforce: project %s compensation package: %w", row.WorkerKey, err)
	}
	base, err := values.NewMoney(row.BasePay, row.Currency, 2, values.RoundingExactRequired)
	if err != nil {
		return false, fmt.Errorf("demoworkforce: project %s base pay: %w", row.WorkerKey, err)
	}
	component, err := aggregates.NewCompensationComponent(tenant, ids.BasePay, ids.Package, effective, nil, recorded, "BASE_PAY", base, payFrequency(row.PayBasis))
	if err != nil {
		return false, fmt.Errorf("demoworkforce: project %s base pay: %w", row.WorkerKey, err)
	}
	if _, err := comp.PutCompensationComponent(ctx, tx, component); err != nil {
		return false, fmt.Errorf("demoworkforce: project %s base pay: %w", row.WorkerKey, err)
	}
	return true, nil
}

// CatalogJob is one job/grade the catalog records.
type CatalogJob struct {
	Code, Grade, Title string
}

// CatalogVacancy is one ladder target the catalog records OPEN positions for.
type CatalogVacancy struct {
	OrgUnit, JobCode, Grade, Title, Location string
}

// AggregateCatalog is the promotion catalog [SeedAggregateCatalog] records.
type AggregateCatalog struct {
	// LegalEntityName names the employing legal entity when none is recorded.
	LegalEntityName string
	Jobs            []CatalogJob
	Vacancies       []CatalogVacancy
	// BudgetOrgUnits each get one compensation pool of BudgetAmount in
	// BudgetCurrency.
	BudgetOrgUnits []string
	BudgetCurrency string
	BudgetAmount   string
	// RecordedAt stamps every row the seed writes.
	RecordedAt time.Time
}

// CatalogSummary counts what one [SeedAggregateCatalog] call wrote.
type CatalogSummary struct {
	Jobs, Vacancies, Budgets int
}

// SeedAggregateCatalog records catalog inside tx (already tenant scoped). It
// is replay-safe: every row it would write is looked up by its deterministic
// identity first and skipped when present.
func SeedAggregateCatalog(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, catalog AggregateCatalog) (CatalogSummary, error) {
	if tx == nil || tenant == uuid.Nil {
		return CatalogSummary{}, fmt.Errorf("demoworkforce: seed catalog: a transaction and tenant are required")
	}
	recorded := catalog.RecordedAt.UTC()
	if recorded.IsZero() {
		return CatalogSummary{}, fmt.Errorf("demoworkforce: seed catalog: recorded_at is required")
	}
	legalID, err := ensureLegalEntity(ctx, tx, tenant, catalog.LegalEntityName, recorded)
	if err != nil {
		return CatalogSummary{}, err
	}
	var summary CatalogSummary
	org, comp := aggregates.OrganizationStore{}, aggregates.CompensationStore{}
	for _, job := range catalog.Jobs {
		if _, err := org.CurrentJob(ctx, tx, tenant, JobID(job.Code, job.Grade), CatalogEffectiveFrom); err == nil {
			continue
		}
		if _, err := ensureJob(ctx, tx, tenant, job.Code, job.Grade, job.Title, recorded); err != nil {
			return CatalogSummary{}, err
		}
		summary.Jobs++
	}
	for _, vacancy := range catalog.Vacancies {
		orgID, err := ensureOrganizationUnit(ctx, tx, tenant, vacancy.OrgUnit, legalID, recorded)
		if err != nil {
			return CatalogSummary{}, err
		}
		jobID, err := ensureJob(ctx, tx, tenant, vacancy.JobCode, vacancy.Grade, vacancy.Title, recorded)
		if err != nil {
			return CatalogSummary{}, err
		}
		for n := 1; n <= VacanciesPerTarget; n++ {
			id := VacancyPositionID(vacancy.OrgUnit, vacancy.JobCode, vacancy.Grade, n)
			if _, err := org.CurrentJobPosition(ctx, tx, tenant, id, CatalogEffectiveFrom); err == nil {
				continue
			}
			code := fmt.Sprintf("VAC-%s-%s-%s-%d", vacancy.OrgUnit, vacancy.JobCode, vacancy.Grade, n)
			if _, err := ensurePosition(ctx, tx, tenant, id, jobID, orgID, legalID, code, vacancy.Location, "1.0000", "OPEN", recorded); err != nil {
				return CatalogSummary{}, err
			}
			summary.Vacancies++
		}
	}
	for _, unit := range catalog.BudgetOrgUnits {
		id := BudgetID(unit)
		if _, err := comp.CurrentWorkforceBudget(ctx, tx, tenant, id, CatalogEffectiveFrom); err == nil {
			continue
		} else if !errors.Is(err, aggregates.ErrNotFound) {
			return CatalogSummary{}, fmt.Errorf("demoworkforce: read budget %s: %w", unit, err)
		}
		budget, err := aggregates.NewWorkforceBudget(tenant, id, CatalogEffectiveFrom, nil, recorded,
			BudgetPoolType, BudgetPoolOwner, unit, BudgetPoolPeriod, catalog.BudgetCurrency, "MONEY", catalog.BudgetAmount, "")
		if err != nil {
			return CatalogSummary{}, fmt.Errorf("demoworkforce: budget %s: %w", unit, err)
		}
		if _, err := comp.PutWorkforceBudget(ctx, tx, budget); err != nil {
			return CatalogSummary{}, fmt.Errorf("demoworkforce: budget %s: %w", unit, err)
		}
		summary.Budgets++
	}
	return summary, nil
}

// Vacancy is one OPEN position [SelectVacancy] chose.
type Vacancy struct {
	PositionID uuid.UUID
	Position   aggregates.JobPosition
}

// SelectVacancy returns the first (lowest ordinal) OPEN catalog position for
// the target job in orgUnit whose live occupancy at businessAt leaves room for
// one full-time placement. It reads only; the position_occupancy capacity
// trigger remains the commit-time fence.
func SelectVacancy(ctx context.Context, ex dbport.Tx, tenant uuid.UUID, orgUnit, jobCode, grade string, businessAt time.Time) (Vacancy, error) {
	org := aggregates.OrganizationStore{}
	recorded := false
	for n := 1; n <= VacanciesPerTarget; n++ {
		id := VacancyPositionID(orgUnit, jobCode, grade, n)
		position, err := org.CurrentJobPosition(ctx, ex, tenant, id, businessAt)
		if errors.Is(err, aggregates.ErrNotFound) {
			continue
		}
		if err != nil {
			return Vacancy{}, fmt.Errorf("demoworkforce: read vacancy %s: %w", id, err)
		}
		recorded = true
		if position.LifecycleState != "OPEN" {
			continue
		}
		occupied, err := OccupiedFTE(ctx, ex, tenant, id, businessAt)
		if err != nil {
			return Vacancy{}, err
		}
		capacity, capErr := values.NewDecimal(position.CapacityFTE, 4, values.RoundingHalfEven)
		used, usedErr := values.NewDecimal(occupied, 4, values.RoundingHalfEven)
		if capErr != nil || usedErr != nil {
			return Vacancy{}, fmt.Errorf("demoworkforce: vacancy %s capacity is not decimal", id)
		}
		remaining, subErr := capacity.Sub(used)
		one, _ := values.NewDecimal("1.0000", 4, values.RoundingHalfEven)
		if subErr != nil {
			return Vacancy{}, subErr
		}
		if remaining.Cmp(one) >= 0 {
			return Vacancy{PositionID: id, Position: position}, nil
		}
	}
	if !recorded {
		return Vacancy{}, fmt.Errorf("%w: %s/%s in %s", ErrNoCatalogVacancy, jobCode, grade, orgUnit)
	}
	return Vacancy{}, fmt.Errorf("%w: %s/%s in %s", ErrNoVacancy, jobCode, grade, orgUnit)
}

// OccupiedFTE sums the live occupancy allocated to a position at businessAt,
// the same sum the position_occupancy_forbid_overcommit trigger compares
// against the position's capacity.
func OccupiedFTE(ctx context.Context, ex dbport.Tx, tenant, positionID uuid.UUID, businessAt time.Time) (string, error) {
	var total string
	err := ex.QueryRow(ctx, `
		SELECT COALESCE(SUM(allocation_fte), 0)::numeric(9,4)::text FROM position_occupancy
		WHERE tenant_id = $1 AND position_ref = $2 AND superseded_at IS NULL
		  AND effective_from <= $3 AND (effective_to IS NULL OR effective_to > $3)`, tenant, positionID, businessAt).Scan(&total)
	if err != nil {
		return "", fmt.Errorf("demoworkforce: read occupancy of position %s: %w", positionID, err)
	}
	return total, nil
}

func ensureLegalEntity(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, name string, recorded time.Time) (uuid.UUID, error) {
	org := aggregates.OrganizationStore{}
	harbor := LegalEntityID(HarborCare.LegalEntity)
	if _, err := org.CurrentLegalEntity(ctx, tx, tenant, harbor, harborCareEffective); err == nil {
		return harbor, nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return uuid.Nil, fmt.Errorf("demoworkforce: no legal entity is recorded and none was named")
	}
	id := LegalEntityID(name)
	if _, err := org.CurrentLegalEntity(ctx, tx, tenant, id, CatalogEffectiveFrom); err == nil {
		return id, nil
	} else if !errors.Is(err, aggregates.ErrNotFound) {
		return uuid.Nil, fmt.Errorf("demoworkforce: read legal entity %s: %w", name, err)
	}
	entity, err := aggregates.NewLegalEntity(tenant, id, CatalogEffectiveFrom, nil, recorded, name, "ACTIVE")
	if err != nil {
		return uuid.Nil, fmt.Errorf("demoworkforce: legal entity %s: %w", name, err)
	}
	if _, err := org.PutLegalEntity(ctx, tx, entity); err != nil {
		return uuid.Nil, fmt.Errorf("demoworkforce: legal entity %s: %w", name, err)
	}
	return id, nil
}

func ensureOrganizationUnit(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, code string, legalID uuid.UUID, recorded time.Time) (uuid.UUID, error) {
	org := aggregates.OrganizationStore{}
	id := OrganizationUnitID(code)
	// HarborCare's own units take effect 2026-01-01 (SeedOrganization); any
	// live row for the identity means the unit is recorded.
	for _, at := range []time.Time{CatalogEffectiveFrom, harborCareEffective} {
		if _, err := org.CurrentOrganizationUnit(ctx, tx, tenant, id, at); err == nil {
			return id, nil
		} else if !errors.Is(err, aggregates.ErrNotFound) {
			return uuid.Nil, fmt.Errorf("demoworkforce: read organization unit %s: %w", code, err)
		}
	}
	unit, err := aggregates.NewOrganizationUnit(tenant, id, CatalogEffectiveFrom, nil, recorded, "DEPARTMENT", code, code, &legalID, nil, "ACTIVE")
	if err != nil {
		return uuid.Nil, fmt.Errorf("demoworkforce: organization unit %s: %w", code, err)
	}
	if _, err := org.PutOrganizationUnit(ctx, tx, unit); err != nil {
		return uuid.Nil, fmt.Errorf("demoworkforce: organization unit %s: %w", code, err)
	}
	return id, nil
}

func ensureJob(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, code, grade, title string, recorded time.Time) (uuid.UUID, error) {
	org := aggregates.OrganizationStore{}
	id := JobID(code, grade)
	if _, err := org.CurrentJob(ctx, tx, tenant, id, CatalogEffectiveFrom); err == nil {
		return id, nil
	} else if !errors.Is(err, aggregates.ErrNotFound) {
		return uuid.Nil, fmt.Errorf("demoworkforce: read job %s/%s: %w", code, grade, err)
	}
	if strings.TrimSpace(title) == "" {
		title = code
	}
	// The family and the FLSA status are properties of the job, so they are
	// looked up from the job code rather than passed in: every caller that
	// records a job would otherwise have to carry the same table, and a
	// caller that forgot would silently classify a coordinator as exempt --
	// which is exactly what a hard-coded "EXEMPT" here used to do to all 47
	// codes at once.
	job, err := aggregates.NewJob(tenant, id, CatalogEffectiveFrom, nil, recorded, code, title, JobFamilyFor(code), grade, JobExemptStatusFor(code))
	if err != nil {
		return uuid.Nil, fmt.Errorf("demoworkforce: job %s/%s: %w", code, grade, err)
	}
	if _, err := org.PutJob(ctx, tx, job); err != nil {
		return uuid.Nil, fmt.Errorf("demoworkforce: job %s/%s: %w", code, grade, err)
	}
	return id, nil
}

func ensurePosition(ctx context.Context, tx dbport.Tx, tenant, id, jobID, orgID, legalID uuid.UUID, code, location, capacity, state string, recorded time.Time) (uuid.UUID, error) {
	org := aggregates.OrganizationStore{}
	if _, err := org.CurrentJobPosition(ctx, tx, tenant, id, CatalogEffectiveFrom); err == nil {
		return id, nil
	} else if !errors.Is(err, aggregates.ErrNotFound) {
		return uuid.Nil, fmt.Errorf("demoworkforce: read position %s: %w", code, err)
	}
	if strings.TrimSpace(capacity) == "" {
		capacity = "1.0000"
	}
	position, err := aggregates.NewJobPosition(tenant, id, jobID, orgID, &legalID, CatalogEffectiveFrom, nil, recorded, code, location, capacity, state)
	if err != nil {
		return uuid.Nil, fmt.Errorf("demoworkforce: position %s: %w", code, err)
	}
	if _, err := org.PutJobPosition(ctx, tx, position); err != nil {
		return uuid.Nil, fmt.Errorf("demoworkforce: position %s: %w", code, err)
	}
	return id, nil
}

// managerWorkerRef resolves a workforce manager reference to the manager's
// worker id text. A reference no recorded worker answers to is the top of the
// recorded chain and projects as no manager.
func managerWorkerRef(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil
	}
	manager, found, err := (workforce.Store{}).Get(ctx, tx, tenant, ref)
	if err != nil {
		return "", fmt.Errorf("demoworkforce: resolve manager %s: %w", ref, err)
	}
	if !found {
		return "", nil
	}
	return manager.WorkerID.String(), nil
}

func aggregateWorkerType(token string) string {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "contractor":
		return "CONTRACTOR"
	case "intern":
		return "INTERN"
	default:
		return "EMPLOYEE"
	}
}

func aggregateLifecycle(token string) string {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "active":
		return "ACTIVE"
	case "terminated", "ended":
		return "TERMINATED"
	case "on_leave", "leave":
		return "ON_LEAVE"
	default:
		return "PENDING"
	}
}

func aggregateEmploymentStatus(token string) string {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "active":
		return "ACTIVE"
	case "terminated", "ended":
		return "ENDED"
	case "on_leave", "leave":
		return "LEAVE"
	default:
		return "PENDING"
	}
}

func payFrequency(basis string) string {
	if strings.EqualFold(strings.TrimSpace(basis), "HOURLY_RATE") {
		return "HOURLY"
	}
	return "ANNUAL"
}
