package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_WF_UI_009_Browser(t *testing.T) {
	draft := WorkflowDraftView{
		DraftID: "draft-9", WorkflowID: "workflow.9", Name: "Keyboard workflow", SemanticVersion: "1.2.3", Revision: 9,
		StartNodeID: "start",
		Nodes: []WorkflowDraftNode{
			{ID: "start", Label: "Start", StepType: "TASK"},
			{ID: "review", Label: "Review", StepType: "APPROVAL"},
			{ID: "complete", Label: "Complete", StepType: "END"},
		},
		Edges: []WorkflowDraftEdge{{FromID: "start", ToID: "review", RouteKey: "SUCCEEDED"}, {FromID: "review", ToID: "complete", RouteKey: "APPROVED"}},
	}
	markup, err := ui.RenderToString(workflowDraftWorkspace(I18nProps{Locale: ResolveProductLocale("en-US")}, draft, nil, "review", func(string) {}, nil, nil, nil, func(WorkflowNodeMove) {}, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`Editable outline`,
		`aria-label="Editable workflow graph"`,
		`Configure Review`,
		`aria-current="step"`,
		`aria-label="Move Review earlier"`,
		`aria-label="Move Review later"`,
		`disabled`,
		`SUCCEEDED`,
		`APPROVED`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("editable outline missing %q:\n%s", want, markup)
		}
	}
	if got := strings.Count(markup, `aria-label="Move Review earlier"`); got != 2 {
		t.Fatalf("graph and outline did not expose the same move command: count=%d", got)
	}
	if got := strings.Count(markup, `class="workflow-outline-edit-row"`); got != len(draft.Nodes) {
		t.Fatalf("outline rows = %d, want %d", got, len(draft.Nodes))
	}
	if strings.Contains(markup, `tabindex="-1"`) {
		t.Fatal("outline removed an edit control from the keyboard tab order")
	}
}

func TestTodo_WF_UI_009_OutlineIsResponsiveThemeableAndLocalized(t *testing.T) {
	css := workflowDesignerStylesheet()
	for _, want := range []string{
		`.workflow-outline-edit-main{display:grid;grid-template-columns:minmax(0,1fr) auto`,
		`@media (max-width:640px)`,
		`@media (max-width:420px){.workflow-outline-edit-main{grid-template-columns:1fr}`,
		`.workflow-outline-move-actions{justify-content:flex-end}`,
		`.workflow-outline-select strong{overflow-wrap:normal;word-break:normal}`,
		`padding-inline-start`,
		`var(--hcm-radius-control)`,
		`var(--hcm-font-size-small)`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("outline stylesheet missing %q", want)
		}
	}
	for _, forbidden := range []string{"padding-left", "margin-left", "#", "rgb(", "hsl("} {
		if strings.Contains(css, forbidden) {
			t.Fatalf("outline stylesheet contains fixed or direction-specific token %q", forbidden)
		}
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		resolved := ResolveProductLocale(locale)
		for _, key := range []string{"workflow_outline_editor.title", "workflow_outline_editor.description", "workflow_outline_editor.move_earlier", "workflow_outline_editor.move_later"} {
			if got := resolved.Text(key, map[string]string{"name": "Review"}); got == "" || strings.Contains(got, "⟦") {
				t.Fatalf("%s %s = %q", locale, key, got)
			}
		}
	}
}

func TestTodo_WF_UI_009_SingularTopologyCopyIsLocalized(t *testing.T) {
	draft := WorkflowDraftView{
		DraftID: "draft-one", WorkflowID: "workflow.one", Name: "One step", SemanticVersion: "0.1.0", Revision: 1,
		StartNodeID: "task_1", Nodes: []WorkflowDraftNode{{ID: "task_1", Label: "Task 1", StepType: "TASK"}},
	}
	for locale, want := range map[string]string{
		"en-US": "1 step · 0 routed outcomes",
		"de-DE": "1 Schritt · 0 geroutete Ergebnisse",
		"ar":    "خطوة واحدة · 0 نتائج موجّهة",
	} {
		markup, err := ui.RenderToString(workflowDraftWorkspace(I18nProps{Locale: ResolveProductLocale(locale)}, draft, nil, "task_1", func(string) {}, nil, nil, nil, func(WorkflowNodeMove) {}, nil, nil))
		if err != nil {
			t.Fatalf("%s render: %v", locale, err)
		}
		if !strings.Contains(markup, want) {
			t.Fatalf("%s singular topology copy missing %q:\n%s", locale, want, markup)
		}
	}
}
