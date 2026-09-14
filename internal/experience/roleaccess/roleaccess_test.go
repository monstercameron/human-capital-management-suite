package roleaccess

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestAssignedRolesUsesDurableAssignmentAndFallsBackToClaims(t *testing.T) {
	snapshot := Snapshot{Assignments: []Assignment{{WorkerRef: "jane", RoleIDs: []string{"manager", "HR_PARTNER", "manager"}}}}
	got := AssignedRoles(snapshot, "JANE", []string{"worker_self"})
	if len(got) != 2 || got[0] != "hr_partner" || got[1] != "manager" {
		t.Fatalf("durable roles = %#v", got)
	}
	got = AssignedRoles(snapshot, "other", []string{"WORKER_SELF"})
	if len(got) != 1 || got[0] != "worker_self" {
		t.Fatalf("fallback roles = %#v", got)
	}
}

func TestValidationRejectsEmptyAssignmentsAndUnsafeRoleIDs(t *testing.T) {
	if !errors.Is(ValidateAssignment(Assignment{WorkerRef: "jane"}), ErrInvalid) {
		t.Fatal("empty assignment was accepted")
	}
	if !errors.Is(ValidateRole(Role{ID: "Admin Role", Name: "Admin"}), ErrInvalid) {
		t.Fatal("unsafe role ID was accepted")
	}
	if !errors.Is(ValidateVisibility(VisibilityPolicy{RoleID: "manager", Mode: "surprise"}), ErrInvalid) {
		t.Fatal("unknown visibility mode was accepted")
	}
}

func TestNormalizeRole_AndValidateRole_Boundaries(t *testing.T) {
	got := NormalizeRole(Role{ID: "  MANAGER ", Name: "  People manager  ", Description: "  governs people  "})
	want := Role{ID: "manager", Name: "People manager", Description: "governs people"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized role = %#v, want %#v", got, want)
	}

	tests := []struct {
		name string
		role Role
	}{{
		name: "empty id", role: Role{Name: "name"},
	}, {
		name: "one character id", role: Role{ID: "a", Name: "name"},
	}, {
		name: "punctuation id", role: Role{ID: "role-name", Name: "name"},
	}, {
		name: "empty name", role: Role{ID: "manager"},
	}, {
		name: "name limit", role: Role{ID: "manager", Name: string(make([]byte, 97))},
	}, {
		name: "description limit", role: Role{ID: "manager", Name: "name", Description: string(make([]byte, 401))},
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(ValidateRole(tt.role), ErrInvalid) {
				t.Fatalf("ValidateRole(%#v) accepted", tt.role)
			}
		})
	}
	if err := ValidateRole(Role{ID: "manager", Name: "name", Description: "description"}); err != nil {
		t.Fatalf("valid role rejected: %v", err)
	}
}

func TestNormalizeRoleIDs_AssignmentAndVisibility(t *testing.T) {
	if got, want := NormalizeRoleIDs([]string{" Manager ", "manager", "HR_PARTNER", "", "bad id"}), []string{"bad id", "hr_partner", "manager"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("role IDs = %#v, want %#v", got, want)
	}
	assignment := NormalizeAssignment(Assignment{WorkerRef: " jane ", RoleIDs: []string{"MANAGER", "manager"}})
	if assignment.WorkerRef != "jane" || !reflect.DeepEqual(assignment.RoleIDs, []string{"manager"}) {
		t.Fatalf("assignment normalization = %#v", assignment)
	}
	for _, assignment := range []Assignment{
		{WorkerRef: "", RoleIDs: []string{"manager"}},
		{WorkerRef: string(make([]byte, 257)), RoleIDs: []string{"manager"}},
		{WorkerRef: "jane", RoleIDs: []string{"bad id"}},
	} {
		if !errors.Is(ValidateAssignment(assignment), ErrInvalid) {
			t.Fatalf("invalid assignment accepted: %#v", assignment)
		}
	}
	if err := ValidateAssignment(Assignment{WorkerRef: "jane", RoleIDs: []string{"manager"}}); err != nil {
		t.Fatalf("valid assignment rejected: %v", err)
	}

	policy := NormalizeVisibility(VisibilityPolicy{RoleID: " MANAGER ", Mode: " allowlist ", OrganizationUnits: []string{" engineering ", "Engineering", " hr "}})
	if policy.RoleID != "manager" || policy.Mode != VisibilityAllowlist || !reflect.DeepEqual(policy.OrganizationUnits, []string{"engineering", "hr"}) {
		t.Fatalf("visibility normalization = %#v", policy)
	}
	for _, mode := range []string{VisibilityAll, VisibilityOwnUnit, VisibilityAllowlist, VisibilityDenylist} {
		if err := ValidateVisibility(VisibilityPolicy{RoleID: "manager", Mode: mode}); err != nil {
			t.Fatalf("mode %q rejected: %v", mode, err)
		}
	}
}

