package productui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// WorkflowEditorProps carries one open draft and the edit commands the host
// can perform. A nil command means the host cannot perform it, and the editor
// leaves the matching control out rather than rendering one that does
// nothing.
type WorkflowEditorProps struct {
	I18nProps
	Draft          WorkflowDraftView
	Palette        []WorkflowPaletteItem
	SelectedNodeID string
	// Status is the host's live region for save progress and refusals. The
	// editor places it in the bar, beside the controls it reports on.
	Status       ui.Node
	CatalogHref  string
	Navigate     func(string)
	OnSelectNode func(string)
	OnInsert     func(WorkflowPaletteItem)
	// OnInsertAfter adds a step and then points the named result at it. The
	// change's ToNodeID is left empty for the host to fill with the new step.
	OnInsertAfter  func(WorkflowPaletteItem, WorkflowOutcomeChange)
	OnRename       func(string)
	OnUpdateNode   func(WorkflowNodeParameterChange)
	OnSetOutcome   func(WorkflowOutcomeChange)
	OnClearOutcome func(WorkflowOutcomeChange)
	OnSetOutcomes  func([]WorkflowOutcomeChange)
	OnBindInput    func(WorkflowInputBindingChange)
	OnRemoveNode   func(string)
	OnOverlay      func(WorkflowTemplateOverlayChange)
	OnHistory      func(string)
}

// workflowSelection remembers which step the author picked and which
// host-supplied selection was current when they picked it. The host's value
// wins again only once it changes, which is how a step inserted by a command,
// or an address opened from a link, becomes the selection without the editor
// fighting the author's own clicks in between.
type workflowSelection struct{ picked, host string }

// WorkflowEditor is the whole authoring surface for one draft: the library on
// the start side, the path in the middle and the selected step on the end
// side. Selection is editor state. The previous surface wrote the selection
// only to the address bar, so the path, its highlight and the inspector each
// kept whatever step they had first rendered, and a save in the inspector
// could land on a step other than the highlighted one.
//
// GoWebComponents v5.0.1, the release this module pins, has no graph canvas
// package; one exists in the framework's unreleased tree. It is not used
// here on purpose as well as by necessity: a draft stores no node positions,
// so there is nothing for free dragging, pan or zoom to edit, and a column
// the page scrolls natively stays usable by keyboard and at 320px without a
// second, parallel outline view.
func WorkflowEditor(props WorkflowEditorProps) ui.Node {
	draft := props.Draft
	host := strings.TrimSpace(props.SelectedNodeID)
	selection := ui.UseState(workflowSelection{host: host})
	selectedID := selection.Get().picked
	if selection.Get().host != host || selectedID == "" {
		selectedID = host
	}
	selectedID = workflowExistingNodeID(draft, selectedID)
	choose := func(nodeID string) {
		selection.Set(workflowSelection{picked: nodeID, host: host})
		if props.OnSelectNode != nil {
			props.OnSelectNode(nodeID)
		}
	}

	path := buildWorkflowPath(draft, selectedID)
	problems := workflowPathProblems(path, func(node WorkflowDraftNode) string { return workflowNodeName(props.I18nProps, node) })
	loose := false
	for _, step := range path.Loose {
		loose = loose || step.Node.ID == selectedID
	}

	// A step added from the library continues from the selected step when that
	// step has a usual result leading nowhere. It never reroutes a result that
	// already goes somewhere; with nothing free to attach to, the new step
	// waits under "Not connected yet" with its own connect control.
	var insert func(WorkflowPaletteItem)
	if props.OnInsert != nil {
		insert = func(item WorkflowPaletteItem) {
			props.OnInsert(item)
		}
		if props.OnInsertAfter != nil {
			insert = func(item WorkflowPaletteItem) {
				if from, route, ok := workflowInsertAnchor(draft, selectedID); ok && strings.EqualFold(item.Kind, "BLOCK") {
					props.OnInsertAfter(item, WorkflowOutcomeChange{FromNodeID: from, RouteKey: route})
					return
				}
				props.OnInsert(item)
			}
		}
	}

	var inspector ui.Node
	if node, ok := workflowNodeByID(draft, selectedID); ok {
		inspector = html.WithKey(ui.CreateElement(WorkflowStepInspector, WorkflowStepInspectorProps{
			I18nProps: props.I18nProps, Draft: draft, Node: node, Palette: props.Palette,
			OnlyStep: len(draft.Nodes) == 1, Loose: loose,
			OnUpdateNode: props.OnUpdateNode, OnSetOutcome: props.OnSetOutcome, OnClearOutcome: props.OnClearOutcome, OnSetOutcomes: props.OnSetOutcomes,
			OnBindInput: props.OnBindInput, OnRemoveNode: props.OnRemoveNode, OnOverlay: props.OnOverlay,
		}), "step-"+node.ID)
	} else {
		inspector = html.Div(html.Props{Key: "step-none", Class: "workflow-inspector workflow-inspector-idle"},
			html.P(html.Props{Class: "muted"}, ui.Text(props.Text("workflow_editor.pick_step"))),
		)
	}

	announce := ""
	if node, ok := workflowNodeByID(draft, selectedID); ok && selection.Get().picked != "" {
		announce = props.Text("workflow_editor.selected_step", map[string]string{"step": bidiIsolate(workflowNodeName(props.I18nProps, node))})
	}
	library := props.Palette
	if len(draft.Nodes) > 0 {
		// A template only starts an empty workflow. Once there are steps it
		// was the tallest entry in the library and could never be used.
		library = make([]WorkflowPaletteItem, 0, len(props.Palette))
		for _, item := range props.Palette {
			if !strings.EqualFold(item.Kind, "TEMPLATE") {
				library = append(library, item)
			}
		}
	}
	return html.Div(html.Props{Class: "workflow-editor"},
		html.P(html.Props{Key: "announce", Class: "sr-only", Raw: map[string]any{"role": "status", "aria-live": "polite"}}, ui.Text(announce)),
		ui.CreateElement(workflowEditorBar, workflowEditorBarProps{
			I18nProps: props.I18nProps, Draft: draft, Problems: problems, Status: props.Status, CatalogHref: props.CatalogHref,
			Navigate: props.Navigate, OnRename: props.OnRename, OnHistory: props.OnHistory, OnSelect: choose,
		}),
		html.Div(html.Props{Class: "workflow-editor-panes"},
			ui.CreateElement(WorkflowPalette, WorkflowPaletteProps{
				I18nProps: props.I18nProps, Items: library, OnInsert: insert,
				CanInsert: func(item WorkflowPaletteItem) bool {
					if strings.EqualFold(item.Kind, "TEMPLATE") {
						return len(draft.Nodes) == 0
					}
					return props.OnInsert != nil
				},
			}),
			workflowPathCanvas(props.I18nProps, path, selectedID, choose, nil, workflowEmptyStarters(props.I18nProps, props.Palette, insert)),
			html.Aside(html.Props{ID: "workflow-step-settings", Class: "workflow-editor-inspector", Aria: map[string]string{"label": props.Text("workflow_editor.inspector_label")}},
				html.A(html.Props{Key: "to-path", Class: "workflow-editor-hop", Href: "#workflow-path"}, productIcon("move-up", ""), ui.Text(props.Text("workflow_editor.back_to_path"))),
				inspector,
			),
		),
		workflowEditorHop(props.I18nProps, draft, selectedID),
	)
}

