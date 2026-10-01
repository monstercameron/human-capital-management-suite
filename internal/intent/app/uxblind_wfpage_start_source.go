package app

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workflowview"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	workflow "github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// workflowStartSource projects the published workflows a viewer may start.
// The catalog is built from ACTIVE published versions visible to the tenant;
// canStart is the shell's already-resolved start authority, so an entry is
// either available or reported as no_capability and the list never disagrees
// with the shell's own grant check.
func workflowStartSource(versions workflowversion.Store) workspace.WorkflowStartSource {
	lister, ok := versions.(workflowversion.CatalogStore)
	if !ok {
		return nil
	}
	return func(ctx context.Context, tenant values.TenantId, canStart bool) ([]workspace.WorkflowStartConfig, error) {
		published, err := lister.ListAll()
		if err != nil {
			return nil, err
		}
		if tenant == "" {
			return nil, errors.New("workflow start catalog: tenant is required")
		}
		candidates := make([]WorkflowStartCandidate, 0, len(published))
		for _, publication := range published {
			if scope := publication.ToolVersions["catalog_tenant"]; scope != "" && scope != tenant.String() {
				continue
			}
			view, err := workflowview.Build(publication, nil)
			if err != nil || view.Name == "" {
				continue
			}
			// Records published before CatalogMetadata existed carry none. They
			// stay discoverable under the definition's own name; only an
			// explicit Hidden flag, or the internal prototype whose legacy
			// records predate that flag, removes a published workflow.
			var metadata workflow.CatalogMetadata
			if publication.Catalog != nil {
				metadata = *publication.Catalog.Clone()
			} else if publication.WorkflowID == prototype.ApprovalWorkflowID {
				metadata.Hidden = true
			}
			if strings.TrimSpace(metadata.DisplayName) == "" {
				metadata.DisplayName = view.Name
			}
			status := string(publication.Status)
			if publication.Status == workflowversion.StatusQuarantined {
				status = string(workflowversion.StatusActive)
			}
			candidates = append(candidates, WorkflowStartCandidate{
				WorkflowID: publication.WorkflowID, Version: publication.DefinitionVersion,
				SemanticVersion: publication.SemanticVersion, Name: metadata.DisplayName,
				Description: metadata.Description, Category: metadata.Category,
				Keywords: append([]string(nil), metadata.Keywords...), Icon: metadata.Icon,
				Status: status, Owner: publication.PublishedBy,
				Hidden: metadata.Hidden, Quarantined: publication.Status == workflowversion.StatusQuarantined,
			})
		}
		entries := BuildWorkflowStartCatalog(ctx, tenant, candidates, func(context.Context, values.TenantId, WorkflowStartCandidate) WorkflowStartDecision {
			if canStart {
				return WorkflowStartDecision{Availability: WorkflowStartAvailable}
			}
			return WorkflowStartDecision{Availability: WorkflowStartNoCapability}
		})
		result := make([]workspace.WorkflowStartConfig, 0, len(entries))
		for _, entry := range entries {
			result = append(result, workspace.WorkflowStartConfig{
				WorkflowID: entry.WorkflowID, Version: entry.Version, SemanticVersion: entry.SemanticVersion,
				Name: entry.Name, Description: entry.Description, Category: entry.Category,
				Keywords: entry.Keywords, Icon: entry.Icon, Owner: entry.Owner, Availability: string(entry.Availability),
			})
		}
		return result, nil
	}
}

// WorkflowStartSource is the workspace's start-catalog port over this cell's
// published workflow versions; nil when the cell has no listable catalog.
func (c *Cell) WorkflowStartSource() workspace.WorkflowStartSource {
	return workflowStartSource(c.WorkflowVersions)
}
