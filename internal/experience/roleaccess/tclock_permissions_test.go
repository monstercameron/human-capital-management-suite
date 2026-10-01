package roleaccess

import "testing"

func TestTodo_TCLOCK016_DefaultTimeClockPagePermissionsAreNarrow(t *testing.T) {
	permissions := DefaultTimeClockPagePermissions()
	if len(permissions) != 5 {
		t.Fatalf("clock page grants = %d, want 5", len(permissions))
	}
	for _, role := range []string{"hcm_admin", "manager", "hr_partner", "payroll_manager", "worker_self"} {
		if !hasPageGrant(permissions, role) {
			t.Fatalf("missing clock page grant for %s", role)
		}
	}
	if hasPageGrant(permissions, "finance_partner") {
		t.Fatal("finance_partner unexpectedly received clock page access")
	}
}

func TestTodo_TCLOCK017_DefaultTimeClockFeaturePermissionsGateKiosk(t *testing.T) {
	permissions := DefaultTimeClockFeaturePermissions()
	for _, role := range []string{"hcm_admin", "manager", "hr_partner", "payroll_manager"} {
		if !hasFeatureGrant(permissions, role, FeatureTimeClockKiosk) {
			t.Fatalf("missing kiosk grant for %s", role)
		}
	}
	for _, role := range []string{"worker_self", "finance_partner"} {
		if hasFeatureGrant(permissions, role, FeatureTimeClockKiosk) {
			t.Fatalf("%s unexpectedly received kiosk grant", role)
		}
	}
	if !hasFeatureGrant(permissions, "worker_self", FeatureTimeClockActions) {
		t.Fatal("worker_self must retain clock action access")
	}
}

func TestTodo_TCLOCK017_ConstrainGeneratedFeaturesReplacesClockCeiling(t *testing.T) {
	generated := []FeaturePermission{
		{RoleID: "worker_self", PageID: PageTimeClock, FeatureID: "content", View: true},
		{RoleID: "worker_self", PageID: PageTimeClock, FeatureID: FeatureTimeClockKiosk, View: true, Create: true},
		{RoleID: "finance_partner", PageID: PageTimeClock, FeatureID: FeatureTimeClockActions, View: true},
		{RoleID: "manager", PageID: PageTimeClock, FeatureID: FeatureTimeClockKiosk, View: true, Create: true},
		{RoleID: "manager", PageID: "people", FeatureID: "directory", View: true},
	}
	constrained := ConstrainDefaultTimeClockFeaturePermissions(generated)
	if hasFeatureGrant(constrained, "worker_self", FeatureTimeClockKiosk) || hasFeatureGrant(constrained, "finance_partner", FeatureTimeClockActions) {
		t.Fatal("clock feature ceiling retained an unauthorized generated grant")
	}
	if !hasFeatureGrant(constrained, "worker_self", "content") {
		t.Fatal("worker_self lost the clock page content grant")
	}
	if !hasFeatureGrant(constrained, "manager", FeatureTimeClockKiosk) {
		t.Fatal("clock feature replacement dropped an authorized grant")
	}
	preserved := false
	for _, permission := range constrained {
		if permission.RoleID == "manager" && permission.PageID == "people" && permission.FeatureID == "directory" && permission.View {
			preserved = true
			break
		}
	}
	if !preserved {
		t.Fatal("non-clock generated feature was not preserved")
	}
}

func TestTodo_TCLOCK017_ConstrainGeneratedFeaturesDoesNotAdmitMissingPage(t *testing.T) {
	generated := []FeaturePermission{{RoleID: "manager", PageID: "people", FeatureID: "directory", View: true}}
	constrained := ConstrainDefaultTimeClockFeaturePermissions(generated)
	if len(constrained) != len(generated) || constrained[0] != generated[0] {
		t.Fatalf("missing clock page changed generated defaults: %#v", constrained)
	}
}

func TestTodo_TCLOCK017_ClockContentRemainsReadableWhileFinanceIsDenied(t *testing.T) {
	page := DefaultTimeClockPagePermissions()
	features := ConstrainDefaultTimeClockFeaturePermissions([]FeaturePermission{
		{RoleID: "worker_self", PageID: PageTimeClock, FeatureID: "content", View: true},
		{RoleID: "finance_partner", PageID: PageTimeClock, FeatureID: "content", View: true},
	})
	if !CanFeatureAction(page, features, PageTimeClock, "content", ActionView) {
		t.Fatal("clock page content unexpectedly denied")
	}
	financePage := []PagePermission{{RoleID: "finance_partner", PageID: PageTimeClock, View: false}}
	if CanFeatureAction(financePage, features, PageTimeClock, "content", ActionView) {
		t.Fatal("finance partner unexpectedly received clock content access")
	}
}

func hasPageGrant(permissions []PagePermission, role string) bool {
	for _, permission := range permissions {
		if permission.RoleID == role && permission.PageID == PageTimeClock && permission.View {
			return true
		}
	}
	return false
}

func hasFeatureGrant(permissions []FeaturePermission, role, feature string) bool {
	for _, permission := range permissions {
		if permission.RoleID == role && permission.PageID == PageTimeClock && permission.FeatureID == feature && permission.View {
			return true
		}
	}
	return false
}
