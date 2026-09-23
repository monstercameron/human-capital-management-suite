package productui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// workflowNoTarget is the value of the "not connected" choice. An option
// whose value is empty falls back to its visible text in the browser, which
// is how the old picker came to send its own placeholder as a step id.
const workflowNoTarget = "__none__"

type WorkflowStepInspectorProps struct {
	I18nProps
	Draft          WorkflowDraftView
	Node           WorkflowDraftNode
	Palette        []WorkflowPaletteItem
	OnlyStep       bool
	Loose          bool
	OnUpdateNode   func(WorkflowNodeParameterChange)
	OnSetOutcome   func(WorkflowOutcomeChange)
	OnClearOutcome func(WorkflowOutcomeChange)
	// OnSetOutcomes connects several results of one step in a single action.
	OnSetOutcomes func([]WorkflowOutcomeChange)
	OnBindInput   func(WorkflowInputBindingChange)
	OnRemoveNode  func(string)
	OnOverlay     func(WorkflowTemplateOverlayChange)
}

// WorkflowStepInspector edits exactly the step it is given. The editor mounts
// it under a key derived from that step, so choosing another step replaces the
// component and every field in it; no field state can outlive its subject.
//
// Each control saves itself when its value is committed. There is one pending
// edit at a time, which is what lets the draft reload after a save without
// discarding anything the author has typed elsewhere.
func WorkflowStepInspector(props WorkflowStepInspectorProps) ui.Node {
	node := props.Node
	tokens := WorkflowViewerProps{I18nProps: props.I18nProps}
	removeClick := ui.UseEvent(func(ui.MouseEvent) {
		if props.OnRemoveNode != nil {
			props.OnRemoveNode(node.ID)
		}
	})

	name := workflowNodeName(props.I18nProps, node)
	// The step's name is edited where it is shown. A heading that read
	// "Snapshot Worker" above an empty field labelled "Name" asked the author
	// which of the two was the name.
	var title ui.Node = html.H3(html.Props{ID: "workflow-inspector-title", Dir: "auto"}, ui.Text(name))
	others := make([]WorkflowNodeParameter, 0, len(node.Parameters))
	for _, parameter := range node.Parameters {
		if parameter.ID == "display_name" && props.OnUpdateNode != nil {
			title = html.WithKey(ui.CreateElement(workflowStepNameField, workflowStepNameFieldProps{
				I18nProps: props.I18nProps, NodeID: node.ID, Name: name, Stored: parameter.Value, Maximum: parameter.Maximum, OnUpdate: props.OnUpdateNode,
			}), "name")
			continue
		}
		if parameter.ID != "display_name" {
			others = append(others, parameter)
		}
	}
	sections := []ui.Node{
		html.Header(html.Props{Key: "head", Class: "workflow-inspector-head"},
			html.P(html.Props{Class: "workflow-inspector-kind"}, ui.Text(displayWorkflowToken(tokens, "step", node.StepType))),
			title,
		),
	}
	if node.Locked {
		sections = append(sections, html.P(html.Props{Key: "lock", Class: "workflow-inspector-note", Raw: map[string]any{"role": "note"}},
			productIcon("privacy", "workflow-inspector-note-icon"),
			html.Span(html.Props{}, ui.Text(props.Text("workflow_editor.locked_detail", map[string]string{"kind": displayWorkflowToken(tokens, "lock", node.LockKind)}))),
		))
	}

	if sources := workflowConnectSources(props.Draft, node); props.Loose && props.OnSetOutcome != nil && len(sources) > 0 {
		sections = append(sections, html.WithKey(ui.CreateElement(workflowConnectFromField, workflowConnectFromFieldProps{
			I18nProps: props.I18nProps, Draft: props.Draft, Node: node, Sources: sources, OnSetOutcome: props.OnSetOutcome,
		}), "connect-from"))
	}

	if len(others) > 0 {
		fields := make([]ui.Node, 0, len(others))
		for _, parameter := range others {
			fields = append(fields, html.WithKey(ui.CreateElement(workflowParameterField, workflowParameterFieldProps{
				I18nProps: props.I18nProps, NodeID: node.ID, Parameter: parameter, OnUpdate: props.OnUpdateNode,
			}), "parameter-"+parameter.ID))
		}
		sections = append(sections, html.Section(html.Props{Key: "settings", Class: "workflow-inspector-section", Aria: map[string]string{"labelledby": "workflow-inspector-settings"}},
			html.H4(html.Props{ID: "workflow-inspector-settings"}, ui.Text(props.Text("workflow_editor.settings"))),
			html.Div(html.Props{Class: "workflow-inspector-fields"}, fields...),
		))
	}

	if !workflowIsExit(node) {
		// The route most work takes comes first. Listed alphabetically, the
		// one result that matters sat under three that rarely happen.
		usual, rare := make([]ui.Node, 0), make([]ui.Node, 0)
		unsetExceptions := make([]string, 0)
		for _, outcome := range node.Outcomes {
			if len(outcome.TargetNodeIDs) == 0 && workflowOutcomeIsException(outcome.RouteKey) {
				unsetExceptions = append(unsetExceptions, outcome.RouteKey)
			}
		}
		for _, outcome := range node.Outcomes {
			row := html.WithKey(ui.CreateElement(workflowOutcomeField, workflowOutcomeFieldProps{
				I18nProps: props.I18nProps, Draft: props.Draft, Node: node, Outcome: outcome,
				OnSetOutcome: props.OnSetOutcome, OnClearOutcome: props.OnClearOutcome,
			}), "outcome-"+outcome.RouteKey)
			if workflowOutcomeIsException(outcome.RouteKey) {
				rare = append(rare, row)
			} else {
				usual = append(usual, row)
			}
		}
		if len(usual) > 0 {
			sections = append(sections, html.Section(html.Props{Key: "outcomes", Class: "workflow-inspector-section", Aria: map[string]string{"labelledby": "workflow-inspector-outcomes"}},
				html.H4(html.Props{ID: "workflow-inspector-outcomes"}, ui.Text(props.Text("workflow_editor.outcomes"))),
				html.Div(html.Props{Class: "workflow-inspector-fields"}, usual...),
			))
		}
		if strings.EqualFold(node.StepType, "DECISION") {
			// A decision with choices was silent about what picks between
			// them, while an empty one explained itself: exactly backwards.
			// The note sits with the choices it is about; below the folded
			// exceptions it read as locking those instead.
			key, icon := "workflow_editor.decision_rule_note", "privacy"
			if len(usual) == 0 {
				key, icon = "workflow_editor.decision_needs_rule", "alert"
			}
			if decides := workflowDecisionBasis(props.I18nProps, node); decides != "" && len(usual) > 0 {
				sections = append(sections, html.P(html.Props{Key: "decision-basis", Class: "workflow-inspector-basis"}, ui.Text(decides)))
			}
			sections = append(sections, html.P(html.Props{Key: "decision-note", Class: "workflow-inspector-note", Raw: map[string]any{"role": "note"}},
				productIcon(icon, "workflow-inspector-note-icon"), html.Span(html.Props{}, ui.Text(props.Text(key)))))
		}
		if len(rare) > 0 {
			// Exception results that are all connected are settled business:
			// four rows reading "Cancelled: Cancelled" took the best part of
			// the pane to say nothing. They fold to one line and open on
			// request, and stay open while any of them leads nowhere.
			details := html.Props{Key: "exceptions", Class: "workflow-inspector-section workflow-inspector-exceptions"}
			if len(unsetExceptions) > 0 {
				details.Raw = map[string]any{"open": ""}
			}
			sections = append(sections, html.Details(details,
				html.Summary(html.Props{},
					productIcon("move-down", "workflow-inspector-chevron"),
					html.H4(html.Props{ID: "workflow-inspector-exceptions"}, ui.Text(props.Text("workflow_editor.exceptions"))),
					html.Span(html.Props{Class: "muted"}, ui.Text(props.Locale.Plural("workflow_editor.exceptions_summary", int64(len(rare))))),
					workflowExceptionsUnset(props.I18nProps, len(unsetExceptions)),
				),
				html.WithKey(ui.CreateElement(workflowExceptionsBulkField, workflowExceptionsBulkFieldProps{
					I18nProps: props.I18nProps, Draft: props.Draft, NodeID: node.ID, RouteKeys: unsetExceptions, OnSetOutcomes: props.OnSetOutcomes,
				}), "bulk"),
				html.Div(html.Props{Class: "workflow-inspector-fields"}, rare...),
			))
		}
		if len(usual)+len(rare) == 0 {
			sections = append(sections, html.P(html.Props{Key: "no-outcomes", Class: "muted"}, ui.Text(props.Text("workflow_inspector.no_outcomes"))))
		}
	}

	if notify := workflowNotifySection(props.I18nProps, node.StepType); notify != nil {
		sections = append(sections, notify)
	}

	if len(node.Bindings) > 0 {
		rows := make([]ui.Node, 0, len(node.Bindings))
		for _, binding := range node.Bindings {
			rows = append(rows, html.WithKey(ui.CreateElement(workflowBindingField, workflowBindingFieldProps{
				I18nProps: props.I18nProps, Draft: props.Draft, NodeID: node.ID, Binding: binding, OnBindInput: props.OnBindInput,
			}), "binding-"+binding.TargetPath))
		}
		sections = append(sections, html.Section(html.Props{Key: "bindings", Class: "workflow-inspector-section", Aria: map[string]string{"labelledby": "workflow-inspector-bindings"}},
			html.H4(html.Props{ID: "workflow-inspector-bindings"}, ui.Text(props.Text("workflow_editor.uses"))),
			html.P(html.Props{Class: "muted workflow-inspector-help"}, ui.Text(props.Text("workflow_editor.uses_help"))),
			html.Div(html.Props{Class: "workflow-inspector-fields"}, rows...),
		))
	}

	if props.Draft.TemplateID != "" && props.OnOverlay != nil && !node.Locked {
		sections = append(sections, html.WithKey(ui.CreateElement(WorkflowOverlayControls, WorkflowOverlayControlsProps{
			I18nProps: props.I18nProps, Node: node, Palette: props.Palette, OnOverlay: props.OnOverlay,
		}), "overlay"))
	}
	if history := workflowOverlayHistory(props.I18nProps, props.Draft); history != nil {
		sections = append(sections, html.WithKey(history, "overlay-history"))
	}

	// A step taken from a governed template is removed through the template
	// change above, which records why. Anything else is the author's own work
	// and is removed plainly; Undo brings it back.
	removable := props.OnRemoveNode != nil && props.Draft.TemplateID == "" && (!node.Locked || props.OnlyStep)
	if removable {
		sections = append(sections, html.Div(html.Props{Key: "remove", Class: "workflow-inspector-remove"},
			html.Button(html.Props{Class: "button ghost compact workflow-inspector-remove-button", Type: "button", OnClick: removeClick},
				productIcon("trash", "button-icon"), ui.Text(props.Text("workflow_editor.remove_step")),
			),
		))
	}
	return html.Section(html.Props{Class: "workflow-inspector", Aria: map[string]string{"labelledby": "workflow-inspector-title"}, Data: map[string]string{"node-id": node.ID}}, sections...)
}

