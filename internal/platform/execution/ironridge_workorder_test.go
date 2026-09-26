package execution

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ironridgeseed"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
	workorderflow "github.com/monstercameron/human-capital-management-suite/internal/workflow/workorder"
)

func TestIronridgeWorkOrderCatalogPublicationIsScopedAndDraft(t *testing.T) {
	registry := version.NewRegistry()
	at := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	first, err := PublishIronridgeWorkOrder(registry, at)
	if err != nil {
		t.Fatal(err)
	}
	if first.WorkflowID != workorderflow.WorkflowIDForTemplate(ironridgeseed.TemplateID) || first.Status != version.StatusDraft || first.ToolVersions["catalog_tenant"] != ironridgeseed.TenantKey {
		t.Fatalf("publication identity/status/scope = %+v", first)
	}
	var plan workflow.CompiledWorkflow
	if err := json.Unmarshal(first.CanonicalPlanBytes, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.TenantScope != ironridgeseed.TenantKey || plan.Name != "Ironridge field work order" || len(plan.Nodes) == 0 {
		t.Fatalf("published plan = %+v", plan)
	}
	again, err := PublishIronridgeWorkOrder(registry, at.Add(time.Hour))
	if err != nil || again.CompiledPlanDigest != first.CompiledPlanDigest || again.PublishedAt != first.PublishedAt {
		t.Fatalf("idempotent publication = %+v, %v", again, err)
	}
}
