package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/clockpunch"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/draftcompile"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

func TestTodo_TCLOCK_WORKFLOW_AuthoringUsesExecutablePunchSession(t *testing.T) {
	entry, ok := localDevelopmentTimeclockWorkflowEntry(ServeConfig{Profile: ServeProfileLocalDev, Tenant: LocalDevTenant})
	if !ok || entry.Kind != designerpalette.KindTemplate || entry.Expansion.Template == nil {
		t.Fatalf("punch session entry = %+v, want a template with definition", entry)
	}
	want := clockpunch.Definition()
	if entry.Expansion.Template.WorkflowID != want.WorkflowID || entry.Expansion.Template.IntentType != want.IntentType {
		t.Fatalf("entry definition identity = %s/%s", entry.Expansion.Template.WorkflowID, entry.Expansion.Template.IntentType)
	}
	plan, err := clockpunch.Compile()
	if err != nil {
		t.Fatalf("compile punch session: %v", err)
	}
	if entry.PublishedPlanDigest != plan.Digest() {
		t.Fatalf("published plan digest = %q, want %q", entry.PublishedPlanDigest, plan.Digest())
	}
	if len(entry.RequiredCapabilities) == 0 {
		t.Fatal("punch session entry omitted its capability references")
	}
	if _, ok := localDevelopmentTimeclockWorkflowEntry(ServeConfig{Profile: ServeProfileStandard, Tenant: LocalDevTenant}); ok {
		t.Fatal("standard profile received the local development punch template")
	}
}

func TestTodo_TCLOCK_WORKFLOW_BootstrapPublishesAndActivatesExactPlan(t *testing.T) {
	registry := &clockAuthoringRegistry{Registry: version.NewRegistry()}
	now := time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC)
	cfg := ServeConfig{Profile: ServeProfileLocalDev, Tenant: demoworkforce.CompanyKey, DevBrowserLogin: true}
	released, err := bootstrapLocalDevTimeclockWorkflowVersion(context.Background(), cfg, registry, func() time.Time { return now })
	if err != nil {
		t.Fatalf("bootstrap punch session: %v", err)
	}
	if len(released) != 1 || released[0].Status != version.StatusActive {
		t.Fatalf("released versions = %+v, want one ACTIVE version", released)
	}
	active, found, err := registry.GetActiveForWorkflow(clockpunch.Definition().WorkflowID)
	if err != nil || !found {
		t.Fatalf("active punch session lookup = found %v, err %v", found, err)
	}
	if active.SemanticVersion != localDevPunchVersion || active.PublishedAt.UTC() != now || len(active.Approvals) != 1 {
		t.Fatalf("active record = %+v, want governed %s with one approval", active, localDevPunchVersion)
	}
	if active.Approvals[0].ApprovedBy == active.PublishedBy || active.Approvals[0].ApprovedBy != "cmd/hcmnext:dev-release-approver" {
		t.Fatalf("approval = %+v, want separate development approver", active.Approvals)
	}
	resolved, err := version.Resolve(registry, clockpunch.Definition().WorkflowID, version.Pin{SemanticVersion: localDevPunchVersion})
	if err != nil || resolved.CompiledPlanDigest != active.CompiledPlanDigest {
		t.Fatalf("runtime pin resolution = %s/%v, want %s", resolved.CompiledPlanDigest, err, active.CompiledPlanDigest)
	}
	plan, err := clockpunch.Compile()
	if err != nil || plan.Digest() != active.CompiledPlanDigest {
		t.Fatalf("published plan no longer reproduces: %s/%v", planDigest(plan), err)
	}

	again, err := bootstrapLocalDevTimeclockWorkflowVersion(context.Background(), cfg, registry, func() time.Time { return now.Add(time.Hour) })
	if err != nil || len(again) != 1 || again[0].CompiledPlanDigest != active.CompiledPlanDigest {
		t.Fatalf("idempotent bootstrap = %+v/%v", again, err)
	}
}

type clockAuthoringRegistry struct {
	*version.Registry
}

func (r *clockAuthoringRegistry) RecordApproval(_ context.Context, _ workflowversionstore.Approval) error {
	return nil
}

