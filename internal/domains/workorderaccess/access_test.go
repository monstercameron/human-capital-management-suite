package workorderaccess

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
)

func accessFixture() (WorkOrder, projectaccess.Project) {
	return WorkOrder{
			Tenant: "tenant-a", Project: "project-a", ID: "order-a", InitiatorID: "field", Private: true,
			Grants: []Grant{
				{Tenant: "tenant-a", Project: "project-a", WorkOrder: "order-a", User: "field", Capability: Read, State: Active},
				{Tenant: "tenant-a", Project: "project-a", WorkOrder: "order-a", User: "finance", Capability: ViewCost, State: Active},
				{Tenant: "tenant-a", Project: "project-a", WorkOrder: "order-a", User: "finance", Capability: ViewPay, State: Revoked},
			},
			Assignments: []Assignment{{Tenant: "tenant-a", WorkOrder: "order-a", Worker: "worker-a", State: Active}},
		}, projectaccess.Project{Tenant: "tenant-a", ID: "project-a", Memberships: []projectaccess.Membership{
			{Tenant: "tenant-a", User: "field", Role: projectaccess.RoleContributor, State: projectaccess.MembershipActive},
			{Tenant: "tenant-a", User: "peer", Role: projectaccess.RoleContributor, State: projectaccess.MembershipActive},
			{Tenant: "tenant-a", User: "finance", Role: projectaccess.RoleManager, State: projectaccess.MembershipActive},
			{Tenant: "tenant-a", User: "former", Role: projectaccess.RoleManager, State: projectaccess.MembershipRevoked},
		}}
}

func TestInitiatorMayRequestPhaseButOtherContributorsNeedAuthority(t *testing.T) {
	o, p := accessFixture()
	o.Private = false
	if d := Authorize(o, p, "field", "tenant-a", Advance); !d.Allowed || d.Reason != "active_initiator" {
		t.Fatalf("active initiator denied phase request: %+v", d)
	}
	if d := Authorize(o, p, "peer", "tenant-a", Advance); d.Allowed {
		t.Fatalf("other contributor received initiator phase authority: %+v", d)
	}
	for i := range p.Memberships {
		if p.Memberships[i].User == "field" {
			p.Memberships[i].State = projectaccess.MembershipRevoked
		}
	}
	if d := Authorize(o, p, "field", "tenant-a", Advance); d.Allowed || d.Reason != "no_active_project_membership" {
		t.Fatalf("revoked initiator retained phase authority: %+v", d)
	}
}

func TestAuthorizeScopesCurrentProjectMembershipAndPrivateOrder(t *testing.T) {
	o, p := accessFixture()
	if d := Authorize(o, p, "field", "tenant-b", Read); d.Allowed || d.Reason != "tenant_or_project_scope" {
		t.Fatalf("cross-tenant access = %+v", d)
	}
	if d := Authorize(o, p, "former", "tenant-a", Read); d.Allowed || d.Reason != "no_active_project_membership" {
		t.Fatalf("revoked project member access = %+v", d)
	}
	if d := Authorize(o, p, "finance", "tenant-a", Read); d.Allowed || d.Reason != "private_order" {
		t.Fatalf("private order leaked to ungranted member: %+v", d)
	}
	if d := Authorize(o, p, "field", "tenant-a", Read); !d.Allowed {
		t.Fatalf("current scoped reader denied: %+v", d)
	}
}

func TestFieldVisibilitySeparatesCostTotalsFromPayRates(t *testing.T) {
	o, p := accessFixture()
	if !FieldVisible(o, p, "finance", "tenant-a", ViewCost) {
		t.Fatal("finance cost total should be visible")
	}
	if FieldVisible(o, p, "finance", "tenant-a", ViewPay) {
		t.Fatal("revoked pay-rate grant must not expose individual rates")
	}
	if FieldVisible(o, p, "field", "tenant-a", ViewCost) {
		t.Fatal("work-order reader without cost grant saw costs")
	}
}

