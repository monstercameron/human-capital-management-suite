package aggregates

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// This file extends the bitemporal read surface store.go establishes to the
// aggregates the promotion terminal resolver must pin at approval time, plus
// the identity finders that resolve a promotion's worker to its related
// rows. Every method here is additive: no existing read changes meaning, and
// each one follows its Current* sibling exactly, with knownAt added.
//
// The terminal resolver (PROMOUX-016) reads baselines through KnownAsOf*,
// never through Current*, so the digests it binds are the ones the approved
// proposal's simulation observed -- recorded at or before the proposal
// revision's ProducedAt -- and not whatever the terminal transaction happens
// to see. A baseline that moved after approval then fails the commit's own
// comparison instead of passing vacuously.

// KnownAsOfEmployment returns the Employment that held at businessAt, using
// only what had been recorded by knownAt.
func (PeopleStore) KnownAsOfEmployment(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt, knownAt time.Time) (Employment, error) {
	row := ex.QueryRow(ctx, knownAsOfSQL("employment", employmentColumns), tenant, entityID, businessAt, knownAt)
	e, err := scanEmployment(row)
	if err != nil {
		return Employment{}, wrapNotFound(err, "employment", entityID)
	}
	return e, nil
}

// KnownAsOfJob returns the Job that held at businessAt, using only what had
// been recorded by knownAt.
func (OrganizationStore) KnownAsOfJob(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt, knownAt time.Time) (Job, error) {
	row := ex.QueryRow(ctx, knownAsOfSQL("job", jobColumns), tenant, entityID, businessAt, knownAt)
	j, err := scanJob(row)
	if err != nil {
		return Job{}, wrapNotFound(err, "job", entityID)
	}
	return j, nil
}

// KnownAsOfJobPosition returns the JobPosition that held at businessAt,
// using only what had been recorded by knownAt.
func (OrganizationStore) KnownAsOfJobPosition(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt, knownAt time.Time) (JobPosition, error) {
	row := ex.QueryRow(ctx, knownAsOfSQL("job_position", jobPositionColumns), tenant, entityID, businessAt, knownAt)
	p, err := scanJobPosition(row)
	if err != nil {
		return JobPosition{}, wrapNotFound(err, "job_position", entityID)
	}
	return p, nil
}

// KnownAsOfCompensationPackage returns the CompensationPackage that held at
// businessAt, using only what had been recorded by knownAt.
func (CompensationStore) KnownAsOfCompensationPackage(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt, knownAt time.Time) (CompensationPackage, error) {
	row := ex.QueryRow(ctx, knownAsOfSQL("compensation_package", compensationPackageColumns), tenant, entityID, businessAt, knownAt)
	p, err := scanCompensationPackage(row)
	if err != nil {
		return CompensationPackage{}, wrapNotFound(err, "compensation_package", entityID)
	}
	return p, nil
}

// KnownAsOfCompensationComponent returns the CompensationComponent that held
// at businessAt, using only what had been recorded by knownAt.
func (CompensationStore) KnownAsOfCompensationComponent(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt, knownAt time.Time) (CompensationComponent, error) {
	row := ex.QueryRow(ctx, knownAsOfSQL("compensation_component", compensationComponentColumns), tenant, entityID, businessAt, knownAt)
	c, err := scanCompensationComponent(row)
	if err != nil {
		return CompensationComponent{}, wrapNotFound(err, "compensation_component", entityID)
	}
	return c, nil
}

// KnownAsOfBudgetReservation returns the BudgetReservation that held at
// businessAt, using only what had been recorded by knownAt.
func (CompensationStore) KnownAsOfBudgetReservation(ctx context.Context, ex Executor, tenant, entityID uuid.UUID, businessAt, knownAt time.Time) (BudgetReservation, error) {
	row := ex.QueryRow(ctx, knownAsOfSQL("budget_reservation", budgetReservationColumns), tenant, entityID, businessAt, knownAt)
	r, err := scanBudgetReservation(row)
	if err != nil {
		return BudgetReservation{}, wrapNotFound(err, "budget_reservation", entityID)
	}
	return r, nil
}

// currentByRefSQL is the finder variant of currentAsOfSQL: the live row
// whose reference column names key, with no entity-id constraint, because a
// finder starts from the reference rather than the entity.
func currentByRefSQL(table, selectCols, refCol string) string {
	return fmt.Sprintf(`
		SELECT %s FROM %s
		WHERE tenant_id = $1 AND superseded_at IS NULL
		  AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)
		  AND %s = $3`, selectCols, table, refCol)
}

