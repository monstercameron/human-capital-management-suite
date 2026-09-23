package application

import (
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/hireexec"
)

// TestTodo_WF_HIRE_001_DesignerOffersTheTemplate proves the designer's catalog
// offers New employee hire as a governed template, and that what an author
// opens is the definition the driver test runs, not a copy that can drift.
func TestTodo_WF_HIRE_001_DesignerOffersTheTemplate(t *testing.T) {
	_, extensions := localDevelopmentWorkflowAuthoring(ServeConfig{Profile: ServeProfileLocalDev, Tenant: LocalDevTenant})
	var template *designerpalette.Entry
	for i := range extensions {
		if extensions[i].ID == "hcmnext.templates.new_hire" {
			template = &extensions[i]
		}
	}
	if template == nil || template.Kind != designerpalette.KindTemplate || template.Expansion.Template == nil {
		t.Fatalf("local development catalog does not offer the new-hire template: %+v", extensions)
	}
	want := hireexec.Definition()
	if !reflect.DeepEqual(*template.Expansion.Template, want) {
		t.Fatal("new-hire authoring template drifted from the executable definition")
	}
	plan, err := hireexec.Compile(want)
	if err != nil {
		t.Fatalf("compile new-hire authoring template: %v", err)
	}
	if template.PublishedPlanDigest == "" || template.PublishedPlanDigest != plan.Digest() {
		t.Fatalf("new-hire template plan digest = %q, want %q", template.PublishedPlanDigest, plan.Digest())
	}
	if got := requiredWorkflowCapabilities(want.Nodes); !reflect.DeepEqual(template.RequiredCapabilities, got) {
		t.Fatalf("new-hire template capability manifest = %+v, want %+v", template.RequiredCapabilities, got)
	}
	if _, standard := localDevelopmentWorkflowAuthoring(ServeConfig{Profile: ServeProfileStandard, Tenant: LocalDevTenant}); len(standard) != 0 {
		t.Fatal("a standard deployment was granted the local authoring catalog")
	}
}
