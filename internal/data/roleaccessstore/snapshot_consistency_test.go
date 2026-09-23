package roleaccessstore

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
)

// TestTodo_RBAC_RT_019_Property is the PROPERTY matrix entry: no feature
// row ever exceeds its page row. Bootstrap-shaped snapshots are clean, an
// orphan feature row violates on every bit it grants, a narrowed page row
// exposes exactly the uncovered bits, and inactive roles never violate.
func TestTodo_RBAC_RT_019_Property(t *testing.T) {
	seeded := roleaccess.Snapshot{
		Roles: roleaccess.DefaultRoles(),
		PagePermissions: []roleaccess.PagePermission{
			{Version: 1, RoleID: "manager", PageID: "journeys", View: true, Create: true, Update: true},
		},
		FeaturePermissions: []roleaccess.FeaturePermission{
			{Version: 1, RoleID: "manager", PageID: "journeys", FeatureID: "content", View: true},
			{Version: 1, RoleID: "manager", PageID: "journeys", FeatureID: "promotion_request", View: true, Create: true, Update: true},
		},
	}
	if violations := FeatureRowsWithinPageRows(seeded); len(violations) != 0 {
		t.Fatalf("seeded snapshot violates: %+v", violations)
	}

	orphan := roleaccess.Snapshot{
		Roles: roleaccess.DefaultRoles(),
		FeaturePermissions: []roleaccess.FeaturePermission{
			{Version: 1, RoleID: "manager", PageID: "journeys", FeatureID: "content", View: true, Update: true},
		},
	}
	violations := FeatureRowsWithinPageRows(orphan)
	if len(violations) != 2 {
		t.Fatalf("orphan feature row violations = %+v, want the 2 granted bits", violations)
	}
	for _, violation := range violations {
		if violation.RoleID != "manager" || violation.PageID != "journeys" || violation.FeatureID != "content" {
			t.Fatalf("violation = %+v, want manager/journeys/content", violation)
		}
	}

	narrowed := seeded
	narrowed.PagePermissions = []roleaccess.PagePermission{
		{Version: 2, RoleID: "manager", PageID: "journeys", View: true},
	}
	violations = FeatureRowsWithinPageRows(narrowed)
	if len(violations) != 2 {
		t.Fatalf("narrowed page violations = %+v, want create+update on promotion_request", violations)
	}
	for _, violation := range violations {
		if violation.FeatureID != "promotion_request" || (violation.Action != roleaccess.ActionCreate && violation.Action != roleaccess.ActionUpdate) {
			t.Fatalf("violation = %+v, want promotion_request create/update", violation)
		}
	}

	dormant := narrowed
	dormant.Roles = append([]roleaccess.Role(nil), roleaccess.DefaultRoles()...)
	for i, role := range dormant.Roles {
		if role.ID == "manager" {
			dormant.Roles[i].Active = false
		}
	}
	if violations := FeatureRowsWithinPageRows(dormant); len(violations) != 0 {
		t.Fatalf("inactive role violates: %+v", violations)
	}
}
