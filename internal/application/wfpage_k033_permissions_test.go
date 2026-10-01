package application

import "testing"

func TestTodo_WFPAGE_033_Application(t *testing.T) {
	permissions := WorkflowPageFeaturePermissions()
	if len(permissions) != 3 || permissions[0] != WorkflowPageDesignPermission || permissions[1] != WorkflowPagePublishPermission || permissions[2] != WorkflowPageStartPermission {
		t.Fatalf("workflow page permissions = %v", permissions)
	}
	if err := ValidateWorkflowPageFeaturePermissions("tenant-a", permissions); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWorkflowPageFeaturePermissions("", permissions); err == nil {
		t.Fatal("permission namespace without tenant was accepted")
	}
	if err := ValidateWorkflowPageFeaturePermissions("tenant-a", []string{WorkflowPageDesignPermission, WorkflowPageDesignPermission}); err == nil {
		t.Fatal("duplicate permission was accepted")
	}
	if err := ValidateWorkflowPageFeaturePermissions("tenant-a", []string{"workflow.page.delete"}); err == nil {
		t.Fatal("unregistered permission was accepted")
	}
}
