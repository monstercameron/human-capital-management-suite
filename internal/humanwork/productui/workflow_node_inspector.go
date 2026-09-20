package productui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type WorkflowNodeInspectorProps struct {
	I18nProps
	Draft          WorkflowDraftView
	Palette        []WorkflowPaletteItem
	SelectedNodeID string
	OnSelect       func(string)
	OnUpdate       func(WorkflowNodeParameterChange)
	OnSetOutcome   func(WorkflowOutcomeChange)
	OnBindInput    func(WorkflowInputBindingChange)
	OnOverlay      func(WorkflowTemplateOverlayChange)
}

func WorkflowNodeInspector(props WorkflowNodeInspectorProps) ui.Node {
	if len(props.Draft.Nodes) == 0 {
		return nil
	}
	initial := props.Draft.StartNodeID
	for _, candidate := range props.Draft.Nodes {
		if candidate.ID == strings.TrimSpace(props.SelectedNodeID) {
			initial = candidate.ID
			break
		}
	}
	if initial == "" {
		initial = props.Draft.Nodes[0].ID
	}
	selectedID := ui.UseState(initial)
	node := props.Draft.Nodes[0]
	for _, candidate := range props.Draft.Nodes {
		if candidate.ID == selectedID.Get() {
			node = candidate
			break
		}
	}
	values := make(map[string]string, len(node.Parameters))
	for _, parameter := range node.Parameters {
		values[parameter.ID] = parameter.Value
	}

	nodeOptions := make([]ui.Node, 0, len(props.Draft.Nodes))
	for _, candidate := range props.Draft.Nodes {
		label := candidate.Label
		if strings.TrimSpace(label) == "" {
			label = DisplayLabel(candidate.ID)
		}
		nodeOptions = append(nodeOptions, html.Option(html.Props{Value: candidate.ID, Selected: candidate.ID == node.ID}, ui.Text(label)))
	}
	selector := html.Props{ID: "workflow-inspector-node", Name: "node_id"}
	selector.OnChange = ui.UseEvent(func(event ui.InputEvent) {
		value := event.GetValue()
		selectedID.Set(value)
		if props.OnSelect != nil {
			props.OnSelect(value)
		}
	})

	fields := make([]ui.Node, 0, len(node.Parameters))
	for _, parameter := range node.Parameters {
		parameter := parameter
		control := workflowParameterControl(parameter, func(value string) { values[parameter.ID] = value })
		fields = append(fields, ui.CreateElement(LabeledControl, LabeledControlProps{
			For: "workflow-parameter-" + parameter.ID, Label: parameter.Label, Control: control,
		}))
	}
	formProps := html.Props{Class: "workflow-node-parameter-form"}
	if props.OnUpdate != nil {
		formProps.OnSubmit = ui.UseEvent(func(event ui.FormEvent) {
			event.PreventDefault()
			copyValues := make(map[string]string, len(values))
			for key, value := range values {
				copyValues[key] = value
			}
			props.OnUpdate(WorkflowNodeParameterChange{NodeID: node.ID, Values: copyValues})
		})
	}
	save := html.Props{Class: "button primary compact", Type: "submit", Disabled: props.OnUpdate == nil || len(fields) == 0}
	parameterForm := html.Form(formProps, append(fields, html.Button(save, ui.Text(props.Text("workflow_inspector.save"))))...)

	lock := ui.Node(nil)
	if node.Locked {
		lock = html.Div(html.Props{Class: "workflow-node-lock", Raw: map[string]any{"role": "note"}},
			html.Span(html.Props{Aria: map[string]string{"hidden": "true"}}, productIcon("lock", "")),
			html.Span(html.Props{}, html.Strong(html.Props{}, ui.Text(props.Text("workflow_inspector.locked"))), ui.Text(" "+props.Text("workflow_inspector.locked_detail", map[string]string{"kind": displayWorkflowToken(WorkflowViewerProps{I18nProps: props.I18nProps}, "lock", node.LockKind)}))),
		)
	}

	overlay := ui.Node(nil)
	if props.Draft.TemplateID != "" {
		overlay = ui.CreateElement(WorkflowOverlayControls, WorkflowOverlayControlsProps{
			I18nProps: props.I18nProps,
			Node:      node,
			Palette:   props.Palette,
			OnOverlay: props.OnOverlay,
		})
	}
	return html.Section(html.Props{Class: "workflow-node-inspector surface-inset", Aria: map[string]string{"labelledby": "workflow-node-inspector-title"}},
		html.Div(html.Props{Class: "workflow-node-inspector-heading"},
			html.Div(html.Props{}, html.P(html.Props{Class: "eyebrow"}, ui.Text(props.Text("workflow_inspector.eyebrow"))), html.H4(html.Props{ID: "workflow-node-inspector-title"}, ui.Text(props.Text("workflow_inspector.title")))),
			ui.CreateElement(LabeledControl, LabeledControlProps{For: selector.ID, Label: props.Text("workflow_inspector.node"), Control: html.Select(selector, nodeOptions...)}),
		),
		lock,
		html.Div(html.Props{Class: "workflow-node-inspector-grid"},
			html.Section(html.Props{Class: "workflow-inspector-panel", Aria: map[string]string{"labelledby": "workflow-parameters-title"}}, html.H5(html.Props{ID: "workflow-parameters-title"}, ui.Text(props.Text("workflow_inspector.parameters"))), parameterForm),
			workflowOutcomeEditor(props, node),
			workflowBindingEditor(props, node),
			overlay,
		),
		workflowOverlayHistory(props),
	)
}

