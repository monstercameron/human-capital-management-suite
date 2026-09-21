package demoworkforce

// Per-worker access-role assignments for the HarborCare demo tenant.
//
// The tenant bootstrap (internal/data/roleaccessstore.Store.Bootstrap) writes
// the role catalog and the role/page grants, but never a single row of
// worker_access_role_set or worker_access_role_assignment (migrations/00238),
// so the database-driven half of role access had no data at all: every
// principal fell back to the roles their credential carried, and the
// directory-backed path was never exercised against the demo population.
//
// This file only adds those rows. It changes no page grant, no visibility
// policy and no gating rule; which pages a role reaches is decided entirely
// by internal/experience/roleaccess and the role_page_permission rows the
// bootstrap already writes.
//
// One consequence has to be respected rather than discovered: roleaccess
// resolves a principal by subject, and a stored assignment REPLACES the
// roles their credential was admitted with. The four local-development
// personas sign in as real HarborCare workers, so their stored assignment is
// pinned to the exact bundle their credential is issued with (see
// DevPersonaRoleAssignments and its test) -- seeding must not quietly grant
// or remove a persona's access. Every other worker is assigned the bundle the
// local-development directory already issues them a credential for, resolved
// from their own record: their grade, their unit, and whether anybody reports
// to them.

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// The role ids the tenant bootstrap writes into access_role. They are
// repeated here as plain strings rather than imported, because this package
// is a data seeder and must not depend on the experience layer; the test
// pins them against roleaccess.DefaultRoles.
const (
	RoleHCMAdmin          = "hcm_admin"
	RoleCompAdmin         = "comp_admin"
	RoleHiringManager     = "hiring_manager"
	RoleManager           = "manager"
	RoleHRPartner         = "hr_partner"
	RoleFinancePartner    = "finance_partner"
	RoleIntentAuthor      = "intent_author"
	RolePromotionOperator = "promotion_operator"
	RoleWorkerSelf        = "worker_self"
)

// The organization units whose staff hold a functional bundle rather than a
// line-management or self-service one.
const (
	PeopleOperationsUnit = "people-operations"
	FinanceUnit          = "finance"
)

// RoleAssignmentActor is recorded as the author of every seeded assignment,
// so an administrator reading the directory can tell a seeded row from one a
// human made.
const RoleAssignmentActor = "system:harborcare-demo-seed"

// DevPersonaRoleAssignments pins the four local-development personas to the
// exact role bundle their signed credential carries. It mirrors
// internal/humanwork/workspace.DevPersonaRoleSets by worker number, and the
// package's test fails if the two ever disagree. Seeding a persona any other
// bundle would silently change what that persona can reach, because a stored
// assignment overrides the credential's own roles.
var DevPersonaRoleAssignments = map[string][]string{
	"HC-21050": {RoleHCMAdmin, RoleCompAdmin, RoleIntentAuthor, RolePromotionOperator},
	"HC-21004": {RoleHiringManager, RoleManager, RoleIntentAuthor},
	"HC-21054": {RoleFinancePartner},
	"HC-21051": {RoleWorkerSelf},
}

// WorkerRoleAssignment is one worker's seeded access-role set.
type WorkerRoleAssignment struct {
	WorkerKey    string
	WorkerNumber string
	RoleIDs      []string
}

// RoleAssignmentSummary counts what one [SeedRoleAssignments] call wrote.
type RoleAssignmentSummary struct {
	Workers     int
	Assignments int
	Skipped     int
}

// The five bundles a HarborCare worker can hold. They are the same five the
// local-development directory already issues credentials for
// (internal/humanwork/workspace.DevEmployeeBundleAccess), deliberately: a
// stored assignment replaces a credential's admitted roles, so seeding any
// other bundle would change what a signed-in employee can reach. The
// package's test pins each one against that fixture.
var (
	AdminBundle       = []string{RoleHCMAdmin, RoleCompAdmin, RoleIntentAuthor, RolePromotionOperator}
	ManagerBundle     = []string{RoleHiringManager, RoleManager, RoleIntentAuthor}
	HRPartnerBundle   = []string{RoleHRPartner}
	FinanceBundle     = []string{RoleFinancePartner}
	SelfServiceBundle = []string{RoleWorkerSelf}
)

