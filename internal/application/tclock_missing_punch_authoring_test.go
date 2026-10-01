package application

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/clockrepair"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/draftcompile"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

func TestTodo_TCLOCK_MISSING_PUNCH_AuthoringUsesExecutableDefinition(t *testing.T) {
	entry, ok := localDevelopmentMissingPunchWorkflowEntry(ServeConfig{Profile: ServeProfileLocalDev, Tenant: LocalDevTenant})
	if !ok || entry.Kind != designerpalette.KindTemplate || entry.Expansion.Template == nil {
		t.Fatalf("entry = %+v, want template definition", entry)
	}
	definition := clockrepair.Definition()
	plan, err := clockrepair.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if entry.Expansion.Template.WorkflowID != definition.WorkflowID || entry.PublishedPlanDigest != plan.Digest() {
		t.Fatalf("entry = %+v, want workflow %s digest %s", entry, definition.WorkflowID, plan.Digest())
	}
	if entry.Name != "Fix a missing punch" || len(entry.RequiredCapabilities) == 0 {
		t.Fatalf("entry metadata = %+v", entry)
	}
}

func TestTodo_TCLOCK_MISSING_PUNCH_PublishedPostgresVersionFeedsDesignerAndPinResolver(t *testing.T) {
	pool, url := versionPool(t)
	store := workflowversionstore.Store{DB: pool}
	cfg := devVersionServeConfig(url, true, demoworkforce.CompanyKey)
	cfg.Profile = ServeProfileLocalDev
	now := time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC)
	released, err := bootstrapLocalDevMissingPunchWorkflowVersion(context.Background(), cfg, store, func() time.Time { return now })
	if err != nil || len(released) != 1 || released[0].Status != version.StatusActive {
		t.Fatalf("bootstrap = %+v/%v", released, err)
	}
	active, found, err := store.GetActiveForWorkflow(clockrepair.Definition().WorkflowID)
	if err != nil || !found {
		t.Fatalf("active = found %v err %v", found, err)
	}
	resolved, err := version.Resolve(store, active.WorkflowID, version.Pin{CompiledPlanDigest: active.CompiledPlanDigest})
	if err != nil || resolved.CompiledPlanDigest != active.CompiledPlanDigest {
		t.Fatalf("pin resolution = %s/%v", resolved.CompiledPlanDigest, err)
	}
	entry, ok := localDevelopmentMissingPunchWorkflowEntry(cfg)
	if !ok || entry.PublishedPlanDigest != active.CompiledPlanDigest {
		t.Fatalf("entry digest %q active digest %q", entry.PublishedPlanDigest, active.CompiledPlanDigest)
	}
	policy := draftcompile.CapabilityPolicyFunc(func(context.Context, values.TenantId, capability.Key) bool { return true })
	listed := (designerpalette.Catalog{Policy: policy, Extensions: []designerpalette.Entry{entry}}).List(context.Background(), values.TenantId(demoworkforce.CompanyKey))
	for _, item := range listed {
		if item.ID == localDevMissingPunchTemplateID && item.PublishedPlanDigest == active.CompiledPlanDigest {
			return
		}
	}
	t.Fatalf("designer listing omitted published missing-punch digest %s", active.CompiledPlanDigest)
}
