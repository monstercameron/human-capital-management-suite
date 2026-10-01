package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestUXBLIND070_DraftRowOffersAuthorizedEditRoute(t *testing.T) {
	props := WorkflowDesignerPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		BaseHref:  "/workspace/app/admin/workflows?locale=en-US",
		Navigate:  func(string) {},
	}
	catalog := []WorkflowCatalogItem{
		{WorkflowID: "published.workflow", Name: "Published workflow", Version: 3, SemanticVersion: "1.2.0", Status: "ACTIVE"},
		{WorkflowID: "draft.workflow", DraftID: "draft-42", Name: "Field work order", Version: 4, SemanticVersion: "1.3.0", Status: "DRAFT"},
	}
	markup, err := ui.RenderToString(uxblindWorkflowScalableCatalog(props, catalog, "draft.workflow"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Edit draft") {
		t.Fatalf("draft row has no edit action:\n%s", markup)
	}
	if !strings.Contains(markup, `href="/workspace/app/admin/workflows?draft=draft-42&amp;locale=en-US"`) {
		t.Fatalf("draft edit action does not use the authorized draft route:\n%s", markup)
	}
	if strings.Count(markup, `workflow-catalog-edit`) != 1 {
		t.Fatalf("expected exactly one draft edit action:\n%s", markup)
	}
	if !strings.Contains(markup, `workflow-list-drafts-heading`) || !strings.Contains(markup, `workflow-list-published-heading`) {
		t.Fatalf("workflow rows are not grouped by publication state:\n%s", markup)
	}
}

func TestUXBLIND070_DraftRowWithoutAuthorizedIDDoesNotInventEditRoute(t *testing.T) {
	props := WorkflowDesignerPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		BaseHref:  "/workspace/app/admin/workflows?locale=en-US",
	}
	row := workflowListRows(props, []WorkflowCatalogItem{{
		WorkflowID: "draft.workflow", Name: "Field work order", SemanticVersion: "1.3.0", Status: "DRAFT",
	}}, workflowListState{}, "")
	if len(row) != 1 {
		t.Fatalf("workflow rows = %d, want one draft", len(row))
	}
	markup, err := ui.RenderToString(workflowListRowAction(props, row[0]))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "Edit draft") || strings.Contains(markup, `href="/workspace/app/admin/workflows?draft=`) || strings.Contains(markup, "workflow-catalog-edit") {
		t.Fatalf("draft without an authorized ID rendered an edit route:\n%s", markup)
	}
}