// workflowInsertAnchor finds the result of the selected step a new step should
// continue from: its first usual result that leads nowhere.
func workflowInsertAnchor(draft WorkflowDraftView, selectedID string) (string, string, bool) {
	node, ok := workflowNodeByID(draft, selectedID)
	if !ok || workflowIsExit(node) {
		return "", "", false
	}
	for _, outcome := range node.Outcomes {
		if len(outcome.TargetNodeIDs) == 0 && !workflowOutcomeIsException(outcome.RouteKey) {
			return node.ID, outcome.RouteKey, true
		}
	}
	return "", "", false
}

// workflowEditorHop is how a narrow screen gets from the path to the selected
// step's settings, which sit below it there. It is an ordinary in-page link,
// so it works before the client starts and needs no scroll scripting; wide
// layouts, where the settings are beside the path, hide it.
func workflowEditorHop(i18n I18nProps, draft WorkflowDraftView, selectedID string) ui.Node {
	node, ok := workflowNodeByID(draft, selectedID)
	if !ok {
		return nil
	}
	return html.A(html.Props{Class: "workflow-editor-hop workflow-editor-hop-settings", Href: "#workflow-step-settings"},
		html.Span(html.Props{Dir: "auto"}, ui.Text(i18n.Text("workflow_editor.edit_step", map[string]string{"step": bidiIsolate(workflowNodeName(i18n, node))}))),
		productIcon("move-down", ""),
	)
}

func workflowExistingNodeID(draft WorkflowDraftView, preferred string) string {
	if _, ok := workflowNodeByID(draft, preferred); ok {
		return preferred
	}
	if _, ok := workflowNodeByID(draft, draft.StartNodeID); ok {
		return draft.StartNodeID
	}
	for _, node := range draft.Nodes {
		if !workflowIsExit(node) {
			return node.ID
		}
	}
	if len(draft.Nodes) > 0 {
		return draft.Nodes[0].ID
	}
	return ""
}

func workflowNodeByID(draft WorkflowDraftView, id string) (WorkflowDraftNode, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return WorkflowDraftNode{}, false
	}
	for _, node := range draft.Nodes {
		if node.ID == id {
			return node, true
		}
	}
	return WorkflowDraftNode{}, false
}

type workflowEditorBarProps struct {
	I18nProps
	Draft       WorkflowDraftView
	Problems    []workflowPathProblem
	Status      ui.Node
	CatalogHref string
	Navigate    func(string)
	OnRename    func(string)
	OnHistory   func(string)
	OnSelect    func(string)
}

// workflowEditorBar holds what is true of the whole draft: its name, whether
// it is saved, the way back through its history, and what is left to finish.
func workflowEditorBar(props workflowEditorBarProps) ui.Node {
	draft := props.Draft
	rename := ui.UseEvent(func(event ui.InputEvent) {
		if name := strings.TrimSpace(event.GetValue()); name != "" && name != draft.Name && props.OnRename != nil {
			props.OnRename(name)
		}
	})
	undoClick := ui.UseEvent(func(ui.MouseEvent) {
		if props.OnHistory != nil {
			props.OnHistory("UNDO")
		}
	})
	redoClick := ui.UseEvent(func(ui.MouseEvent) {
		if props.OnHistory != nil {
			props.OnHistory("REDO")
		}
	})

	title := ui.Node(html.H2(html.Props{ID: "workflow-draft-title", Class: "workflow-editor-name", Dir: "auto"}, ui.Text(draft.Name)))
	if props.OnRename != nil {
		title = html.Div(html.Props{Class: "workflow-editor-name-field"},
			html.H2(html.Props{ID: "workflow-draft-title", Class: "sr-only", Dir: "auto"}, ui.Text(draft.Name)),
			html.Label(html.Props{For: "workflow-draft-name", Class: "sr-only"}, ui.Text(props.Text("workflow_editor.name_label"))),
			html.Input(html.Props{
				ID: "workflow-draft-name", Name: "workflow_name", Type: "text", Value: draft.Name, Class: "workflow-editor-name",
				Required: true, MaxLength: 120, AutoComplete: "off", Dir: "auto", OnChange: rename,
			}),
			productIcon("edit", "workflow-editor-name-icon"),
		)
	}

	undo := html.Props{Class: "button ghost compact icon-button", Type: "button", Disabled: !draft.CanUndo || props.OnHistory == nil, OnClick: undoClick,
		Title: props.Text("workflow_draft.undo"), Aria: map[string]string{"label": props.Text("workflow_draft.undo")}}
	redo := html.Props{Class: "button ghost compact icon-button", Type: "button", Disabled: !draft.CanRedo || props.OnHistory == nil, OnClick: redoClick,
		Title: props.Text("workflow_draft.redo"), Aria: map[string]string{"label": props.Text("workflow_draft.redo")}}

	return html.Header(html.Props{Class: "workflow-editor-bar"},
		softwareLink(props.Navigate, html.Props{Class: "workflow-editor-back"}, props.CatalogHref,
			productIcon("collapse", "workflow-editor-back-icon"), html.Span(html.Props{}, ui.Text(props.Text("workflow_editor.back"))),
		),
		html.Div(html.Props{Class: "workflow-editor-identity"},
			title,
			html.Span(html.Props{Class: "workflow-editor-version"}, ui.Text(props.Text("workflow_editor.version", map[string]string{"version": draft.SemanticVersion}))),
			workflowEditorParity(props.I18nProps, draft),
		),
		html.Div(html.Props{Class: "workflow-editor-tools"},
			props.Status,
			html.Div(html.Props{Class: "workflow-editor-history", Raw: map[string]any{"role": "group"}, Aria: map[string]string{"label": props.Text("workflow_draft.history_controls")}},
				html.Button(undo, productIcon("undo", "")),
				html.Button(redo, productIcon("redo", "")),
			),
			workflowEditorChanges(props.I18nProps, draft),
			ui.CreateElement(workflowEditorProblems, workflowEditorProblemsProps{I18nProps: props.I18nProps, Problems: props.Problems, Empty: len(draft.Nodes) == 0, OnSelect: props.OnSelect}),
		),
	)
}

