package execution

import (
	"fmt"
	goruntime "runtime"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ironridgeseed"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
	workorderflow "github.com/monstercameron/human-capital-management-suite/internal/workflow/workorder"
)

// PublishIronridgeWorkOrder publishes the demo's reviewed field-work graph for
// inspection. It remains DRAFT: publishing a graph does not grant authority to
// run its external effects or to advance a work order through phase gates.
func PublishIronridgeWorkOrder(store version.Store, at time.Time) (version.CompiledVersion, error) {
	issued, err := ironridgeseed.PublishedTemplate()
	if err != nil {
		return version.CompiledVersion{}, fmt.Errorf("publish Ironridge template: %w", err)
	}
	template := workorderflow.DefaultTemplate()
	template.TenantScope = ironridgeseed.TenantKey
	template.OrganizationScope = ironridgeseed.TenantKey + "/projects"
	definition, err := workorderflow.DefinitionForPin(template, workorderflow.TemplatePin{
		TemplateID: ironridgeseed.TemplateID, Version: ironridgeseed.TemplateVersion, Digest: issued.Digest(),
	})
	if err != nil {
		return version.CompiledVersion{}, err
	}
	definition.Name = "Ironridge field work order"
	plan, err := workorderflow.Compile(definition)
	if err != nil {
		return version.CompiledVersion{}, fmt.Errorf("compile Ironridge work order: %w", err)
	}
	return version.Publish(store, definition, plan, workorderflow.CompileOptions(), version.PublishMeta{
		SemanticVersion: ironridgeseed.TemplateVersion, PublishedAt: at,
		PublishedBy: ironridgeseed.TemplatePublishedBy,
		ToolVersions: map[string]string{
			"go": goruntime.Version(), "publisher": "internal/platform/execution", "catalog_tenant": ironridgeseed.TenantKey,
		},
	})
}
