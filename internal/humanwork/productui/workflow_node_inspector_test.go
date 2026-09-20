package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func wfui006InspectorDraft() WorkflowDraftView {
	return WorkflowDraftView{
		DraftID: "draft-6", WorkflowID: "workflow.promotion", Name: "Promotion", SemanticVersion: "1.1.1", Revision: 3,
		StartNodeID: "snapshot", TemplateID: "template.promotion", TemplateVersion: 1,
		Nodes: []WorkflowDraftNode{
			{ID: "snapshot", StepType: "ACTION", Locked: true, LockKind: "START", Parameters: []WorkflowNodeParameter{{ID: "display_name", Label: "Display name", Kind: "TEXT", Value: "Snapshot worker", Maximum: 120}}},
			{ID: "await_payroll", StepType: "SIGNAL", Parameters: []WorkflowNodeParameter{{ID: "signal_timeout_seconds", Label: "Close after seconds", Kind: "INTEGER", Value: "3600", Minimum: 0, Maximum: 31536000}}},
		},
		Edges:    []WorkflowDraftEdge{{FromID: "snapshot", ToID: "await_payroll", RouteKey: "SUCCEEDED"}},
		Overlays: []WorkflowTemplateOverlay{{Operation: "ADD", EntryID: "kernel.task", EntryVersion: 1}},
	}
}

func TestTodo_WF_UI_006_ProductInspectorRendersTypedFormAndLockedControls(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(WorkflowNodeInspector, WorkflowNodeInspectorProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale(DefaultProductLocale)}, Draft: wfui006InspectorDraft(),
		Palette:  []WorkflowPaletteItem{{ID: "kernel.task", Version: 1, Name: "Task", Kind: "BLOCK"}},
		OnUpdate: func(WorkflowNodeParameterChange) {}, OnOverlay: func(WorkflowTemplateOverlayChange) {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="workflow-inspector-node"`, `id="workflow-parameter-display_name"`, `name="display_name"`, `Required control`, `disabled`, `Template change history · 1`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("inspector markup missing %q:\n%s", want, markup)
		}
	}
}

func TestTodo_WF_UI_006_OverlayRequiresAnAuditReason(t *testing.T) {
	draft := wfui006InspectorDraft()
	draft.StartNodeID = "await_payroll"
	markup, err := ui.RenderToString(ui.CreateElement(WorkflowNodeInspector, WorkflowNodeInspectorProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale(DefaultProductLocale)},
		Draft:     draft,
		Palette:   []WorkflowPaletteItem{{ID: "kernel.task", Version: 1, Name: "Task", Kind: "BLOCK"}},
		OnOverlay: func(WorkflowTemplateOverlayChange) {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"Omit step", "Replace step"} {
		button := buttonByText(t, markup, label)
		if !strings.Contains(button, " disabled") {
			t.Fatalf("%q must stay disabled until an audit reason is entered: %s", label, button)
		}
	}
	if !strings.Contains(markup, `aria-describedby="workflow-overlay-reason-help"`) {
		t.Fatal("overlay reason is not programmatically associated with its guidance")
	}
}

func buttonByText(t *testing.T, markup, text string) string {
	t.Helper()
	end := strings.Index(markup, text+"</button>")
	if end < 0 {
		t.Fatalf("button %q not found", text)
	}
	start := strings.LastIndex(markup[:end], "<button")
	if start < 0 {
		t.Fatalf("button %q has no opening tag", text)
	}
	return markup[start:end]
}

func TestTodo_WF_UI_006_Browser(t *testing.T) {
	draft := wfui006InspectorDraft()
	markup, err := ui.RenderToString(ui.CreateElement(WorkflowNodeInspector, WorkflowNodeInspectorProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale(DefaultProductLocale)}, Draft: draft, SelectedNodeID: "await_payroll",
		Palette:  []WorkflowPaletteItem{{ID: "kernel.task", Version: 1, Name: "Task", Kind: "BLOCK"}},
		OnUpdate: func(WorkflowNodeParameterChange) {}, OnOverlay: func(WorkflowTemplateOverlayChange) {}, OnSelect: func(string) {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`type="number"`, `min="0"`, `max="31536000"`, `Save parameters`, `Omit step`, `Replace step`, `aria-labelledby="workflow-node-inspector-title"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("interactive inspector markup missing %q:\n%s", want, markup)
		}
	}
	if !strings.Contains(markup, `<option selected value="await_payroll">`) {
		t.Fatalf("route-selected editor step was not preserved across render:\n%s", markup)
	}
}

func TestTodo_WF_UI_006_Locales(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		markup, err := ui.RenderToString(ui.CreateElement(WorkflowNodeInspector, WorkflowNodeInspectorProps{I18nProps: I18nProps{Locale: ResolveProductLocale(locale)}, Draft: wfui006InspectorDraft()}))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(markup, "workflow_inspector.") {
			t.Fatalf("%s inspector exposed untranslated keys: %s", locale, markup)
		}
	}
}

func TestTodo_WF_UI_007_BrowserRendersKeyboardOutcomeAndBindingEditors(t *testing.T) {
	draft := wfui006InspectorDraft()
	draft.Nodes[0].Outcomes = []WorkflowDraftOutcome{{RouteKey: "SUCCEEDED", TargetNodeIDs: []string{"await_payroll"}}}
	draft.Nodes[0].Bindings = []WorkflowDraftBinding{{
		TargetPath: "worker_id", TargetType: "WorkerID", SourceKind: "NODE_OUTPUT", SourceNodeID: "snapshot_source", SourcePath: "worker_id",
		Candidates: []WorkflowDraftBindingCandidate{{SourceNodeID: "await_payroll", SourcePath: "worker_id", ValueType: "WorkerID"}},
	}}
	markup, err := ui.RenderToString(ui.CreateElement(WorkflowNodeInspector, WorkflowNodeInspectorProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale(DefaultProductLocale)}, Draft: draft,
		OnSetOutcome: func(WorkflowOutcomeChange) {}, OnBindInput: func(WorkflowInputBindingChange) {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="workflow-outcomes-title"`, `class="labeled-control"`, `name="target_node"`, `type="submit"`, `Connect`, `id="workflow-bindings-title"`, `name="source_output"`, `Bind input`, `WorkerID`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("WF-UI-007 editor markup missing %q:\n%s", want, markup)
		}
	}
	css := workflowDesignerStylesheet()
	for _, want := range []string{`.workflow-node-inspector .labeled-control{display:grid`, `.workflow-link-editor-row>*{min-width:0}`, `@media (max-width:760px){.workflow-link-editor-row{grid-template-columns:1fr}`} {
		if !strings.Contains(css, want) {
			t.Fatalf("WF-UI-007 responsive editor stylesheet missing %q", want)
		}
	}
}

func TestTodo_WF_UI_007_Locales(t *testing.T) {
	draft := wfui006InspectorDraft()
	draft.Nodes[0].Outcomes = []WorkflowDraftOutcome{{RouteKey: "SUCCEEDED", TargetNodeIDs: []string{"await_payroll"}}}
	draft.Nodes[0].Bindings = []WorkflowDraftBinding{{TargetPath: "worker_id", TargetType: "WorkerID"}}
	for _, locale := range []string{"de-DE", "ar"} {
		markup, err := ui.RenderToString(ui.CreateElement(WorkflowNodeInspector, WorkflowNodeInspectorProps{I18nProps: I18nProps{Locale: ResolveProductLocale(locale)}, Draft: draft}))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(markup, "workflow_inspector.") {
			t.Fatalf("%s link editor exposed untranslated keys: %s", locale, markup)
		}
	}
}
