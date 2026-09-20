package productui

import (
	"strconv"
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
	ID             string
	Projection     workflowview.View
	SelectedNodeID string
	AuthoringOrder []string
	OnSelectNode   func(string)
	OnMoveNode     func(WorkflowNodeMove)
}

// WorkflowViewer is the read-only workflow visualization used before the
// authoring surface is admitted. The decorative graph is optimized for visual
// scanning; the always-present outline is the semantic, keyboard and mobile
// representation of the exact same nodes and routes.
func WorkflowViewer(props WorkflowViewerProps) ui.Node {
	projection := props.Projection
	id := strings.TrimSpace(props.ID)
	if id == "" {
		id = "workflow-viewer"
	}
	titleID := id + "-title"
	graphTitleID := id + "-graph-title"
	outlineTitleID := id + "-outline-title"

	metadata := []ui.Node{
		workflowViewerBadge(props.Text("workflow_viewer.publication_status"), displayWorkflowToken(props, "publication", projection.PublicationStatus), "publication"),
		workflowViewerBadge(props.Text("workflow_viewer.version"), "v"+strconv.FormatUint(uint64(projection.Version), 10)+" · "+projection.SemanticVersion, "version"),
	}
	if projection.HasRun && projection.RunDisclosed {
		metadata = append(metadata, workflowViewerBadge(props.Text("workflow_viewer.run_status"), displayWorkflowStatus(props, projection.RuntimeStatus), "run"))
	} else if projection.HasRun {
		metadata = append(metadata, workflowViewerBadge(props.Text("workflow_viewer.run_status"), props.Text("workflow_viewer.restricted"), "restricted"))
	} else {
		metadata = append(metadata, workflowViewerBadge(props.Text("workflow_viewer.run_status"), props.Text("workflow_viewer.definition_only"), "definition"))
	}

	children := []ui.Node{
		html.Header(html.Props{Class: "workflow-viewer-header"},
			html.Div(html.Props{Class: "workflow-viewer-heading"},
				html.P(html.Props{Class: "eyebrow"}, ui.Text(props.Text("workflow_viewer.eyebrow"))),
				html.H2(html.Props{ID: titleID, Raw: map[string]any{"dir": "auto"}}, ui.Text(projection.Name)),
				html.P(html.Props{Class: "workflow-viewer-description"}, ui.Text(props.Text("workflow_viewer.description"))),
			),
			html.Div(html.Props{Class: "workflow-viewer-metadata", Aria: map[string]string{"label": props.Text("workflow_viewer.metadata")}}, metadata...),
		),
	}
	if !projection.Completeness {
		children = append(children, workflowViewerCompleteness(props, projection))
	}
	if len(projection.Nodes) == 0 {
		children = append(children, ui.CreateElement(EmptyState, EmptyStateProps{
			Title: props.Text("workflow_viewer.empty_title"), Description: props.Text("workflow_viewer.empty_description"), Role: "status",
		}))
		return html.Section(html.Props{ID: id, Class: "surface workflow-viewer", Aria: map[string]string{"labelledby": titleID}}, children...)
	}

	children = append(children,
		html.Div(html.Props{Class: "workflow-viewer-layout"},
			html.Section(html.Props{Class: "workflow-viewer-graph", Raw: map[string]any{"aria-hidden": "true"}},
				html.H3(html.Props{ID: graphTitleID}, ui.Text(props.Text("workflow_viewer.graph_title"))),
				workflowViewerGraph(props, projection),
			),
			html.Section(html.Props{Class: "workflow-viewer-outline", Aria: map[string]string{"labelledby": outlineTitleID}},
				html.Div(html.Props{Class: "workflow-viewer-outline-heading"},
					html.H3(html.Props{ID: outlineTitleID}, ui.Text(props.Text("workflow_viewer.outline_title"))),
					html.P(html.Props{}, ui.Text(props.Text("workflow_viewer.outline_description"))),
				),
				workflowViewerOutline(props, projection),
			),
		),
	)
	return html.Section(html.Props{ID: id, Class: "surface workflow-viewer", Aria: map[string]string{"labelledby": titleID}}, children...)
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

func workflowViewerGraph(props WorkflowViewerProps, projection workflowview.View) ui.Node {
	order := make(map[string]int, len(props.AuthoringOrder))
	for index, nodeID := range props.AuthoringOrder {
		order[nodeID] = index
	}
	columns := make([]ui.Node, 0, projection.MaxDepth+1)
	for depth := 0; depth <= projection.MaxDepth; depth++ {
		cards := []ui.Node{}
		for _, node := range projection.Nodes {
			if node.Depth != depth {
				continue
			}
			cards = append(cards, workflowViewerGraphNode(props, node, order[node.ID], len(props.AuthoringOrder)))
		}
		columns = append(columns, html.Div(html.Props{Class: "workflow-viewer-column", Data: map[string]string{"depth": strconv.Itoa(depth)}},
			html.P(html.Props{Class: "workflow-viewer-stage"}, ui.Text(props.Text("workflow_viewer.stage", map[string]string{"number": strconv.Itoa(depth + 1)}))),
			html.Div(html.Props{Class: "workflow-viewer-column-nodes"}, cards...),
		))
	}
	return html.Div(html.Props{Class: "workflow-viewer-graph-scroll"},
		html.Div(html.Props{Class: "workflow-viewer-graph-flow"}, columns...),
	)
}

func workflowViewerGraphNode(props WorkflowViewerProps, node workflowview.Node, authoringIndex, authoringCount int) ui.Node {
	routes := make([]ui.Node, 0, len(node.Routes))
	for _, route := range node.Routes {
		routes = append(routes, html.Li(html.Props{},
			html.Span(html.Props{Class: "workflow-viewer-route-key"}, ui.Text(displayWorkflowToken(props, "outcome", route.Key))),
			productIcon("history-forward", "workflow-viewer-route-arrow"),
			html.Span(html.Props{Class: "workflow-viewer-route-target", Raw: map[string]any{"dir": "auto"}}, ui.Text(labelForNodeID(route.TargetID))),
		))
	}
	class := "workflow-viewer-node"
	if node.ID == props.SelectedNodeID {
		class += " selected"
	}
	children := []ui.Node{
		html.Div(html.Props{Class: "workflow-viewer-node-topline"},
			html.Span(html.Props{Class: "workflow-viewer-node-type"}, ui.Text(displayWorkflowToken(props, "step", node.StepType))),
			html.Span(html.Props{Class: "workflow-viewer-node-status"}, ui.Text(workflowNodeStatus(props, node))),
		),
		html.H4(html.Props{Raw: map[string]any{"dir": "auto"}}, ui.Text(node.Label)),
		workflowViewerNodeFlags(props, node),
		html.Ul(html.Props{Class: "workflow-viewer-routes"}, routes...),
	}
	if props.OnSelectNode != nil || props.OnMoveNode != nil {
		selectProps := html.Props{Class: "button secondary compact", Type: "button", Disabled: props.OnSelectNode == nil}
		if props.OnSelectNode != nil {
			nodeID := node.ID
			selectProps.OnClick = ui.UseEvent(func(ui.MouseEvent) { props.OnSelectNode(nodeID) })
		}
		earlier := html.Props{Class: "button ghost compact icon-button", Type: "button", Disabled: props.OnMoveNode == nil || authoringIndex <= 0, Title: props.Text("workflow_outline_editor.move_earlier", map[string]string{"name": node.Label}), Aria: map[string]string{"label": props.Text("workflow_outline_editor.move_earlier", map[string]string{"name": node.Label})}}
		later := html.Props{Class: "button ghost compact icon-button", Type: "button", Disabled: props.OnMoveNode == nil || authoringCount == 0 || authoringIndex >= authoringCount-1, Title: props.Text("workflow_outline_editor.move_later", map[string]string{"name": node.Label}), Aria: map[string]string{"label": props.Text("workflow_outline_editor.move_later", map[string]string{"name": node.Label})}}
		if props.OnMoveNode != nil {
			nodeID := node.ID
			earlier.OnClick = ui.UseEvent(func(ui.MouseEvent) { props.OnMoveNode(WorkflowNodeMove{NodeID: nodeID, Direction: "EARLIER"}) })
			later.OnClick = ui.UseEvent(func(ui.MouseEvent) { props.OnMoveNode(WorkflowNodeMove{NodeID: nodeID, Direction: "LATER"}) })
		}
		children = append(children, html.Div(html.Props{Class: "workflow-viewer-node-authoring"}, html.Button(selectProps, ui.Text(props.Text("workflow_outline_editor.configure", map[string]string{"name": node.Label}))), html.Button(earlier, productIcon("move-up", "")), html.Button(later, productIcon("move-down", ""))))
	}
	return html.Article(html.Props{Class: class, Data: map[string]string{"node-id": node.ID, "state": string(node.State)}}, children...)
}

func workflowViewerOutline(props WorkflowViewerProps, projection workflowview.View) ui.Node {
	groups := make([]ui.Node, 0, projection.MaxDepth+1)
	for depth := 0; depth <= projection.MaxDepth; depth++ {
		items := []ui.Node{}
		for _, node := range projection.Nodes {
			if node.Depth != depth {
				continue
			}
			routes := make([]ui.Node, 0, len(node.Routes))
			for _, route := range node.Routes {
				routes = append(routes, html.Li(html.Props{}, ui.Text(props.Text("workflow_viewer.route", map[string]string{
					"route":  bidiIsolate(displayWorkflowToken(props, "outcome", route.Key)),
					"target": bidiIsolate(labelForNodeID(route.TargetID)),
				}))))
			}
			items = append(items, html.Li(html.Props{Class: "workflow-viewer-outline-node", Data: map[string]string{"node-id": node.ID, "state": string(node.State)}},
				html.Div(html.Props{Class: "workflow-viewer-outline-summary"},
					html.Strong(html.Props{Raw: map[string]any{"dir": "auto"}}, ui.Text(node.Label)),
					html.Span(html.Props{Class: "workflow-viewer-outline-type"}, ui.Text(displayWorkflowToken(props, "step", node.StepType))),
					html.Span(html.Props{Class: "workflow-viewer-outline-status"}, ui.Text(workflowNodeStatus(props, node))),
				),
				workflowViewerNodeFlags(props, node),
				html.Ul(html.Props{Class: "workflow-viewer-outline-routes"}, routes...),
			))
		}
		groups = append(groups, html.Section(html.Props{Class: "workflow-viewer-outline-stage", Data: map[string]string{"depth": strconv.Itoa(depth)}},
			html.H4(html.Props{}, ui.Text(props.Text("workflow_viewer.stage", map[string]string{"number": strconv.Itoa(depth + 1)}))),
			html.Ol(html.Props{}, items...),
		))
	}
	return html.Div(html.Props{Class: "workflow-viewer-outline-stages"}, groups...)
}

func workflowViewerNodeFlags(props WorkflowViewerProps, node workflowview.Node) ui.Node {
	flags := []ui.Node{}
	if node.Start {
		flags = append(flags, html.Span(html.Props{Class: "workflow-viewer-flag"}, ui.Text(props.Text("workflow_viewer.start"))))
	}
	if node.Terminal {
		flags = append(flags, html.Span(html.Props{Class: "workflow-viewer-flag"}, ui.Text(props.Text("workflow_viewer.terminal"))))
	}
	if node.Current {
		flags = append(flags, html.Span(html.Props{Class: "workflow-viewer-flag current"}, ui.Text(props.Text("workflow_viewer.current"))))
	}
	if len(flags) == 0 {
		return ui.Fragment()
	}
	return html.Div(html.Props{Class: "workflow-viewer-flags"}, flags...)
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
		declareGlobal(".workflow-viewer", gwccss.Display.Grid, gwccss.Gap(gwccss.VarLength("hcm-space-4")), gwccss.Raw("padding", "clamp(18px,2.2vw,28px)"))
		declareGlobal(".workflow-viewer-header", gwccss.Display.Flex, gwccss.Raw("align-items", "flex-start"), gwccss.Raw("justify-content", "space-between"), gwccss.Gap(gwccss.VarLength("hcm-space-4")))
		declareGlobal(".workflow-viewer-heading", gwccss.Raw("max-inline-size", "var(--hcm-measure-readable)"))
		declareGlobal(".workflow-viewer-heading h2,.workflow-viewer-graph h3,.workflow-viewer-outline h3,.workflow-viewer-node h4,.workflow-viewer-outline-stage h4", gwccss.Margin(gwccss.Zero))
		declareGlobal(".workflow-viewer-description,.workflow-viewer-outline-heading p,.workflow-viewer-notice p", gwccss.Raw("margin", "6px 0 0"), gwccss.TextColor(gwccss.Var("muted")))
		declareGlobal(".workflow-viewer-metadata", gwccss.Display.Flex, gwccss.Raw("flex-wrap", "wrap"), gwccss.Gap(gwccss.Px(8)), gwccss.Raw("justify-content", "flex-end"))
		declareGlobal(".workflow-viewer-badge", gwccss.Display.InlineFlex, gwccss.Raw("align-items", "center"), gwccss.Gap(gwccss.Px(6)), gwccss.Raw("padding", "6px 10px"), gwccss.Border(gwccss.Px(1), gwccss.Var("line")), gwccss.Rounded(gwccss.VarLength("hcm-radius-status")), gwccss.Bg(gwccss.Var("surface-subtle")), gwccss.Raw("font-size", "var(--hcm-font-size-small)"))
		declareGlobal(".workflow-viewer-badge-label", gwccss.TextColor(gwccss.Var("muted")))
		declareGlobal(".workflow-viewer-layout", gwccss.Display.Grid, gwccss.Raw("grid-template-columns", "minmax(0,2fr) minmax(280px,0.85fr)"), gwccss.Gap(gwccss.VarLength("hcm-space-4")), gwccss.Raw("align-items", "start"))
		declareGlobal(".workflow-viewer-graph,.workflow-viewer-outline", gwccss.Display.Grid, gwccss.Gap(gwccss.VarLength("hcm-space-3")), gwccss.Raw("min-inline-size", "0"))
		declareGlobal(".workflow-viewer-graph-scroll", gwccss.Raw("overflow-x", "auto"), gwccss.Raw("overflow-y", "hidden"), gwccss.Raw("overscroll-behavior-inline", "contain"), gwccss.Raw("scrollbar-gutter", "stable"), gwccss.Border(gwccss.Px(1), gwccss.Var("line")), gwccss.Rounded(gwccss.VarLength("hcm-radius-surface")), gwccss.Bg(gwccss.Var("canvas")), gwccss.Padding(gwccss.VarLength("hcm-space-3")))
		declareGlobal(".workflow-viewer-graph-flow", gwccss.Display.Grid, gwccss.Raw("grid-auto-flow", "column"), gwccss.Raw("grid-auto-columns", "minmax(220px,1fr)"), gwccss.Gap(gwccss.Px(28)), gwccss.Raw("min-inline-size", "max-content"))
		declareGlobal(".workflow-viewer-column", gwccss.Display.Grid, gwccss.Raw("grid-template-rows", "auto 1fr"), gwccss.Gap(gwccss.Px(10)), gwccss.Raw("position", "relative"))
		declareGlobal(".workflow-viewer-column:not(:last-child)::after", gwccss.Raw("content", "\"\""), gwccss.Position.Absolute, gwccss.Raw("inset-inline-end", "-18px"), gwccss.Raw("inset-block", "36px 8px"), gwccss.Raw("border-inline-end", "1px dashed var(--line)"))
		declareGlobal(".workflow-viewer-stage", gwccss.Margin(gwccss.Zero), gwccss.TextColor(gwccss.Var("muted")), gwccss.Raw("font", "var(--hcm-type-label)"), gwccss.Raw("text-transform", "uppercase"), gwccss.Raw("letter-spacing", "var(--hcm-tracking-caps)"))
		declareGlobal(".workflow-viewer-column-nodes", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(12)), gwccss.Raw("align-content", "start"))
		declareGlobal(".workflow-viewer-node", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(9)), gwccss.Padding(gwccss.Px(14)), gwccss.Border(gwccss.Px(1), gwccss.Var("line")), gwccss.Rounded(gwccss.VarLength("hcm-radius-control")), gwccss.Bg(gwccss.Var("surface")), gwccss.Raw("box-shadow", "var(--hcm-shadow-resting)"), gwccss.Raw("border-inline-start-width", "4px"))
		declareGlobal(".workflow-viewer-node[data-state=waiting],.workflow-viewer-outline-node[data-state=waiting]", gwccss.Raw("border-inline-start-color", "var(--warning)"))
		declareGlobal(".workflow-viewer-node[data-state=running],.workflow-viewer-node[data-state=current],.workflow-viewer-outline-node[data-state=running],.workflow-viewer-outline-node[data-state=current]", gwccss.Raw("border-inline-start-color", "var(--accent)"))
		declareGlobal(".workflow-viewer-node[data-state=succeeded],.workflow-viewer-outline-node[data-state=succeeded]", gwccss.Raw("border-inline-start-color", "var(--success)"))
		declareGlobal(".workflow-viewer-node[data-state=failed],.workflow-viewer-node[data-state=cancelled],.workflow-viewer-outline-node[data-state=failed],.workflow-viewer-outline-node[data-state=cancelled]", gwccss.Raw("border-inline-start-color", "var(--danger)"))
		declareGlobal(".workflow-viewer-node-topline,.workflow-viewer-outline-summary", gwccss.Display.Flex, gwccss.Raw("align-items", "center"), gwccss.Raw("justify-content", "space-between"), gwccss.Gap(gwccss.Px(8)))
		declareGlobal(".workflow-viewer-node-type,.workflow-viewer-outline-type", gwccss.Raw("font", "var(--hcm-type-code)"), gwccss.TextColor(gwccss.Var("muted")))
		declareGlobal(".workflow-viewer-node-status,.workflow-viewer-outline-status", gwccss.Raw("font", "var(--hcm-type-label)"), gwccss.TextColor(gwccss.Var("ink")))
		declareGlobal(".workflow-viewer-flags", gwccss.Display.Flex, gwccss.Raw("flex-wrap", "wrap"), gwccss.Gap(gwccss.Px(6)))
		declareGlobal(".workflow-viewer-flag", gwccss.Raw("padding", "3px 7px"), gwccss.Rounded(gwccss.VarLength("hcm-radius-status")), gwccss.Bg(gwccss.Var("soft")), gwccss.Raw("font", "var(--hcm-type-label)"))
		declareGlobal(".workflow-viewer-flag.current", gwccss.Bg(gwccss.Var("warning-bg")), gwccss.TextColor(gwccss.Var("ink")))
		declareGlobal(".workflow-viewer-routes,.workflow-viewer-outline-routes,.workflow-viewer-outline-stages ol,.workflow-viewer-evidence", gwccss.Margin(gwccss.Zero), gwccss.Padding(gwccss.Zero), gwccss.Raw("list-style", "none"))
		declareGlobal(".workflow-viewer-routes", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(6)))
		declareGlobal(".workflow-viewer-routes li", gwccss.Display.Grid, gwccss.Raw("grid-template-columns", "minmax(0,auto) 14px minmax(0,1fr)"), gwccss.Raw("align-items", "center"), gwccss.Gap(gwccss.Px(6)), gwccss.Raw("font-size", "var(--hcm-font-size-small)"))
		declareGlobal(".workflow-viewer-route-key", gwccss.TextColor(gwccss.Var("muted")), gwccss.Raw("font", "var(--hcm-type-code)"))
		declareGlobal(".workflow-viewer-route-arrow", gwccss.W(gwccss.Px(14)), gwccss.H(gwccss.Px(14)), gwccss.TextColor(gwccss.Var("muted")))
		declareGlobal(".workflow-viewer-outline", gwccss.Border(gwccss.Px(1), gwccss.Var("line")), gwccss.Rounded(gwccss.VarLength("hcm-radius-surface")), gwccss.Padding(gwccss.VarLength("hcm-space-3")))
		declareGlobal(".workflow-viewer-outline-stages", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(18)))
		declareGlobal(".workflow-viewer-outline-stage", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(8)))
		declareGlobal(".workflow-viewer-outline-stage>ol", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(8)))
		declareGlobal(".workflow-viewer-outline-node", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(7)), gwccss.Raw("padding", "10px 12px"), gwccss.Raw("border-inline-start", "3px solid var(--line)"), gwccss.Bg(gwccss.Var("surface-subtle")), gwccss.Rounded(gwccss.VarLength("hcm-radius-control")))
		declareGlobal(".workflow-viewer-outline-summary", gwccss.Raw("display", "grid"), gwccss.Raw("grid-template-columns", "minmax(0,1fr) auto"))
		declareGlobal(".workflow-viewer-outline-status", gwccss.Raw("grid-column", "1 / -1"))
		declareGlobal(".workflow-viewer-outline-routes", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(4)), gwccss.Raw("font-size", "var(--hcm-font-size-small)"), gwccss.TextColor(gwccss.Var("muted")))
		declareGlobal(".workflow-viewer-notice", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(6)), gwccss.Padding(gwccss.Px(14)), gwccss.Border(gwccss.Px(1), gwccss.Var("warning")), gwccss.Rounded(gwccss.VarLength("hcm-radius-control")), gwccss.Bg(gwccss.Var("warning-bg")))
		declareGlobal(".workflow-viewer-evidence", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(4)), gwccss.Raw("font", "var(--hcm-type-code)"))
		declareGlobal(".workflow-viewer-evidence li::before", gwccss.Raw("content", "\"– \""))
		declareGlobal(".workflow-viewer-route-target,.workflow-viewer-outline-summary strong", gwccss.Raw("min-inline-size", "0"), gwccss.Raw("overflow-wrap", "anywhere"))
		declareGlobal(".workflow-viewer-layout", mediaRule(gwccss.MaxW(900), gwccss.Raw("grid-template-columns", "1fr")))
		declareGlobal(".workflow-viewer-graph", mediaRule(gwccss.MaxW(640), gwccss.Display.None))
		declareGlobal(".workflow-viewer-header", mediaRule(gwccss.MaxW(640), gwccss.Raw("flex-direction", "column")))
		declareGlobal(".workflow-viewer-metadata", mediaRule(gwccss.MaxW(640), gwccss.Raw("justify-content", "flex-start")))
		declareGlobal(".workflow-viewer", mediaRule(gwccss.MaxW(360), gwccss.Padding(gwccss.Px(14))))
	})
}