func workflowOutcomeEditor(props WorkflowNodeInspectorProps, node WorkflowDraftNode) ui.Node {
	rows := make([]ui.Node, 0, len(node.Outcomes))
	for index, outcome := range node.Outcomes {
		outcome := outcome
		selected := ""
		if len(outcome.TargetNodeIDs) > 0 {
			selected = outcome.TargetNodeIDs[0]
		}
		id := fmt.Sprintf("workflow-outcome-%s-%d", workflowControlID(node.ID), index)
		options := []ui.Node{html.Option(html.Props{Value: "", Selected: selected == ""}, ui.Text(props.Text("workflow_inspector.choose_target")))}
		for _, candidate := range props.Draft.Nodes {
			label := strings.TrimSpace(candidate.Label)
			if label == "" {
				label = DisplayLabel(candidate.ID)
			}
			options = append(options, html.Option(html.Props{Value: candidate.ID, Selected: candidate.ID == selected}, ui.Text(label)))
		}
		selectProps := html.Props{ID: id, Name: "target_node", Required: true}
		selectProps.OnChange = ui.UseEvent(func(event ui.InputEvent) { selected = event.GetValue() })
		formProps := html.Props{Class: "workflow-link-editor-row"}
		if props.OnSetOutcome != nil {
			formProps.OnSubmit = ui.UseEvent(func(event ui.FormEvent) {
				event.PreventDefault()
				if strings.TrimSpace(selected) != "" {
					props.OnSetOutcome(WorkflowOutcomeChange{FromNodeID: node.ID, RouteKey: outcome.RouteKey, ToNodeID: selected})
				}
			})
		}
		current := props.Text("workflow_inspector.not_connected")
		if len(outcome.TargetNodeIDs) > 0 {
			labels := make([]string, 0, len(outcome.TargetNodeIDs))
			for _, target := range outcome.TargetNodeIDs {
				labels = append(labels, DisplayLabel(target))
			}
			current = strings.Join(labels, ", ")
		}
		rows = append(rows, html.Form(formProps,
			html.Div(html.Props{Class: "workflow-link-editor-copy"},
				html.Strong(html.Props{}, ui.Text(DisplayLabel(outcome.RouteKey))),
				html.Span(html.Props{Class: "muted"}, ui.Text(props.Text("workflow_inspector.current_target", map[string]string{"target": current}))),
			),
			ui.CreateElement(LabeledControl, LabeledControlProps{For: id, Label: props.Text("workflow_inspector.target_step"), Control: html.Select(selectProps, options...)}),
			html.Button(html.Props{Class: "button secondary compact", Type: "submit", Disabled: props.OnSetOutcome == nil}, ui.Text(props.Text("workflow_inspector.connect"))),
		))
	}
	if len(rows) == 0 {
		rows = append(rows, html.P(html.Props{Class: "muted workflow-link-editor-empty"}, ui.Text(props.Text("workflow_inspector.no_outcomes"))))
	}
	return html.Section(html.Props{Class: "workflow-inspector-panel workflow-link-editor-panel", Aria: map[string]string{"labelledby": "workflow-outcomes-title"}},
		html.H5(html.Props{ID: "workflow-outcomes-title"}, ui.Text(props.Text("workflow_inspector.outcomes"))),
		html.P(html.Props{Class: "muted workflow-inspector-help"}, ui.Text(props.Text("workflow_inspector.outcomes_help"))),
		html.Div(html.Props{Class: "workflow-link-editor"}, rows...),
	)
}

