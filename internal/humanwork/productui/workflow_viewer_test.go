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
		`aria-hidden="true" class="workflow-viewer-graph"`,
		`class="workflow-viewer-outline"`,
		`Promotion approval`,
		`Publication</span><strong>Active`,
		`Version</span><strong>v7 · 2026.9`,
		`Run</span><strong>Waiting`,
		`<path d="M4 12h16M14 6l6 6-6 6"></path>`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("workflow viewer missing %q:\n%s", want, markup)
		}
	}

	// Every disclosed node is rendered once in the visual graph and once in
	// the semantic outline. The shared projection prevents the two views from
	// silently disagreeing.
	for _, node := range workflowViewerFixture().Nodes {
		if got := strings.Count(markup, `data-node-id="`+node.ID+`"`); got != 2 {
			t.Fatalf("node %q rendered %d times, want graph and outline", node.ID, got)
		}
	}
}

func TestTodo_WF_UI_002_Browser(t *testing.T) {
	css := workflowViewerStylesheet()
	for _, want := range []string{
		`@media (max-width:900px)`,
		`.workflow-viewer-layout{grid-template-columns:1fr`,
		`@media (max-width:640px)`,
		`.workflow-viewer-graph{display:none`,
		`.workflow-viewer-header{flex-direction:column`,
		`@media (max-width:360px)`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("responsive workflow viewer stylesheet missing %q:\n%s", want, css)
		}
	}
	if strings.Contains(css, `.workflow-viewer-outline{display:none`) {
		t.Fatal("mobile stylesheet must keep the semantic outline visible")
	}

	markup := renderWorkflowViewer(t, workflowViewerFixture(), DefaultProductLocale)
	if !strings.Contains(markup, `The same steps and routes in reading order.`) {
		t.Fatal("mobile-primary outline lacks a plain-language description")
	}
}

func TestTodo_WF_UI_002_LiveRunOverlay(t *testing.T) {
	projection := workflowViewerFixture()
	markup := renderWorkflowViewer(t, projection, DefaultProductLocale)

	if got := strings.Count(markup, `data-node-id="manager_review" data-state="waiting"`); got != 2 {
		t.Fatalf("live waiting state rendered %d times, want graph and outline", got)
	}
	if got := strings.Count(markup, `Current`); got < 2 {
		t.Fatalf("current-node flag rendered %d times, want graph and outline", got)
	}
	lowerMarkup := strings.ToLower(markup)
	for _, route := range []string{"approved", "rejected"} {
		if !strings.Contains(lowerMarkup, route) {
			t.Fatalf("live workflow omitted route %q", route)
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
}

func TestTodo_WF_UI_002_PartialRunEvidence(t *testing.T) {
	projection := workflowViewerFixture()
	projection.Completeness = false
	projection.Gaps = []string{"attempt 2 is not yet indexed"}
	projection.Nodes[1].RuntimeGap = true

	markup := renderWorkflowViewer(t, projection, DefaultProductLocale)
	if !strings.Contains(markup, `some run evidence could not be resolved`) ||
		!strings.Contains(markup, `attempt 2 is not yet indexed`) ||
		strings.Count(markup, `Evidence unavailable`) != 2 {
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
	if strings.Contains(markup, `workflow-viewer-graph`) {
		t.Fatal("empty workflow must not render an empty graph viewport")
	}
}

func TestTodo_WF_UI_002_Locales(t *testing.T) {
	for _, test := range []struct {
		locale string
		wants  []string
	}{
		{locale: "de-DE", wants: []string{"Ablaufgliederung", "Genehmigung", "Genehmigt", "Veröffentlichung</span><strong>Aktiv"}},
		{locale: "ar", wants: []string{"مخطط تفصيلي لسير العمل", "موافقة", "تمت الموافقة", "النشر</span><strong>نشط"}},
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
	css := workflowViewerStylesheet()
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
