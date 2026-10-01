package app

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designeredit"
)

// DeleteWorkflowDraft is the application boundary for the authoring delete
// command. The caller supplies authenticated context, while designeredit
// re-checks ownership and whether the document is still untouched before the
// store is allowed to perform the side effect.
func (c *Cell) DeleteWorkflowDraft(ctx context.Context, tenant values.TenantId, author, draftID string) error {
	if c == nil || c.WorkflowDraftAuthoring == nil {
		return designeredit.ErrInvalid
	}
	return c.WorkflowDraftAuthoring.Delete(ctx, tenant, author, draftID)
}