type workflowParameterFieldProps struct {
	I18nProps
	NodeID    string
	Parameter WorkflowNodeParameter
	OnUpdate  func(WorkflowNodeParameterChange)
}

func workflowParameterField(props workflowParameterFieldProps) ui.Node {
	parameter := props.Parameter
	commit := ui.UseEvent(func(event ui.InputEvent) {
		value := strings.TrimSpace(event.GetValue())
		if props.OnUpdate == nil || value == parameter.Value || (parameter.Required && value == "") {
			return
		}
		props.OnUpdate(WorkflowNodeParameterChange{NodeID: props.NodeID, Values: map[string]string{parameter.ID: value}})
	})
	id := "workflow-parameter-" + workflowControlID(parameter.ID)
	label := props.Text("workflow_editor.parameter." + parameter.ID)
	if strings.HasPrefix(label, "⟦") {
		label = parameter.Label
	}
	var control ui.Node
	if strings.EqualFold(parameter.Kind, "ENUM") {
		options := make([]ui.Node, 0, len(parameter.Options))
		for _, option := range parameter.Options {
			options = append(options, html.Option(html.Props{Key: option, Value: option, Selected: option == parameter.Value}, ui.Text(DisplayLabel(option))))
		}
		control = html.Select(html.Props{ID: id, Name: parameter.ID, Required: parameter.Required, Disabled: props.OnUpdate == nil, OnChange: commit}, options...)
	} else {
		input := html.Props{ID: id, Name: parameter.ID, Type: "text", Value: parameter.Value, Required: parameter.Required, AutoComplete: "off", Disabled: props.OnUpdate == nil, OnChange: commit, Dir: "auto"}
		if strings.EqualFold(parameter.Kind, "INTEGER") {
			input.Type, input.Dir = "number", ""
			input.Min, input.Max = fmt.Sprint(parameter.Minimum), fmt.Sprint(parameter.Maximum)
		} else if parameter.Maximum > 0 {
			input.MaxLength = int(parameter.Maximum)
		}
		control = html.Input(input)
	}
	return ui.CreateElement(LabeledControl, LabeledControlProps{For: id, Label: label, Control: control})
}

