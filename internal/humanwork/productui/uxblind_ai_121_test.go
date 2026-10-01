package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workflowview"
)

func TestTodo_UXBLIND_121(t *testing.T) {
	selected := workflowview.View{
		WorkflowID:        "clock.time",
		Name:              "Clock in and clock out",
		SemanticVersion:   "1.0.0",
		PublicationStatus: "ACTIVE",
		Nodes:             []workflowview.Node{{ID: "start", Label: "Clock in", Start: true}},
		Completeness:      true,
	}
	props := WorkflowDesignerPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Catalog: []WorkflowCatalogItem{
			{WorkflowID: "clock.time", Name: selected.Name, SemanticVersion: selected.SemanticVersion, Status: selected.PublicationStatus},
			{WorkflowID: "a", Name: "A", SemanticVersion: "1.0.0", Status: "ACTIVE"},
			{WorkflowID: "b", Name: "B", SemanticVersion: "0.1.0", Status: "ACTIVE"},
			{WorkflowID: "c", Name: "C", SemanticVersion: "0.1.0", Status: "ACTIVE"},
			{WorkflowID: "d", Name: "D", SemanticVersion: "0.1.0", Status: "DRAFT"},
		},
		Selected: &selected,
		BaseHref: "/workspace/app/admin/workflows",
		OnCreate: func(WorkflowDraftCreateRequest) {},
	}
	markup, err := ui.RenderToString(workflowCatalogPage(props, ui.Text("")))
	if err != nil {
		t.Fatalf("render workflow designer contract: %v", err)
	}
	for _, want := range []string{
		`id="workflow-list-published-heading">Published workflows`,
		`class="count-badge">4</span>`,
		`id="workflow-list-drafts-heading">Draft workflows`,
		`class="count-badge">1</span>`,
		`id="workflow-published-identity-title">Clock in and clock out`,
		`data-tone="positive">Active</span>`,
		`Version 1.0.0`,
		`Published · read-only`,
		`Create newer version`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("workflow designer missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, `>Published workflows<span class="count-badge">`) {
		t.Fatal("catalog heading still concatenates its label and count")
	}
	if strings.Index(markup, `id="workflow-published-identity-title"`) > strings.Index(markup, `id="workflow-published-version-title"`) {
		t.Fatal("workflow version editor renders before workflow identity")
	}
	if strings.Index(markup, `id="workflow-published-version-title"`) > strings.Index(markup, `id="workflow-designer-viewer-title"`) {
		t.Fatal("workflow steps render before the version header")
	}
}

func TestTodo_UXBLIND_121_Browser(t *testing.T) {
	selected := workflowview.View{WorkflowID: "clock.time", Name: "Uhr ein- und ausstempeln", SemanticVersion: "1.0.0", PublicationStatus: "ACTIVE"}
	markup, err := ui.RenderToString(workflowCatalogPage(WorkflowDesignerPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("de-DE")},
		Catalog:   []WorkflowCatalogItem{{WorkflowID: selected.WorkflowID, Name: selected.Name, Status: selected.PublicationStatus}},
		Selected:  &selected,
	}, ui.Text("")))
	if err != nil {
		t.Fatalf("render localized workflow designer contract: %v", err)
	}
	for _, want := range []string{"Veröffentlichte Workflows", "Workflow-Entwürfe", "Veröffentlicht · schreibgeschützt", "Neuere Version erstellen"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("German workflow designer missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "âŸ¦workflow_") {
		t.Fatal("localized workflow designer contains an untranslated catalog key")
	}
}

func TestTodo_UXBLIND_121_Security(t *testing.T) {
	selected := workflowview.View{WorkflowID: "clock.time", Name: "Canonical workflow", SemanticVersion: "2.0.0", PublicationStatus: "ACTIVE"}
	markup, err := ui.RenderToString(workflowPublishedIdentity(WorkflowDesignerPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
	}, selected))
	if err != nil {
		t.Fatalf("render selected workflow identity: %v", err)
	}
	if !strings.Contains(markup, "Canonical workflow") || strings.Contains(markup, "Untrusted catalog name") {
		t.Fatalf("selected workflow identity did not stay bound to its authorized projection: %s", markup)
	}
}
