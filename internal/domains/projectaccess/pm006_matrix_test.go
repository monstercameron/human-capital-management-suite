package projectaccess

import (
	"errors"
	"testing"
)

var pm006AllCapabilities = []Capability{
	ReadProject, ReadTask,
	CreateTask, EditTask, MoveTask, Comment,
	ManageMembers, ManageViews, ManageProject, ConfigureWorkflow, PublishConfiguration, Export, Archive,
}

// TestTodo_PM_006_Security proves the RED lines: a copied board URL grants
// nothing, and removed members retain no reads, streams, cursors, or search
// results (every derived surface funnels through Authorize).
func TestTodo_PM_006_Security(t *testing.T) {
	p := fixture()
	for _, capability := range pm006AllCapabilities {
		if d := Authorize(p, "outsider", "t1", capability, nil); d.Allowed || d.Reason != "no_active_membership" {
			t.Fatalf("copied URL granted %s: %+v", capability, d)
		}
	}
	invited, err := Invite(p, "manager", "new", RoleViewer, "t1", 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range pm006AllCapabilities {
		if d := Authorize(invited, "new", "t1", capability, nil); d.Allowed || d.Reason != "no_active_membership" {
			t.Fatalf("pending invite granted %s: %+v", capability, d)
		}
	}
	accepted, err := AcceptInvitation(invited, "new", "t1")
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := Revoke(accepted, "manager", "new", "t1")
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range pm006AllCapabilities {
		if d := Authorize(revoked, "new", "t1", capability, nil); d.Allowed || d.Reason != "no_active_membership" {
			t.Fatalf("revoked member retained %s: %+v", capability, d)
		}
	}
	if _, err := AcceptInvitation(revoked, "new", "t1"); !errors.Is(err, ErrInvitationNotFound) {
		t.Fatalf("revoked invite accepted: %v", err)
	}
	if _, err := ChangeRole(revoked, "manager", "new", RoleContributor, "t1"); !errors.Is(err, ErrMemberNotFound) {
		t.Fatalf("revoked member role changed: %v", err)
	}
}

// TestTodo_PM_006_Integration walks invite, accept, role change, removal, and
// re-invite while proving reads always reflect the current policy snapshot.
func TestTodo_PM_006_Integration(t *testing.T) {
	p := fixture()
	invited, err := Invite(p, "manager", "new", RoleContributor, "t1", 1)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := AcceptInvitation(invited, "new", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if d := Authorize(accepted, "new", "t1", CreateTask, nil); !d.Allowed {
		t.Fatalf("contributor denied create: %+v", d)
	}
	demoted, err := ChangeRole(accepted, "manager", "new", RoleViewer, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if d := Authorize(demoted, "new", "t1", CreateTask, nil); d.Allowed || d.Reason != "role_denied" {
		t.Fatalf("demoted member retained create: %+v", d)
	}
	if d := Authorize(accepted, "new", "t1", CreateTask, nil); !d.Allowed {
		t.Fatalf("role change mutated the prior snapshot: %+v", d)
	}
	revoked, err := Revoke(demoted, "manager", "new", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if d := Authorize(revoked, "new", "t1", ReadTask, nil); d.Allowed {
		t.Fatalf("revocation not immediate: %+v", d)
	}
	reinvited, err := Invite(revoked, "manager", "new", RoleViewer, "t1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if d := Authorize(reinvited, "new", "t1", ReadTask, nil); d.Allowed {
		t.Fatalf("re-invite granted access before accept: %+v", d)
	}
	reaccepted, err := AcceptInvitation(reinvited, "new", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if d := Authorize(reaccepted, "new", "t1", ReadTask, nil); !d.Allowed {
		t.Fatalf("re-accepted member denied: %+v", d)
	}
}

// TestTodo_PM_006_Golden pins the full role-by-capability decision table.
func TestTodo_PM_006_Golden(t *testing.T) {
	p := fixture()
	users := map[Role]UserID{RoleOwner: "owner", RoleManager: "manager", RoleContributor: "member", RoleViewer: "viewer"}
	grants := map[Role]map[Capability]bool{}
	for role := range users {
		grants[role] = map[Capability]bool{}
		for _, capability := range pm006AllCapabilities {
			grants[role][capability] = role == RoleOwner || role == RoleManager
		}
	}
	for _, capability := range []Capability{ReadProject, ReadTask, CreateTask, EditTask, MoveTask, Comment} {
		grants[RoleContributor][capability] = true
	}
	for _, capability := range []Capability{ReadProject, ReadTask} {
		grants[RoleViewer][capability] = true
	}
	for role, user := range users {
		for _, capability := range pm006AllCapabilities {
			d := Authorize(p, user, "t1", capability, nil)
			if d.Allowed != grants[role][capability] {
				t.Fatalf("golden %s/%s = %+v", role, capability, d)
			}
			wantReason := "role_denied"
			if grants[role][capability] {
				wantReason = "role_grant"
			}
			if d.Reason != wantReason {
				t.Fatalf("golden reason %s/%s = %+v", role, capability, d)
			}
		}
	}
	if d := Authorize(p, "owner", "t1", Capability("unknown"), nil); d.Allowed || d.Reason != "role_denied" {
		t.Fatalf("unknown capability granted: %+v", d)
	}
}