func TestProjectRoleDefaultsAreLimitedAndPrivateOrdersFailClosed(t *testing.T) {
	o, p := accessFixture()
	o.Private = false
	if d := Authorize(o, p, "field", "tenant-a", Request); !d.Allowed || d.Reason != "active_project_role" {
		t.Fatalf("contributor request permission = %+v", d)
	}
	if d := Authorize(o, p, "field", "tenant-a", RecordCost); d.Allowed {
		t.Fatalf("contributor received manager-only cost authority: %+v", d)
	}
	if d := Authorize(o, p, "finance", "tenant-a", RecordCost); !d.Allowed {
		t.Fatalf("active manager denied cost authority: %+v", d)
	}
	if FieldVisible(o, p, "finance", "tenant-a", ViewPay) {
		t.Fatal("project manager role must not imply individual pay-rate access")
	}
	o.Private = true
	if d := Authorize(o, p, "finance", "tenant-a", RecordCost); d.Allowed || d.Reason != "private_order" {
		t.Fatalf("private order without a stored scoped grant did not fail closed: %+v", d)
	}
}

func TestExplicitDenyOverridesRoleAndGrant(t *testing.T) {
	o, p := accessFixture()
	o.Private = false
	o.Denials = []Denial{{Tenant: "tenant-a", Project: "project-a", WorkOrder: "order-a", User: "finance", Capability: ViewCost, State: Active}}
	if d := Authorize(o, p, "finance", "tenant-a", ViewCost); d.Allowed || d.Reason != "explicit_deny" {
		t.Fatalf("explicit denial failed to override manager role: %+v", d)
	}
	o.Grants = append(o.Grants, Grant{Tenant: "tenant-a", Project: "project-a", WorkOrder: "order-a", User: "finance", Capability: ViewCost, State: Active})
	if d := Authorize(o, p, "finance", "tenant-a", ViewCost); d.Allowed || d.Reason != "explicit_deny" {
		t.Fatalf("explicit denial failed to override scoped grant: %+v", d)
	}
}

type workerDirectory map[WorkerID]bool

func (d workerDirectory) Eligible(_ TenantID, worker WorkerID) bool { return d[worker] }

func TestAuthorizeWorkerRechecksAssignmentAndDirectoryEligibility(t *testing.T) {
	o, _ := accessFixture()
	if d := AuthorizeWorker(o, "tenant-a", "worker-a", workerDirectory{"worker-a": true}); !d.Allowed {
		t.Fatalf("eligible assigned worker denied: %+v", d)
	}
	if d := AuthorizeWorker(o, "tenant-a", "worker-a", workerDirectory{}); d.Allowed || d.Reason != "worker_ineligible" {
		t.Fatalf("revoked worker remained eligible: %+v", d)
	}
	o.Assignments[0].State = Revoked
	if d := AuthorizeWorker(o, "tenant-a", "worker-a", workerDirectory{"worker-a": true}); d.Allowed || d.Reason != "no_active_assignment" {
		t.Fatalf("revoked assignment remained usable: %+v", d)
	}
}

func TestCanDecideRequiresDifferentCurrentlyAuthorizedApprover(t *testing.T) {
	if d := CanDecide("initiator", "initiator", true); d.Allowed || d.Reason != "requester_approver_separation" {
		t.Fatalf("self approval allowed: %+v", d)
	}
	if d := CanDecide("initiator", "approver", false); d.Allowed || d.Reason != "approver_not_authorized_at_decision_time" {
		t.Fatalf("revoked approver authority accepted: %+v", d)
	}
	if err := ValidateRequest("initiator", "initiator"); err != ErrSelfApproval {
		t.Fatalf("self approval validation error = %v", err)
	}
	if d := CanDecide("initiator", "approver", true); !d.Allowed {
		t.Fatalf("independent authorized approver denied: %+v", d)
	}
}