type workflowOutcomeFieldProps struct {
	I18nProps
	Draft          WorkflowDraftView
	Node           WorkflowDraftNode
	Outcome        WorkflowDraftOutcome
	OnSetOutcome   func(WorkflowOutcomeChange)
	OnClearOutcome func(WorkflowOutcomeChange)
}

// workflowOutcomeField connects one declared result of a step. Choosing a
// target is the whole interaction; there is no second button to press.
func workflowOutcomeField(props workflowOutcomeFieldProps) ui.Node {
	node, outcome := props.Node, props.Outcome
	current := ""
	if len(outcome.TargetNodeIDs) > 0 {
		current = outcome.TargetNodeIDs[0]
	}
	change := ui.UseEvent(func(event ui.InputEvent) {
		value := strings.TrimSpace(event.GetValue())
		switch {
		case value == current:
		case value == workflowNoTarget || value == "":
			if props.OnClearOutcome != nil && current != "" {
				props.OnClearOutcome(WorkflowOutcomeChange{FromNodeID: node.ID, RouteKey: outcome.RouteKey})
			}
		case props.OnSetOutcome != nil:
			props.OnSetOutcome(WorkflowOutcomeChange{FromNodeID: node.ID, RouteKey: outcome.RouteKey, ToNodeID: value})
		}
	})

	steps, exits := make([]ui.Node, 0), make([]ui.Node, 0)
	for _, candidate := range props.Draft.Nodes {
		if candidate.ID == node.ID {
			continue
		}
		option := html.Option(html.Props{Key: candidate.ID, Value: candidate.ID, Selected: candidate.ID == current}, ui.Text(workflowNodeName(props.I18nProps, candidate)))
		if workflowIsExit(candidate) {
			exits = append(exits, option)
		} else {
			steps = append(steps, option)
		}
	}
	none := html.Props{Key: workflowNoTarget, Value: workflowNoTarget, Selected: current == ""}
	if props.OnClearOutcome == nil && current != "" {
		none.Disabled = true
	}
	options := []ui.Node{html.Option(none, ui.Text(props.Text("workflow_inspector.not_connected")))}
	if len(steps) > 0 {
		options = append(options, html.Tag("optgroup", html.Props{Key: "steps", Raw: map[string]any{"label": props.Text("workflow_editor.target_steps")}}, steps...))
	}
	if len(exits) > 0 {
		options = append(options, html.Tag("optgroup", html.Props{Key: "exits", Raw: map[string]any{"label": props.Text("workflow_editor.target_exits")}}, exits...))
	}

	id := "workflow-outcome-" + workflowControlID(node.ID) + "-" + workflowControlID(outcome.RouteKey)
	label := displayWorkflowToken(WorkflowViewerProps{I18nProps: props.I18nProps}, "outcome", outcome.RouteKey)
	class := "workflow-inspector-route"
	if current == "" {
		class += " open"
	}
	selectProps := html.Props{Key: "select", ID: id, Name: "target_node", Disabled: props.OnSetOutcome == nil, OnChange: change}
	labelChildren := []ui.Node{html.Span(html.Props{}, ui.Text(label))}
	if current == "" {
		selectProps.Aria = map[string]string{"invalid": "true"}
	}
	// An exception's flag is counted once on the folded heading. A flag per
	// row showed four warnings beside a badge that, rightly, counted one.
	if current == "" && !workflowOutcomeIsException(outcome.RouteKey) {
		labelChildren = append(labelChildren, html.Span(html.Props{Class: "workflow-inspector-unset"}, productIcon("alert", "workflow-inspector-unset-icon"), ui.Text(props.Text("workflow_editor.not_set"))))
	}
	children := []ui.Node{
		html.Label(html.Props{Key: "label", For: id}, labelChildren...),
		html.Select(selectProps, options...),
	}
	if len(outcome.TargetNodeIDs) > 1 {
		also := make([]string, 0, len(outcome.TargetNodeIDs)-1)
		for _, target := range outcome.TargetNodeIDs[1:] {
			if other, ok := workflowNodeByID(props.Draft, target); ok {
				also = append(also, workflowNodeName(props.I18nProps, other))
			}
		}
		children = append(children, html.P(html.Props{Key: "also", Class: "muted workflow-inspector-help"}, ui.Text(props.Text("workflow_editor.also_goes_to", map[string]string{"targets": strings.Join(also, ", ")}))))
	}
	return html.Div(html.Props{Class: class, Data: map[string]string{"route": outcome.RouteKey}}, children...)
}