// PlanRoleAssignments derives every worker's access-role set from the
// workforce plan. Executives administer, people-operations staff support the
// organization, finance staff decide finance approvals, line managers manage,
// and everybody else has self-service. The four quick-pick personas listed in
// [DevPersonaRoleAssignments] keep their own credential's bundle instead,
// because the demo's separated promotion depends on exactly those four
// authorities.
func PlanRoleAssignments(tenant uuid.UUID) ([]WorkerRoleAssignment, error) {
	employees, err := Plan(tenant)
	if err != nil {
		return nil, err
	}
	managers := make(map[string]bool, len(employees))
	for _, employee := range employees {
		if employee.ManagerKey != "" {
			managers[employee.ManagerKey] = true
		}
	}
	assignments := make([]WorkerRoleAssignment, 0, len(employees))
	for _, employee := range employees {
		roles := DevPersonaRoleAssignments[employee.Row.WorkerNumber]
		if len(roles) == 0 {
			roles = RolesForWorker(employee, managers[employee.Row.WorkerKey])
		}
		assignments = append(assignments, WorkerRoleAssignment{
			WorkerKey:    employee.Row.WorkerKey,
			WorkerNumber: employee.Row.WorkerNumber,
			RoleIDs:      append([]string(nil), roles...),
		})
	}
	return assignments, nil
}

// RolesForWorker reads a worker's bundle off their own record: an E grade is
// this plan's executive band, people operations and finance are functional
// partners whatever their grade, and somebody who has a report is a line
// manager. Everybody else holds self-service. The order matters and mirrors
// the local-development directory's own classification exactly.
func RolesForWorker(employee Employee, managesAnybody bool) []string {
	switch {
	case strings.HasPrefix(employee.Row.Grade, "E"):
		return AdminBundle
	case employee.Organization.Code == PeopleOperationsUnit:
		return HRPartnerBundle
	case employee.Organization.Code == FinanceUnit:
		return FinanceBundle
	case managesAnybody:
		return ManagerBundle
	default:
		return SelfServiceBundle
	}
}

// SeedRoleAssignments records every worker's access-role set inside tx, which
// the caller owns. It requires the tenant's role catalog to be bootstrapped
// already: worker_access_role_assignment references access_role.
//
// It is replay-safe and, more than that, non-destructive: a worker whose set
// already exists is left exactly as it is, version and all, so an
// administrator who edited somebody's roles does not lose that edit the next
// time the demo tenant starts.
func SeedRoleAssignments(ctx context.Context, tx dbport.Tx, tenant uuid.UUID) (RoleAssignmentSummary, error) {
	if tx == nil || tenant == uuid.Nil {
		return RoleAssignmentSummary{}, fmt.Errorf("demoworkforce: seed role assignments: a transaction and tenant are required")
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return RoleAssignmentSummary{}, err
	}
	planned, err := PlanRoleAssignments(tenant)
	if err != nil {
		return RoleAssignmentSummary{}, err
	}
	var summary RoleAssignmentSummary
	for _, assignment := range planned {
		created, err := insertOnce(ctx, tx, `
			INSERT INTO worker_access_role_set (tenant_id, worker_ref, version, updated_by)
			VALUES ($1, $2, 1, $3) ON CONFLICT DO NOTHING`,
			tenant, assignment.WorkerKey, RoleAssignmentActor)
		if err != nil {
			return RoleAssignmentSummary{}, fmt.Errorf("demoworkforce: seed role set for %s: %w", assignment.WorkerKey, err)
		}
		if !created {
			summary.Skipped++
			continue
		}
		summary.Workers++
		for _, roleID := range assignment.RoleIDs {
			if _, err := tx.Exec(ctx, `
				INSERT INTO worker_access_role_assignment (tenant_id, worker_ref, role_id)
				VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, tenant, assignment.WorkerKey, roleID); err != nil {
				return RoleAssignmentSummary{}, fmt.Errorf("demoworkforce: assign %s to %s: %w", roleID, assignment.WorkerKey, err)
			}
			summary.Assignments++
		}
	}
	return summary, nil
}
