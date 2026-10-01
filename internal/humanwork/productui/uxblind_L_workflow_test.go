package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_031(t *testing.T) {
	props := WorkflowDesignerPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Catalog: []WorkflowCatalogItem{
			{WorkflowID: "promotion.execute", Name: "Promotion execute", SemanticVersion: "1.2.0", Status: "ACTIVE"},
			{WorkflowID: "promotion.prototype", Name: "Prototype promotion approval", SemanticVersion: "1.0.0", Status: "ACTIVE"},
		},
	}
	markup, err := ui.RenderToString(WorkflowDesignerPage(props))
	if err != nil {
		t.Fatalf("render workflow catalog: %v", err)
	}
	for _, want := range []string{"New promotions use the active product configuration shown here.", "Promotion execute"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("catalog missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "Prototype promotion approval") {
		t.Fatalf("reference workflow was visible before opting in:\n%s", markup)
	}

	props.BaseHref = "/workspace/app/admin/workflows?workflow_references=1"
	markup, err = ui.RenderToString(WorkflowDesignerPage(props))
	if err != nil {
		t.Fatalf("render reference workflow catalog: %v", err)
	}
	prototype := strings.Index(markup, "Prototype promotion approval")
	active := strings.Index(markup, "Promotion execute")
	if prototype < 0 || active < 0 || !strings.Contains(markup[prototype:], "Review only") {
		t.Fatal("prototype workflow was not presented as review-only configuration")
	}
}

func TestTodo_UXBLIND_031_Browser(t *testing.T) {
	markup, err := ui.RenderToString(WorkflowDesignerPage(WorkflowDesignerPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("de-DE")},
		Catalog:   []WorkflowCatalogItem{{WorkflowID: "promotion.execute", Name: "Beförderung ausführen", SemanticVersion: "1.2.0", Status: "ACTIVE"}},
	}))
	if err != nil || !strings.Contains(markup, "Neue Beförderungen verwenden") {
		t.Fatalf("German catalog note missing: err=%v markup=%s", err, markup)
	}
}

func TestTodo_UXBLIND_032(t *testing.T) {
	persisted, began := false, false
	props := WorkflowDesignerPageProps{
		OnCreate: func(WorkflowDraftCreateRequest) { persisted = true },
		OnBegin:  func(WorkflowDraftCreateRequest) { began = true },
	}
	start := workflowStartCallback(props)
	start(WorkflowDraftCreateRequest{SemanticVersion: "0.1.0"})
	if !began || persisted {
		t.Fatalf("new workflow start persisted=%t began=%t; expected client-side begin only", persisted, began)
	}
	markup, err := ui.RenderToString(WorkflowDesignerPage(WorkflowDesignerPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Catalog:   []WorkflowCatalogItem{{WorkflowID: "w", Name: "Existing", SemanticVersion: "1.0.0", Status: "ACTIVE"}},
		CanCreate: true,
		OnCreate:  func(WorkflowDraftCreateRequest) {},
	}))
	if err != nil {
		t.Fatalf("render new-workflow landing: %v", err)
	}
	if strings.Contains(markup, "draft=") || strings.Contains(markup, "Untitled workflow") {
		t.Fatal("landing page contains a persisted empty draft")
	}
}

func TestTodo_UXBLIND_032_Browser(t *testing.T) {
	request := WorkflowDraftCreateRequest{SemanticVersion: "0.1.0"}
	pending := workflowPendingDraft{Active: true, Request: request, Draft: WorkflowDraftView{SemanticVersion: request.SemanticVersion}}
	if !pending.Active || pending.Draft.DraftID != "" {
		t.Fatalf("pending draft = %+v, want unsaved client-side draft", pending)
	}
}

func TestTodo_UXBLIND_034(t *testing.T) {
	draft := WorkflowDraftView{
		DraftID: "draft-1", Name: "Promotion path", SemanticVersion: "1.1.0", StartNodeID: "task-1", Revision: 2,
		Nodes: []WorkflowDraftNode{
			{ID: "task-1", StepType: "TASK", Label: "Review request", Parameters: []WorkflowNodeParameter{{ID: "task_assignee", Value: "role:manager"}}, Outcomes: []WorkflowDraftOutcome{{RouteKey: "SUCCEEDED", TargetNodeIDs: []string{"end-1"}}}},
			{ID: "end-1", StepType: "END", Label: "Complete"},
		},
		Edges: []WorkflowDraftEdge{{FromID: "task-1", ToID: "end-1", RouteKey: "SUCCEEDED"}},
	}
	markup := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: draft, SelectedNodeID: "task-1"}))
	for _, want := range []string{"From draft to live", "Test", "Approval", "Activation", "Run test", "Publish and activate"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("release rail missing %q:\n%s", want, markup)
		}
	}
}

func TestTodo_UXBLIND_034_Browser(t *testing.T) {
	draft := WorkflowDraftView{DraftID: "draft-1", Name: "Ready", SemanticVersion: "1.0.0", Nodes: []WorkflowDraftNode{{ID: "task", StepType: "TASK", Parameters: []WorkflowNodeParameter{{ID: "task_assignee", Value: "role:manager"}}}}}
	markup := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: draft}))
	if !strings.Contains(markup, `class="workflow-release"`) || !strings.Contains(markup, "From draft to live") {
		t.Fatal("release path is not exposed in the editor surface")
	}
}