type workflowBindingFieldProps struct {
	I18nProps
	Draft       WorkflowDraftView
	NodeID      string
	Binding     WorkflowDraftBinding
	OnBindInput func(WorkflowInputBindingChange)
}

func workflowBindingField(props workflowBindingFieldProps) ui.Node {
	binding := props.Binding
	const separator = "\x1f"
	current := ""
	if binding.SourceNodeID != "" && binding.SourcePath != "" {
		current = binding.SourceNodeID + separator + binding.SourcePath
	}
	change := ui.UseEvent(func(event ui.InputEvent) {
		value := event.GetValue()
		parts := strings.SplitN(value, separator, 2)
		if props.OnBindInput == nil || value == current || len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return
		}
		props.OnBindInput(WorkflowInputBindingChange{TargetNodeID: props.NodeID, TargetPath: binding.TargetPath, SourceNodeID: parts[0], SourcePath: parts[1]})
	})

	id := "workflow-binding-" + workflowControlID(props.NodeID) + "-" + workflowControlID(binding.TargetPath)
	label := html.Label(html.Props{Key: "label", For: id, Dir: "auto"}, ui.Text(DisplayLabel(binding.TargetPath)))

	// A value that comes from the request itself, or a fixed value, is part
	// of the definition and is not something this picker can offer back.
	if current == "" && strings.TrimSpace(binding.SourceKind) != "" {
		return html.Div(html.Props{Class: "workflow-inspector-route"},
			label,
			html.P(html.Props{Key: "fixed", ID: id, Class: "workflow-inspector-fixed"}, ui.Text(displayWorkflowToken(WorkflowViewerProps{I18nProps: props.I18nProps}, "source", binding.SourceKind))),
		)
	}

	options := make([]ui.Node, 0, len(binding.Candidates)+1)
	placeholder := "workflow_inspector.not_bound"
	if len(binding.Candidates) == 0 {
		placeholder = "workflow_inspector.no_compatible_outputs"
	}
	options = append(options, html.Option(html.Props{Key: workflowNoTarget, Value: workflowNoTarget, Selected: current == "", Disabled: true}, ui.Text(props.Text(placeholder))))
	for _, candidate := range binding.Candidates {
		value := candidate.SourceNodeID + separator + candidate.SourcePath
		source := candidate.SourceNodeID
		if other, ok := workflowNodeByID(props.Draft, candidate.SourceNodeID); ok {
			source = workflowNodeName(props.I18nProps, other)
		}
		// The label already names the input. When the offered output has the
		// same name, which is the usual case, the option is just the step it
		// comes from; "Worker Id: Worker Id from Snapshot Worker" said one
		// fact three times and ran into the select's arrow.
		text := props.Text("workflow_editor.binding_option", map[string]string{"field": bidiIsolate(DisplayLabel(candidate.SourcePath)), "step": bidiIsolate(source)})
		if strings.EqualFold(DisplayLabel(candidate.SourcePath), DisplayLabel(binding.TargetPath)) {
			text = props.Text("workflow_editor.binding_from", map[string]string{"step": bidiIsolate(source)})
		}
		options = append(options, html.Option(html.Props{Key: value, Value: value, Selected: value == current}, ui.Text(text)))
	}
	class := "workflow-inspector-route"
	if current == "" {
		class += " open"
	}
	return html.Div(html.Props{Class: class},
		label,
		html.Select(html.Props{Key: "select", ID: id, Name: "source_output", Disabled: props.OnBindInput == nil || len(binding.Candidates) == 0, OnChange: change}, options...),
	)
}

