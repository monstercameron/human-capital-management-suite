package aggregates

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Batched reads for list surfaces (PROMOUX-015 follow-up). A page that shows
// N workers must not issue N ActiveEmploymentForWorker, N
// PrimaryAssignmentForEmployment and N Current* round trips; every method
// here answers the same question its single-entity sibling answers, for a
// whole page, in one statement.
//
// Each one keeps its sibling's exact meaning:
//
//   - the same WHERE clause (tenant, superseded_at IS NULL and the same
//     half-open effective interval), so a row the sibling would not see is
//     not seen here either;
//   - a key that matches nothing is simply absent from the returned map,
//     which is what the sibling's aggregates.ErrNotFound means to every
//     caller that already handles it as absence;
//   - at most one row per key. put() supersedes every live row whose
//     business range overlaps the one it inserts, so a coordinate has at
//     most one live row per entity; should a hand-written row ever break
//     that, the first row read wins, exactly as QueryRow's own arbitrary
//     single-row pick does.
//
// Nothing here writes, and nothing here widens disclosure: the tenant
// predicate is the same bound parameter the siblings bind, and RLS still
// applies to the caller's transaction.

// currentAsOfAnySQL is [currentAsOfSQL] keyed by a set rather than by one
// entity, over whichever column the caller batches on (entity_id for a
// Current* read, a reference column for a finder). The column name is one of
// this package's own Go constants, never request input.
func currentAsOfAnySQL(table, selectCols, keyCol string) string {
	return fmt.Sprintf(`
		SELECT %s FROM %s
		WHERE tenant_id = $1 AND superseded_at IS NULL
		  AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)
		  AND %s = ANY($3::text[]::uuid[])`, selectCols, table, keyCol)
}

// batchKeys renders a key set for the ANY(...) parameter, dropping the
// duplicates a caller naturally collects when many rows point at one
// reference.
func batchKeys(ids []uuid.UUID) []string {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id.String())
	}
	return out
}

// queryByKeys runs one batched statement and indexes its rows by key. scan is
// the table's own single-row scanner (dbport.Rows satisfies dbport.Row), and
// key reads the column the result is indexed by.
func queryByKeys[T any](
	ctx context.Context, ex Executor, sql, what string, tenant uuid.UUID, businessAt time.Time, keys []uuid.UUID,
	scan func(dbport.Row) (T, error), key func(T) uuid.UUID,
) (map[uuid.UUID]T, error) {
	out := map[uuid.UUID]T{}
	params := batchKeys(keys)
	if len(params) == 0 {
		return out, nil
	}
	rows, err := ex.Query(ctx, sql, tenant, businessAt, params)
	if err != nil {
		return nil, fmt.Errorf("aggregates: read %s for %d keys: %w", what, len(params), err)
	}
	defer rows.Close()
	for rows.Next() {
		value, scanErr := scan(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("aggregates: scan %s: %w", what, scanErr)
		}
		if _, taken := out[key(value)]; taken {
			continue
		}
		out[key(value)] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("aggregates: iterate %s: %w", what, err)
	}
	return out, nil
}

// ActiveEmploymentsForWorkers is [PeopleStore.ActiveEmploymentForWorker] for a
// set of workers, keyed by worker reference.
func (PeopleStore) ActiveEmploymentsForWorkers(
	ctx context.Context, ex Executor, tenant uuid.UUID, workerIDs []uuid.UUID, businessAt time.Time,
) (map[uuid.UUID]Employment, error) {
	return queryByKeys(ctx, ex, currentAsOfAnySQL("employment", employmentColumns, "worker_ref"),
		"employments", tenant, businessAt, workerIDs, scanEmployment, func(e Employment) uuid.UUID { return e.WorkerRef })
}

// PrimaryAssignmentsForEmployments is
// [PeopleStore.PrimaryAssignmentForEmployment] for a set of employments,
// keyed by employment reference. The primary_flag predicate is the sibling's
// own.
func (PeopleStore) PrimaryAssignmentsForEmployments(
	ctx context.Context, ex Executor, tenant uuid.UUID, employmentIDs []uuid.UUID, businessAt time.Time,
) (map[uuid.UUID]Assignment, error) {
	return queryByKeys(ctx, ex, currentAsOfAnySQL("assignment", assignmentColumns, "employment_ref")+` AND primary_flag`,
		"assignments", tenant, businessAt, employmentIDs, scanAssignment, func(a Assignment) uuid.UUID { return a.EmploymentRef })
}