func workflowBindingEditor(props WorkflowNodeInspectorProps, node WorkflowDraftNode) ui.Node {
	rows := make([]ui.Node, 0, len(node.Bindings))
	for index, binding := range node.Bindings {
		binding := binding
		selectedNode, selectedPath := binding.SourceNodeID, binding.SourcePath
		id := fmt.Sprintf("workflow-binding-%s-%d", workflowControlID(node.ID), index)
		options := make([]ui.Node, 0, len(binding.Candidates)+1)
		if len(binding.Candidates) == 0 {
			options = append(options, html.Option(html.Props{Value: "", Selected: true}, ui.Text(props.Text("workflow_inspector.no_compatible_outputs"))))
		} else {
			for _, candidate := range binding.Candidates {
				value := candidate.SourceNodeID + "\x1f" + candidate.SourcePath
				selected := candidate.SourceNodeID == selectedNode && candidate.SourcePath == selectedPath
				options = append(options, html.Option(html.Props{Value: value, Selected: selected}, ui.Text(DisplayLabel(candidate.SourceNodeID)+" · "+DisplayLabel(candidate.SourcePath)+" ("+candidate.ValueType+")")))
				if selectedNode == "" && len(options) == 1 {
					selectedNode, selectedPath = candidate.SourceNodeID, candidate.SourcePath
				}
			}
		}
		selectProps := html.Props{ID: id, Name: "source_output", Disabled: len(binding.Candidates) == 0}
		selectProps.OnChange = ui.UseEvent(func(event ui.InputEvent) {
			parts := strings.SplitN(event.GetValue(), "\x1f", 2)
			if len(parts) == 2 {
				selectedNode, selectedPath = parts[0], parts[1]
			}
		})
		formProps := html.Props{Class: "workflow-link-editor-row"}
		if props.OnBindInput != nil {
			formProps.OnSubmit = ui.UseEvent(func(event ui.FormEvent) {
				event.PreventDefault()
				if selectedNode != "" && selectedPath != "" {
					props.OnBindInput(WorkflowInputBindingChange{TargetNodeID: node.ID, TargetPath: binding.TargetPath, SourceNodeID: selectedNode, SourcePath: selectedPath})
				}
			})
		}
		current := props.Text("workflow_inspector.not_bound")
		if binding.SourceNodeID != "" && binding.SourcePath != "" {
			current = DisplayLabel(binding.SourceNodeID) + " · " + DisplayLabel(binding.SourcePath)
		}
		rows = append(rows, html.Form(formProps,
			html.Div(html.Props{Class: "workflow-link-editor-copy"},
				html.Strong(html.Props{}, ui.Text(DisplayLabel(binding.TargetPath))),
				html.Span(html.Props{Class: "status-chip", Data: map[string]string{"tone": "neutral"}}, ui.Text(binding.TargetType)),
				html.Span(html.Props{Class: "muted"}, ui.Text(props.Text("workflow_inspector.current_source", map[string]string{"source": current}))),
			),
			ui.CreateElement(LabeledControl, LabeledControlProps{For: id, Label: props.Text("workflow_inspector.source_output"), Control: html.Select(selectProps, options...)}),
			html.Button(html.Props{Class: "button secondary compact", Type: "submit", Disabled: props.OnBindInput == nil || len(binding.Candidates) == 0}, ui.Text(props.Text("workflow_inspector.bind"))),
		))
	}
	if len(rows) == 0 {
		rows = append(rows, html.P(html.Props{Class: "muted workflow-link-editor-empty"}, ui.Text(props.Text("workflow_inspector.no_inputs"))))
	}
	return html.Section(html.Props{Class: "workflow-inspector-panel workflow-link-editor-panel", Aria: map[string]string{"labelledby": "workflow-bindings-title"}},
		html.H5(html.Props{ID: "workflow-bindings-title"}, ui.Text(props.Text("workflow_inspector.bindings"))),
		html.P(html.Props{Class: "muted workflow-inspector-help"}, ui.Text(props.Text("workflow_inspector.bindings_help"))),
		html.Div(html.Props{Class: "workflow-link-editor"}, rows...),
	)
}

