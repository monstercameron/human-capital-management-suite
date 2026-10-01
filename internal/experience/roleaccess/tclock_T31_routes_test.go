package roleaccess

import "testing"

func TestTClockT31Routes(t *testing.T) {
	permissions := DefaultTimePagePermissions()
	for _, role := range []string{"hcm_admin", "manager", "hr_partner", "hiring_manager", "payroll_manager", "worker_self"} {
		if !hasTClockPageGrant(permissions, role, PageTimecard) {
			t.Fatalf("timecard grant missing for %s", role)
		}
	}
	for _, role := range []string{"hcm_admin", "manager", "hr_partner", "hiring_manager", "payroll_manager"} {
		if !hasTClockPageGrant(permissions, role, PageCrewSchedule) {
			t.Fatalf("schedule grant missing for %s", role)
		}
	}
	for _, role := range []string{"hcm_admin", "comp_admin"} {
		if !hasTClockPageGrant(permissions, role, PageClockDevices) {
			t.Fatalf("device fleet grant missing for %s", role)
		}
	}
	if hasTClockPageGrant(permissions, "worker_self", PageClockDevices) || hasTClockPageGrant(permissions, "finance_partner", PageCrewSchedule) {
		t.Fatal("time-clock route grants widened beyond their audiences")
	}
}

func hasTClockPageGrant(permissions []PagePermission, role, page string) bool {
	for _, permission := range permissions {
		if permission.RoleID == role && permission.PageID == page && permission.View {
			return true
		}
	}
	return false
}
