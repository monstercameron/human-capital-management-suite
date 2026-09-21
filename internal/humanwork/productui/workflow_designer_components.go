package productui

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workflowview"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// WorkflowCatalogItem is the bounded navigation record for one published
// workflow version. It contains presentation metadata only; selecting it does
// not grant read, edit, or publication authority.
type WorkflowCatalogItem struct {
	WorkflowID      string
	Name            string
	Version         uint32
	SemanticVersion string
	Status          string
	ActiveRuns      int
}

type WorkflowDesignerPageProps struct {
	I18nProps
	Catalog        []WorkflowCatalogItem
	Palette        []WorkflowPaletteItem
	Selected       *workflowview.View
	Draft          *WorkflowDraftView
	BaseHref       string
	Navigate       func(string)
	CanCreate      bool
	OnCreate       func(WorkflowDraftCreateRequest)
	OnInsert       func(WorkflowPaletteItem)
	OnUpdateNode   func(WorkflowNodeParameterChange)
	OnSetOutcome   func(WorkflowOutcomeChange)
	OnBindInput    func(WorkflowInputBindingChange)
	OnMoveNode     func(WorkflowNodeMove)
	OnOverlay      func(WorkflowTemplateOverlayChange)
	OnHistory      func(string)
	SelectedNodeID string
	OnSelectNode   func(string)
}

// WorkflowDraftCreateRequest keeps presentation choices separate from the
// transport request. A new workflow uses no base; a replacement pins the
// immutable publication it supersedes and names its own release version.
type WorkflowDraftCreateRequest struct {
	Template          *WorkflowPaletteItem
	WorkflowID        string
	Name              string
	SemanticVersion   string
	BaseVersionDigest string
}

type WorkflowDraftView struct {
	DraftID, WorkflowID, Name, SemanticVersion, BaseVersionDigest, ExpiresAt      string
	StartNodeID, DefinitionDigest, BaseDefinitionDigest, TemplateDefinitionDigest string
	MatchesBaseDefinition, MatchesTemplateDefinition                              bool
	TemplateID                                                                    string
	TemplateVersion                                                               uint32
	Revision                                                                      uint64
	HistoryPosition, HistoryLength                                                uint64
	HistoryLabel, LayoutMode                                                      string
	CanUndo, CanRedo                                                              bool
	Nodes                                                                         []WorkflowDraftNode
	Edges                                                                         []WorkflowDraftEdge
	Groups                                                                        []WorkflowDraftGroup
	Overlays                                                                      []WorkflowTemplateOverlay
	Changes                                                                       []WorkflowDraftSemanticChange
}

type WorkflowDraftSemanticChange struct {
	Kind, Operation, SubjectID, Field, Before, After string
}

type WorkflowDraftNode struct {
	ID, StepType, GroupID, Label, LockKind string
	Locked                                 bool
	Parameters                             []WorkflowNodeParameter
	Outcomes                               []WorkflowDraftOutcome
	Bindings                               []WorkflowDraftBinding
}
type WorkflowDraftOutcome struct {
	RouteKey      string
	TargetNodeIDs []string
}
type WorkflowDraftBindingCandidate struct{ SourceNodeID, SourcePath, ValueType string }
type WorkflowDraftBinding struct {
	TargetPath, TargetType               string
	SourceKind, SourceNodeID, SourcePath string
	Candidates                           []WorkflowDraftBindingCandidate
}
type WorkflowNodeParameter struct {
	ID, Label, Kind, Value string
	Required               bool
	Minimum, Maximum       int64
	Options                []string
}
type WorkflowTemplateOverlay struct {
	Operation, TargetNodeID, EntryID, Reason string
	EntryVersion                             uint32
}
type WorkflowNodeParameterChange struct {
	NodeID string
	Values map[string]string
}
type WorkflowOutcomeChange struct{ FromNodeID, RouteKey, ToNodeID string }
type WorkflowInputBindingChange struct{ TargetNodeID, TargetPath, SourceNodeID, SourcePath string }
type WorkflowNodeMove struct{ NodeID, Direction string }
type WorkflowTemplateOverlayChange struct {
	Operation, TargetNodeID, EntryID, Reason string
	EntryVersion                             uint32
}
type WorkflowDraftEdge struct{ FromID, ToID, RouteKey string }
type WorkflowDraftGroup struct {
	ID, Name, EntryID string
	EntryVersion      uint32
	Collapsed         bool
	NodeIDs           []string
}

