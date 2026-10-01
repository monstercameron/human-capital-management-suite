package application

import (
	"fmt"
	"strings"
)

// Workflow-page permissions are feature permissions, not aliases for one
// broad workflow role. The policy decision point can register this closed
// tenant-scoped set independently from page and workflow start permissions.
const (
	WorkflowPageDesignPermission  = "workflow.page.design"
	WorkflowPagePublishPermission = "workflow.page.publish"
	WorkflowPageStartPermission   = "workflow.page.start"
)

func WorkflowPageFeaturePermissions() []string {
	return []string{WorkflowPageDesignPermission, WorkflowPagePublishPermission, WorkflowPageStartPermission}
}

func ValidateWorkflowPageFeaturePermissions(tenant string, permissions []string) error {
	if strings.TrimSpace(tenant) == "" {
		return fmt.Errorf("application: workflow-page permission namespace requires a tenant")
	}
	seen := map[string]bool{}
	for _, permission := range permissions {
		if seen[permission] {
			return fmt.Errorf("application: duplicate workflow-page permission %q", permission)
		}
		seen[permission] = true
		known := false
		for _, registered := range WorkflowPageFeaturePermissions() {
			if permission == registered {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("application: unknown workflow-page permission %q", permission)
		}
	}
	return nil
}