func (r *clockAuthoringRegistry) ActivateApproved(_ context.Context, digest string, supersede bool) (version.CompiledVersion, error) {
	return version.Activate(r.Registry, digest, version.ActivationEvidence{
		Authorized: true, ApprovedBy: platformexecution.DevReleaseApprover,
		Authority: "authority:local-development-bootstrap", Reason: "fixture report", ApprovedAt: time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC),
		ReviewedPlanDigest: digest, TestsPassed: true, SupersedeActive: supersede,
	})
}

func TestTodo_TCLOCK_WORKFLOW_BootstrapIsGated(t *testing.T) {
	for name, cfg := range map[string]ServeConfig{
		"no browser login": {Profile: ServeProfileLocalDev, Tenant: demoworkforce.CompanyKey},
		"standard profile": {Profile: ServeProfileStandard, Tenant: demoworkforce.CompanyKey, DevBrowserLogin: true},
		"other tenant":     {Profile: ServeProfileLocalDev, Tenant: "acme-production", DevBrowserLogin: true},
	} {
		t.Run(name, func(t *testing.T) {
			if released, err := bootstrapLocalDevTimeclockWorkflowVersion(context.Background(), cfg, version.NewRegistry(), nil); err != nil || released != nil {
				t.Fatalf("gated bootstrap = %+v/%v, want no publication", released, err)
			}
		})
	}
	cfg := ServeConfig{Profile: ServeProfileLocalDev, Tenant: demoworkforce.CompanyKey, DevBrowserLogin: true}
	for name, clock := range map[string]func() time.Time{
		"missing clock": nil,
		"zero clock":    func() time.Time { return time.Time{} },
	} {
		t.Run(name, func(t *testing.T) {
			_, err := bootstrapLocalDevTimeclockWorkflowVersion(context.Background(), cfg, &clockAuthoringRegistry{Registry: version.NewRegistry()}, clock)
			if !errors.Is(err, errTimeclockBootstrapClock) {
				t.Fatalf("bootstrap clock error = %v, want %v", err, errTimeclockBootstrapClock)
			}
		})
	}
}

func TestTodo_TCLOCK_WORKFLOW_PublishedPostgresVersionFeedsDesignerAndPinResolver(t *testing.T) {
	pool, url := versionPool(t)
	store := workflowversionstore.Store{DB: pool}
	cfg := devVersionServeConfig(url, true, demoworkforce.CompanyKey)
	cfg.Profile = ServeProfileLocalDev
	now := time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC)
	released, err := bootstrapLocalDevTimeclockWorkflowVersion(context.Background(), cfg, store, func() time.Time { return now })
	if err != nil || len(released) != 1 || released[0].Status != version.StatusActive {
		t.Fatalf("durable clock bootstrap = %+v/%v", released, err)
	}
	active, found, err := store.GetActiveForWorkflow(clockpunch.Definition().WorkflowID)
	if err != nil || !found {
		t.Fatalf("durable active clock version = found %v, err %v", found, err)
	}
	resolved, err := version.Resolve(store, active.WorkflowID, version.Pin{CompiledPlanDigest: active.CompiledPlanDigest})
	if err != nil || resolved.CompiledPlanDigest != active.CompiledPlanDigest {
		t.Fatalf("pinned runtime resolution = %s/%v, want %s", resolved.CompiledPlanDigest, err, active.CompiledPlanDigest)
	}
	entry, ok := localDevelopmentTimeclockWorkflowEntry(cfg)
	if !ok || entry.PublishedPlanDigest != active.CompiledPlanDigest {
		t.Fatalf("designer entry digest = %q, durable active digest = %q", entry.PublishedPlanDigest, active.CompiledPlanDigest)
	}
	policy := draftcompile.CapabilityPolicyFunc(func(context.Context, values.TenantId, capability.Key) bool { return true })
	catalog := designerpalette.Catalog{Policy: policy, Extensions: []designerpalette.Entry{entry}}
	listed := catalog.List(context.Background(), values.TenantId(demoworkforce.CompanyKey))
	var foundEntry *designerpalette.Entry
	for i := range listed {
		if listed[i].ID == localDevPunchTemplateID {
			foundEntry = &listed[i]
			break
		}
	}
	if foundEntry == nil || foundEntry.PublishedPlanDigest != active.CompiledPlanDigest {
		t.Fatalf("designer listing = %+v, want published clock version %s", foundEntry, active.CompiledPlanDigest)
	}
}

func planDigest(plan *workflow.CompiledWorkflow) string {
	if plan == nil {
		return ""
	}
	return plan.Digest()
}
