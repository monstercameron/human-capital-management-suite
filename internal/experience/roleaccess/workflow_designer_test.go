package roleaccess

import "testing"

func TestTodo_WF_UI_002_WorkflowDesignerDefaultPermissions(t *testing.T) {
	permissions := DefaultPagePermissions()
	admin := EffectivePagePermissions(Snapshot{PagePermissions: permissions}, []string{"hcm_admin"})
	author := EffectivePagePermissions(Snapshot{PagePermissions: permissions}, []string{"intent_author"})
	manager := EffectivePagePermissions(Snapshot{PagePermissions: permissions}, []string{"manager"})
	if !CanPageAction(admin, "workflow-designer", ActionView) || !CanPageAction(admin, "workflow-designer", ActionCreate) || !CanPageAction(admin, "workflow-designer", ActionUpdate) {
		t.Fatal("HCM administrator lacks workflow designer permissions")
	}
	if !CanPageAction(author, "workflow-designer", ActionView) || !CanPageAction(author, "workflow-designer", ActionCreate) || !CanPageAction(author, "workflow-designer", ActionUpdate) || CanPageAction(author, "workflow-designer", ActionDelete) {
		t.Fatal("workflow author permissions do not preserve author/reviewer boundaries")
	}
	if CanPageAction(manager, "workflow-designer", ActionView) {
		t.Fatal("manager unexpectedly received workflow designer access")
	}
}
