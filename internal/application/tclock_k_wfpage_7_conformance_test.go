package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/hireexec"
)

func TestTodo_WFPAGE_035(t *testing.T) {
	policy, entries := localDevelopmentWorkflowAuthoring(ServeConfig{Profile: ServeProfileLocalDev, Tenant: LocalDevTenant})
	if policy == nil {
		t.Fatal("local development workflow authoring did not compose a tenant policy")
	}
	var found bool
	for _, entry := range entries {
		if entry.ID != "hcmnext.templates.new_hire" {
			continue
		}
		found = true
		if entry.Expansion.Template == nil || entry.PublishedPlanDigest == "" {
			t.Fatalf("new-hire reference entry is not published: %+v", entry)
		}
		plan, err := hireexec.Compile(*entry.Expansion.Template)
		if err != nil || plan.Digest() != entry.PublishedPlanDigest {
			t.Fatalf("new-hire entry plan = %v / %q, want published digest %q", err, plan.Digest(), entry.PublishedPlanDigest)
		}
	}
	if !found {
		t.Fatal("local development catalog omitted the New hire reference workflow")
	}
	if definition, ok := productui.LookupPage(productui.PageWorkflowStart); !ok || !definition.Admitted || definition.Route != "/workspace/app/workflows" {
		t.Fatalf("workflow start route = %+v, ok=%v", definition, ok)
	}
	if !productui.PageVisible(productui.PageWorkflowStart, []string{"hiring_manager"}) {
		t.Fatal("hiring manager cannot see the workflow start page")
	}
	if productui.PageVisible(productui.PageWorkflowDesigner, []string{"hiring_manager"}) {
		t.Fatal("hiring manager received designer visibility")
	}
}

func TestTodo_WFPAGE_035_Security(t *testing.T) {
	policy, _ := localDevelopmentWorkflowAuthoring(ServeConfig{Profile: ServeProfileLocalDev, Tenant: LocalDevTenant})
	key := capability.Key{ID: "hcmnext.people.commit_new_hire", Version: 1}
	if !policy.AllowsCapability(context.Background(), values.TenantId(LocalDevTenant), key) {
		t.Fatal("the owning tenant was denied its New hire capability")
	}
	for _, tenant := range []values.TenantId{"tenant-other", "", "tenant-other-2"} {
		if policy.AllowsCapability(context.Background(), tenant, key) {
			t.Fatalf("tenant %q crossed the workflow capability boundary", tenant)
		}
	}
	if standardPolicy, standardEntries := localDevelopmentWorkflowAuthoring(ServeConfig{Profile: ServeProfileStandard, Tenant: LocalDevTenant}); standardPolicy != nil || len(standardEntries) != 0 {
		t.Fatalf("standard deployment exposed local workflow authoring: policy=%v entries=%d", standardPolicy != nil, len(standardEntries))
	}
}

func TestTodo_WFPAGE_035_Conformance(t *testing.T) {
	for _, roles := range [][]string{{productui.RoleHCMAdmin}, {"hiring_manager"}, {"worker_self"}} {
		designer := productui.PageVisible(productui.PageWorkflowDesigner, roles)
		start := productui.PageVisible(productui.PageWorkflowStart, roles)
		if roles[0] == productui.RoleHCMAdmin && !designer {
			t.Fatal("HCM administrator lost workflow designer access")
		}
		if roles[0] == "hiring_manager" && designer {
			t.Fatal("hiring manager gained workflow designer access")
		}
		if roles[0] == "worker_self" && (designer || !start) {
			t.Fatal("individual contributor lost start access or gained designer access")
		}
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := productui.ApplyLocale(productui.NewView(productui.PageWorkflowStart, "tenant-a", "user", "scope"), productui.ResolveProductLocale(locale))
		if view.Locale.Resolved == "" || view.Locale.Direction == "" {
			t.Fatalf("locale %q did not resolve direction metadata: %+v", locale, view.Locale)
		}
	}
}