// CurrentOrganizationUnits is [OrganizationStore.CurrentOrganizationUnit] for
// a set of units, keyed by entity id.
func (OrganizationStore) CurrentOrganizationUnits(
	ctx context.Context, ex Executor, tenant uuid.UUID, entityIDs []uuid.UUID, businessAt time.Time,
) (map[uuid.UUID]OrganizationUnit, error) {
	return queryByKeys(ctx, ex, currentAsOfAnySQL("organization_unit", organizationUnitColumns, "entity_id"),
		"organization units", tenant, businessAt, entityIDs, scanOrganizationUnit, func(o OrganizationUnit) uuid.UUID { return o.EntityID })
}

// CurrentJobPositions is [OrganizationStore.CurrentJobPosition] for a set of
// positions, keyed by entity id.
func (OrganizationStore) CurrentJobPositions(
	ctx context.Context, ex Executor, tenant uuid.UUID, entityIDs []uuid.UUID, businessAt time.Time,
) (map[uuid.UUID]JobPosition, error) {
	return queryByKeys(ctx, ex, currentAsOfAnySQL("job_position", jobPositionColumns, "entity_id"),
		"job positions", tenant, businessAt, entityIDs, scanJobPosition, func(p JobPosition) uuid.UUID { return p.EntityID })
}

// CurrentJobs is [OrganizationStore.CurrentJob] for a set of jobs, keyed by
// entity id.
func (OrganizationStore) CurrentJobs(
	ctx context.Context, ex Executor, tenant uuid.UUID, entityIDs []uuid.UUID, businessAt time.Time,
) (map[uuid.UUID]Job, error) {
	return queryByKeys(ctx, ex, currentAsOfAnySQL("job", jobColumns, "entity_id"),
		"jobs", tenant, businessAt, entityIDs, scanJob, func(j Job) uuid.UUID { return j.EntityID })
}

// CurrentLegalEntities is [OrganizationStore.CurrentLegalEntity] for a set of
// legal entities, keyed by entity id.
func (OrganizationStore) CurrentLegalEntities(
	ctx context.Context, ex Executor, tenant uuid.UUID, entityIDs []uuid.UUID, businessAt time.Time,
) (map[uuid.UUID]LegalEntity, error) {
	return queryByKeys(ctx, ex, currentAsOfAnySQL("legal_entity", legalEntityColumns, "entity_id"),
		"legal entities", tenant, businessAt, entityIDs, scanLegalEntity, func(e LegalEntity) uuid.UUID { return e.EntityID })
}

// ActivePackagesForWorkers is [CompensationStore.ActivePackageForWorker] for a
// set of workers, keyed by worker reference.
func (CompensationStore) ActivePackagesForWorkers(
	ctx context.Context, ex Executor, tenant uuid.UUID, workerIDs []uuid.UUID, businessAt time.Time,
) (map[uuid.UUID]CompensationPackage, error) {
	return queryByKeys(ctx, ex, currentAsOfAnySQL("compensation_package", compensationPackageColumns, "worker_ref"),
		"compensation packages", tenant, businessAt, workerIDs, scanCompensationPackage,
		func(p CompensationPackage) uuid.UUID { return p.WorkerRef })
}

// BasePayComponentsForPackages is
// [CompensationStore.BasePayComponentForPackage] for a set of packages, keyed
// by package reference. The BASE_PAY predicate is the sibling's own.
func (CompensationStore) BasePayComponentsForPackages(
	ctx context.Context, ex Executor, tenant uuid.UUID, packageIDs []uuid.UUID, businessAt time.Time,
) (map[uuid.UUID]CompensationComponent, error) {
	return queryByKeys(ctx, ex,
		currentAsOfAnySQL("compensation_component", compensationComponentColumns, "package_ref")+` AND component_type = 'BASE_PAY'`,
		"base pay components", tenant, businessAt, packageIDs, scanCompensationComponent,
		func(c CompensationComponent) uuid.UUID { return c.PackageRef })
}
