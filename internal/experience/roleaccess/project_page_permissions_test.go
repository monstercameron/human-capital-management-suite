package roleaccess

import "testing"

func TestTodo_PM_029_ProjectPageDefaultsAreVisibleWithoutRecordGrants(t *testing.T) {
	defaults := Snapshot{PagePermissions: DefaultPagePermissions()}
	roles := []string{"hcm_admin", "comp_admin", "manager", "hr_partner", "hiring_manager", "payroll_manager", "worker_self", "finance_partner", "intent_author", "promotion_operator"}
	for _, role := range roles {
		permissions := EffectivePagePermissions(defaults, []string{role})
		for _, page := range []string{"projects", "project"} {
			if !CanPageAction(permissions, page, ActionView) {
				t.Errorf("%s cannot view the %s page", role, page)
			}
			for _, action := range []string{ActionCreate, ActionUpdate, ActionDelete} {
				if CanPageAction(permissions, page, action) {
					t.Errorf("%s gained %s permission on the %s page", role, action, page)
				}
			}
		}
	}
	if got := EffectivePagePermissions(defaults, []string{"unrecognized"}); len(got) != 0 {
		t.Fatalf("unrecognized role gained project page permissions: %#v", got)
	}
}
