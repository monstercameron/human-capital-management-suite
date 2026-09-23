package roleaccess

import (
	"errors"
	"testing"
)

// TestTodo_RBAC_RT_017 pins RBAC-RT-017: an inactive role contributes no
// grant anywhere (page, feature, visibility) and system roles cannot be
// renamed, deactivated or stripped of their system designation.
func TestTodo_RBAC_RT_017(t *testing.T) {
	snapshot := Snapshot{
		Roles: []Role{
			{ID: "report_author", Name: "Report author", Active: false},
			{ID: "manager", Name: "Manager", Active: true},
		},
		PagePermissions: []PagePermission{
			{RoleID: "report_author", PageID: "insights", View: true, Create: true},
			{RoleID: "manager", PageID: "insights", View: true},
		},
		FeaturePermissions: []FeaturePermission{
			{RoleID: "report_author", PageID: "settings", FeatureID: "profile_summary", View: true, Create: true},
			{RoleID: "manager", PageID: "settings", FeatureID: "profile_summary", View: true},
		},
		Policies: []VisibilityPolicy{
			{RoleID: "report_author", Mode: VisibilityAllowlist, OrganizationUnits: []string{"engineering"}},
		},
	}

	if got := EffectivePagePermissions(snapshot, []string{"report_author"}); len(got) != 0 {
		t.Fatalf("inactive role granted page permissions: %#v", got)
	}
	effective := EffectivePagePermissions(snapshot, []string{"report_author", "manager"})
	if len(effective) != 1 || effective[0].PageID != "insights" || !effective[0].View || effective[0].Create {
		t.Fatalf("inactive create grant leaked into effective pages: %#v", effective)
	}
	if CanPageAction(effective, "insights", ActionCreate) {
		t.Fatalf("inactive role granted insights/create: %#v", effective)
	}

	if got := EffectiveFeaturePermissions(snapshot, []string{"report_author"}); len(got) != 0 {
		t.Fatalf("inactive role granted feature permissions: %#v", got)
	}
	features := EffectiveFeaturePermissions(snapshot, []string{"manager", "report_author"})
	if len(features) != 1 || features[0].FeatureID != "profile_summary" || !features[0].View || features[0].Create {
		t.Fatalf("inactive create grant leaked into effective features: %#v", features)
	}

	if got := PoliciesForRoles(snapshot, []string{"report_author"}); len(got) != 0 {
		t.Fatalf("inactive role kept a visibility policy: %#v", got)
	}
	active := PoliciesForRoles(snapshot, []string{"manager"})
	if len(active) != 1 || active[0].RoleID != "manager" || active[0].Mode != VisibilityOwnUnit {
		t.Fatalf("active unconfigured role lost its default policy: %#v", active)
	}

	system := Role{ID: "hcm_admin", Name: "HCM administrator", Description: "Administers HCM.", System: true, Active: true}
	renamed := system
	renamed.Name = "HCM administrator (renamed)"
	if err := ValidateRoleUpdate(system, renamed); !errors.Is(err, ErrInvalid) {
		t.Fatalf("system role rename accepted: %v", err)
	}
	deactivated := system
	deactivated.Active = false
	if err := ValidateRoleUpdate(system, deactivated); !errors.Is(err, ErrInvalid) {
		t.Fatalf("system role deactivation accepted: %v", err)
	}
	desystemed := system
	desystemed.System = false
	if err := ValidateRoleUpdate(system, desystemed); !errors.Is(err, ErrInvalid) {
		t.Fatalf("system role designation stripping accepted: %v", err)
	}
	edited := system
	edited.Description = "Updated duty description."
	if err := ValidateRoleUpdate(system, edited); err != nil {
		t.Fatalf("system role description edit refused: %v", err)
	}
}

// TestTodo_RBAC_RT_017_Security probes the adversarial variants: smuggled
// case, union leaks across roles, an inactive ALL policy, and update guard
// bypass attempts.
func TestTodo_RBAC_RT_017_Security(t *testing.T) {
	snapshot := Snapshot{
		Roles: []Role{
			{ID: "manager", Name: "Manager", Active: false},
			{ID: "worker_self", Name: "Employee self-service", Active: true},
		},
		PagePermissions: []PagePermission{
			{RoleID: " MANAGER ", PageID: "insights", View: true, Delete: true},
			{RoleID: "worker_self", PageID: "insights", View: true},
		},
		FeaturePermissions: []FeaturePermission{
			{RoleID: "MANAGER", PageID: "settings", FeatureID: "profile_summary", View: true, Delete: true},
			{RoleID: "worker_self", PageID: "settings", FeatureID: "profile_summary", View: true},
		},
		Policies: []VisibilityPolicy{
			{RoleID: "manager", Mode: VisibilityAll},
		},
	}

	effective := EffectivePagePermissions(snapshot, []string{"MANAGER", "worker_self"})
	if CanPageAction(effective, "insights", ActionDelete) {
		t.Fatalf("case-smuggled inactive delete grant leaked: %#v", effective)
	}
	if !CanPageAction(effective, "insights", ActionView) {
		t.Fatalf("active view grant lost while filtering inactive role: %#v", effective)
	}
	features := EffectiveFeaturePermissions(snapshot, []string{"manager", "worker_self"})
	for _, permission := range features {
		if permission.Delete {
			t.Fatalf("inactive feature delete grant leaked: %#v", features)
		}
	}
	policies := PoliciesForRoles(snapshot, []string{"manager"})
	if len(policies) != 0 {
		t.Fatalf("inactive ALL visibility policy survived: %#v", policies)
	}
	evaluator := NewVisibilityEvaluator(policies, "engineering")
	if evaluator.Allows("finance", false) {
		t.Fatal("inactive ALL policy still admits another unit")
	}

	system := Role{ID: "hcm_admin", Name: "HCM administrator", System: true, Active: true}
	padded := system
	padded.Name = "  HCM administrator (renamed)  "
	if err := ValidateRoleUpdate(system, padded); !errors.Is(err, ErrInvalid) {
		t.Fatalf("padded system role rename accepted: %v", err)
	}
	cased := system
	cased.Name = "hcm administrator"
	if err := ValidateRoleUpdate(system, cased); !errors.Is(err, ErrInvalid) {
		t.Fatalf("case-only system role rename accepted: %v", err)
	}
	swapped := system
	swapped.ID = "hcm_admin_clone"
	if err := ValidateRoleUpdate(system, swapped); !errors.Is(err, ErrInvalid) {
		t.Fatalf("system role id swap accepted: %v", err)
	}
	nameless := system
	nameless.Name = ""
	if err := ValidateRoleUpdate(system, nameless); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid system role update accepted: %v", err)
	}

	custom := Role{ID: "regional_auditor", Name: "Regional auditor", Active: true}
	customRename := custom
	customRename.Name = "Regional auditor (renamed)"
	if err := ValidateRoleUpdate(custom, customRename); err != nil {
		t.Fatalf("custom role rename refused: %v", err)
	}
	customOff := custom
	customOff.Active = false
	if err := ValidateRoleUpdate(custom, customOff); err != nil {
		t.Fatalf("custom role deactivation refused: %v", err)
	}
	if err := ValidateRoleUpdate(custom, Role{ID: "regional_auditor"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("nameless custom role update accepted")
	}
}
