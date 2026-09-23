package roleaccessstore

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
)

// FeatureExceedingPage names one feature grant bit the stored page row does
// not cover. The slice is empty exactly when every feature row sits within
// its page row.
type FeatureExceedingPage struct {
	RoleID    string
	PageID    string
	FeatureID string
	Action    string
}

// FeatureRowsWithinPageRows proves the RT-019 invariant over a stored
// snapshot: no feature row ever exceeds its page row. Every granted feature
// bit must already be granted by the same role's page row; a feature row
// with no page row at all exceeds on every bit it grants. Inactive roles
// are excluded the same way the effective permissions exclude them: a
// disabled role grants nothing, so it cannot exceed either.
func FeatureRowsWithinPageRows(snapshot roleaccess.Snapshot) []FeatureExceedingPage {
	inactive := map[string]bool{}
	for _, role := range snapshot.Roles {
		if !role.Active {
			inactive[strings.ToLower(strings.TrimSpace(role.ID))] = true
		}
	}
	pages := map[string]roleaccess.PagePermission{}
	for _, permission := range snapshot.PagePermissions {
		permission = roleaccess.NormalizePagePermission(permission)
		if inactive[strings.ToLower(permission.RoleID)] {
			continue
		}
		pages[permission.RoleID+"\x00"+permission.PageID] = permission
	}
	var violations []FeatureExceedingPage
	for _, permission := range snapshot.FeaturePermissions {
		permission = roleaccess.NormalizeFeaturePermission(permission)
		if inactive[strings.ToLower(permission.RoleID)] {
			continue
		}
		page, ok := pages[permission.RoleID+"\x00"+permission.PageID]
		for _, action := range []struct {
			name    string
			granted bool
		}{
			{roleaccess.ActionView, permission.View},
			{roleaccess.ActionCreate, permission.Create},
			{roleaccess.ActionUpdate, permission.Update},
			{roleaccess.ActionDelete, permission.Delete},
		} {
			if !action.granted {
				continue
			}
			if !ok || !roleaccess.CanPageAction([]roleaccess.PagePermission{page}, permission.PageID, action.name) {
				violations = append(violations, FeatureExceedingPage{
					RoleID:    permission.RoleID,
					PageID:    permission.PageID,
					FeatureID: permission.FeatureID,
					Action:    action.name,
				})
			}
		}
	}
	return violations
}