// workflowOutcomeIsException sorts a step's results into the route work
// usually takes and the routes it takes when something goes wrong. It reads
// the compiler's outcome vocabulary; an author's own decision branches are
// never exceptions.
func workflowOutcomeIsException(routeKey string) bool {
	switch strings.ToUpper(strings.TrimSpace(routeKey)) {
	case "REJECTED", "FAIL", "FAILED", "BLOCKED", "DENIED", "EXPIRED", "TIMED_OUT", "LATE", "INVALIDATED", "PARTIAL", "DEGRADED",
		"REPAIR_REQUIRED", "UNKNOWN", "AMBIGUOUS", "CANCELLED", "WITHDRAWN":
		return true
	default:
		return false
	}
}

type workflowStepNameFieldProps struct {
	I18nProps
	NodeID, Name, Stored string
	Maximum              int64
	OnUpdate             func(WorkflowNodeParameterChange)
}

func workflowStepNameField(props workflowStepNameFieldProps) ui.Node {
	commit := ui.UseEvent(func(event ui.InputEvent) {
		value := strings.TrimSpace(event.GetValue())
		if props.OnUpdate == nil || value == props.Stored || value == props.Name {
			return
		}
		props.OnUpdate(WorkflowNodeParameterChange{NodeID: props.NodeID, Values: map[string]string{"display_name": value}})
	})
	input := html.Props{ID: "workflow-inspector-name", Name: "display_name", Type: "text", Value: props.Name, Class: "workflow-inspector-name", AutoComplete: "off", Dir: "auto", OnChange: commit}
	if props.Maximum > 0 {
		input.MaxLength = int(props.Maximum)
	}
	return html.Div(html.Props{Class: "workflow-inspector-name-field"},
		html.H3(html.Props{ID: "workflow-inspector-title", Class: "sr-only", Dir: "auto"}, ui.Text(props.Name)),
		html.Label(html.Props{For: "workflow-inspector-name", Class: "sr-only"}, ui.Text(props.Text("workflow_editor.step_name_label"))),
		html.Input(input),
		productIcon("edit", "workflow-editor-name-icon"),
	)
}