// ActiveEmploymentForWorker resolves the worker's employment active at
// businessAt. Identity resolution reads current state: the KnownAsOf*
// methods above then pin the baseline the approval observed, and the commit
// refuses when the two disagree.
func (PeopleStore) ActiveEmploymentForWorker(ctx context.Context, ex Executor, tenant, workerID uuid.UUID, businessAt time.Time) (Employment, error) {
	row := ex.QueryRow(ctx, currentByRefSQL("employment", employmentColumns, "worker_ref"), tenant, businessAt, workerID)
	e, err := scanEmployment(row)
	if err != nil {
		return Employment{}, wrapNotFound(err, "employment", workerID)
	}
	return e, nil
}

// PrimaryAssignmentForEmployment resolves the employment's primary assignment
// active at businessAt.
func (PeopleStore) PrimaryAssignmentForEmployment(ctx context.Context, ex Executor, tenant, employmentID uuid.UUID, businessAt time.Time) (Assignment, error) {
	row := ex.QueryRow(ctx, currentByRefSQL("assignment", assignmentColumns, "employment_ref")+` AND primary_flag`, tenant, businessAt, employmentID)
	a, err := scanAssignment(row)
	if err != nil {
		return Assignment{}, wrapNotFound(err, "assignment", employmentID)
	}
	return a, nil
}

// ActivePackageForWorker resolves the worker's compensation package active at
// businessAt.
func (CompensationStore) ActivePackageForWorker(ctx context.Context, ex Executor, tenant, workerID uuid.UUID, businessAt time.Time) (CompensationPackage, error) {
	row := ex.QueryRow(ctx, currentByRefSQL("compensation_package", compensationPackageColumns, "worker_ref"), tenant, businessAt, workerID)
	p, err := scanCompensationPackage(row)
	if err != nil {
		return CompensationPackage{}, wrapNotFound(err, "compensation_package", workerID)
	}
	return p, nil
}

// BasePayComponentForPackage resolves the package's base-pay component active
// at businessAt. There is exactly one base-pay component per package: the
// component the promotion's pay change supersedes.
func (CompensationStore) BasePayComponentForPackage(ctx context.Context, ex Executor, tenant, packageID uuid.UUID, businessAt time.Time) (CompensationComponent, error) {
	row := ex.QueryRow(ctx, currentByRefSQL("compensation_component", compensationComponentColumns, "package_ref")+` AND component_type = 'BASE_PAY'`, tenant, businessAt, packageID)
	c, err := scanCompensationComponent(row)
	if err != nil {
		return CompensationComponent{}, wrapNotFound(err, "compensation_component", packageID)
	}
	return c, nil
}

// ReservationForProposal resolves the held budget reservation a proposal
// carries, by the proposal revision id the reservation was recorded under.
// A promotion that reserved no budget has no row, and the resolver refuses
// rather than committing unreserved spend.
func (CompensationStore) ReservationForProposal(ctx context.Context, ex Executor, tenant, proposalID uuid.UUID, businessAt time.Time) (BudgetReservation, error) {
	row := ex.QueryRow(ctx, currentByRefSQL("budget_reservation", budgetReservationColumns, "proposal_ref"), tenant, businessAt, proposalID)
	r, err := scanBudgetReservation(row)
	if err != nil {
		return BudgetReservation{}, wrapNotFound(err, "budget_reservation", proposalID)
	}
	return r, nil
}

// WorkerByRef resolves a worker by entity id or by worker number. Proposal
// material names managers the way the form captured them -- a worker number
// as often as an id -- so the resolver accepts both spellings and refuses
// anything else, rather than guessing which worker a bare string means.
func (PeopleStore) WorkerByRef(ctx context.Context, ex Executor, tenant uuid.UUID, ref string, businessAt time.Time) (Worker, error) {
	if id, err := uuid.Parse(ref); err == nil {
		return PeopleStore{}.CurrentWorker(ctx, ex, tenant, id, businessAt)
	}
	row := ex.QueryRow(ctx, currentByRefSQL("worker", workerColumns, "worker_number"), tenant, businessAt, ref)
	w, err := scanWorker(row)
	if err != nil {
		return Worker{}, wrapNotFound(err, "worker", uuid.Nil)
	}
	return w, nil
}