func WorkflowDesignerPage(props WorkflowDesignerPageProps) ui.Node {
	catalog := append([]WorkflowCatalogItem(nil), props.Catalog...)
	sort.SliceStable(catalog, func(i, j int) bool {
		left, right := strings.ToLower(catalog[i].Name), strings.ToLower(catalog[j].Name)
		if left == right {
			if catalog[i].WorkflowID == catalog[j].WorkflowID {
				return catalog[i].Version > catalog[j].Version
			}
			return catalog[i].WorkflowID < catalog[j].WorkflowID
		}
		return left < right
	})

	selectedID := ""
	if props.Selected != nil {
		selectedID = props.Selected.WorkflowID
	}
	items := make([]ui.Node, 0, len(catalog))
	for _, item := range catalog {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = item.WorkflowID
		}
		status := displayWorkflowToken(WorkflowViewerProps{I18nProps: props.I18nProps}, "publication", item.Status)
		version := "v" + item.SemanticVersion
		if item.SemanticVersion == "" {
			version = fmt.Sprintf("definition %d", item.Version)
		}
		class := "workflow-catalog-link"
		current := "false"
		if selectedID != "" && item.WorkflowID == selectedID {
			class += " active"
			current = "page"
		}
		items = append(items, html.Li(html.Props{},
			softwareLink(props.Navigate, html.Props{Class: class, Raw: map[string]any{"aria-current": current}}, workflowDesignerHref(props.BaseHref, item.WorkflowID, ""),
				html.Span(html.Props{Class: "workflow-catalog-title", Dir: "auto"}, ui.Text(name)),
				html.Span(html.Props{Class: "workflow-catalog-meta"},
					html.Span(html.Props{}, ui.Text(version)),
					html.Span(html.Props{Class: "status-chip", Data: map[string]string{"tone": workflowPublicationTone(item.Status)}}, ui.Text(status)),
				),
			),
		))
	}

	catalogBody := ui.Node(html.Ul(html.Props{Class: "workflow-catalog-list", Raw: map[string]any{"role": "list"}}, items...))
	if len(items) == 0 {
		catalogBody = html.Div(html.Props{Class: "workflow-catalog-empty", Raw: map[string]any{"role": "status"}},
			html.Strong(html.Props{}, ui.Text(props.Text("workflow_designer.empty_catalog_title"))),
			html.P(html.Props{Class: "muted"}, ui.Text(props.Text("workflow_designer.empty_catalog_detail"))),
		)
	}

	workspace := ui.Node(html.Div(html.Props{Class: "surface workflow-designer-empty", Raw: map[string]any{"role": "status"}},
		html.Div(html.Props{Class: "workflow-designer-empty-icon", Aria: map[string]string{"hidden": "true"}}, productIcon("studio", "")),
		html.H2(html.Props{}, ui.Text(props.Text("workflow_designer.choose_title"))),
		html.P(html.Props{Class: "muted"}, ui.Text(props.Text("workflow_designer.choose_detail"))),
	))
	if props.Draft != nil {
		workspace = workflowDraftWorkspace(props.I18nProps, *props.Draft, props.Palette, props.SelectedNodeID, props.OnSelectNode, props.OnUpdateNode, props.OnSetOutcome, props.OnBindInput, props.OnMoveNode, props.OnOverlay, props.OnHistory)
	} else if props.Selected != nil {
		workspace = html.Div(html.Props{Class: "workflow-published-workspace"},
			workflowPublishedVersionActions(props, *props.Selected),
			WorkflowViewer(WorkflowViewerProps{
				I18nProps:  props.I18nProps,
				ID:         "workflow-designer-viewer",
				Projection: *props.Selected,
			}),
		)
	}

	action := ui.Node(nil)
	if props.CanCreate {
		createProps := html.Props{Class: "button primary compact workflow-draft-create", Type: "button"}
		if props.OnCreate == nil {
			createProps.Disabled = true
			createProps.Title = props.Text("workflow_designer.create_unavailable")
		} else {
			createProps.OnClick = ui.UseEvent(func(ui.MouseEvent) { props.OnCreate(WorkflowDraftCreateRequest{SemanticVersion: "0.1.0"}) })
		}
		action = html.Button(createProps,
			productIcon("plus", "button-icon"), ui.Text(props.Text("workflow_designer.create")),
		)
	}

	headerChildren := []ui.Node{
		html.Div(html.Props{},
			html.P(html.Props{Class: "eyebrow"}, ui.Text(props.Text("workflow_designer.eyebrow"))),
			html.H2(html.Props{ID: "workflow-designer-heading"}, ui.Text(props.Text("workflow_designer.heading"))),
			html.P(html.Props{Class: "muted"}, ui.Text(props.Text("workflow_designer.description"))),
		),
	}
	if action != nil {
		headerChildren = append(headerChildren, action)
	}

	return html.Section(html.Props{Class: "workflow-designer-page", Aria: map[string]string{"labelledby": "workflow-designer-heading"}},
		html.Header(html.Props{Class: "workflow-designer-header"}, headerChildren...),
		html.P(html.Props{ID: "workflow-designer-status", Class: "workflow-designer-status muted", Raw: map[string]any{"role": "status", "aria-live": "polite"}}, ui.Text("")),
		html.Div(html.Props{Class: "workflow-designer-layout"},
			html.Aside(html.Props{Class: "surface workflow-catalog", Aria: map[string]string{"labelledby": "workflow-catalog-heading"}},
				html.Div(html.Props{Class: "workflow-catalog-heading"},
					html.H3(html.Props{ID: "workflow-catalog-heading"}, ui.Text(props.Text("workflow_designer.catalog_title"))),
					html.Span(html.Props{Class: "count-badge"}, ui.Text(fmt.Sprintf("%d", len(catalog)))),
				),
				html.P(html.Props{Class: "muted workflow-catalog-description"}, ui.Text(props.Text("workflow_designer.catalog_detail"))),
				catalogBody,
			),
			html.Div(html.Props{Class: "workflow-designer-workspace"}, workspace),
			ui.CreateElement(WorkflowPalette, WorkflowPaletteProps{
				I18nProps: props.I18nProps, Items: props.Palette,
				OnInsert: func(item WorkflowPaletteItem) {
					if strings.EqualFold(item.Kind, "TEMPLATE") {
						if props.OnCreate != nil {
							selected := item
							props.OnCreate(WorkflowDraftCreateRequest{Template: &selected})
						}
						return
					}
					if props.Draft != nil {
						if props.Draft.TemplateID != "" && props.OnOverlay != nil {
							props.OnOverlay(WorkflowTemplateOverlayChange{Operation: "ADD", EntryID: item.ID, EntryVersion: item.Version})
						} else {
							props.OnInsert(item)
						}
					}
				},
				CanInsert: func(item WorkflowPaletteItem) bool {
					if strings.EqualFold(item.Kind, "TEMPLATE") {
						return props.OnCreate != nil
					}
					return props.Draft != nil && (props.OnInsert != nil || (props.Draft.TemplateID != "" && props.OnOverlay != nil))
				},
			}),
		),
	)
}