type workflowConnectSource struct{ NodeID, RouteKey string }

// workflowConnectSources lists the results of other steps that lead nowhere
// yet, which are the places a step nothing leads to can be attached without
// rerouting anything that already works.
func workflowConnectSources(draft WorkflowDraftView, node WorkflowDraftNode) []workflowConnectSource {
	sources := make([]workflowConnectSource, 0)
	for _, candidate := range draft.Nodes {
		if candidate.ID == node.ID || workflowIsExit(candidate) {
			continue
		}
		for _, outcome := range candidate.Outcomes {
			if len(outcome.TargetNodeIDs) == 0 {
				sources = append(sources, workflowConnectSource{NodeID: candidate.ID, RouteKey: outcome.RouteKey})
			}
		}
	}
	return sources
}

type workflowConnectFromFieldProps struct {
	I18nProps
	Draft        WorkflowDraftView
	Node         WorkflowDraftNode
	Sources      []workflowConnectSource
	OnSetOutcome func(WorkflowOutcomeChange)
}

// workflowConnectFromField attaches a step nothing leads to. The old flow
// made the author work out which earlier step to open and which of its
// pickers to change; this asks the question from the step they are on.
func workflowConnectFromField(props workflowConnectFromFieldProps) ui.Node {
	const separator = "\x1f"
	change := ui.UseEvent(func(event ui.InputEvent) {
		parts := strings.SplitN(event.GetValue(), separator, 2)
		if props.OnSetOutcome == nil || len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return
		}
		props.OnSetOutcome(WorkflowOutcomeChange{FromNodeID: parts[0], RouteKey: parts[1], ToNodeID: props.Node.ID})
	})
	tokens := WorkflowViewerProps{I18nProps: props.I18nProps}
	options := []ui.Node{html.Option(html.Props{Key: workflowNoTarget, Value: workflowNoTarget, Selected: true, Disabled: true}, ui.Text(props.Text("workflow_editor.connect_from_choose")))}
	for _, source := range props.Sources {
		from, _ := workflowNodeByID(props.Draft, source.NodeID)
		text := props.Text("workflow_editor.connect_from_option", map[string]string{
			"step": bidiIsolate(workflowNodeName(props.I18nProps, from)), "route": bidiIsolate(displayWorkflowToken(tokens, "outcome", source.RouteKey)),
		})
		value := source.NodeID + separator + source.RouteKey
		options = append(options, html.Option(html.Props{Key: value, Value: value}, ui.Text(text)))
	}
	return html.Section(html.Props{Class: "workflow-inspector-section workflow-inspector-connect", Aria: map[string]string{"labelledby": "workflow-inspector-connect"}},
		html.H4(html.Props{ID: "workflow-inspector-connect"}, ui.Text(props.Text("workflow_editor.connect_from"))),
		html.P(html.Props{Class: "muted workflow-inspector-help"}, ui.Text(props.Text("workflow_editor.connect_from_help"))),
		html.Select(html.Props{ID: "workflow-connect-from", Name: "connect_from", OnChange: change, Aria: map[string]string{"labelledby": "workflow-inspector-connect"}}, options...),
	)
}

