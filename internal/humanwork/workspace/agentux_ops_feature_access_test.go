package workspace

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// A tenant whose role policy carries feature-level grants admits a page only
// when its content feature is granted. The derived owner grant added the page
// alone, so an administrator on such a tenant was refused Agent operations.
func TestAgentOperationsOwnerGrantSurvivesFeatureLevelPolicy(t *testing.T) {
	admin := roleaccess.FeaturePermission{RoleID: productui.RoleHCMAdmin, PageID: string(productui.PageChatSettings), FeatureID: string(productui.FeatureActions), View: true, Update: true}
	owner := productAccess{
		configured: true, featuresConfigured: true, roles: []string{productui.RoleHCMAdmin},
		permissions: []roleaccess.PagePermission{{RoleID: productui.RoleHCMAdmin, PageID: string(productui.PageChatSettings), View: true, Update: true}},
		features:    []roleaccess.FeaturePermission{admin},
	}
	if !agentsAdmin(owner) {
		t.Fatal("fixture is not an agent owner")
	}
	if owner.can(productui.PageAgentOperations, roleaccess.ActionView) {
		t.Fatal("the stored policy already grants the page; the test proves nothing")
	}
	projected := projectAgentOperationsAccess(owner)
	if !projected.can(productui.PageAgentOperations, roleaccess.ActionView) {
		t.Fatalf("owner refused Agent operations under feature-level policy: pages=%+v features=%+v", projected.permissions, projected.features)
	}
	if projected.can(productui.PageAgentOperations, roleaccess.ActionUpdate) {
		t.Fatal("the derived grant is view only")
	}

	employee := productAccess{
		configured: true, featuresConfigured: true, roles: []string{"worker_self"},
		permissions: []roleaccess.PagePermission{{RoleID: "worker_self", PageID: string(productui.PageChatSettings), View: true}},
		features:    []roleaccess.FeaturePermission{{RoleID: "worker_self", PageID: string(productui.PageChatSettings), FeatureID: string(productui.FeatureContent), View: true}},
	}
	if projectAgentOperationsAccess(employee).can(productui.PageAgentOperations, roleaccess.ActionView) {
		t.Fatal("a person who cannot change agent settings received Agent operations")
	}
}