func workflowPublishedVersionActions(props WorkflowDesignerPageProps, selected workflowview.View) ui.Node {
	nextVersion := ""
	if value, err := workflowversion.NextPatchVersion(selected.SemanticVersion); err == nil {
		nextVersion = value
	}
	input := html.Props{
		ID: "workflow-next-semantic-version", Name: "semantic_version", Type: "text", Value: nextVersion,
		Placeholder: "1.1.0", AutoComplete: "off", Required: true,
		Pattern: `(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?`,
		Raw:     map[string]any{"spellcheck": "false"},
		Aria:    map[string]string{"describedby": "workflow-version-help"},
	}
	input.OnInput = ui.UseEvent(func(event ui.InputEvent) { nextVersion = strings.TrimSpace(event.GetValue()) })
	button := html.Props{Class: "button primary compact", Type: "submit", Disabled: props.OnCreate == nil}
	form := html.Props{Class: "workflow-version-create"}
	if props.OnCreate != nil {
		form.OnSubmit = ui.UseEvent(func(event ui.FormEvent) {
			event.PreventDefault()
			props.OnCreate(WorkflowDraftCreateRequest{
				WorkflowID: selected.WorkflowID, Name: selected.Name, SemanticVersion: nextVersion, BaseVersionDigest: selected.PlanDigest,
			})
		})
	}
	return html.Section(html.Props{Class: "surface workflow-published-version", Aria: map[string]string{"labelledby": "workflow-published-version-title"}},
		html.Div(html.Props{},
			html.P(html.Props{Class: "eyebrow"}, ui.Text(props.Text("workflow_version.published_eyebrow"))),
			html.H3(html.Props{ID: "workflow-published-version-title"}, ui.Text(props.Text("workflow_version.published_title", map[string]string{"version": selected.SemanticVersion}))),
			html.P(html.Props{ID: "workflow-version-help", Class: "muted"}, ui.Text(props.Text("workflow_version.published_help"))),
		),
		html.Form(form,
			ui.CreateElement(LabeledControl, LabeledControlProps{For: input.ID, Label: props.Text("workflow_version.next_label"), Control: html.Input(input)}),
			html.Button(button, productIcon("plus", "button-icon"), ui.Text(props.Text("workflow_version.create"))),
		),
	)
}

func workflowDraftWorkspace(i18n I18nProps, draft WorkflowDraftView, palette []WorkflowPaletteItem, selectedNodeID string, onSelectNode func(string), onUpdate func(WorkflowNodeParameterChange), onSetOutcome func(WorkflowOutcomeChange), onBindInput func(WorkflowInputBindingChange), onMoveNode func(WorkflowNodeMove), onOverlay func(WorkflowTemplateOverlayChange), onHistory func(string)) ui.Node {
	nodesByID := make(map[string]WorkflowDraftNode, len(draft.Nodes))
	grouped := make(map[string]bool, len(draft.Nodes))
	for _, node := range draft.Nodes {
		nodesByID[node.ID] = node
		if node.GroupID != "" {
			grouped[node.ID] = true
		}
	}
	groups := make([]ui.Node, 0, len(draft.Groups))
	for _, group := range draft.Groups {
		nodes := make([]ui.Node, 0, len(group.NodeIDs))
		for _, id := range group.NodeIDs {
			if node, ok := nodesByID[id]; ok {
				nodes = append(nodes, workflowDraftNodeRow(i18n, node))
			}
		}
		details := html.Props{Class: "workflow-draft-group", Data: map[string]string{"group-id": group.ID, "entry-id": group.EntryID}}
		if !group.Collapsed {
			details.Raw = map[string]any{"open": ""}
		}
		groups = append(groups, html.Details(details,
			html.Summary(html.Props{},
				html.Span(html.Props{Class: "workflow-draft-group-title"}, ui.Text(group.Name)),
				html.Span(html.Props{Class: "count-badge"}, ui.Text(fmt.Sprint(len(nodes)))),
			),
			html.Ul(html.Props{Class: "workflow-draft-node-list", Raw: map[string]any{"role": "list"}}, nodes...),
		))
	}
	ungrouped := make([]ui.Node, 0, len(draft.Nodes))
	for _, node := range draft.Nodes {
		if !grouped[node.ID] {
			ungrouped = append(ungrouped, workflowDraftNodeRow(i18n, node))
		}
	}
	body := make([]ui.Node, 0, len(groups)+1)
	if len(ungrouped) > 0 {
		body = append(body, html.Ul(html.Props{Class: "workflow-draft-node-list", Raw: map[string]any{"role": "list"}}, ungrouped...))
	}
	body = append(body, groups...)
	if len(body) == 0 {
		body = append(body, html.Div(html.Props{Class: "workflow-draft-empty", Raw: map[string]any{"role": "status"}},
			html.Strong(html.Props{}, ui.Text(i18n.Text("workflow_draft.empty_title"))),
			html.P(html.Props{Class: "muted"}, ui.Text(i18n.Text("workflow_draft.empty_detail"))),
		))
	}
	projection := workflowDraftGraphProjection(draft)
	if len(projection.Nodes) > 0 {
		// The graph and its semantic outline are the canonical whole-draft
		// representation. Keep fragment groups as authoring affordances, but do
		// not repeat every ungrouped node in a third flat list.
		body = append([]ui.Node{workflowDraftTopology(i18n, draft, projection, selectedNodeID, onSelectNode, onMoveNode)}, groups...)
	}
	body = append(body, ui.CreateElement(WorkflowNodeInspector, WorkflowNodeInspectorProps{I18nProps: i18n, Draft: draft, Palette: palette, SelectedNodeID: selectedNodeID, OnSelect: onSelectNode, OnUpdate: onUpdate, OnSetOutcome: onSetOutcome, OnBindInput: onBindInput, OnOverlay: onOverlay}))
	undo := html.Props{Class: "button ghost compact", Type: "button", Disabled: !draft.CanUndo || onHistory == nil, Aria: map[string]string{"label": i18n.Text("workflow_draft.undo")}, Title: i18n.Text("workflow_draft.undo")}
	redo := html.Props{Class: "button ghost compact", Type: "button", Disabled: !draft.CanRedo || onHistory == nil, Aria: map[string]string{"label": i18n.Text("workflow_draft.redo")}, Title: i18n.Text("workflow_draft.redo")}
	if onHistory != nil {
		undo.OnClick = ui.UseEvent(func(ui.MouseEvent) { onHistory("UNDO") })
		redo.OnClick = ui.UseEvent(func(ui.MouseEvent) { onHistory("REDO") })
	}
	historyPosition, historyLength := draft.HistoryPosition, draft.HistoryLength
	if historyPosition == 0 || historyLength == 0 {
		historyPosition, historyLength = 1, 1
	}
	history := html.Div(html.Props{Class: "workflow-draft-history-controls", Raw: map[string]any{"role": "group"}, Aria: map[string]string{"label": i18n.Text("workflow_draft.history_controls")}},
		html.Button(undo, productIcon("history-back", ""), html.Span(html.Props{}, ui.Text(i18n.Text("workflow_draft.undo")))),
		html.Span(html.Props{Class: "workflow-draft-history-position", Aria: map[string]string{"live": "polite"}}, ui.Text(i18n.Text("workflow_draft.history_position", map[string]string{"position": fmt.Sprint(historyPosition), "length": fmt.Sprint(historyLength)}))),
		html.Button(redo, html.Span(html.Props{}, ui.Text(i18n.Text("workflow_draft.redo"))), productIcon("history-forward", "")),
	)
	return html.Section(html.Props{Class: "surface workflow-draft-workspace", Aria: map[string]string{"labelledby": "workflow-draft-title"}},
		html.Header(html.Props{Class: "workflow-draft-header"},
			html.Div(html.Props{},
				html.P(html.Props{Class: "eyebrow"}, ui.Text(i18n.Text("workflow_draft.eyebrow_version", map[string]string{"version": draft.SemanticVersion}))),
				html.H3(html.Props{ID: "workflow-draft-title", Dir: "auto"}, ui.Text(draft.Name)),
				html.P(html.Props{Class: "muted"}, ui.Text(i18n.Text("workflow_draft.identity", map[string]string{"id": draft.WorkflowID}))),
			),
			html.Div(html.Props{Class: "workflow-draft-header-actions"},
				html.Span(html.Props{Class: "status-chip", Data: map[string]string{"tone": "positive"}, Raw: map[string]any{"role": "status"}}, ui.Text(i18n.Text("workflow_draft.saved_revision", map[string]string{"revision": fmt.Sprint(draft.Revision)}))),
				history,
			),
		),
		workflowDraftChanges(i18n, draft),
		html.Div(html.Props{Class: "workflow-draft-groups"}, body...),
	)
}

