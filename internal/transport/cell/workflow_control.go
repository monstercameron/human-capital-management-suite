package cell

import (
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	transportworkflow "github.com/monstercameron/human-capital-management-suite/internal/transport/workflow"
)

// workflowDependencies threads the cell's inspection reader and, when the cell
// composed them, EP-WF-002's governed workflow controls into the workflow
// transport. A cell without controls leaves Control nil, so the four control
// RPCs refuse with FAILED_PRECONDITION and perform no transition.
func workflowDependencies(c *app.Cell, instances app.WorkflowInstanceReader, cursorKey []byte) transportworkflow.Dependencies {
	deps := transportworkflow.Dependencies{Instances: newWorkflowReader(instances), CursorKey: append([]byte(nil), cursorKey...)}
	if c != nil && c.WorkflowControl != nil && c.WorkflowTenantIDs != nil {
		deps.Control, deps.TenantIDs = c.WorkflowControl, c.WorkflowTenantIDs
	}
	return deps
}

func workflowDependenciesRef(c *app.Cell, instances app.WorkflowInstanceReader, cursorKey []byte) *transportworkflow.Dependencies {
	deps := workflowDependencies(c, instances, cursorKey)
	return &deps
}
