package roleaccess

import "testing"

func TestDocsDefaultPageGrantIsReadOnly(t *testing.T) {
	for _, role := range []string{"hcm_admin", "comp_admin", "manager", "hr_partner", "hiring_manager", "payroll_manager", "worker_self", "finance_partner", "intent_author", "promotion_operator"} {
		permissions := EffectivePagePermissions(Snapshot{PagePermissions: DefaultPagePermissions()}, []string{role})
		if !CanPageAction(permissions, "docs", ActionView) {
			t.Errorf("%s cannot view Docs", role)
		}
		if role != "hcm_admin" && role != "comp_admin" && CanPageAction(permissions, "docs", ActionUpdate) {
			t.Errorf("%s unexpectedly can update Docs through its page grant", role)
		}
	}
}