func workflowControlID(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, value)
}

func workflowParameterControl(parameter WorkflowNodeParameter, onInput func(string)) ui.Node {
	id := "workflow-parameter-" + parameter.ID
	if strings.EqualFold(parameter.Kind, "ENUM") {
		options := make([]ui.Node, 0, len(parameter.Options))
		for _, option := range parameter.Options {
			options = append(options, html.Option(html.Props{Value: option, Selected: option == parameter.Value}, ui.Text(DisplayLabel(option))))
		}
		props := html.Props{ID: id, Name: parameter.ID, Required: parameter.Required}
		props.OnChange = ui.UseEvent(func(event ui.InputEvent) { onInput(event.GetValue()) })
		return html.Select(props, options...)
	}
	typeName := "text"
	if strings.EqualFold(parameter.Kind, "INTEGER") {
		typeName = "number"
	}
	props := html.Props{ID: id, Name: parameter.ID, Type: typeName, Value: parameter.Value, Required: parameter.Required, AutoComplete: "off"}
	if typeName == "number" {
		props.Min, props.Max = fmt.Sprint(parameter.Minimum), fmt.Sprint(parameter.Maximum)
	}
	props.OnInput = ui.UseEvent(func(event ui.InputEvent) { onInput(event.GetValue()) })
	return html.Input(props)
}

type WorkflowOverlayControlsProps struct {
	I18nProps
	Node      WorkflowDraftNode
	Palette   []WorkflowPaletteItem
	OnOverlay func(WorkflowTemplateOverlayChange)
}