// workflowEditorParity says whether the draft still equals the released
// definition it started from. It is the one fact a reviewer needs before
// reading a diff, so it stays in the bar; an untouched new workflow has
// nothing to compare against and shows nothing.
func workflowEditorParity(i18n I18nProps, draft WorkflowDraftView) ui.Node {
	parity, tone, key := "", "", ""
	switch {
	case strings.TrimSpace(draft.TemplateDefinitionDigest) != "":
		parity, tone, key = "changed", "warning", "workflow_editor.parity_template_changed"
		if draft.MatchesTemplateDefinition {
			parity, tone, key = "exact", "positive", "workflow_editor.parity_template_exact"
		}
	case strings.TrimSpace(draft.BaseDefinitionDigest) != "":
		parity, tone, key = "changed", "warning", "workflow_editor.parity_changed"
		if draft.MatchesBaseDefinition {
			parity, tone, key = "exact", "positive", "workflow_editor.parity_exact"
		}
	default:
		return nil
	}
	return html.Span(html.Props{Class: "status-chip workflow-editor-parity", Data: map[string]string{"tone": tone, "parity": parity}}, ui.Text(i18n.Text(key)))
}

func workflowEditorChanges(i18n I18nProps, draft WorkflowDraftView) ui.Node {
	items := make([]ui.Node, 0, len(draft.Changes))
	for index, change := range draft.Changes {
		operation := displayWorkflowToken(WorkflowViewerProps{I18nProps: i18n}, "change", change.Operation)
		copy := i18n.Text("workflow_editor.change", map[string]string{
			"kind":    displayWorkflowToken(WorkflowViewerProps{I18nProps: i18n}, "change", change.Kind),
			"subject": bidiIsolate(DisplayLabel(change.SubjectID)),
			"field":   bidiIsolate(DisplayLabel(change.Field)),
		})
		items = append(items, html.Li(html.Props{Key: fmt.Sprintf("change-%d", index), Class: "workflow-editor-change"},
			html.Span(html.Props{Class: "status-chip", Data: map[string]string{"tone": workflowChangeTone(change.Operation)}}, ui.Text(operation)),
			html.Span(html.Props{Dir: "auto"}, ui.Text(strings.TrimSpace(copy))),
		))
	}
	if len(items) == 0 {
		items = append(items, html.Li(html.Props{Key: "change-none", Class: "muted"}, ui.Text(i18n.Text("workflow_draft.no_changes"))))
	}
	// A draft nobody has edited has no history worth a control; "Change 1 of
	// 1" on an empty workflow was a number about nothing.
	if draft.HistoryLength <= 1 {
		return nil
	}
	return html.Details(html.Props{Class: "workflow-editor-popover workflow-editor-changes", Data: map[string]string{"hcm-transient-popover": "workflow-history", "hcm-popover-grace-ms": transientPopoverGraceMilliseconds}, Raw: map[string]any{"name": "workflow-editor-popover"}},
		html.Summary(html.Props{Class: "button ghost compact"}, ui.Text(i18n.Text("workflow_editor.history"))),
		html.Div(html.Props{Class: "workflow-editor-popover-body"},
			html.P(html.Props{Class: "muted"}, ui.Text(i18n.Text("workflow_editor.history_position", map[string]string{"position": i18n.Locale.FormatNumber(fmt.Sprint(draft.HistoryPosition), 0), "length": i18n.Locale.FormatNumber(fmt.Sprint(draft.HistoryLength), 0)}))),
			html.Strong(html.Props{}, ui.Text(i18n.Text("workflow_draft.changes_title"))),
			html.Ul(html.Props{Class: "workflow-editor-change-list", Raw: map[string]any{"role": "list"}}, items...),
		),
	)
}

func workflowChangeTone(operation string) string {
	switch strings.ToUpper(strings.TrimSpace(operation)) {
	case "ADDED":
		return "positive"
	case "REMOVED":
		return "danger"
	default:
		return "warning"
	}
}

type workflowEditorProblemsProps struct {
	I18nProps
	Problems []workflowPathProblem
	// Empty is a draft with no steps. It has nothing unfinished and nothing
	// finished, so it shows neither a count nor a "ready" mark.
	Empty    bool
	OnSelect func(string)
}

