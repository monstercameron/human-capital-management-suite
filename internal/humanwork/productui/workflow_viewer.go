package productui

import (
	"strings"
	"unicode"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workflowview"
)

// WorkflowViewerProps renders one authorized immutable publication. The graph
// and outline consume the same projection, so neither view can invent a route
// or disagree about live node state.
type WorkflowViewerProps struct {
	I18nProps
	ID         string
	Projection workflowview.View
}

// WorkflowViewer shows one published definition, and a live run of it when the
// viewer may see one, as the same path the editor draws. One rendering of a
// workflow means a released version and the draft that succeeds it read the
// same way; the stage-column graph and the separate outline it replaces did
// not even agree with the editor about which step came first.
//
// A step shows a run state only when the run was disclosed. A withheld run
// used to leave every step reading "not started", which is a confident answer
// the viewer was never given.
func WorkflowViewer(props WorkflowViewerProps) ui.Node {
	projection := props.Projection
	id := strings.TrimSpace(props.ID)
	if id == "" {
		id = "workflow-viewer"
	}
	titleID := id + "-title"

	metadata := []ui.Node{
		workflowViewerBadge(props.Text("workflow_viewer.publication_status"), displayWorkflowToken(props, "publication", projection.PublicationStatus), "publication"),
		workflowViewerBadge(props.Text("workflow_viewer.version"), projection.SemanticVersion, "version"),
	}
	if projection.HasRun && projection.RunDisclosed {
		metadata = append(metadata, workflowViewerBadge(props.Text("workflow_viewer.run_status"), displayWorkflowStatus(props, projection.RuntimeStatus), "run"))
	} else if projection.HasRun {
		metadata = append(metadata, workflowViewerBadge(props.Text("workflow_viewer.run_status"), props.Text("workflow_viewer.restricted"), "restricted"))
	}

	children := []ui.Node{
		html.Header(html.Props{Key: "header", Class: "workflow-viewer-header"},
			html.H2(html.Props{ID: titleID, Raw: map[string]any{"dir": "auto"}}, ui.Text(projection.Name)),
			html.Div(html.Props{Class: "workflow-viewer-metadata", Aria: map[string]string{"label": props.Text("workflow_viewer.metadata")}}, metadata...),
		),
	}
	if !projection.Completeness {
		children = append(children, html.WithKey(workflowViewerCompleteness(props, projection), "completeness"))
	}
	if len(projection.Nodes) == 0 {
		children = append(children, html.WithKey(ui.CreateElement(EmptyState, EmptyStateProps{
			Title: props.Text("workflow_viewer.empty_title"), Description: props.Text("workflow_viewer.empty_description"), Role: "status",
		}), "empty"))
		return html.Section(html.Props{ID: id, Class: "workflow-viewer", Aria: map[string]string{"labelledby": titleID}}, children...)
	}

	draft, states := workflowViewerPath(props, projection)
	children = append(children, html.WithKey(workflowPathCanvas(props.I18nProps, buildWorkflowPath(draft, ""), "", nil, states, nil), "path"))
	return html.Section(html.Props{ID: id, Class: "workflow-viewer", Aria: map[string]string{"labelledby": titleID}}, children...)
}

// workflowViewerPath restates a published projection in the terms the path
// reads. Every route of a compiled plan is connected, so a released workflow
// has nothing "left to finish"; run state is carried only for a disclosed
// run.
func workflowViewerPath(props WorkflowViewerProps, projection workflowview.View) (WorkflowDraftView, map[string]workflowPathState) {
	draft := WorkflowDraftView{WorkflowID: projection.WorkflowID, Name: projection.Name, SemanticVersion: projection.SemanticVersion}
	states := make(map[string]workflowPathState)
	for _, node := range projection.Nodes {
		if node.Start {
			draft.StartNodeID = node.ID
		}
		step := WorkflowDraftNode{ID: node.ID, StepType: node.StepType}
		if label := strings.TrimSpace(node.Label); label != "" && label != labelForNodeID(node.ID) {
			step.Label = label
		}
		seen := make(map[string]bool, len(node.Routes))
		for _, route := range node.Routes {
			draft.Edges = append(draft.Edges, WorkflowDraftEdge{FromID: node.ID, ToID: route.TargetID, RouteKey: route.Key})
			if !seen[route.Key] {
				seen[route.Key] = true
				step.Outcomes = append(step.Outcomes, WorkflowDraftOutcome{RouteKey: route.Key})
			}
		}
		draft.Nodes = append(draft.Nodes, step)
		if projection.HasRun && projection.RunDisclosed {
			states[node.ID] = workflowPathState{Key: string(node.State), Label: workflowNodeStatus(props, node), Tone: workflowNodeTone(node), Current: node.Current}
		}
	}
	return draft, states
}

func workflowNodeTone(node workflowview.Node) string {
	switch node.State {
	case workflowview.NodeSucceeded:
		return "positive"
	case workflowview.NodeFailed:
		return "danger"
	case workflowview.NodeWaiting, workflowview.NodeUnknown:
		return "warning"
	case workflowview.NodeCancelled:
		return "neutral"
	default:
		if node.Current {
			return "info"
		}
		return "neutral"
	}
}