func TestPoliciesForRoles_AndDefaultRoles(t *testing.T) {
	snapshot := Snapshot{Policies: []VisibilityPolicy{
		{RoleID: " MANAGER ", Mode: VisibilityAll},
		{RoleID: "manager", Mode: "invalid"},
		{RoleID: "hr_partner", Mode: VisibilityOwnUnit},
	}}
	got := PoliciesForRoles(snapshot, []string{"MANAGER", "unknown"})
	if len(got) != 1 || got[0].RoleID != "manager" || got[0].Mode != VisibilityAll {
		t.Fatalf("policies = %#v", got)
	}
	roles := DefaultRoles()
	if len(roles) != 9 {
		t.Fatalf("default role count = %d, want 9", len(roles))
	}
	for _, role := range roles {
		if !role.System || !role.Active {
			t.Fatalf("default role is not active system role: %#v", role)
		}
		if err := ValidateRole(role); err != nil {
			t.Fatalf("default role %q invalid: %v", role.ID, err)
		}
	}

	var _ Store = (*testStore)(nil)
	_ = values.TenantId("tenant")
}

func TestPoliciesForRolesDefaultsActiveUnconfiguredRolesToOwnUnit(t *testing.T) {
	snapshot := Snapshot{Roles: DefaultRoles()}
	got := PoliciesForRoles(snapshot, []string{"worker_self"})
	if len(got) != 1 || got[0].RoleID != "worker_self" || got[0].Mode != VisibilityOwnUnit {
		t.Fatalf("default visibility policies = %#v", got)
	}
	if got := PoliciesForRoles(snapshot, []string{"unrecognized"}); len(got) != 0 {
		t.Fatalf("unrecognized admitted role gained a policy: %#v", got)
	}
}

func TestPagePermissionsAreAdditiveAndKeepCRUDIndependent(t *testing.T) {
	snapshot := Snapshot{PagePermissions: []PagePermission{
		{RoleID: "worker_self", PageID: "insights", View: true},
		{RoleID: "report_author", PageID: "insights", View: true, Create: true},
		{RoleID: "unrelated", PageID: "insights", View: true, Delete: true},
	}}
	effective := EffectivePagePermissions(snapshot, []string{"worker_self", "report_author"})
	if !CanPageAction(effective, "insights", ActionView) || !CanPageAction(effective, "insights", ActionCreate) {
		t.Fatalf("merged report permissions = %#v", effective)
	}
	if CanPageAction(effective, "insights", ActionUpdate) || CanPageAction(effective, "insights", ActionDelete) {
		t.Fatalf("ungranted report mutation leaked into %#v", effective)
	}
	if !errors.Is(ValidatePagePermission(PagePermission{RoleID: "worker_self", PageID: "insights", Create: true}), ErrInvalid) {
		t.Fatal("create without view was accepted")
	}
	if err := ValidatePagePermission(PagePermission{RoleID: "worker_self", PageID: "custom-report", View: true}); err != nil {
		t.Fatalf("customer page permission rejected: %v", err)
	}
	defaults := DefaultPagePermissions()
	worker := EffectivePagePermissions(Snapshot{PagePermissions: defaults}, []string{"worker_self"})
	if !CanPageAction(worker, "insights", ActionView) || CanPageAction(worker, "insights", ActionCreate) {
		t.Fatalf("employee report defaults = %#v", worker)
	}
	for _, page := range []string{"organization", "org-explorer", "org-outline", "org-responsive"} {
		if !CanPageAction(worker, page, ActionView) {
			t.Errorf("employee cannot view the implemented organization surface %q", page)
		}
		if CanPageAction(worker, page, ActionCreate) || CanPageAction(worker, page, ActionUpdate) || CanPageAction(worker, page, ActionDelete) {
			t.Errorf("employee gained mutation access to the organization surface %q", page)
		}
	}
	admin := EffectivePagePermissions(Snapshot{PagePermissions: defaults}, []string{"hcm_admin"})
	for _, page := range []string{"organization", "org-explorer", "org-outline", "org-responsive"} {
		if !CanPageAction(admin, page, ActionView) {
			t.Errorf("administrator cannot view the implemented organization surface %q", page)
		}
	}
}

type testStore struct{}

func (*testStore) Bootstrap(context.Context, values.TenantId, string) error { return nil }
func (*testStore) Load(context.Context, values.TenantId, string) (Snapshot, error) {
	return Snapshot{}, nil
}
func (*testStore) SaveRole(context.Context, values.TenantId, string, Role) (Role, error) {
	return Role{}, nil
}
func (*testStore) SaveAssignment(context.Context, values.TenantId, string, Assignment) (Assignment, error) {
	return Assignment{}, nil
}
func (*testStore) SaveVisibility(context.Context, values.TenantId, string, string, VisibilityPolicy) (VisibilityPolicy, error) {
	return VisibilityPolicy{}, nil
}
func (*testStore) SavePagePermission(context.Context, values.TenantId, string, PagePermission) (PagePermission, error) {
	return PagePermission{}, nil
}