func workflowEditorProblems(props workflowEditorProblemsProps) ui.Node {
	if props.Empty {
		return nil
	}
	if len(props.Problems) == 0 {
		return html.Span(html.Props{Class: "status-chip workflow-editor-ready", Data: map[string]string{"tone": "positive"}}, ui.Text(props.Text("workflow_editor.ready")))
	}
	items := make([]ui.Node, 0, len(props.Problems))
	for index, problem := range props.Problems {
		vars := make(map[string]string, len(problem.Vars))
		for key, value := range problem.Vars {
			if key == "route" {
				value = displayWorkflowToken(WorkflowViewerProps{I18nProps: props.I18nProps}, "outcome", value)
			}
			vars[key] = bidiIsolate(value)
		}
		text := props.Text(problem.Key, vars)
		if problem.Count > 0 {
			text = props.Locale.Plural(problem.Key, problem.Count)
			for key, value := range vars {
				text = strings.ReplaceAll(text, "{"+key+"}", value)
			}
		}
		items = append(items, html.WithKey(ui.CreateElement(workflowEditorProblemRow, workflowEditorProblemRowProps{
			Text: text, NodeID: problem.NodeID, OnSelect: props.OnSelect,
		}), fmt.Sprintf("problem-%d", index)))
	}
	return html.Details(html.Props{Class: "workflow-editor-popover workflow-editor-problems", Data: map[string]string{"hcm-transient-popover": "workflow-problems", "hcm-popover-grace-ms": transientPopoverGraceMilliseconds}, Raw: map[string]any{"name": "workflow-editor-popover"}},
		html.Summary(html.Props{Class: "button ghost compact", Data: map[string]string{"tone": "warning"}}, ui.Text(props.Locale.Plural("workflow_editor.problem_count", int64(len(props.Problems))))),
		html.Div(html.Props{Class: "workflow-editor-popover-body"},
			html.Strong(html.Props{}, ui.Text(props.Text("workflow_editor.problems_title"))),
			html.Ul(html.Props{Class: "workflow-editor-problem-list", Raw: map[string]any{"role": "list"}}, items...),
		),
	)
}

type workflowEditorProblemRowProps struct {
	Text, NodeID string
	OnSelect     func(string)
}

func workflowEditorProblemRow(props workflowEditorProblemRowProps) ui.Node {
	click := ui.UseEvent(func(ui.MouseEvent) {
		if props.OnSelect != nil && props.NodeID != "" {
			props.OnSelect(props.NodeID)
		}
	})
	if props.NodeID == "" || props.OnSelect == nil {
		return html.Li(html.Props{}, html.Span(html.Props{}, ui.Text(props.Text)))
	}
	return html.Li(html.Props{}, html.Button(html.Props{Class: "workflow-editor-problem", Type: "button", OnClick: click},
		productIcon("expand", "workflow-editor-problem-icon"), html.Span(html.Props{}, ui.Text(props.Text))))
}

// workflowPathCanvas renders the path as an ordered list. Each row is a real
// button, so the same markup is the pointer, keyboard, screen-reader and
// narrow-screen way to read and select steps.
// workflowPathState is the run state shown on a step when the path is
// overlaying a live run. A draft has no run and passes none, so no step of a
// draft ever claims to be "not started".
type workflowPathState struct {
	Key, Label, Tone string
	Current          bool
}

func workflowPathCanvas(i18n I18nProps, path workflowPath, selectedID string, onSelect func(string), states map[string]workflowPathState, starters ui.Node) ui.Node {
	if len(path.Steps)+len(path.Loose)+len(path.Exits) == 0 {
		return html.Section(html.Props{Class: "workflow-path workflow-path-empty", Aria: map[string]string{"label": i18n.Text("workflow_editor.path_label")}},
			html.Div(html.Props{Class: "workflow-path-empty-copy"},
				html.H3(html.Props{}, ui.Text(i18n.Text("workflow_editor.empty_title"))),
				html.P(html.Props{Class: "muted"}, ui.Text(i18n.Text("workflow_editor.empty_detail"))),
				starters,
			),
		)
	}
	linkedExits := make(map[string]bool)
	for _, group := range [][]workflowPathStep{path.Steps, path.Loose} {
		for _, step := range group {
			if step.Node.ID == selectedID {
				for _, exit := range step.Exits {
					linkedExits[exit.ExitID] = true
				}
			}
		}
	}

	rows := make([]ui.Node, 0, len(path.Steps))
	for index, step := range path.Steps {
		rows = append(rows, html.WithKey(ui.CreateElement(workflowPathRow, workflowPathRowProps{
			I18nProps: i18n, Step: step, Number: index + 1, Lanes: path.Lanes, Selected: step.Node.ID == selectedID,
			Last: index == len(path.Steps)-1, OnSelect: onSelect, State: states[step.Node.ID],
		}), "row-"+step.Node.ID))
	}
	children := []ui.Node{}
	if len(rows) > 0 {
		children = append(children, html.Ol(html.Props{Key: "path-steps", Class: "workflow-path-steps", Data: map[string]string{"lanes": fmt.Sprint(path.Lanes)}}, rows...))
	}
	// A draft with no End is counted in the header but belongs to no step, so
	// it is said where the path stops: the author was told three things were
	// left and could find two.
	if len(path.Steps)+len(path.Loose) > 0 && len(path.Exits) == 0 {
		children = append(children, html.P(html.Props{Key: "path-no-exit", Class: "workflow-path-no-exit", Raw: map[string]any{"role": "note"}},
			productIcon("alert", "workflow-path-todo-icon"), html.Span(html.Props{}, ui.Text(i18n.Text("workflow_editor.problem_no_exit")))))
	}
	if len(path.Loose) > 0 {
		loose := make([]ui.Node, 0, len(path.Loose))
		for _, step := range path.Loose {
			loose = append(loose, html.WithKey(ui.CreateElement(workflowPathRow, workflowPathRowProps{
				I18nProps: i18n, Step: step, Selected: step.Node.ID == selectedID, Last: true, Loose: true, OnSelect: onSelect, State: states[step.Node.ID],
			}), "row-"+step.Node.ID))
		}
		children = append(children, html.Section(html.Props{Key: "path-loose", Class: "workflow-path-loose", Aria: map[string]string{"labelledby": "workflow-path-loose-title"}},
			html.H3(html.Props{ID: "workflow-path-loose-title"}, ui.Text(i18n.Text("workflow_editor.loose_title"))),
			html.P(html.Props{Class: "muted"}, ui.Text(i18n.Text("workflow_editor.loose_detail"))),
			html.Ul(html.Props{Class: "workflow-path-steps", Raw: map[string]any{"role": "list"}, Data: map[string]string{"lanes": "0"}}, loose...),
		))
	}

	exitsHint := i18n.Text("workflow_editor.exits_hint")
	if len(linkedExits) > 0 {
		for _, group := range [][]workflowPathStep{path.Steps, path.Loose} {
			for _, step := range group {
				if step.Node.ID == selectedID {
					exitsHint = i18n.Text("workflow_editor.exits_hint_selected", map[string]string{"step": bidiIsolate(workflowNodeName(i18n, step.Node))})
				}
			}
		}
	}
	for _, exit := range path.Exits {
		if exit.Node.ID == selectedID {
			exitsHint = i18n.Text("workflow_editor.exits_hint_end")
		}
	}
	exits := make([]ui.Node, 0, len(path.Exits))
	for _, exit := range path.Exits {
		exits = append(exits, html.WithKey(ui.CreateElement(workflowPathExitRow, workflowPathExitRowProps{
			I18nProps: i18n, Exit: exit, Selected: exit.Node.ID == selectedID, Linked: linkedExits[exit.Node.ID], Dimmed: len(linkedExits) > 0, OnSelect: onSelect, State: states[exit.Node.ID],
		}), "exit-"+exit.Node.ID))
	}
	var railKey ui.Node
	if path.Lanes > 0 {
		railKey = html.Ul(html.Props{Key: "rail-key", Class: "workflow-path-rail-key", Raw: map[string]any{"role": "list"}},
			html.Li(html.Props{}, html.Span(html.Props{Class: "workflow-path-rail-sample", Aria: map[string]string{"hidden": "true"}}), ui.Text(i18n.Text("workflow_editor.rail_skip"))),
			html.Li(html.Props{}, html.Span(html.Props{Class: "workflow-path-rail-sample back", Aria: map[string]string{"hidden": "true"}}), ui.Text(i18n.Text("workflow_editor.rail_loop"))),
		)
	}
	var rail ui.Node
	if len(exits) > 0 {
		rail = html.Section(html.Props{Key: "path-exits", Class: "workflow-path-exits", Aria: map[string]string{"labelledby": "workflow-path-exits-title"}},
			html.Div(html.Props{Class: "workflow-path-exits-heading"},
				html.H3(html.Props{ID: "workflow-path-exits-title"}, ui.Text(i18n.Text("workflow_editor.exits_title"))),
				html.P(html.Props{Class: "muted"}, ui.Text(exitsHint)),
			),
			html.Ul(html.Props{Class: "workflow-path-exit-list", Raw: map[string]any{"role": "list"}}, exits...),
			railKey,
		)
	}
	return html.Section(html.Props{ID: "workflow-path", Class: "workflow-path", Aria: map[string]string{"label": i18n.Text("workflow_editor.path_label")}},
		html.Div(html.Props{Key: "path-main", Class: "workflow-path-main"}, children...),
		rail,
	)
}

