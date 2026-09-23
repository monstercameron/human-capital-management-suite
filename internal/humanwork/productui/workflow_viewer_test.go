package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workflowview"
)

func TestTodo_WF_UI_002(t *testing.T) {
	markup := renderWorkflowViewer(t, workflowViewerFixture(), DefaultProductLocale)

	for _, want := range []string{
		`id="promotion-workflow-title"`,
		`aria-labelledby="promotion-workflow-title"`,
		`class="workflow-path"`,
		`Promotion approval`,
		`Publication</span><strong>Active`,
		`Version</span><strong>2026.9.0`,
		`Run</span><strong>Waiting`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("workflow viewer missing %q:\n%s", want, markup)
		}
	}

	// A published workflow is drawn once, by the same path the editor uses.
	// The stage-column graph and the outline beside it rendered every node
	// twice and could disagree with the editor about their order.
	for _, node := range workflowViewerFixture().Nodes {
		if got := strings.Count(markup, `data-node-id="`+node.ID+`"`); got != 1 {
			t.Fatalf("node %q rendered %d times, want once", node.ID, got)
		}
	}
	if strings.Contains(markup, "<button") {
		t.Fatal("a read-only published workflow renders controls that do nothing")
	}
}

func TestTodo_WF_UI_002_Browser(t *testing.T) {
	// The viewer has no layout of its own to go wrong at narrow widths: it is
	// the editor's path, whose stylesheet carries the breakpoints.
	css := workflowEditorStylesheet()
	for _, want := range []string{`@media (max-width:820px)`, `@media (max-width:420px)`, `.workflow-path{grid-template-columns:1fr}`} {
		if !strings.Contains(css, want) {
			t.Fatalf("path stylesheet missing %q", want)
		}
	}
	markup := renderWorkflowViewer(t, workflowViewerFixture(), DefaultProductLocale)
	if !strings.Contains(markup, `<ol`) || !strings.Contains(markup, `Prepare request`) {
		t.Fatal("the published path is not an ordered, readable list")
	}
}

func TestTodo_WF_UI_002_LiveRunOverlay(t *testing.T) {
	projection := workflowViewerFixture()
	markup := renderWorkflowViewer(t, projection, DefaultProductLocale)

	if got := strings.Count(markup, `data-state="waiting"`); got != 1 {
		t.Fatalf("live waiting state rendered %d times, want once", got)
	}
	if !strings.Contains(markup, `workflow-path-step current`) && !strings.Contains(markup, ` current`) {
		t.Fatal("the step the run is waiting on is not marked current")
	}
	lowerMarkup := strings.ToLower(markup)
	for _, route := range []string{"approved", "rejected", "waiting", "completed"} {
		if !strings.Contains(lowerMarkup, route) {
			t.Fatalf("live workflow omitted %q", route)
		}
	}
}

func TestTodo_WF_UI_002_RedactedRun(t *testing.T) {
	projection := workflowViewerFixture()
	projection.RunDisclosed = false
	projection.Completeness = false
	projection.Redactions = []string{"node attempts"}
	projection.Nodes[1].Status = ""
	projection.Nodes[1].State = workflowview.NodeUnknown
	projection.Nodes[1].Current = false

	markup := renderWorkflowViewer(t, projection, DefaultProductLocale)
	for _, want := range []string{
		`Run</span><strong>Restricted`,
		`Some run details are unavailable`,
		`details you are not authorized to view`,
		`node attempts`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("redacted viewer missing %q", want)
		}
	}
	if strings.Contains(markup, `WAITING`) {
		t.Fatal("redacted viewer leaked raw runtime status")
	}
	for _, claimed := range []string{`data-state=`, "Not started", "Waiting"} {
		if strings.Contains(markup, claimed) {
			t.Fatalf("a run the viewer may not see still shows a per-step state %q", claimed)
		}
	}
}

func TestTodo_WF_UI_002_PartialRunEvidence(t *testing.T) {
	projection := workflowViewerFixture()
	projection.Completeness = false
	projection.Gaps = []string{"attempt 2 is not yet indexed"}
	projection.Nodes[1].RuntimeGap = true

	markup := renderWorkflowViewer(t, projection, DefaultProductLocale)
	if !strings.Contains(markup, `some run evidence could not be resolved`) ||
		!strings.Contains(markup, `attempt 2 is not yet indexed`) ||
		strings.Count(markup, `Evidence unavailable`) != 1 {
		t.Fatalf("partial evidence state is incomplete:\n%s", markup)
	}
}

func TestTodo_WF_UI_002_EmptyDefinition(t *testing.T) {
	projection := workflowViewerFixture()
	projection.Nodes = nil
	projection.Edges = nil
	projection.MaxDepth = 0

	markup := renderWorkflowViewer(t, projection, DefaultProductLocale)
	if !strings.Contains(markup, `This workflow has no steps`) || !strings.Contains(markup, `role="status"`) {
		t.Fatalf("empty workflow needs an announced empty state:\n%s", markup)
	}
	if strings.Contains(markup, `class="workflow-path"`) {
		t.Fatal("empty workflow must not render an empty path")
	}
}

