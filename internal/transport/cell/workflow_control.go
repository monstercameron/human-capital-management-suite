package cell

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transportworkflow "github.com/monstercameron/human-capital-management-suite/internal/transport/workflow"
)

// workflowDependencies threads the cell's inspection reader and, when the cell
// composed them, EP-WF-002's governed workflow controls into the workflow
// transport. A cell without controls leaves Control nil, so the four control
// RPCs refuse with FAILED_PRECONDITION and perform no transition.
func workflowDependencies(c *app.Cell, instances app.WorkflowInstanceReader, cursorKey, previousCursorKey []byte) transportworkflow.Dependencies {
	deps := transportworkflow.Dependencies{Instances: newWorkflowReader(instances), CursorKey: append([]byte(nil), cursorKey...), PreviousCursorKey: append([]byte(nil), previousCursorKey...)}
	if c != nil {
		if catalog, ok := c.WorkflowVersions.(transportworkflow.DefinitionReader); ok {
			deps.Definitions = catalog
		}
		if c.WorkflowDrafts != nil {
			deps.Drafts = workflowDraftReader{cell: c}
		}
		if c.WorkflowDraftCompiler != nil {
			deps.DraftCompiler = c.WorkflowDraftCompiler
		}
		if c.WorkflowPalette != nil {
			deps.Palette = c.WorkflowPalette
		}
		if c.WorkflowDraftAuthoring != nil {
			deps.DraftAuthoring = c.WorkflowDraftAuthoring
		}
	}
	// RBAC-RT-004: the hook resolves the caller's strictly durable roles
	// from the cell's role store (never credential claims) and gates every
	// action by capability. A cell composed without a role store denies
	// everything; supervision of an instance's subjects additionally needs
	// the relationship directory RBAC-RT-007 delivers, so the workflow
	// transport leaves Dependencies.Supervision nil until then.
	var store roleaccess.Store
	if c != nil {
		store = c.RoleAccess
	}
	deps.Authorize = workflowAuthorizer(store)
	if c != nil && c.WorkflowControl != nil && c.WorkflowTenantIDs != nil {
		deps.Control, deps.TenantIDs = c.WorkflowControl, c.WorkflowTenantIDs
	}
	return deps
}

type workflowDraftReader struct {
	cell *app.Cell
}

func (r workflowDraftReader) ReadWorkflowDraft(ctx context.Context, tenant values.TenantId, draftID string) (transportworkflow.Draft, error) {
	draft, err := r.cell.ReadWorkflowDraftRecord(ctx, tenant, draftID)
	if errors.Is(err, app.ErrWorkflowDraftNotFound) {
		return transportworkflow.Draft{}, transportworkflow.ErrNotFound
	}
	if err != nil {
		return transportworkflow.Draft{}, err
	}
	return transportworkflow.Draft{
		DraftID: draft.DraftID, AuthorRef: draft.AuthorRef,
		Revision: draft.Revision, Document: draft.Document,
	}, nil
}

func workflowDependenciesRef(c *app.Cell, instances app.WorkflowInstanceReader, cursorKey, previousCursorKey []byte) *transportworkflow.Dependencies {
	deps := workflowDependencies(c, instances, cursorKey, previousCursorKey)
	return &deps
}
