package roleaccess

import "testing"

// TestTodo_RBAC_RT_005 is RBAC-RT-005's PRIMARY: one rule decides diagnostics
// disclosure everywhere, from the server-resolved role set. Administrators
// and auditors are disclosed to by role; anyone else needs the tenant's own
// journey-diagnostics page grant; everything missing denies.
func TestTodo_RBAC_RT_005(t *testing.T) {
	granted := Snapshot{PagePermissions: []PagePermission{
		{RoleID: "promotion_operator", PageID: PageJourneyDiagnostics, View: true},
	}}
	cases := []struct {
		name     string
		snapshot Snapshot
		subject  string
		admitted []string
		want     bool
	}{
		{"admitted hcm_admin", Snapshot{}, "principal:admin", []string{"hcm_admin"}, true},
		{"admitted comp_admin", Snapshot{}, "principal:comp", []string{"comp_admin"}, true},
		{"admitted auditor", Snapshot{}, "principal:auditor", []string{"auditor"}, true},
		{"durable auditor", Snapshot{Assignments: []Assignment{{WorkerRef: "rbac-auditor", RoleIDs: []string{"auditor"}}}}, "rbac-auditor", []string{"worker_self"}, true},
		{"operator page grant", granted, "rbac-operator", []string{"promotion_operator"}, true},
		{"manager without grant", granted, "rbac-dana", []string{"manager"}, false},
		{"worker without grant", granted, "rbac-eli", []string{"worker_self"}, false},
		{"no roles at all", granted, "principal:none", nil, false},
		{"empty snapshot denies", Snapshot{}, "rbac-dana", []string{"manager"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CanDiscloseDiagnostics(c.snapshot, c.subject, c.admitted); got != c.want {
				t.Fatalf("CanDiscloseDiagnostics(%q, %v) = %v, want %v", c.subject, c.admitted, got, c.want)
			}
		})
	}
}

// TestTodo_RBAC_RT_005_Security pins the authorization-before-claims order
// the rule exists for: a durable assignment wholly replaces the admitted
// credential roles, so a revoked administrator's still-valid credential and
// a forged auditor claim grant no diagnostics.
func TestTodo_RBAC_RT_005_Security(t *testing.T) {
	snapshot := Snapshot{
		Assignments: []Assignment{
			{WorkerRef: "rbac-rex", RoleIDs: []string{"worker_self"}},
			{WorkerRef: "rbac-eli", RoleIDs: []string{"worker_self"}},
		},
		PagePermissions: []PagePermission{
			{RoleID: "promotion_operator", PageID: PageJourneyDiagnostics, View: true},
		},
	}
	if CanDiscloseDiagnostics(snapshot, "rbac-rex", []string{"comp_admin"}) {
		t.Fatal("revoked administrator kept diagnostics through the credential")
	}
	// A durable self-service assignment is not widened by auditor or
	// administrator claims on the credential: escalation rides the durable
	// assignment, never the token.
	if CanDiscloseDiagnostics(snapshot, "rbac-eli", []string{"auditor", "hcm_admin"}) {
		t.Fatal("admitted auditor/administrator claims widened a durable self-service assignment")
	}
	if CanDiscloseDiagnostics(Snapshot{}, "principal:operator", []string{"hcmnext.trust.role.operator"}) {
		t.Fatal("operator credential granted diagnostics without the journey-diagnostics page grant")
	}
}
