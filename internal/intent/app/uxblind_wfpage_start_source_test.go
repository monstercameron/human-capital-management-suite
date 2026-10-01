package app

import (
	"context"
	"testing"
	"time"

	workflowcore "github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

func wfpageStartRegistry(t *testing.T, activate bool) *workflowversion.Registry {
	t.Helper()
	return wfpageStartRegistryWith(t, activate, func(definition *workflowcore.Definition) {
		definition.Catalog = &workflowcore.CatalogMetadata{DisplayName: "Promotion review", Category: "People changes", Description: "Review a governed promotion request.", Keywords: []string{"promotion", "approval"}, Icon: "promote"}
	})
}

func wfpageStartRegistryWith(t *testing.T, activate bool, mutate func(*workflowcore.Definition)) *workflowversion.Registry {
	t.Helper()
	plan, err := prototype.CompileApproval()
	if err != nil {
		t.Fatal(err)
	}
	registry := workflowversion.NewRegistry()
	definition := prototype.ApprovalDefinition()
	mutate(&definition)
	published, err := workflowversion.Publish(registry, definition, plan, workflowcore.Options{
		Phase: workflowcore.PhaseP1B, IRSchemaVersion: prototype.ApprovalIRSchemaV1,
	}, workflowversion.PublishMeta{SemanticVersion: "1.0.0", PublishedAt: time.Date(2026, 9, 19, 15, 4, 5, 0, time.UTC), PublishedBy: "principal:release-manager"})
	if err != nil {
		t.Fatal(err)
	}
	if activate {
		if _, err := workflowversion.Activate(registry, published.CompiledPlanDigest, workflowversion.ActivationEvidence{
			ApprovedBy: "principal:reviewer", Authority: "role:change-governance", Reason: "fixture",
			ApprovedAt: published.PublishedAt.Add(time.Minute), ReviewedPlanDigest: published.CompiledPlanDigest,
			Authorized: true, TestsPassed: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return registry
}

func TestTodo_WFPAGE_002_WorkspaceSource(t *testing.T) {
	source := workflowStartSource(wfpageStartRegistry(t, true))
	if source == nil {
		t.Fatal("registry must yield a start source")
	}
	allowed, err := source(context.Background(), "tenant-a", true)
	if err != nil || len(allowed) != 1 || allowed[0].Availability != string(WorkflowStartAvailable) || allowed[0].Name != "Promotion review" || allowed[0].Category != "People changes" || allowed[0].Description == "" || len(allowed[0].Keywords) == 0 || allowed[0].Icon == "" {
		t.Fatalf("allowed = %+v, %v; want one available entry with a business name", allowed, err)
	}
	denied, err := source(context.Background(), "tenant-a", false)
	if err != nil || len(denied) != 1 || denied[0].Availability != string(WorkflowStartNoCapability) {
		t.Fatalf("denied = %+v, %v; want the same entry as no_capability", denied, err)
	}
	if _, err := source(context.Background(), "", true); err == nil {
		t.Fatal("an empty tenant must be refused")
	}
}

func TestTodo_WFPAGE_002_WorkspaceSourceOmitsUnpublishedAndNonStores(t *testing.T) {
	inactive, err := workflowStartSource(wfpageStartRegistry(t, false))(context.Background(), "tenant-a", true)
	if err != nil || len(inactive) != 0 {
		t.Fatalf("a published but never activated version leaked: %+v, %v", inactive, err)
	}
	if workflowStartSource(nil) != nil {
		t.Fatal("a nil store must yield no source")
	}
}

// A workflow published without explicit catalog metadata (every record
// written before CatalogMetadata existed) is still startable under its own
// definition name; only an explicit Hidden flag, or the legacy internal
// prototype, removes it.
func TestTodo_WFPAGE_002_PublishedWithoutMetadataStillAppears(t *testing.T) {
	registry := wfpageStartRegistryWith(t, true, func(definition *workflowcore.Definition) {
		definition.Catalog = &workflowcore.CatalogMetadata{}
	})
	got, err := workflowStartSource(registry)(context.Background(), "tenant-a", true)
	if err != nil || len(got) != 1 || got[0].Name != "Prototype promotion approval" || got[0].Availability != string(WorkflowStartAvailable) {
		t.Fatalf("empty metadata = %+v, %v; want the workflow listed under its definition name", got, err)
	}
	legacy := wfpageStartRegistryWith(t, true, func(definition *workflowcore.Definition) { definition.Catalog = nil })
	hidden, err := workflowStartSource(legacy)(context.Background(), "tenant-a", true)
	if err != nil || len(hidden) != 0 {
		t.Fatalf("legacy prototype record without metadata = %+v, %v; want it excluded", hidden, err)
	}
	explicit := wfpageStartRegistryWith(t, true, func(definition *workflowcore.Definition) {
		definition.Catalog = &workflowcore.CatalogMetadata{Hidden: true, DisplayName: "Secret"}
	})
	if got, _ := workflowStartSource(explicit)(context.Background(), "tenant-a", true); len(got) != 0 {
		t.Fatalf("explicitly hidden workflow leaked: %+v", got)
	}
}