// workflowEmptyStarters offers the first step where the author is looking. An
// empty canvas that only said "add a step from the library" pointed away from
// itself.
func workflowEmptyStarters(i18n I18nProps, palette []WorkflowPaletteItem, insert func(WorkflowPaletteItem)) ui.Node {
	if insert == nil {
		return nil
	}
	buttons := make([]ui.Node, 0, 3)
	for _, stepType := range []string{"TASK", "APPROVAL", "DECISION"} {
		for _, item := range palette {
			if strings.EqualFold(item.Kind, "BLOCK") && strings.EqualFold(item.StepType, stepType) && strings.EqualFold(strings.TrimSpace(item.Name), DisplayLabel(stepType)) {
				buttons = append(buttons, html.WithKey(ui.CreateElement(workflowStarterButton, workflowStarterButtonProps{I18nProps: i18n, Item: item, OnInsert: insert}), "starter-"+item.ID))
				break
			}
		}
	}
	if len(buttons) == 0 {
		return nil
	}
	return html.Div(html.Props{Class: "workflow-path-starters"}, buttons...)
}

type workflowStarterButtonProps struct {
	I18nProps
	Item     WorkflowPaletteItem
	OnInsert func(WorkflowPaletteItem)
}

func workflowStarterButton(props workflowStarterButtonProps) ui.Node {
	click := ui.UseEvent(func(ui.MouseEvent) { props.OnInsert(props.Item) })
	return html.Button(html.Props{Class: "button secondary workflow-palette-insert workflow-path-starter", Type: "button", OnClick: click},
		productIcon("plus", "button-icon"),
		html.Span(html.Props{}, html.Strong(html.Props{}, ui.Text(workflowPaletteName(props.I18nProps, props.Item))), html.Small(html.Props{Class: "muted"}, ui.Text(workflowPaletteDescription(props.I18nProps, props.Item)))),
	)
}

type workflowPathRowProps struct {
	I18nProps
	Step     workflowPathStep
	Number   int
	Lanes    int
	Selected bool
	Last     bool
	Loose    bool
	OnSelect func(string)
	State    workflowPathState
}

// workflowNodeName is the name a step or exit is shown by. An author's own
// label always wins. An unnamed exit whose id follows the end_<outcome>
// convention takes the catalog's word for that outcome, so the exit a German
// author sees is "Abgelehnt" in the legend, on the route and in every picker,
// not "Rejected" in two of the three.
func workflowNodeName(i18n I18nProps, node WorkflowDraftNode) string {
	if label := strings.TrimSpace(node.Label); label != "" {
		return label
	}
	if id := strings.TrimSpace(node.ID); workflowIsExit(node) && len(id) > 4 && strings.EqualFold(id[:4], "end_") && strings.Trim(id[4:], "0123456789_") != "" {
		return displayWorkflowToken(WorkflowViewerProps{I18nProps: i18n}, "outcome", id[4:])
	}
	return DisplayLabel(node.ID)
}

func workflowPathMark(tone string, shape int, title string) ui.Node {
	props := html.Props{Class: "workflow-path-mark", Data: map[string]string{"tone": tone, "shape": fmt.Sprint(shape % 4)}}
	if title != "" {
		props.Title = title
	}
	return html.Span(props)
}