func workflowDraftChanges(i18n I18nProps, draft WorkflowDraftView) ui.Node {
	items := make([]ui.Node, 0, len(draft.Changes))
	for _, change := range draft.Changes {
		subject := DisplayLabel(change.SubjectID)
		field := strings.ReplaceAll(change.Field, "_", " ")
		copy := i18n.Text("workflow_draft.change_summary", map[string]string{
			"operation": displayWorkflowToken(WorkflowViewerProps{I18nProps: i18n}, "change", change.Operation),
			"kind":      displayWorkflowToken(WorkflowViewerProps{I18nProps: i18n}, "change", change.Kind),
			"subject":   subject, "field": field,
		})
		items = append(items, html.Li(html.Props{Class: "workflow-draft-change-row"},
			html.Span(html.Props{Class: "status-chip", Data: map[string]string{"tone": workflowChangeTone(change.Operation)}}, ui.Text(displayWorkflowToken(WorkflowViewerProps{I18nProps: i18n}, "change", change.Operation))),
			html.Span(html.Props{Dir: "auto"}, ui.Text(copy)),
		))
	}
	if len(items) == 0 {
		items = append(items, html.Li(html.Props{Class: "workflow-draft-change-empty muted"}, ui.Text(i18n.Text("workflow_draft.no_changes"))))
	}
	label := strings.TrimSpace(draft.HistoryLabel)
	if label == "" {
		label = i18n.Text("workflow_draft.history_default")
	}
	return html.Details(html.Props{Class: "workflow-draft-changes", Raw: map[string]any{"open": ""}},
		html.Summary(html.Props{},
			html.Span(html.Props{}, html.Strong(html.Props{}, ui.Text(i18n.Text("workflow_draft.changes_title"))), html.Small(html.Props{Class: "muted", Dir: "auto"}, ui.Text(label))),
			html.Span(html.Props{Class: "count-badge"}, ui.Text(fmt.Sprint(len(draft.Changes)))),
		),
		html.Ul(html.Props{Class: "workflow-draft-change-list", Raw: map[string]any{"role": "list"}}, items...),
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

func workflowDraftTopology(i18n I18nProps, draft WorkflowDraftView, projection workflowview.View, selectedNodeID string, onSelectNode func(string), onMoveNode func(WorkflowNodeMove)) ui.Node {
	parity, tone, copyKey := "new", "neutral", "workflow_draft.parity_new"
	if strings.TrimSpace(draft.TemplateDefinitionDigest) != "" {
		parity, tone, copyKey = "changed", "warning", "workflow_draft.parity_template_changed"
		if draft.MatchesTemplateDefinition {
			parity, tone, copyKey = "exact", "positive", "workflow_draft.parity_template_exact"
		}
	} else if strings.TrimSpace(draft.BaseDefinitionDigest) != "" {
		parity, tone, copyKey = "changed", "warning", "workflow_draft.parity_changed"
		if draft.MatchesBaseDefinition {
			parity, tone, copyKey = "exact", "positive", "workflow_draft.parity_exact"
		}
	}
	authoringOrder := make([]string, 0, len(draft.Nodes))
	for _, node := range draft.Nodes {
		authoringOrder = append(authoringOrder, node.ID)
	}
	viewerProps := WorkflowViewerProps{I18nProps: i18n, ID: "workflow-draft-topology", Projection: projection, SelectedNodeID: selectedNodeID, AuthoringOrder: authoringOrder, OnSelectNode: onSelectNode, OnMoveNode: onMoveNode}
	topologySummaryKey := "workflow_draft.topology_summary"
	if len(draft.Nodes) == 1 {
		topologySummaryKey = "workflow_draft.topology_summary_one"
	}
	layoutMode := strings.ToLower(strings.TrimSpace(draft.LayoutMode))
	if layoutMode == "" {
		layoutMode = "auto"
	}
	return html.Section(html.Props{Class: "workflow-draft-topology", Data: map[string]string{"parity": parity, "layout-mode": layoutMode}, Aria: map[string]string{"labelledby": "workflow-draft-topology-title"}},
		html.Div(html.Props{Class: "workflow-draft-topology-heading"},
			html.Div(html.Props{},
				html.H4(html.Props{ID: "workflow-draft-topology-title"}, ui.Text(i18n.Text("workflow_draft.topology_title"))),
				html.P(html.Props{Class: "muted"}, ui.Text(i18n.Text(topologySummaryKey, map[string]string{"nodes": fmt.Sprint(len(draft.Nodes)), "routes": fmt.Sprint(len(draft.Edges))}))),
			),
			html.Div(html.Props{Class: "workflow-draft-topology-badges"},
				html.Span(html.Props{Class: "status-chip", Data: map[string]string{"tone": "info"}}, ui.Text(i18n.Text("workflow_draft.auto_layout"))),
				html.Span(html.Props{Class: "status-chip", Data: map[string]string{"tone": tone}}, ui.Text(i18n.Text(copyKey))),
			),
		),
		html.Div(html.Props{Class: "workflow-viewer-layout workflow-draft-topology-layout"},
			html.Section(html.Props{Class: "workflow-viewer-graph", Aria: map[string]string{"label": i18n.Text("workflow_outline_editor.graph_label")}},
				html.H3(html.Props{}, ui.Text(i18n.Text("workflow_viewer.graph_title"))),
				workflowViewerGraph(viewerProps, projection),
			),
			html.Section(html.Props{Class: "workflow-viewer-outline workflow-outline-editor", Aria: map[string]string{"label": i18n.Text("workflow_outline_editor.title")}},
				html.Div(html.Props{Class: "workflow-viewer-outline-heading"},
					html.H3(html.Props{}, ui.Text(i18n.Text("workflow_outline_editor.title"))),
					html.P(html.Props{}, ui.Text(i18n.Text("workflow_outline_editor.description"))),
				),
				workflowOutlineEditor(i18n, draft, projection, selectedNodeID, onSelectNode, onMoveNode),
			),
		),
	)
}

// workflowOutlineEditor is the semantic, keyboard-first peer of the visual
// graph. It emits only shared edit commands; it never mutates a local copy of
// the graph or infers execution order from list position.
func workflowOutlineEditor(i18n I18nProps, draft WorkflowDraftView, projection workflowview.View, selectedNodeID string, onSelectNode func(string), onMoveNode func(WorkflowNodeMove)) ui.Node {
	byID := make(map[string]workflowview.Node, len(projection.Nodes))
	for _, node := range projection.Nodes {
		byID[node.ID] = node
	}
	items := make([]ui.Node, 0, len(draft.Nodes))
	for index, draftNode := range draft.Nodes {
		node := byID[draftNode.ID]
		label := DisplayLabel(draftNode.ID)
		if strings.TrimSpace(draftNode.Label) != "" {
			label = draftNode.Label
		}
		selectProps := html.Props{Class: "workflow-outline-select", Type: "button"}
		if draftNode.ID == selectedNodeID {
			selectProps.Raw = map[string]any{"aria-current": "step"}
			selectProps.Class += " active"
		}
		if onSelectNode == nil {
			selectProps.Disabled = true
		} else {
			nodeID := draftNode.ID
			selectProps.OnClick = ui.UseEvent(func(ui.MouseEvent) { onSelectNode(nodeID) })
		}
		earlier := html.Props{Class: "button ghost compact icon-button", Type: "button", Disabled: index == 0 || onMoveNode == nil, Title: i18n.Text("workflow_outline_editor.move_earlier", map[string]string{"name": label}), Aria: map[string]string{"label": i18n.Text("workflow_outline_editor.move_earlier", map[string]string{"name": label})}}
		later := html.Props{Class: "button ghost compact icon-button", Type: "button", Disabled: index == len(draft.Nodes)-1 || onMoveNode == nil, Title: i18n.Text("workflow_outline_editor.move_later", map[string]string{"name": label}), Aria: map[string]string{"label": i18n.Text("workflow_outline_editor.move_later", map[string]string{"name": label})}}
		if onMoveNode != nil {
			nodeID := draftNode.ID
			earlier.OnClick = ui.UseEvent(func(ui.MouseEvent) { onMoveNode(WorkflowNodeMove{NodeID: nodeID, Direction: "EARLIER"}) })
			later.OnClick = ui.UseEvent(func(ui.MouseEvent) { onMoveNode(WorkflowNodeMove{NodeID: nodeID, Direction: "LATER"}) })
		}
		routes := make([]ui.Node, 0, len(node.Routes))
		for _, route := range node.Routes {
			routes = append(routes, html.Li(html.Props{},
				html.Code(html.Props{}, ui.Text(route.Key)),
				html.Span(html.Props{Aria: map[string]string{"hidden": "true"}}, ui.Text("›")),
				html.Span(html.Props{Dir: "auto"}, ui.Text(DisplayLabel(route.TargetID))),
			))
		}
		items = append(items, html.Li(html.Props{Class: "workflow-outline-edit-row", Data: map[string]string{"node-id": draftNode.ID}},
			html.Div(html.Props{Class: "workflow-outline-edit-main"},
				html.Button(selectProps, html.Span(html.Props{Class: "workflow-outline-step-number", Aria: map[string]string{"hidden": "true"}}, ui.Text(fmt.Sprint(index+1))), html.Span(html.Props{}, html.Strong(html.Props{Dir: "auto"}, ui.Text(label)), html.Small(html.Props{Class: "muted"}, ui.Text(displayWorkflowToken(WorkflowViewerProps{I18nProps: i18n}, "step", draftNode.StepType))))),
				html.Div(html.Props{Class: "workflow-outline-move-actions"}, html.Button(earlier, productIcon("move-up", "")), html.Button(later, productIcon("move-down", ""))),
			),
			html.Ul(html.Props{Class: "workflow-outline-edit-routes", Raw: map[string]any{"role": "list"}}, routes...),
		))
	}
	return html.Ol(html.Props{Class: "workflow-outline-edit-list"}, items...)
}

// workflowDraftGraphProjection derives presentation-only depth, lanes and
// routes from the exact durable draft graph. It never invents workflow
// behavior: every rendered route comes from draft.Edges and every card comes
// from draft.Nodes.
func workflowDraftGraphProjection(draft WorkflowDraftView) workflowview.View {
	result := workflowview.View{
		WorkflowID: draft.WorkflowID, Name: draft.Name, SemanticVersion: draft.SemanticVersion,
		PublicationStatus: "DRAFT", Completeness: true,
		Edges: make([]workflowview.Edge, 0, len(draft.Edges)),
	}
	if len(draft.Nodes) == 0 {
		return result
	}
	byID := make(map[string]WorkflowDraftNode, len(draft.Nodes))
	position := make(map[string]int, len(draft.Nodes))
	outgoing := make(map[string][]WorkflowDraftEdge, len(draft.Nodes))
	for index, node := range draft.Nodes {
		byID[node.ID], position[node.ID] = node, index
	}
	for index, edge := range draft.Edges {
		if _, fromOK := byID[edge.FromID]; !fromOK {
			continue
		}
		if _, toOK := byID[edge.ToID]; !toOK {
			continue
		}
		outgoing[edge.FromID] = append(outgoing[edge.FromID], edge)
		result.Edges = append(result.Edges, workflowview.Edge{ID: fmt.Sprintf("draft-edge-%03d", index), FromID: edge.FromID, ToID: edge.ToID, RouteKey: edge.RouteKey})
	}
	for id := range outgoing {
		sort.SliceStable(outgoing[id], func(i, j int) bool {
			if outgoing[id][i].RouteKey == outgoing[id][j].RouteKey {
				return outgoing[id][i].ToID < outgoing[id][j].ToID
			}
			return outgoing[id][i].RouteKey < outgoing[id][j].RouteKey
		})
	}
	start := strings.TrimSpace(draft.StartNodeID)
	if _, ok := byID[start]; !ok {
		start = draft.Nodes[0].ID
	}
	queue := []string{start}
	depths := map[string]int{start: 0}
	ordered := make([]string, 0, len(draft.Nodes))
	discovered := map[string]bool{start: true}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		ordered = append(ordered, id)
		for _, edge := range outgoing[id] {
			if !discovered[edge.ToID] {
				discovered[edge.ToID] = true
				depths[edge.ToID] = depths[id] + 1
				queue = append(queue, edge.ToID)
			}
		}
	}
	for _, node := range draft.Nodes {
		if !discovered[node.ID] {
			ordered = append(ordered, node.ID)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if depths[ordered[i]] == depths[ordered[j]] {
			return position[ordered[i]] < position[ordered[j]]
		}
		return depths[ordered[i]] < depths[ordered[j]]
	})
	lanes := make(map[int]int)
	for _, id := range ordered {
		node, depth := byID[id], depths[id]
		lane := lanes[depth]
		lanes[depth]++
		routes := make([]workflowview.Route, 0, len(outgoing[id]))
		for _, edge := range outgoing[id] {
			routes = append(routes, workflowview.Route{Key: edge.RouteKey, TargetID: edge.ToID})
		}
		result.Nodes = append(result.Nodes, workflowview.Node{
			ID: id, Label: DisplayLabel(id), StepType: node.StepType, Depth: depth, Lane: lane,
			Start: id == start, Terminal: strings.EqualFold(node.StepType, "END"), State: workflowview.NodeNotStarted, Routes: routes,
		})
		if depth > result.MaxDepth {
			result.MaxDepth = depth
		}
		if lane > result.MaxLane {
			result.MaxLane = lane
		}
	}
	return result
}

func workflowDraftNodeRow(i18n I18nProps, node WorkflowDraftNode) ui.Node {
	return html.Li(html.Props{Class: "workflow-draft-node", Data: map[string]string{"node-id": node.ID}},
		html.Span(html.Props{Class: "workflow-draft-node-icon", Aria: map[string]string{"hidden": "true"}}, productIcon("workflow", "")),
		html.Span(html.Props{},
			html.Strong(html.Props{Dir: "auto"}, ui.Text(DisplayLabel(node.ID))),
			html.Span(html.Props{Class: "muted"}, ui.Text(displayWorkflowToken(WorkflowViewerProps{I18nProps: i18n}, "step", node.StepType))),
		),
	)
}

func workflowDesignerHref(base, workflowID, runID string) string {
	parsed, err := url.Parse(base)
	if err != nil {
		return base
	}
	values := parsed.Query()
	values.Set("workflow", strings.TrimSpace(workflowID))
	values.Del("run")
	if strings.TrimSpace(runID) != "" {
		values.Set("run", strings.TrimSpace(runID))
	}
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

func workflowPublicationTone(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "ACTIVE":
		return "positive"
	case "QUARANTINED":
		return "warning"
	case "RETIRED":
		return "neutral"
	default:
		return "info"
	}
}

func workflowDesignerStylesheet() string {
	return `.workflow-designer-page{display:grid;gap:var(--hcm-space-3)}
.workflow-designer-header{display:flex;align-items:flex-start;justify-content:space-between;gap:var(--hcm-space-2)}
.workflow-designer-status:empty{display:none}.workflow-designer-status{margin:0;padding:var(--hcm-space-2);border-radius:var(--hcm-radius-control);background:var(--soft)}
.workflow-designer-header h2{margin:0}.workflow-designer-header .muted{max-width:68ch;margin-block-end:0}
.workflow-designer-header .button{display:inline-flex;align-items:center;gap:var(--hcm-space-1);flex:none}.workflow-designer-header .button-icon{inline-size:1rem;block-size:1rem;flex:none}
.workflow-designer-layout{display:grid;grid-template-columns:minmax(15rem,19rem) minmax(0,1fr) minmax(15rem,18rem);gap:var(--hcm-space-3);align-items:start}
.workflow-catalog{padding:var(--hcm-space-2);position:sticky;inset-block-start:var(--hcm-space-2);max-block-size:calc(100dvh - var(--hcm-space-4));overflow:auto}
.workflow-catalog-heading{display:flex;align-items:center;justify-content:space-between;gap:var(--hcm-space-1)}
.workflow-catalog-heading h3{margin:0}.workflow-catalog-description{margin-block:var(--hcm-space-1) var(--hcm-space-2)}
.workflow-catalog-list{list-style:none;margin:0;padding:0;display:grid;gap:var(--hcm-space-1)}
.workflow-catalog-link{display:grid;gap:var(--hcm-space-1);padding:var(--hcm-space-2);border:1px solid transparent;border-radius:var(--hcm-radius-control);color:var(--ink);text-decoration:none;transition:background-color var(--hcm-motion-fast) var(--hcm-motion-easing),border-color var(--hcm-motion-fast) var(--hcm-motion-easing),transform var(--hcm-motion-fast) var(--hcm-motion-easing)}
.workflow-catalog-link:hover,.workflow-catalog-link:focus-visible{background:var(--hcm-hover-surface,var(--soft));border-color:var(--control-border,var(--line))}
.workflow-catalog-link:active{transform:translateY(1px)}.workflow-catalog-link.active{background:var(--soft);border-color:var(--accent)}
.workflow-catalog-title{font-weight:650}.workflow-catalog-meta{display:flex;align-items:center;justify-content:space-between;gap:var(--hcm-space-1);font-size:var(--hcm-font-size-small);color:var(--muted)}
.workflow-catalog-empty{padding-block:var(--hcm-space-2)}.workflow-catalog-empty p{margin-block-end:0}
.workflow-designer-workspace{min-width:0}.workflow-designer-empty{min-block-size:24rem;display:grid;place-content:center;text-align:center;padding:var(--hcm-space-4)}
.workflow-designer-empty h2{margin-block:var(--hcm-space-2) var(--hcm-space-1)}.workflow-designer-empty p{max-width:46ch;margin:0 auto}
.workflow-designer-empty-icon{inline-size:3rem;block-size:3rem;margin:auto;display:grid;place-items:center;border-radius:var(--hcm-radius-control);background:var(--soft);color:var(--accent)}
.workflow-designer-empty-icon svg{inline-size:1.5rem;block-size:1.5rem}
.workflow-published-workspace{display:grid;gap:var(--hcm-space-2)}.workflow-published-version{padding:var(--hcm-space-3);display:flex;align-items:end;justify-content:space-between;gap:var(--hcm-space-3);border-inline-start:3px solid var(--accent)}
.workflow-published-version h3,.workflow-published-version p{margin:0}.workflow-published-version .muted{margin-block-start:var(--hcm-space-1);max-width:60ch}.workflow-version-create{display:flex;align-items:end;gap:var(--hcm-space-2);min-width:min(100%,22rem)}.workflow-version-create .labeled-control{flex:1}.workflow-version-create .button{display:inline-flex;align-items:center;gap:var(--hcm-space-1);white-space:nowrap}
.workflow-draft-workspace{padding:var(--hcm-space-3);display:grid;gap:var(--hcm-space-3);min-width:0}.workflow-draft-header{display:flex;align-items:flex-start;justify-content:space-between;gap:var(--hcm-space-2)}
.workflow-draft-header h3,.workflow-draft-header p{margin:0}.workflow-draft-header .muted{margin-block-start:var(--hcm-space-1)}.workflow-draft-groups{display:grid;gap:var(--hcm-space-2)}
.workflow-draft-header-actions{display:grid;justify-items:end;gap:var(--hcm-space-1)}.workflow-draft-history-controls{display:flex;align-items:center;gap:.25rem;padding:.25rem;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--soft)}.workflow-draft-history-controls .button{display:inline-flex;align-items:center;gap:.35rem}.workflow-draft-history-controls svg{inline-size:1rem;block-size:1rem}.workflow-draft-history-position{padding-inline:.35rem;color:var(--muted);font-size:var(--hcm-font-size-small);font-variant-numeric:tabular-nums;white-space:nowrap}.workflow-draft-changes{border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface)}.workflow-draft-changes>summary{display:flex;align-items:center;justify-content:space-between;gap:var(--hcm-space-2);padding:var(--hcm-space-2);cursor:pointer}.workflow-draft-changes>summary>span:first-child{display:grid;gap:.125rem}.workflow-draft-change-list{list-style:none;margin:0;padding:0 var(--hcm-space-2) var(--hcm-space-2);display:grid;gap:var(--hcm-space-1)}.workflow-draft-change-row{display:grid;grid-template-columns:auto minmax(0,1fr);align-items:center;gap:var(--hcm-space-1);padding:var(--hcm-space-1);border-block-start:1px solid var(--line)}.workflow-draft-change-empty{padding:var(--hcm-space-1);border-block-start:1px solid var(--line)}
.workflow-draft-topology{display:grid;gap:var(--hcm-space-3);padding-block-end:var(--hcm-space-3);border-block-end:1px solid var(--line)}.workflow-draft-topology-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:var(--hcm-space-2)}.workflow-draft-topology-heading h4,.workflow-draft-topology-heading p{margin:0}.workflow-draft-topology-heading .muted{margin-block-start:var(--hcm-space-1)}.workflow-draft-topology-layout{grid-template-columns:minmax(0,1.65fr) minmax(16rem,.75fr)}
.workflow-draft-topology-badges{display:flex;flex-wrap:wrap;justify-content:flex-end;gap:var(--hcm-space-1)}
.workflow-draft-group{border:1px solid var(--control-border,var(--line));border-radius:var(--hcm-radius-control);background:var(--surface)}.workflow-draft-group>summary{display:flex;justify-content:space-between;gap:var(--hcm-space-2);align-items:center;padding:var(--hcm-space-2);cursor:pointer;font-weight:650}
.workflow-draft-node-list{list-style:none;margin:0;padding:0;display:grid;gap:var(--hcm-space-1)}.workflow-draft-group .workflow-draft-node-list{padding:0 var(--hcm-space-2) var(--hcm-space-2)}
.workflow-draft-node{display:flex;align-items:center;gap:var(--hcm-space-2);padding:var(--hcm-space-2);border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--soft)}.workflow-draft-node>span:last-child{display:grid;gap:.125rem;min-width:0}.workflow-draft-node-icon{display:grid;place-items:center;inline-size:2rem;block-size:2rem;border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--accent);flex:none}.workflow-draft-node-icon svg{inline-size:1rem;block-size:1rem}.workflow-draft-empty{padding:var(--hcm-space-4);text-align:center}.workflow-draft-empty p{margin-block-end:0}
.workflow-outline-edit-list{list-style:none;margin:0;padding:0;display:grid;gap:var(--hcm-space-1)}.workflow-outline-edit-row{display:grid;gap:.375rem;padding:var(--hcm-space-1);border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface)}.workflow-outline-edit-main{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:var(--hcm-space-1);align-items:center}.workflow-outline-select{display:grid;grid-template-columns:1.75rem minmax(0,1fr);align-items:center;gap:var(--hcm-space-1);inline-size:100%;padding:var(--hcm-space-1);border:1px solid transparent;border-radius:var(--hcm-radius-control);background:transparent;color:var(--ink);text-align:start;cursor:pointer}.workflow-outline-select>span:last-child{display:grid;min-width:0}.workflow-outline-select:hover,.workflow-outline-select:focus-visible{background:var(--hcm-hover-surface,var(--soft));border-color:var(--control-border,var(--line))}.workflow-outline-select.active{background:var(--soft);border-color:var(--accent)}.workflow-outline-step-number{display:grid;place-items:center;inline-size:1.75rem;block-size:1.75rem;border-radius:999px;background:var(--soft);color:var(--muted);font-size:var(--hcm-font-size-small);font-variant-numeric:tabular-nums}.workflow-outline-move-actions{display:flex;gap:.125rem}.workflow-outline-move-actions .icon-button{inline-size:2rem;block-size:2rem;padding:.375rem}.workflow-outline-move-actions svg{inline-size:1rem;block-size:1rem}.workflow-outline-edit-routes{list-style:none;margin:0;padding:0;padding-inline-start:2.75rem;display:grid;gap:.125rem;color:var(--muted);font-size:var(--hcm-font-size-small)}.workflow-outline-edit-routes li{display:flex;align-items:baseline;gap:.35rem;min-width:0}.workflow-outline-edit-routes:empty{display:none}
.workflow-viewer-node-authoring{display:flex;align-items:center;gap:var(--hcm-space-1);padding-block-start:var(--hcm-space-1);border-block-start:1px solid var(--line)}.workflow-viewer-node-authoring .button:first-child{margin-inline-end:auto}.workflow-viewer-node-authoring .icon-button{inline-size:2rem;block-size:2rem;padding:.375rem}.workflow-viewer-node-authoring svg{inline-size:1rem;block-size:1rem}.workflow-viewer-node.selected{outline:2px solid var(--accent);outline-offset:2px}
.workflow-node-inspector{display:grid;gap:var(--hcm-space-2);padding:var(--hcm-space-3);border:1px solid var(--line);border-radius:var(--hcm-radius-surface);background:var(--soft);min-width:0}.workflow-node-inspector-heading{display:flex;align-items:end;justify-content:space-between;gap:var(--hcm-space-2)}.workflow-node-inspector-heading h4,.workflow-node-inspector-heading p,.workflow-inspector-panel h5{margin:0}.workflow-node-inspector .labeled-control{display:grid;gap:var(--hcm-space-1);min-width:0}.workflow-node-inspector .labeled-control>:where(input,select,textarea){inline-size:100%;min-width:0}.workflow-node-inspector-heading .labeled-control{min-width:min(100%,18rem)}.workflow-node-inspector-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:var(--hcm-space-2);min-width:0}.workflow-inspector-panel{display:grid;align-content:start;gap:var(--hcm-space-2);padding:var(--hcm-space-2);border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);min-width:0}.workflow-link-editor-panel{grid-column:1/-1}.workflow-inspector-help{margin:0}.workflow-node-parameter-form{display:grid;gap:var(--hcm-space-2);min-width:0}.workflow-node-parameter-form .button{justify-self:start}.workflow-link-editor{display:grid;gap:var(--hcm-space-2);min-width:0}.workflow-link-editor-row{display:grid;grid-template-columns:minmax(8rem,.8fr) minmax(12rem,1.4fr) auto;align-items:end;gap:var(--hcm-space-2);padding-block-start:var(--hcm-space-2);border-block-start:1px solid var(--line);min-width:0}.workflow-link-editor-row>*{min-width:0}.workflow-link-editor-row:first-child{padding-block-start:0;border-block-start:0}.workflow-link-editor-copy{display:flex;flex-wrap:wrap;align-items:center;gap:.25rem var(--hcm-space-1);min-width:0}.workflow-link-editor-copy>.muted{flex-basis:100%;font-size:var(--hcm-font-size-small)}.workflow-link-editor-row .button{white-space:nowrap}.workflow-link-editor-empty{margin:0}.workflow-node-lock{display:flex;align-items:flex-start;gap:var(--hcm-space-1);padding:var(--hcm-space-2);border:1px solid var(--hcm-color-warning);border-radius:var(--hcm-radius-control);background:var(--hcm-color-warning-surface)}.workflow-node-lock svg{inline-size:1rem;block-size:1rem;flex:none}.workflow-overlay-actions{display:flex;flex-wrap:wrap;gap:var(--hcm-space-1)}.workflow-overlay-history>summary{cursor:pointer;font-weight:650}.workflow-overlay-history ol{display:grid;gap:var(--hcm-space-1);padding-inline-start:1.5rem}.workflow-overlay-history li{display:grid;gap:.125rem}
@media (max-width:1440px){.workflow-designer-layout{grid-template-columns:minmax(15rem,19rem) minmax(0,1fr)}.workflow-catalog{position:static;max-block-size:none}.workflow-palette{grid-column:1/-1}.workflow-palette-groups{grid-template-columns:repeat(auto-fit,minmax(16rem,1fr))}.workflow-published-version{align-items:stretch;flex-direction:column}.workflow-version-create{min-width:0;max-width:30rem}.workflow-draft-topology-layout{grid-template-columns:1fr}}
@media (max-width:900px){.workflow-designer-layout{grid-template-columns:1fr}.workflow-catalog{position:static;max-block-size:none}.workflow-catalog-list{grid-template-columns:repeat(auto-fit,minmax(13rem,1fr))}.workflow-palette{grid-column:auto}.workflow-draft-topology-layout{grid-template-columns:1fr}}
@media (max-width:760px){.workflow-link-editor-row{grid-template-columns:1fr}.workflow-link-editor-row .button{justify-self:start}}@media (max-width:640px){.workflow-designer-header{flex-direction:column}.workflow-designer-header .button{align-self:flex-start}.workflow-catalog-list{grid-template-columns:1fr}.workflow-designer-empty{min-block-size:18rem;padding:var(--hcm-space-3)}.workflow-draft-header,.workflow-draft-topology-heading,.workflow-node-inspector-heading{flex-direction:column;align-items:stretch}.workflow-draft-header-actions{justify-items:stretch}.workflow-draft-history-controls{justify-content:space-between}.workflow-node-inspector-grid{grid-template-columns:1fr}.workflow-outline-edit-routes{padding-inline-start:var(--hcm-space-2)}}@media (max-width:420px){.workflow-outline-edit-main{grid-template-columns:1fr}.workflow-outline-move-actions{justify-content:flex-end}.workflow-outline-select strong{overflow-wrap:normal;word-break:normal}.workflow-draft-history-controls .button span{position:absolute;inline-size:1px;block-size:1px;overflow:hidden;clip-path:inset(50%)}.workflow-draft-change-row{grid-template-columns:1fr}}`
}
