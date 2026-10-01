package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_116(t *testing.T) {
	const workflowID = "people.employee.lifecycle.workflow.with.a.long.identifier"
	props := WorkflowDesignerPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		BaseHref:  "/workspace/app/admin/workflows",
	}
	markup, err := ui.RenderToString(uxblindWorkflowScalableCatalog(props, []WorkflowCatalogItem{
		{WorkflowID: workflowID, Name: "Employee lifecycle", Version: 3, SemanticVersion: "1.3.0", Status: "ACTIVE"},
	}, workflowID))
	if err != nil {
		t.Fatalf("render workflow catalog: %v", err)
	}
	if !strings.Contains(markup, `title="`+workflowID+`"`) {
		t.Fatalf("workflow ID is missing its full title:\n%s", markup)
	}

	for _, want := range []string{
		"Name, workflow ID, or category",
		"Recently updated",
		"All 1",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("catalog control missing %q:\n%s", want, markup)
		}
	}

	for _, want := range []string{
		"grid-template-columns:minmax(20rem,24rem) minmax(0,1fr)",
		"@media (max-width:1099px){.workflow-designer-layout{grid-template-columns:1fr}",
		"grid-template-columns:minmax(0,1fr)",
		"text-overflow:ellipsis",
	} {
		if !strings.Contains(workflowDesignerStylesheet()+workflowListStylesheet(), want) {
			t.Fatalf("workflow designer layout styles missing %q", want)
		}
	}
}

func TestTodo_UXBLIND_116_Browser(t *testing.T) {
	const workflowID = "custom.workflow.identifier.that.must.remain-discoverable"
	props := WorkflowDesignerPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("de-DE")},
		BaseHref:  "/workspace/app/admin/workflows",
	}
	markup, err := ui.RenderToString(uxblindWorkflowScalableCatalog(props, []WorkflowCatalogItem{
		{WorkflowID: workflowID, Name: "Mitarbeiterlebenszyklus", Version: 1, SemanticVersion: "1.0.0", Status: "ACTIVE"},
	}, workflowID))
	if err != nil {
		t.Fatalf("render workflow catalog browser contract: %v", err)
	}
	for _, want := range []string{
		`title="` + workflowID + `"`,
		"Workflows suchen",
		"Zuletzt aktualisiert",
		"Alle 1",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("workflow browser contract missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(workflowListStylesheet(), "overflow-wrap:anywhere") {
		t.Fatal("workflow IDs still allow token wrapping instead of truncating")
	}
}