func workflowPathRow(props workflowPathRowProps) ui.Node {
	step := props.Step
	click := ui.UseEvent(func(ui.MouseEvent) {
		if props.OnSelect != nil {
			props.OnSelect(step.Node.ID)
		}
	})
	tokens := WorkflowViewerProps{I18nProps: props.I18nProps}
	name := workflowNodeName(props.I18nProps, step.Node)

	// An exit is named on the step that reaches it. A row of bare dots needed
	// the legend to be read, and the legend is not always on screen.
	const shownExits = 4
	marks := make([]ui.Node, 0, len(step.Exits))
	described := make([]string, 0, len(step.Exits)+len(step.Jumps)+len(step.Open))
	for index, exit := range step.Exits {
		routes := make([]string, 0, len(exit.RouteKeys))
		for _, key := range exit.RouteKeys {
			routes = append(routes, displayWorkflowToken(tokens, "outcome", key))
		}
		exitName := workflowNodeName(props.I18nProps, exit.ExitNode)
		sentence := props.Text("workflow_editor.route_to", map[string]string{"route": bidiIsolate(strings.Join(routes, ", ")), "target": bidiIsolate(exitName)})
		described = append(described, sentence)
		if index < shownExits {
			marks = append(marks, html.Span(html.Props{Key: fmt.Sprintf("mark-%d", index), Class: "workflow-path-exit-chip", Title: sentence},
				workflowPathMark(exit.Tone, exit.Shape, ""), html.Span(html.Props{Dir: "auto"}, ui.Text(exitName))))
		}
	}
	if extra := len(step.Exits) - shownExits; extra > 0 {
		hidden := make([]string, 0, extra)
		for _, exit := range step.Exits[shownExits:] {
			hidden = append(hidden, workflowNodeName(props.I18nProps, exit.ExitNode))
		}
		marks = append(marks, html.Span(html.Props{Key: "mark-more", Class: "workflow-path-exit-chip more", Title: strings.Join(hidden, ", ")}, ui.Text(props.Text("workflow_editor.more_exits", map[string]string{"count": props.Locale.FormatNumber(fmt.Sprint(extra), 0)}))))
	}
	jumps := make([]ui.Node, 0, len(step.Jumps))
	for index, jump := range step.Jumps {
		routes := make([]string, 0, len(jump.RouteKeys))
		for _, key := range jump.RouteKeys {
			routes = append(routes, displayWorkflowToken(tokens, "outcome", key))
		}
		key := "workflow_editor.jump"
		if jump.Back {
			key = "workflow_editor.jump_back"
		}
		if len(routes) > 1 {
			key += "_many"
		}
		text := props.Text(key, map[string]string{"route": bidiIsolate(strings.Join(routes, props.Text("workflow_editor.list_join"))), "target": bidiIsolate(workflowNodeName(props.I18nProps, jump.ToNode))})
		jumps = append(jumps, html.WithKey(ui.CreateElement(workflowPathJumpNote, workflowPathJumpNoteProps{Text: text, ToID: jump.ToID, Back: jump.Back, OnSelect: props.OnSelect}), fmt.Sprintf("jump-%d", index)))
	}
	if len(step.Open) > 0 {
		described = append(described, props.Locale.Plural("workflow_editor.open_count", int64(len(step.Open))))
	}
	// A decision's results are equals. Writing one on the line to the next
	// row and the other as a note beneath made the first look like the path
	// and the second like a footnote, so a decision lists every way out in
	// one place and its connector carries no word.
	decision := strings.EqualFold(step.Node.StepType, "DECISION") && step.Continues != "" && len(step.Jumps) > 0
	if decision {
		text := props.Text("workflow_editor.continues", map[string]string{
			"route": bidiIsolate(displayWorkflowToken(tokens, "outcome", step.Continues)), "target": bidiIsolate(workflowNodeName(props.I18nProps, step.ContinuesTo)),
		})
		first := html.WithKey(ui.CreateElement(workflowPathJumpNote, workflowPathJumpNoteProps{Text: text, ToID: step.ContinuesTo.ID, Primary: true, OnSelect: props.OnSelect}), "jump-next")
		jumps = append([]ui.Node{first}, jumps...)
	}
	arrivals := make([]ui.Node, 0, len(step.Arrivals))
	for index, arrival := range step.Arrivals {
		routes := make([]string, 0, len(arrival.RouteKeys))
		for _, key := range arrival.RouteKeys {
			routes = append(routes, displayWorkflowToken(tokens, "outcome", key))
		}
		text := props.Text("workflow_editor.arrives_from", map[string]string{
			"route": bidiIsolate(strings.Join(routes, props.Text("workflow_editor.list_join"))), "step": bidiIsolate(workflowNodeName(props.I18nProps, arrival.From)),
		})
		arrivals = append(arrivals, html.WithKey(ui.CreateElement(workflowPathJumpNote, workflowPathJumpNoteProps{Text: text, ToID: arrival.From.ID, Back: !arrival.Back, Arrival: true, OnSelect: props.OnSelect}), fmt.Sprintf("arrival-%d", index)))
	}

	class := "workflow-path-step"
	if props.Selected {
		class += " selected"
	}
	if props.Loose {
		class += " loose"
	}
	button := html.Props{Class: class, Type: "button", OnClick: click, Data: map[string]string{"node-id": step.Node.ID, "step-type": strings.ToLower(step.Node.StepType)}}
	if props.Selected {
		button.Raw = map[string]any{"aria-current": "step"}
	}
	number := ui.Node(html.Span(html.Props{Class: "workflow-path-number", Aria: map[string]string{"hidden": "true"}}, ui.Text(props.Locale.FormatNumber(fmt.Sprint(props.Number), 0))))
	if props.Loose {
		number = html.Span(html.Props{Class: "workflow-path-number", Aria: map[string]string{"hidden": "true"}}, ui.Text("—"))
	}
	meta := []ui.Node{html.Span(html.Props{Key: "type"}, ui.Text(displayWorkflowToken(tokens, "step", step.Node.StepType)))}
	if step.Group != "" {
		meta = append(meta, html.Span(html.Props{Key: "group", Class: "workflow-path-flag", Dir: "auto", Data: map[string]string{"group-id": step.Node.GroupID}}, ui.Text(step.Group)))
	}
	if step.Start {
		meta = append(meta, html.Span(html.Props{Key: "start", Class: "workflow-path-flag"}, ui.Text(props.Text("workflow_viewer.start"))))
	}
	if step.Node.Locked {
		meta = append(meta, html.Span(html.Props{Key: "lock", Class: "workflow-path-flag", Title: props.Text("workflow_editor.required")}, productIcon("privacy", "workflow-path-lock"), html.Span(html.Props{Class: "sr-only"}, ui.Text(props.Text("workflow_editor.required")))))
	}
	if ways := len(step.Jumps) + len(step.Open); strings.EqualFold(step.Node.StepType, "DECISION") && ways > 0 {
		if step.Continues != "" {
			ways++
		}
		for _, route := range step.Open {
			if workflowOutcomeIsException(route) {
				ways--
			}
		}
		if ways > 1 {
			meta = append(meta, html.Span(html.Props{Key: "ways", Class: "workflow-path-flag"}, ui.Text(props.Locale.Plural("workflow_editor.ways_out", int64(ways)))))
		}
	}
	if props.State.Label != "" {
		meta = append(meta, html.Span(html.Props{Key: "state", Class: "workflow-path-state", Data: map[string]string{"tone": props.State.Tone}}, ui.Text(props.State.Label)))
	}
	if props.State.Current {
		button.Class += " current"
	}
	if props.State.Key != "" {
		button.Data["state"] = props.State.Key
	}
	var todo ui.Node
	unfinished, exceptionsOpen := len(step.Unbound), false
	for _, route := range step.Open {
		if workflowOutcomeIsException(route) {
			exceptionsOpen = true
		} else {
			unfinished++
		}
	}
	if exceptionsOpen {
		unfinished++
	}
	// Not being connected is itself one of the things the header counts, so a
	// loose step's badge includes it and the badges add up to the header.
	if props.Loose {
		unfinished++
	}
	if unfinished > 0 {
		todo = html.Span(html.Props{Key: "todo", Class: "workflow-path-todo"}, productIcon("alert", "workflow-path-todo-icon"), html.Span(html.Props{}, ui.Text(props.Locale.Plural("workflow_editor.todo_count", int64(unfinished)))))
	}
	element := html.Button
	if props.OnSelect == nil {
		element = html.Div
		button.Type, button.OnClick = "", ui.Handler{}
		button.Class += " static"
	}
	var markRow ui.Node
	if len(marks) > 0 {
		markRow = html.Span(html.Props{Key: "marks", Class: "workflow-path-marks", Aria: map[string]string{"hidden": "true"}}, marks...)
	}
	card := element(button,
		number,
		html.Span(html.Props{Class: "workflow-path-copy"},
			// An inline dir=auto isolates the name's direction the way bdi
			// does, without changing the block's alignment; on the block it
			// sent a Latin name to the far edge of an Arabic card, away from
			// its number.
			html.Strong(html.Props{}, html.Span(html.Props{Dir: "auto"}, ui.Text(name))),
			html.Span(html.Props{Class: "workflow-path-meta"}, meta...),
			markRow,
		),
		todo,
		html.Span(html.Props{Class: "sr-only"}, ui.Text(strings.Join(described, ". "))),
	)

	// The space under a step always carries words. It names the result that
	// leads on when one does, and it says, separately and in warning colour,
	// how many of the step's results lead nowhere. The two used to be
	// alternatives, so a step that continued never showed that it was also
	// unfinished, and the last step of a draft showed nothing at all.
	linkClass, linkWord := "workflow-path-link", ""
	switch {
	case decision:
	case step.Continues != "":
		linkWord = displayWorkflowToken(tokens, "outcome", step.Continues)
	case props.Last && len(step.Open) == 0:
		linkClass = ""
	case len(step.Open) > 0:
		linkClass += " broken unfinished"
	case len(step.Exits) > 0 && len(step.Jumps) == 0:
		linkClass, linkWord = linkClass+" broken", props.Text("workflow_editor.all_results_end")
	default:
		linkClass, linkWord = linkClass+" broken", props.Text("workflow_editor.ends_here")
	}
	var link ui.Node
	if linkClass != "" {
		children := make([]ui.Node, 0, 2)
		if linkWord != "" {
			children = append(children, html.Span(html.Props{Key: "word"}, ui.Text(linkWord)))
		}
		if len(step.Open) > 0 {
			// The badge on the step owns the number, and it groups the rare
			// results as one thing to finish. A second count here, of every
			// loose result, put two different amber numbers on one step.
			openKey := "workflow_editor.open_none_lead"
			switch {
			case step.Continues != "":
				openKey = "workflow_editor.open_others"
			case len(step.Exits) > 0 || len(step.Jumps) > 0:
				openKey = "workflow_editor.open_some"
			}
			children = append(children, html.Span(html.Props{Key: "open", Class: "workflow-path-link-open"}, productIcon("alert", "workflow-path-todo-icon"), ui.Text(props.Text(openKey))))
		}
		link = html.Div(html.Props{Key: "link", Class: linkClass}, children...)
	}
	var jumpList ui.Node
	if len(jumps) > 0 {
		jumpList = html.Ul(html.Props{Key: "jumps", Class: "workflow-path-jumps", Raw: map[string]any{"role": "list"}}, jumps...)
	}
	// The rail is drawn in two stretches per row. Beside the step a lane may
	// turn in to meet it; beside whatever follows the step a lane only passes
	// through. Splitting it keeps the turn level with the step however tall
	// the rest of the row grows.
	var arrivalList ui.Node
	if len(arrivals) > 0 {
		arrivalList = html.Ul(html.Props{Key: "arrivals", Class: "workflow-path-jumps workflow-path-arrivals", Raw: map[string]any{"role": "list"}}, arrivals...)
	}
	return html.Li(html.Props{Class: "workflow-path-row"},
		workflowPathRailBefore(step.Rail, props.Lanes),
		html.Div(html.Props{Key: "before", Class: "workflow-path-before"}, arrivalList),
		workflowPathRail(step.Rail, props.Lanes, true),
		html.WithKey(card, "card"),
		workflowPathRail(step.Rail, props.Lanes, false),
		html.Div(html.Props{Key: "after", Class: "workflow-path-after"}, jumpList, link),
	)
}

