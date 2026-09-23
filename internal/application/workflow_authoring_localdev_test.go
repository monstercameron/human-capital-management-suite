package application

import (
	"context"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

func TestTodo_WF_UI_005_LocalDevelopmentCatalogIsExplicitAndTenantScoped(t *testing.T) {
	policy, extensions := localDevelopmentWorkflowAuthoring(ServeConfig{Profile: ServeProfileLocalDev, Tenant: LocalDevTenant})
	if policy == nil || len(extensions) != 3 {
		t.Fatalf("local development workflow authoring = policy %T, extensions %+v", policy, extensions)
	}
	key := capability.Key{ID: "hcmnext.people.promote_worker", Version: 1}
	if !policy.AllowsCapability(context.Background(), values.TenantId(LocalDevTenant), key) {
		t.Fatal("local development tenant was denied its explicit catalog")
	}
	if policy.AllowsCapability(context.Background(), "another-tenant", key) || policy.AllowsCapability(context.Background(), values.TenantId(LocalDevTenant), capability.Key{}) {
		t.Fatal("local development policy broadened beyond the configured tenant and a versioned capability key")
	}
	if extensions[0].Kind != designerpalette.KindFragment || extensions[1].Kind != designerpalette.KindTemplate {
		t.Fatalf("local development extension kinds = %+v", extensions)
	}
	extensions[1].RequiredCapabilities[0].ID = "mutated"
	_, fresh := localDevelopmentWorkflowAuthoring(ServeConfig{Profile: ServeProfileLocalDev, Tenant: LocalDevTenant})
	if fresh[1].RequiredCapabilities[0].ID == "mutated" {
		t.Fatal("local development catalog aliases a previous caller")
	}
}

func TestTodo_WF_UI_005_StandardServeProfileDoesNotGrantAuthoringCapabilities(t *testing.T) {
	policy, extensions := localDevelopmentWorkflowAuthoring(ServeConfig{Profile: ServeProfileStandard, Tenant: LocalDevTenant})
	if policy != nil || len(extensions) != 0 {
		t.Fatalf("standard profile received local authoring grants: policy %T extensions %+v", policy, extensions)
	}
}

func TestTodo_WF_UI_005_PromotionTemplateMatchesExecutableGraph(t *testing.T) {
	_, extensions := localDevelopmentWorkflowAuthoring(ServeConfig{Profile: ServeProfileLocalDev, Tenant: LocalDevTenant})

	var template *designerpalette.Entry
	for i := range extensions {
		if extensions[i].ID == "hcmnext.templates.promotion" {
			template = &extensions[i]
			break
		}
	}
	if template == nil || template.Expansion.Template == nil {
		t.Fatal("local development catalog does not expose the governed promotion template")
	}

	want := promotionexec.Definition()
	if !reflect.DeepEqual(*template.Expansion.Template, want) {
		t.Fatal("promotion authoring template drifted from the executable promotion definition")
	}
	plan, err := promotionexec.Compile(want)
	if err != nil {
		t.Fatalf("compile promotion authoring template: %v", err)
	}
	if template.PublishedPlanDigest != plan.Digest() {
		t.Fatalf("promotion authoring template plan digest = %q, want executable %q", template.PublishedPlanDigest, plan.Digest())
	}
	if got := requiredWorkflowCapabilities(want.Nodes); !reflect.DeepEqual(template.RequiredCapabilities, got) {
		t.Fatalf("promotion template capability manifest = %+v, want %+v", template.RequiredCapabilities, got)
	}

	waits := map[string]string{
		promotionexec.NodeAwaitPayrollConfirmation: "hcmnext.integrations.payroll",
		promotionexec.NodeAwaitAccessConfirmation:  "hcmnext.integrations.iam",
	}
	for nodeID, source := range waits {
		found := false
		for _, node := range template.Expansion.Template.Nodes {
			if node.ID != nodeID {
				continue
			}
			found = true
			if node.Signal == nil || len(node.Signal.AcceptedSources) != 1 || node.Signal.AcceptedSources[0] != source {
				t.Fatalf("template node %q does not preserve its single provider source %q", nodeID, source)
			}
		}
		if !found {
			t.Fatalf("promotion authoring template is missing provider wait %q", nodeID)
		}
	}
}