func workflowViewerBadge(label, value, kind string) ui.Node {
	return html.Span(html.Props{Class: "workflow-viewer-badge", Data: map[string]string{"kind": kind}},
		html.Span(html.Props{Class: "workflow-viewer-badge-label"}, ui.Text(label)),
		html.Strong(html.Props{}, ui.Text(value)),
	)
}

func workflowViewerCompleteness(props WorkflowViewerProps, projection workflowview.View) ui.Node {
	detail := props.Text("workflow_viewer.partial_description")
	if !projection.RunDisclosed {
		detail = props.Text("workflow_viewer.restricted_description")
	}
	items := append([]string(nil), projection.Redactions...)
	items = append(items, projection.Gaps...)
	var evidence ui.Node
	if len(items) > 0 {
		rows := make([]ui.Node, 0, len(items))
		for _, item := range items {
			rows = append(rows, html.Li(html.Props{}, ui.Text(item)))
		}
		evidence = html.Ul(html.Props{Class: "workflow-viewer-evidence"}, rows...)
	}
	return html.Div(html.Props{Class: "workflow-viewer-notice", Role: "status"},
		html.Strong(html.Props{}, ui.Text(props.Text("workflow_viewer.partial_title"))),
		html.P(html.Props{}, ui.Text(detail)), evidence,
	)
}

func workflowNodeStatus(props WorkflowViewerProps, node workflowview.Node) string {
	if node.RuntimeGap {
		return props.Text("workflow_viewer.state.gap")
	}
	if node.Status != "" {
		return displayWorkflowStatus(props, node.Status)
	}
	return props.Text("workflow_viewer.state." + strings.ReplaceAll(string(node.State), "-", "_"))
}

func displayWorkflowStatus(props WorkflowViewerProps, status string) string {
	return displayWorkflowToken(props, "status", status)
}

func displayWorkflowToken(props WorkflowViewerProps, namespace, value string) string {
	normalized := strings.NewReplacer("-", "_", ".", "_", "/", "_").Replace(strings.ToLower(strings.TrimSpace(value)))
	key := "workflow_viewer." + namespace + "." + normalized
	translated := props.Text(key)
	if strings.HasPrefix(translated, "⟦") {
		return labelForNodeID(value)
	}
	return translated
}

func bidiIsolate(value string) string {
	if value == "" {
		return ""
	}
	return "\u2068" + value + "\u2069"
}

func labelForNodeID(id string) string {
	parts := strings.FieldsFunc(id, func(r rune) bool { return r == '_' || r == '-' || r == '.' || r == '/' })
	for index, part := range parts {
		if part == "" {
			continue
		}
		runes := []rune(strings.ToLower(part))
		runes[0] = unicode.ToUpper(runes[0])
		parts[index] = string(runes)
	}
	return strings.Join(parts, " ")
}

func workflowViewerStylesheet() string {
	return buildTypedSheet(func() {
		declareGlobal(".workflow-viewer", gwccss.Display.Grid, gwccss.Gap(gwccss.VarLength("hcm-space-3")), gwccss.Raw("min-inline-size", "0"))
		declareGlobal(".workflow-viewer-header", gwccss.Display.Flex, gwccss.Raw("flex-wrap", "wrap"), gwccss.Raw("align-items", "center"), gwccss.Raw("justify-content", "space-between"), gwccss.Gap(gwccss.VarLength("hcm-space-2")))
		declareGlobal(".workflow-viewer-header h2", gwccss.Margin(gwccss.Zero), gwccss.Raw("font-size", "1.25rem"))
		declareGlobal(".workflow-viewer-metadata", gwccss.Display.Flex, gwccss.Raw("flex-wrap", "wrap"), gwccss.Gap(gwccss.Px(8)))
		declareGlobal(".workflow-viewer-badge", gwccss.Display.InlineFlex, gwccss.Raw("align-items", "center"), gwccss.Gap(gwccss.Px(6)), gwccss.Raw("padding", "2px 10px"), gwccss.Border(gwccss.Px(1), gwccss.Var("line")), gwccss.Raw("border-radius", "999px"), gwccss.Raw("font-size", "var(--hcm-font-size-small)"))
		declareGlobal(".workflow-viewer-badge strong", gwccss.Raw("font-weight", "600"))
		declareGlobal(".workflow-viewer-badge-label", gwccss.TextColor(gwccss.Var("muted")))
		declareGlobal(".workflow-viewer-notice", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(6)), gwccss.Padding(gwccss.Px(14)), gwccss.Border(gwccss.Px(1), gwccss.Var("warning")), gwccss.Rounded(gwccss.VarLength("hcm-radius-control")), gwccss.Bg(gwccss.Var("warning-bg")))
		declareGlobal(".workflow-viewer-notice p", gwccss.Margin(gwccss.Zero), gwccss.TextColor(gwccss.Var("muted")))
		declareGlobal(".workflow-viewer-evidence", gwccss.Margin(gwccss.Zero), gwccss.Padding(gwccss.Zero), gwccss.Raw("list-style", "none"), gwccss.Display.Grid, gwccss.Gap(gwccss.Px(4)), gwccss.Raw("font-size", "var(--hcm-font-size-small)"))
	})
}