type workflowPathJumpNoteProps struct {
	Text, ToID string
	Back       bool
	Arrival    bool
	Primary    bool
	OnSelect   func(string)
}

// workflowPathJumpNote names a route that leaves the column and, in the
// editor, takes the author to where it lands.
func workflowPathJumpNote(props workflowPathJumpNoteProps) ui.Node {
	click := ui.UseEvent(func(ui.MouseEvent) {
		if props.OnSelect != nil {
			props.OnSelect(props.ToID)
		}
	})
	icon := "move-down"
	if props.Back {
		icon = "move-up"
	}
	class := "workflow-path-jump"
	if props.Primary {
		class += " primary"
	}
	if props.OnSelect == nil {
		return html.Li(html.Props{Class: class + " static"}, productIcon(icon, "workflow-path-jump-icon"), html.Span(html.Props{}, ui.Text(props.Text)))
	}
	return html.Li(html.Props{}, html.Button(html.Props{Class: class, Type: "button", OnClick: click},
		productIcon(icon, "workflow-path-jump-icon"), html.Span(html.Props{}, ui.Text(props.Text))))
}

// workflowPathRail draws the jump lanes crossing one row, the way a revision
// graph draws branches beside commits. Each row owns its own stretches of
// rail and the drawing scales to them, so rows may be any height and nothing
// has to be measured in the browser.
func workflowPathRail(cells []workflowRailCell, lanes int, besideStep bool) ui.Node {
	key, class := "rail-after", "workflow-path-rail"
	if besideStep {
		key = "rail-step"
	}
	if lanes == 0 {
		return html.Div(html.Props{Key: key, Class: class, Aria: map[string]string{"hidden": "true"}})
	}
	const pitch = 12
	width := lanes * pitch
	shapes := make([]ui.Node, 0, len(cells))
	arrives := false
	for lane, cell := range cells {
		// Lane 0 is nearest the step, so short hops hug the path and long
		// ones travel on the outside.
		x := width - lane*pitch - pitch/2
		d := ""
		if besideStep {
			if cell.Up {
				d += fmt.Sprintf("M%d 0V50", x)
			}
			if cell.Down {
				d += fmt.Sprintf("M%d 50V100", x)
			}
			if cell.Turn {
				d += fmt.Sprintf("M%d 50H%d", x, width)
				arrives = arrives || cell.Arrives
			}
		} else if cell.Down {
			d = fmt.Sprintf("M%d 0V100", x)
		}
		if d == "" {
			continue
		}
		line := "workflow-rail-line"
		if cell.Back {
			line += " back"
		}
		if cell.Active {
			line += " active"
		}
		shapes = append(shapes, html.Tag("path", html.Props{Key: fmt.Sprintf("lane-%d", lane), Class: line, Raw: map[string]any{"d": d, "vector-effect": "non-scaling-stroke"}}))
	}
	if arrives {
		class += " arrives"
	}
	return html.Div(html.Props{Key: key, Class: class, Aria: map[string]string{"hidden": "true"}},
		html.Tag("svg", html.Props{Raw: map[string]any{"viewBox": fmt.Sprintf("0 0 %d 100", width), "preserveAspectRatio": "none", "focusable": "false"}}, shapes...),
	)
}