func TestTodo_WF_UI_002_Locales(t *testing.T) {
	for _, test := range []struct {
		locale string
		wants  []string
	}{
		{locale: "de-DE", wants: []string{"Ausgänge", "Genehmigung", "Genehmigt", "Veröffentlichung</span><strong>Aktiv"}},
		{locale: "ar", wants: []string{"المخارج", "موافقة", "تمت الموافقة", "النشر</span><strong>نشط"}},
	} {
		t.Run(test.locale, func(t *testing.T) {
			markup := renderWorkflowViewer(t, workflowViewerFixture(), test.locale)
			for _, want := range test.wants {
				if !strings.Contains(markup, want) {
					t.Fatalf("%s workflow viewer missing translated copy %q", test.locale, want)
				}
			}
			if strings.Contains(markup, "⟦workflow_viewer.") {
				t.Fatalf("%s workflow viewer exposed an untranslated message key", test.locale)
			}
			if !strings.Contains(markup, `dir="auto"`) {
				t.Fatalf("%s workflow viewer does not isolate dynamic authored labels", test.locale)
			}
		})
	}
}

func TestTodo_WF_UI_002_ThemeContract(t *testing.T) {
	css := workflowViewerStylesheet() + workflowEditorStylesheet()
	for _, token := range []string{"var(--surface)", "var(--canvas)", "var(--ink)", "var(--muted)", "var(--line)", "var(--accent)"} {
		if !strings.Contains(css, token) {
			t.Fatalf("workflow viewer does not consume theme token %q", token)
		}
	}
	for _, forbidden := range []string{"#", "rgb(", "hsl(", "margin-left", "padding-left", "border-left"} {
		if strings.Contains(css, forbidden) {
			t.Fatalf("workflow viewer stylesheet contains non-theme or non-logical declaration %q", forbidden)
		}
	}
}

func TestTodo_WF_UI_002_Deterministic(t *testing.T) {
	projection := workflowViewerFixture()
	first := renderWorkflowViewer(t, projection, DefaultProductLocale)
	second := renderWorkflowViewer(t, projection, DefaultProductLocale)
	if first != second {
		t.Fatal("the same immutable workflow projection rendered different markup")
	}
}

func renderWorkflowViewer(t *testing.T, projection workflowview.View, locale string) string {
	t.Helper()
	markup, err := ui.RenderToString(WorkflowViewer(WorkflowViewerProps{
		I18nProps:  I18nProps{Locale: ResolveProductLocale(locale)},
		ID:         "promotion-workflow",
		Projection: projection,
	}))
	if err != nil {
		t.Fatalf("render workflow viewer: %v", err)
	}
	return markup
}

func workflowViewerFixture() workflowview.View {
	return workflowview.View{
		WorkflowID:        "promotion.approval",
		Name:              "Promotion approval",
		Version:           7,
		SemanticVersion:   "2026.9.0",
		PlanDigest:        "sha256:test",
		PublicationStatus: "ACTIVE",
		HasRun:            true,
		RunDisclosed:      true,
		InstanceID:        "run-1042",
		RuntimeStatus:     "WAITING",
		Completeness:      true,
		MaxDepth:          2,
		Nodes: []workflowview.Node{
			{ID: "draft", Label: "Prepare request", StepType: "TASK", Start: true, State: workflowview.NodeSucceeded, Status: "COMPLETED", RuntimeKnown: true, Routes: []workflowview.Route{{Key: "submitted", TargetID: "manager_review"}}},
			{ID: "manager_review", Label: "Manager review", StepType: "APPROVAL", Depth: 1, Current: true, Attempt: 1, State: workflowview.NodeWaiting, Status: "WAITING", RuntimeKnown: true, Routes: []workflowview.Route{{Key: "approved", TargetID: "complete"}, {Key: "rejected", TargetID: "declined"}}},
			{ID: "complete", Label: "Complete promotion", StepType: "END", Depth: 2, Terminal: true, State: workflowview.NodeNotStarted},
			{ID: "declined", Label: "Close request", StepType: "END", Depth: 2, Lane: 1, Terminal: true, State: workflowview.NodeNotStarted},
		},
		Edges: []workflowview.Edge{
			{ID: "draft:submitted:manager_review", FromID: "draft", ToID: "manager_review", RouteKey: "submitted"},
			{ID: "manager_review:approved:complete", FromID: "manager_review", ToID: "complete", RouteKey: "approved"},
			{ID: "manager_review:rejected:declined", FromID: "manager_review", ToID: "declined", RouteKey: "rejected"},
		},
	}
}