// WorkflowOverlayControls is a component, rather than an inline helper, so
// its reason and replacement choices survive the rerenders triggered while an
// author types. That also lets the buttons stay unavailable until a complete,
// auditable overlay command can be sent.
func WorkflowOverlayControls(props WorkflowOverlayControlsProps) ui.Node {
	blocks := make([]WorkflowPaletteItem, 0)
	for _, item := range props.Palette {
		if strings.EqualFold(item.Kind, "BLOCK") {
			blocks = append(blocks, item)
		}
	}
	sort.SliceStable(blocks, func(i, j int) bool { return blocks[i].Name < blocks[j].Name })
	initialReplacement := ""
	options := make([]ui.Node, 0, len(blocks))
	for index, item := range blocks {
		if index == 0 {
			initialReplacement = fmt.Sprintf("%s@%d", item.ID, item.Version)
		}
		options = append(options, html.Option(html.Props{Value: fmt.Sprintf("%s@%d", item.ID, item.Version)}, ui.Text(item.Name)))
	}
	reason := ui.UseState("")
	replacementValue := ui.UseState(initialReplacement)
	replacement := html.Props{ID: "workflow-overlay-replacement", Name: "replacement", Value: replacementValue.Get()}
	replacement.OnChange = ui.UseEvent(func(event ui.InputEvent) { replacementValue.Set(event.GetValue()) })
	reasonProps := html.Props{
		ID: "workflow-overlay-reason", Name: "reason", Type: "text", Value: reason.Get(),
		Required: true, MaxLength: 240, AutoComplete: "off",
		Aria: map[string]string{"describedby": "workflow-overlay-reason-help"},
	}
	reasonProps.OnInput = ui.UseEvent(func(event ui.InputEvent) { reason.Set(event.GetValue()) })
	disabled := props.OnOverlay == nil || props.Node.Locked || strings.TrimSpace(reason.Get()) == ""
	omit := html.Props{Class: "button secondary compact", Type: "button", Disabled: disabled}
	replace := html.Props{Class: "button secondary compact", Type: "button", Disabled: disabled || len(options) == 0}
	if props.OnOverlay != nil {
		omit.OnClick = ui.UseEvent(func(ui.MouseEvent) {
			props.OnOverlay(WorkflowTemplateOverlayChange{Operation: "OMIT", TargetNodeID: props.Node.ID, Reason: strings.TrimSpace(reason.Get())})
		})
		replace.OnClick = ui.UseEvent(func(ui.MouseEvent) {
			parts := strings.Split(replacementValue.Get(), "@")
			if len(parts) != 2 {
				return
			}
			var replacementVersion uint32
			if _, err := fmt.Sscan(parts[1], &replacementVersion); err != nil || replacementVersion == 0 {
				return
			}
			props.OnOverlay(WorkflowTemplateOverlayChange{Operation: "REPLACE", TargetNodeID: props.Node.ID, EntryID: parts[0], EntryVersion: replacementVersion, Reason: strings.TrimSpace(reason.Get())})
		})
	}
	return html.Section(html.Props{Class: "workflow-inspector-panel", Aria: map[string]string{"labelledby": "workflow-overlay-title"}},
		html.H5(html.Props{ID: "workflow-overlay-title"}, ui.Text(props.Text("workflow_inspector.overlay"))),
		html.P(html.Props{ID: "workflow-overlay-reason-help", Class: "muted"}, ui.Text(props.Text("workflow_inspector.overlay_help"))),
		ui.CreateElement(LabeledControl, LabeledControlProps{For: reasonProps.ID, Label: props.Text("workflow_inspector.reason"), Control: html.Input(reasonProps)}),
		ui.CreateElement(LabeledControl, LabeledControlProps{For: replacement.ID, Label: props.Text("workflow_inspector.replacement"), Control: html.Select(replacement, options...)}),
		html.Div(html.Props{Class: "workflow-overlay-actions"}, html.Button(omit, ui.Text(props.Text("workflow_inspector.omit"))), html.Button(replace, ui.Text(props.Text("workflow_inspector.replace")))),
	)
}

func workflowOverlayHistory(props WorkflowNodeInspectorProps) ui.Node {
	if len(props.Draft.Overlays) == 0 {
		return nil
	}
	items := make([]ui.Node, 0, len(props.Draft.Overlays))
	for _, overlay := range props.Draft.Overlays {
		summary := overlay.Operation
		if overlay.TargetNodeID != "" {
			summary += " · " + DisplayLabel(overlay.TargetNodeID)
		}
		if overlay.EntryID != "" {
			summary += " · " + DisplayLabel(overlay.EntryID)
		}
		items = append(items, html.Li(html.Props{}, html.Strong(html.Props{}, ui.Text(summary)), html.Span(html.Props{Class: "muted"}, ui.Text(overlay.Reason))))
	}
	return html.Details(html.Props{Class: "workflow-overlay-history"}, html.Summary(html.Props{}, ui.Text(props.Text("workflow_inspector.history", map[string]string{"count": fmt.Sprint(len(items))}))), html.Ol(html.Props{}, items...))
}