// workflowPathRailBefore continues the lanes that are already running above a
// row through the space its arrival notes take up.
func workflowPathRailBefore(cells []workflowRailCell, lanes int) ui.Node {
	if lanes == 0 {
		return html.Div(html.Props{Key: "rail-before", Class: "workflow-path-rail", Aria: map[string]string{"hidden": "true"}})
	}
	const pitch = 12
	width := lanes * pitch
	shapes := make([]ui.Node, 0, len(cells))
	for lane, cell := range cells {
		if !cell.Up {
			continue
		}
		line := "workflow-rail-line"
		if cell.Back {
			line += " back"
		}
		if cell.Active {
			line += " active"
		}
		x := width - lane*pitch - pitch/2
		shapes = append(shapes, html.Tag("path", html.Props{Key: fmt.Sprintf("lane-%d", lane), Class: line, Raw: map[string]any{"d": fmt.Sprintf("M%d 0V100", x), "vector-effect": "non-scaling-stroke"}}))
	}
	return html.Div(html.Props{Key: "rail-before", Class: "workflow-path-rail", Aria: map[string]string{"hidden": "true"}},
		html.Tag("svg", html.Props{Raw: map[string]any{"viewBox": fmt.Sprintf("0 0 %d 100", width), "preserveAspectRatio": "none", "focusable": "false"}}, shapes...),
	)
}

type workflowPathExitRowProps struct {
	I18nProps
	Exit     workflowPathExit
	Selected bool
	Linked   bool
	Dimmed   bool
	OnSelect func(string)
	State    workflowPathState
}

func workflowPathExitRow(props workflowPathExitRowProps) ui.Node {
	click := ui.UseEvent(func(ui.MouseEvent) {
		if props.OnSelect != nil {
			props.OnSelect(props.Exit.Node.ID)
		}
	})
	class := "workflow-path-exit"
	if props.Selected {
		class += " selected"
	}
	if props.Linked {
		class += " linked"
	} else if props.Dimmed {
		class += " dimmed"
	}
	button := html.Props{Class: class, Type: "button", OnClick: click, Data: map[string]string{"node-id": props.Exit.Node.ID, "tone": props.Exit.Tone}}
	if props.Selected {
		button.Raw = map[string]any{"aria-current": "step"}
	}
	element := html.Button
	if props.OnSelect == nil {
		element = html.Div
		button.Type, button.OnClick = "", ui.Handler{}
		button.Class += " static"
	}
	if props.State.Current {
		button.Class += " current"
	}
	return html.Li(html.Props{},
		element(button,
			workflowPathMark(props.Exit.Tone, props.Exit.Shape, ""),
			html.Strong(html.Props{Dir: "auto"}, ui.Text(workflowNodeName(props.I18nProps, props.Exit.Node))),
			html.Span(html.Props{Class: "workflow-path-exit-count"}, ui.Text(props.Locale.Plural("workflow_editor.exit_route_count", int64(props.Exit.Routes)))),
		),
	)
}
