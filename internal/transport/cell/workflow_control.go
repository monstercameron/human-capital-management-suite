package cell

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transportworkflow "github.com/monstercameron/human-capital-management-suite/internal/transport/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// workflowDependencies threads the cell's inspection reader and, when the cell
// composed them, EP-WF-002's governed workflow controls into the workflow
// transport. A cell without controls leaves Control nil, so the four control
// RPCs refuse with FAILED_PRECONDITION and perform no transition.
func workflowDependencies(c *app.Cell, instances app.WorkflowInstanceReader, cursorKey []byte) transportworkflow.Dependencies {
	deps := transportworkflow.Dependencies{Instances: newWorkflowReader(instances), CursorKey: append([]byte(nil), cursorKey...)}
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
	deps.Authorize = func(principal *trust.Principal, action string) bool {
		switch action {
		case transportworkflow.ActionListWorkflowPublications,
			transportworkflow.ActionGetWorkflowDefinitionView,
			transportworkflow.ActionCompileWorkflowDraft,
			transportworkflow.ActionListWorkflowBlocks,
			transportworkflow.ActionCreateWorkflowDraft,
			transportworkflow.ActionGetWorkflowDraft,
			transportworkflow.ActionInsertWorkflowPaletteEntry,
			transportworkflow.ActionUpdateWorkflowDraftNode,
			transportworkflow.ActionSetWorkflowDraftOutcome,
			transportworkflow.ActionBindWorkflowDraftInput,
			transportworkflow.ActionMoveWorkflowDraftNode,
			transportworkflow.ActionNavigateWorkflowDraftHistory,
			transportworkflow.ActionApplyWorkflowTemplateOverlay:
		default:
			return true
		}
		return principal != nil && (principal.HasRole("hcm_admin") || principal.HasRole("comp_admin") || principal.HasRole("intent_author"))
	}
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

func workflowDependenciesRef(c *app.Cell, instances app.WorkflowInstanceReader, cursorKey []byte) *transportworkflow.Dependencies {
	deps := workflowDependencies(c, instances, cursorKey)
	return &deps
}