type workflowExceptionsBulkFieldProps struct {
	I18nProps
	Draft         WorkflowDraftView
	NodeID        string
	RouteKeys     []string
	OnSetOutcomes func([]WorkflowOutcomeChange)
}

// workflowExceptionsBulkField sends every exception result that leads nowhere
// to one exit. A new step declares four or five of them, and wiring each by
// hand was most of the work of adding a step.
func workflowExceptionsBulkField(props workflowExceptionsBulkFieldProps) ui.Node {
	change := ui.UseEvent(func(event ui.InputEvent) {
		target := strings.TrimSpace(event.GetValue())
		if props.OnSetOutcomes == nil || target == "" || target == workflowNoTarget {
			return
		}
		changes := make([]WorkflowOutcomeChange, 0, len(props.RouteKeys))
		for _, route := range props.RouteKeys {
			changes = append(changes, WorkflowOutcomeChange{FromNodeID: props.NodeID, RouteKey: route, ToNodeID: target})
		}
		props.OnSetOutcomes(changes)
	})
	if props.OnSetOutcomes == nil || len(props.RouteKeys) < 2 {
		return nil
	}
	options := []ui.Node{html.Option(html.Props{Key: workflowNoTarget, Value: workflowNoTarget, Selected: true, Disabled: true}, ui.Text(props.Text("workflow_editor.bulk_choose")))}
	for _, candidate := range props.Draft.Nodes {
		if workflowIsExit(candidate) {
			options = append(options, html.Option(html.Props{Key: candidate.ID, Value: candidate.ID}, ui.Text(workflowNodeName(props.I18nProps, candidate))))
		}
	}
	if len(options) == 1 {
		return html.P(html.Props{Class: "muted workflow-inspector-help"}, ui.Text(props.Text("workflow_editor.bulk_needs_exit")))
	}
	id := "workflow-exceptions-bulk-" + workflowControlID(props.NodeID)
	return html.Div(html.Props{Class: "workflow-inspector-bulk"},
		html.Label(html.Props{For: id}, ui.Text(props.Locale.Plural("workflow_editor.bulk_label", int64(len(props.RouteKeys))))),
		html.Select(html.Props{ID: id, Name: "bulk_exception_target", OnChange: change}, options...),
	)
}

// workflowExceptionsUnset is the one warning the folded exceptions carry.
func workflowExceptionsUnset(i18n I18nProps, unset int) ui.Node {
	if unset == 0 {
		return nil
	}
	return html.Span(html.Props{Key: "unset", Class: "workflow-inspector-unset"}, productIcon("alert", "workflow-inspector-unset-icon"),
		ui.Text(i18n.Locale.Plural("workflow_editor.exceptions_unset", int64(unset))))
}

// workflowDecisionBasis says, in one sentence, what a decision chooses between
// and which values it reads. The rule itself is not in a draft, so this names
// what the draft does know rather than inventing a condition.
func workflowDecisionBasis(i18n I18nProps, node WorkflowDraftNode) string {
	tokens := WorkflowViewerProps{I18nProps: i18n}
	choices := make([]string, 0, len(node.Outcomes))
	for _, outcome := range node.Outcomes {
		if !workflowOutcomeIsException(outcome.RouteKey) {
			choices = append(choices, bidiIsolate(displayWorkflowToken(tokens, "outcome", outcome.RouteKey)))
		}
	}
	inputs := make([]string, 0, len(node.Bindings))
	for _, binding := range node.Bindings {
		inputs = append(inputs, bidiIsolate(DisplayLabel(binding.TargetPath)))
	}
	if len(choices) == 0 {
		return ""
	}
	if len(inputs) == 0 {
		return i18n.Text("workflow_editor.decision_basis_plain", map[string]string{"choices": strings.Join(choices, ", ")})
	}
	return i18n.Text("workflow_editor.decision_basis", map[string]string{"choices": strings.Join(choices, ", "), "inputs": strings.Join(inputs, ", ")})
}
