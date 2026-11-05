package projectaccess

import (
	"errors"
	"testing"
	"time"
)

func fixture() Project {
	return Project{Tenant: "t1", ID: "p1", ClassLevel: 2, Memberships: []Membership{
		{Tenant: "t1", User: "owner", Role: RoleOwner, State: MembershipActive},
		{Tenant: "t1", User: "manager", Role: RoleManager, State: MembershipActive},
		{Tenant: "t1", User: "member", Role: RoleContributor, State: MembershipActive},
		{Tenant: "t1", User: "viewer", Role: RoleViewer, State: MembershipActive},
	}}
}

func TestTodo_PM_006(t *testing.T) {
	p := fixture()
	invited, err := Invite(p, "manager", "new", RoleViewer, "t1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if d := Authorize(invited, "new", "t1", ReadProject, nil); d.Allowed {
		t.Fatal("pending invite granted access")
	}
	accepted, err := AcceptInvitation(invited, "new", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if d := Authorize(accepted, "new", "t1", ReadProject, nil); !d.Allowed {
		t.Fatalf("accepted member denied: %+v", d)
	}
	changed, err := ChangeRole(accepted, "manager", "new", RoleContributor, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if d := Authorize(changed, "new", "t1", CreateTask, nil); !d.Allowed {
		t.Fatalf("changed role not effective: %+v", d)
	}
	revoked, err := Revoke(changed, "manager", "new", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if d := Authorize(revoked, "new", "t1", ReadTask, nil); d.Allowed || d.Reason != "no_active_membership" {
		t.Fatalf("revoked access retained: %+v", d)
	}
	if d := Authorize(p, "new", "t1", ReadProject, nil); d.Allowed {
		t.Fatal("operation mutated original project")
	}
}

func TestTodo_PM_007(t *testing.T) {
	p := fixture()
	transferred, audit, err := TransferOwnership(p, "owner", "manager", "t1", nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if audit != (Transfer{From: "owner", To: "manager"}) {
		t.Fatalf("unexpected transfer record: %+v", audit)
	}
	if activeOwnerCount(transferred) != 1 {
		t.Fatalf("active owners = %d, want 1", activeOwnerCount(transferred))
	}
	if role, ok := activeRole(transferred, "manager"); !ok || role != RoleOwner {
		t.Fatalf("new owner = %q, active=%v", role, ok)
	}
	if role, ok := activeRole(transferred, "owner"); !ok || role != RoleManager {
		t.Fatalf("former owner role = %q, active=%v", role, ok)
	}
	if _, err := Revoke(transferred, "manager", "manager", "t1"); !errors.Is(err, ErrOwnerRequired) {
		t.Fatalf("last-owner removal error = %v", err)
	}
	grant := RecoveryGrant{Tenant: "t1", Project: "p1", Actor: "recovery", Purpose: "owner unavailable", Approver: "approver", StartsAt: time.Unix(10, 0), EndsAt: time.Unix(20, 0)}
	recovered, record, err := TransferOwnership(p, "recovery", "member", "t1", &grant, time.Unix(15, 0))
	if err != nil {
		t.Fatal(err)
	}
	if record.From != "recovery" || record.To != "member" || activeOwnerCount(recovered) < 1 {
		t.Fatalf("bad recovery transfer: %+v; owners=%d", record, activeOwnerCount(recovered))
	}
	if _, _, err = TransferOwnership(p, "recovery", "member", "t1", &grant, time.Unix(20, 0)); !errors.Is(err, ErrRecoveryNotAuthorized) {
		t.Fatalf("expired recovery grant error = %v", err)
	}
}

func TestTodo_PM_007_Security(t *testing.T) {
	p := fixture()
	good := RecoveryGrant{Tenant: "t1", Project: "p1", Actor: "recovery", Purpose: "owner unavailable", Approver: "approver", StartsAt: time.Unix(10, 0), EndsAt: time.Unix(20, 0)}
	denied := []struct {
		name  string
		actor UserID
		grant *RecoveryGrant
		at    time.Time
	}{
		{"no grant for non-owner", "manager", nil, time.Time{}},
		{"no grant for outsider", "outsider", nil, time.Time{}},
		{"empty purpose", "recovery", &RecoveryGrant{Tenant: "t1", Project: "p1", Actor: "recovery", Approver: "approver", StartsAt: time.Unix(10, 0), EndsAt: time.Unix(20, 0)}, time.Unix(15, 0)},
		{"self-approved", "recovery", &RecoveryGrant{Tenant: "t1", Project: "p1", Actor: "recovery", Purpose: "owner unavailable", Approver: "recovery", StartsAt: time.Unix(10, 0), EndsAt: time.Unix(20, 0)}, time.Unix(15, 0)},
		{"missing approver", "recovery", &RecoveryGrant{Tenant: "t1", Project: "p1", Actor: "recovery", Purpose: "owner unavailable", StartsAt: time.Unix(10, 0), EndsAt: time.Unix(20, 0)}, time.Unix(15, 0)},
		{"wrong tenant", "recovery", &RecoveryGrant{Tenant: "other", Project: "p1", Actor: "recovery", Purpose: "owner unavailable", Approver: "approver", StartsAt: time.Unix(10, 0), EndsAt: time.Unix(20, 0)}, time.Unix(15, 0)},
		{"wrong project", "recovery", &RecoveryGrant{Tenant: "t1", Project: "p2", Actor: "recovery", Purpose: "owner unavailable", Approver: "approver", StartsAt: time.Unix(10, 0), EndsAt: time.Unix(20, 0)}, time.Unix(15, 0)},
		{"wrong actor", "impostor", &good, time.Unix(15, 0)},
		{"before window", "recovery", &good, time.Unix(9, 0)},
		{"at window end", "recovery", &good, time.Unix(20, 0)},
		{"after window", "recovery", &good, time.Unix(21, 0)},
		{"zero-length window", "recovery", &RecoveryGrant{Tenant: "t1", Project: "p1", Actor: "recovery", Purpose: "owner unavailable", Approver: "approver", StartsAt: time.Unix(15, 0), EndsAt: time.Unix(15, 0)}, time.Unix(15, 0)},
	}
	for _, tc := range denied {
		if _, _, err := TransferOwnership(p, tc.actor, "member", "t1", tc.grant, tc.at); !errors.Is(err, ErrRecoveryNotAuthorized) {
			t.Fatalf("%s: error = %v, want ErrRecoveryNotAuthorized", tc.name, err)
		}
	}
	if activeOwnerCount(p) != 1 {
		t.Fatalf("denied attempts mutated owners: %d", activeOwnerCount(p))
	}
	// Recovery cannot self-grant ownership.
	if _, _, err := TransferOwnership(p, "recovery", "recovery", "t1", &good, time.Unix(15, 0)); err == nil {
		t.Fatal("recovery self-grant succeeded")
	}
	// A successful recovery transfer does not make the operator a member or reader.
	recovered, _, err := TransferOwnership(p, "recovery", "member", "t1", &good, time.Unix(15, 0))
	if err != nil {
		t.Fatal(err)
	}
	if d := Authorize(recovered, "recovery", "t1", ReadTask, nil); d.Allowed || d.Reason != "no_active_membership" {
		t.Fatalf("recovery operator gained read access: %+v", d)
	}
}

func TestTodo_PM_007_Golden(t *testing.T) {
	p := fixture()
	transferred, audit, err := TransferOwnership(p, "owner", "manager", "t1", nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if audit != (Transfer{From: "owner", To: "manager"}) {
		t.Fatalf("transfer record = %+v", audit)
	}
	wantRoles := map[UserID]Role{"owner": RoleManager, "manager": RoleOwner, "member": RoleContributor, "viewer": RoleViewer}
	for user, want := range wantRoles {
		role, ok := activeRole(transferred, user)
		if !ok || role != want {
			t.Fatalf("transfer role %q = %q active=%v, want %q", user, role, ok, want)
		}
	}
	if n := activeOwnerCount(transferred); n != 1 {
		t.Fatalf("transfer owners = %d, want exactly 1", n)
	}
	// Recovery at the inclusive window start adds an owner without demoting anyone.
	grant := RecoveryGrant{Tenant: "t1", Project: "p1", Actor: "recovery", Purpose: "owner unavailable", Approver: "approver", StartsAt: time.Unix(10, 0), EndsAt: time.Unix(20, 0)}
	recovered, record, err := TransferOwnership(p, "recovery", "member", "t1", &grant, time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if record != (Transfer{From: "recovery", To: "member"}) {
		t.Fatalf("recovery record = %+v", record)
	}
	wantRecovery := map[UserID]Role{"owner": RoleOwner, "manager": RoleManager, "member": RoleOwner, "viewer": RoleViewer}
	for user, want := range wantRecovery {
		role, ok := activeRole(recovered, user)
		if !ok || role != want {
			t.Fatalf("recovery role %q = %q active=%v, want %q", user, role, ok, want)
		}
	}
	if n := activeOwnerCount(recovered); n != 2 {
		t.Fatalf("recovery owners = %d, want exactly 2", n)
	}
}

func TestTodo_PM_036(t *testing.T) {
	p := fixture()
	if d := Authorize(p, "owner", "other-tenant", Archive, nil); d.Allowed || d.Reason != "tenant_scope" {
		t.Fatalf("cross-tenant decision: %+v", d)
	}
	if d := Authorize(p, "owner", "t1", Archive, map[Capability]bool{Archive: true}); d.Allowed || d.Reason != "explicit_deny" {
		t.Fatalf("explicit deny did not win: %+v", d)
	}
	if d := Authorize(p, "viewer", "t1", EditTask, nil); d.Allowed {
		t.Fatalf("viewer edited task: %+v", d)
	}
	if d := Authorize(p, "viewer", "t1", ReadTask, nil); !d.Allowed {
		t.Fatalf("viewer could not read task: %+v", d)
	}
	if d := Authorize(p, "owner", "", ReadProject, nil); d.Allowed {
		t.Fatalf("empty tenant allowed: %+v", d)
	}
}

func TestMembershipRejectsOutOfScopeAndInvalidOperations(t *testing.T) {
	p := fixture()
	if _, err := Invite(p, "manager", "new", RoleViewer, "other", 1); !errors.Is(err, ErrTenantMismatch) {
		t.Fatal(err)
	}
	if _, err := Invite(p, "viewer", "new", RoleViewer, "t1", 1); !errors.Is(err, ErrUnauthorized) {
		t.Fatal(err)
	}
	if _, err := Invite(p, "manager", "new", RoleOwner, "t1", 1); !errors.Is(err, ErrInvalidRole) {
		t.Fatal(err)
	}
	if _, err := Invite(p, "manager", "new", RoleViewer, "t1", 3); !errors.Is(err, ErrUnauthorized) {
		t.Fatal(err)
	}
	invited, err := Invite(p, "manager", "new", RoleViewer, "t1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = AcceptInvitation(invited, "new", "other"); !errors.Is(err, ErrTenantMismatch) {
		t.Fatal(err)
	}
	if _, err = AcceptInvitation(p, "new", "t1"); !errors.Is(err, ErrInvitationNotFound) {
		t.Fatal(err)
	}
	if _, err = ChangeRole(p, "manager", "owner", RoleViewer, "t1"); !errors.Is(err, ErrUnauthorized) {
		t.Fatal(err)
	}
	if _, err = ChangeRole(p, "manager", "missing", RoleViewer, "t1"); !errors.Is(err, ErrMemberNotFound) {
		t.Fatal(err)
	}
	if _, err = Revoke(p, "manager", "owner", "t1"); !errors.Is(err, ErrOwnerRequired) {
		t.Fatalf("last-owner revocation error = %v", err)
	}
	if _, err = Revoke(p, "manager", "missing", "t1"); !errors.Is(err, ErrMemberNotFound) {
		t.Fatal(err)
	}
	badGrant := RecoveryGrant{Tenant: "wrong", Project: "p1", Actor: "recovery", Purpose: "", Approver: "recovery", StartsAt: time.Unix(1, 0), EndsAt: time.Unix(2, 0)}
	if _, _, err = TransferOwnership(p, "viewer", "member", "t1", &badGrant, time.Unix(1, 0)); !errors.Is(err, ErrRecoveryNotAuthorized) {
		t.Fatal(err)
	}
	if roleAllows("UNRECOGNIZED", ReadProject) || roleAllows(RoleOwner, "unknown") {
		t.Fatal("unknown role or capability granted access")
	}
}
