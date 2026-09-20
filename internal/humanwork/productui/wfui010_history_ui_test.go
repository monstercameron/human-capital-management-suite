package productui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_WF_UI_010_Browser(t *testing.T) {
	draft := WorkflowDraftView{
		DraftID: "draft-10", WorkflowID: "workflow.people.change", Name: "People change", SemanticVersion: "1.2.0", Revision: 8,
		HistoryPosition: 2, HistoryLength: 3, HistoryLabel: "Connect review", CanUndo: true, CanRedo: true, LayoutMode: "AUTO", StartNodeID: "start",
		Nodes: []WorkflowDraftNode{
			{ID: "end", StepType: "END"},
			{ID: "review", StepType: "APPROVAL"},
			{ID: "start", StepType: "START"},
		},
		Edges: []WorkflowDraftEdge{{FromID: "start", ToID: "review", RouteKey: "NEXT"}, {FromID: "review", ToID: "end", RouteKey: "APPROVED"}},
		Changes: []WorkflowDraftSemanticChange{
			{Kind: "EDGE", Operation: "ADDED", SubjectID: "review", Field: "APPROVED", After: "end"},
			{Kind: "PARAMETER", Operation: "UPDATED", SubjectID: "review", Field: "display_name", Before: "Review", After: "Manager review"},
		},
	}
	markup, err := ui.RenderToString(WorkflowDesignerPage(WorkflowDesignerPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Draft: &draft,
		OnHistory: func(string) {}, OnSelectNode: func(string) {}, BaseHref: Path(PageWorkflowDesigner),
	}))
	if err != nil {
		t.Fatalf("render workflow history: %v", err)
	}
	for _, want := range []string{
		`class="workflow-draft-history-controls"`, `aria-label="Undo"`, `aria-label="Redo"`,
		`Change 2 of 3`, `What changed`, `Connect review`, `Added`, `Updated`, `Auto layout`,
		`data-layout-mode="auto"`, `class="workflow-viewer-graph"`, `class="workflow-viewer-outline workflow-outline-editor"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("workflow history markup missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, `aria-label="Undo" disabled`) || strings.Contains(markup, `aria-label="Redo" disabled`) {
		t.Fatalf("available history action rendered disabled:\n%s", markup)
	}
	styles := workflowDesignerStylesheet()
	for _, want := range []string{
		`.workflow-draft-history-controls{display:flex`, `.workflow-draft-change-row{display:grid`,
		`@media (max-width:420px)`, `.workflow-draft-history-controls .button span{position:absolute`,
	} {
		if !strings.Contains(styles, want) {
			t.Fatalf("responsive workflow history CSS missing %q", want)
		}
	}

	first, second := workflowDraftGraphProjection(draft), workflowDraftGraphProjection(draft)
	if !reflect.DeepEqual(first, second) || len(first.Nodes) != 3 || first.MaxDepth != 2 {
		t.Fatalf("automatic imported-draft layout is not deterministic: first=%+v second=%+v", first, second)
	}
}

func TestTodo_WF_UI_010_BrowserLocalizesHistoryControls(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		draft := WorkflowDraftView{DraftID: "draft-10", WorkflowID: "workflow.people.change", Name: "People change", SemanticVersion: "1.2.0", Revision: 2, HistoryPosition: 1, HistoryLength: 1, LayoutMode: "AUTO"}
		markup, err := ui.RenderToString(WorkflowDesignerPage(WorkflowDesignerPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale(locale)}, Draft: &draft, BaseHref: Path(PageWorkflowDesigner)}))
		if err != nil {
			t.Fatalf("render %s: %v", locale, err)
		}
		if strings.Contains(markup, "⟦") || !strings.Contains(markup, `disabled`) {
			t.Fatalf("localized disabled history controls invalid for %s:\n%s", locale, markup)
		}
	}
}
