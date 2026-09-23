package workflow

import (
	"context"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
)

// Palette lists the server-filtered workflow authoring catalog. It returns
// values rather than pointers so a handler cannot mutate the registry.
type Palette interface {
	List(context.Context, values.TenantId) []designerpalette.Entry
}

func (s *server) ListWorkflowBlocks(ctx context.Context, _ *workflowv1.ListWorkflowBlocksRequest) (*workflowv1.ListWorkflowBlocksResponse, error) {
	principal, invocation, err := trustedContext(ctx)
	if err != nil {
		return nil, err
	}
	if !s.authorized(ctx, principal, ActionListWorkflowBlocks) {
		return nil, denied(invocation, principal)
	}
	if s.deps.Palette == nil {
		return nil, projectReadError(errPaletteUnavailable, invocation, principal)
	}
	entries := s.deps.Palette.List(ctx, principal.Tenant())
	response := &workflowv1.ListWorkflowBlocksResponse{Entries: make([]*workflowv1.WorkflowPaletteEntry, 0, len(entries))}
	for _, entry := range entries {
		response.Entries = append(response.Entries, &workflowv1.WorkflowPaletteEntry{
			Id: entry.ID, Version: entry.Version, Name: entry.Name, Kind: string(entry.Kind),
			Domain: entry.Domain, Description: entry.Description, EffectClass: string(entry.EffectClass),
			Reversal: entry.Reversal, Status: entry.Status, StepType: string(entry.StepType),
		})
	}
	return response, nil
}
