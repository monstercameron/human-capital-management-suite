package demoworkforce

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// TestPlanRoleAssignmentsUsesTheBootstrappedVocabulary proves every assigned
// role is one the tenant bootstrap actually writes into access_role, that
// every worker gets exactly one set, and that the functional, line-management
// and self-service bundles land where the record says they should.
func TestPlanRoleAssignmentsUsesTheBootstrappedVocabulary(t *testing.T) {
	tenant := uuid.MustParse("3ad6fcb4-1e0f-4d73-9f76-2a7b3c9c4a10")
	assignments, err := PlanRoleAssignments(tenant)
	if err != nil {
		t.Fatalf("PlanRoleAssignments: %v", err)
	}
	if len(assignments) != NewWorkerCount {
		t.Fatalf("assignments = %d, want %d", len(assignments), NewWorkerCount)
	}
	catalog := map[string]bool{}
	for _, role := range roleaccess.DefaultRoles() {
		catalog[role.ID] = true
	}
	employees, err := Plan(tenant)
	if err != nil {
		t.Fatal(err)
	}
	byKey := make(map[string]Employee, len(employees))
	for _, employee := range employees {
		byKey[employee.Row.WorkerKey] = employee
	}
	managers := map[string]bool{}
	for _, employee := range employees {
		if employee.ManagerKey != "" {
			managers[employee.ManagerKey] = true
		}
	}
	seen := map[string]bool{}
	counts := map[string]int{}
	for _, assignment := range assignments {
		if seen[assignment.WorkerKey] {
			t.Fatalf("%s was assigned twice", assignment.WorkerKey)
		}
		seen[assignment.WorkerKey] = true
		if len(assignment.RoleIDs) == 0 {
			t.Fatalf("%s has no roles", assignment.WorkerKey)
		}
		for _, roleID := range assignment.RoleIDs {
			if !catalog[roleID] {
				t.Fatalf("%s holds %q, which the tenant bootstrap never writes", assignment.WorkerKey, roleID)
			}
			counts[roleID]++
		}
		if _, persona := DevPersonaRoleAssignments[assignment.WorkerNumber]; persona {
			continue
		}
		// The seeded bundle has to be the bundle the local-development
		// directory already issues this employee a credential for: a stored
		// assignment replaces the credential's admitted roles, so any other
		// bundle would change what that employee can reach.
		employee := byKey[assignment.WorkerKey]
		bundle := workspace.DevEmployeeBundleSelf
		switch {
		case strings.HasPrefix(employee.Row.Grade, "E"):
			bundle = workspace.DevEmployeeBundleExecutive
		case employee.Organization.Code == PeopleOperationsUnit:
			bundle = workspace.DevEmployeeBundlePeopleOps
		case employee.Organization.Code == FinanceUnit:
			bundle = workspace.DevEmployeeBundleFinance
		case managers[assignment.WorkerKey]:
			bundle = workspace.DevEmployeeBundleManager
		}
		access, ok := workspace.DevEmployeeBundleAccess(bundle)
		if !ok {
			t.Fatalf("%s resolves no directory bundle", assignment.WorkerKey)
		}
		if !sameRoleSet(assignment.RoleIDs, access.Roles) {
			t.Fatalf("%s is seeded %v but signs in with %v", assignment.WorkerKey, assignment.RoleIDs, access.Roles)
		}
	}
	for _, roleID := range []string{RoleManager, RoleHRPartner, RoleFinancePartner, RoleHCMAdmin, RoleWorkerSelf} {
		if counts[roleID] == 0 {
			t.Fatalf("no worker holds %q; the bundles are not exercised", roleID)
		}
	}
}

// sameRoleSet reports whether two role bundles hold the same roles.
func sameRoleSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	have := make(map[string]int, len(left))
	for _, role := range left {
		have[role]++
	}
	for _, role := range right {
		have[role]--
	}
	for _, remaining := range have {
		if remaining != 0 {
			return false
		}
	}
	return true
}

