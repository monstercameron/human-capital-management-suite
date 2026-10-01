package projectaccess

import "testing"

func TestTodo_PM_036_Security(t *testing.T) {
	p := fixture()
	for _, tc := range []struct {
		name, user string
		tenant     TenantID
		cap        Capability
		reason     string
	}{
		{name: "foreign tenant", user: "owner", tenant: "tenant-other", cap: ReadTask, reason: "tenant_scope"},
		{name: "revoked member", user: "missing", tenant: "t1", cap: ReadTask, reason: "no_active_membership"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decision := Authorize(p, UserID(tc.user), tc.tenant, tc.cap, nil)
			if decision.Allowed || decision.Reason != tc.reason {
				t.Fatalf("decision = %+v, want denied for %s", decision, tc.reason)
			}
		})
	}
}

func TestTodo_PM_036_Conformance(t *testing.T) {
	p := fixture()
	decision := Authorize(p, "owner", "t1", ReadTask, nil)
	if !decision.Allowed {
		t.Fatalf("current project member denied: %+v", decision)
	}
	revoked, err := Revoke(p, "manager", "member", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if decision := Authorize(revoked, "member", "t1", ReadTask, nil); decision.Allowed || decision.Reason != "no_active_membership" {
		t.Fatalf("revoked member retained project access: %+v", decision)
	}
}