// TestDevPersonaRoleAssignmentsMatchTheIssuedCredentials is the guard that
// keeps seeding from changing what a persona can reach: a stored assignment
// replaces the roles a credential was admitted with, so the seeded bundle has
// to be the credential's own bundle, exactly.
func TestDevPersonaRoleAssignmentsMatchTheIssuedCredentials(t *testing.T) {
	tenant := uuid.MustParse("3ad6fcb4-1e0f-4d73-9f76-2a7b3c9c4a10")
	employees, err := Plan(tenant)
	if err != nil {
		t.Fatal(err)
	}
	// The persona-to-worker binding lives in internal/application; these are
	// the four worker numbers it names.
	personaWorkers := map[string]string{
		"admin": "HC-21050", "hiring-manager": "HC-21004",
		"finance-partner": "HC-21054", "individual-contributor": "HC-21051",
	}
	numbers := map[string]bool{}
	for _, employee := range employees {
		numbers[employee.Row.WorkerNumber] = true
	}
	checked := 0
	for _, set := range workspace.DevPersonaRoleSets() {
		number, named := personaWorkers[set.ID]
		if !named {
			t.Fatalf("persona %q has no seeded worker; the seed would leave its stored roles unpinned", set.ID)
		}
		if !numbers[number] {
			t.Fatalf("persona %q names %s, which is not in the demo workforce", set.ID, number)
		}
		seeded, ok := DevPersonaRoleAssignments[number]
		if !ok {
			t.Fatalf("persona %q (%s) has no pinned assignment", set.ID, number)
		}
		if len(seeded) != len(set.Roles) {
			t.Fatalf("persona %q seeded %v, credential carries %v", set.ID, seeded, set.Roles)
		}
		want := map[string]bool{}
		for _, role := range set.Roles {
			want[role] = true
		}
		for _, role := range seeded {
			if !want[role] {
				t.Fatalf("persona %q seeded %q, which its credential does not carry", set.ID, role)
			}
		}
		checked++
	}
	if checked != len(DevPersonaRoleAssignments) {
		t.Fatalf("checked %d personas, but %d assignments are pinned", checked, len(DevPersonaRoleAssignments))
	}
}

// TestSeedRoleAssignmentsLandsTenantScopedRowsAndReplaysAsANoOp proves the
// assignment rows land against the bootstrapped catalog, are scoped to the
// seeding tenant, and that a replay neither duplicates nor rewrites a set an
// administrator has since edited.
func TestSeedRoleAssignmentsLandsTenantScopedRowsAndReplaysAsANoOp(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedAggregateTenant(t, db)
	ctx := context.Background()
	bootstrapRoleCatalog(t, db, tenantID)

	var first RoleAssignmentSummary
	inAggregateTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		first, err = SeedRoleAssignments(ctx, tx, tenantID)
		return err
	})
	if first.Workers != NewWorkerCount || first.Skipped != 0 || first.Assignments < NewWorkerCount {
		t.Fatalf("first seed = %+v", first)
	}

	var second RoleAssignmentSummary
	inAggregateTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		second, err = SeedRoleAssignments(ctx, tx, tenantID)
		return err
	})
	if second.Workers != 0 || second.Assignments != 0 || second.Skipped != NewWorkerCount {
		t.Fatalf("replayed seed = %+v; every set should have been skipped", second)
	}

	var sets, assignments, leaked int
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM worker_access_role_set WHERE tenant_id = $1`, tenantID).Scan(&sets); err != nil {
		t.Fatal(err)
	}
	if sets != NewWorkerCount {
		t.Fatalf("role sets = %d, want %d", sets, NewWorkerCount)
	}
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM worker_access_role_assignment WHERE tenant_id = $1`, tenantID).Scan(&assignments); err != nil {
		t.Fatal(err)
	}
	if assignments != first.Assignments {
		t.Fatalf("assignment rows = %d, want %d", assignments, first.Assignments)
	}
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM worker_access_role_set WHERE tenant_id <> $1`, tenantID).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatalf("%d role sets landed outside the seeded tenant", leaked)
	}

	// The seeded rows are what roleaccess resolves a principal's roles from.
	planned, err := PlanRoleAssignments(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	var stored []string
	rows, err := db.Conn.Query(ctx, `SELECT role_id FROM worker_access_role_assignment WHERE tenant_id = $1 AND worker_ref = $2 ORDER BY role_id`, tenantID, planned[49].WorkerKey)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var roleID string
		if err := rows.Scan(&roleID); err != nil {
			t.Fatal(err)
		}
		stored = append(stored, roleID)
	}
	rows.Close()
	if len(stored) != len(planned[49].RoleIDs) {
		t.Fatalf("%s stored %v, planned %v", planned[49].WorkerKey, stored, planned[49].RoleIDs)
	}

	if _, err := SeedRoleAssignments(ctx, nil, tenantID); err == nil {
		t.Fatal("a role assignment seed without a transaction was accepted")
	}
}

// bootstrapRoleCatalog writes the same access_role rows the tenant bootstrap
// writes. worker_access_role_assignment references them, so the catalog has
// to exist before any worker can be assigned.
func bootstrapRoleCatalog(t *testing.T, db *pgtest.DB, tenantID uuid.UUID) {
	t.Helper()
	for _, role := range roleaccess.DefaultRoles() {
		db.Exec(t, `INSERT INTO access_role (tenant_id, role_id, version, name, description, system_role, active, updated_by)
			VALUES ($1, $2, 1, $3, $4, true, true, 'system:test') ON CONFLICT DO NOTHING`,
			tenantID, role.ID, role.Name, role.Description)
	}
}
