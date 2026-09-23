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
	OnClearOutcome func(WorkflowOutcomeChange)
	OnInsertAfter  func(WorkflowPaletteItem, WorkflowOutcomeChange)
	OnSetOutcomes  func([]WorkflowOutcomeChange)
	OnRemoveNode   func(string)
	OnRename       func(string)
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

// WorkflowDesignerPage is two surfaces behind one route. With no draft open
// it is the library of published workflows; with a draft open it is the
// editor and nothing else. The two were previously stacked on one page, which
// put the step library a screen below the steps it adds to.
func WorkflowDesignerPage(props WorkflowDesignerPageProps) ui.Node {
	status := html.P(html.Props{Key: "status", ID: "workflow-designer-status", Class: "workflow-designer-status", Raw: map[string]any{"role": "status", "aria-live": "polite"}}, ui.Text(""))
	if props.Draft != nil {
		return html.Section(html.Props{Class: "workflow-designer-page editing", Aria: map[string]string{"labelledby": "workflow-draft-title"}},
			html.WithKey(ui.CreateElement(WorkflowEditor, WorkflowEditorProps{
				I18nProps: props.I18nProps, Draft: *props.Draft, Palette: props.Palette, SelectedNodeID: props.SelectedNodeID, Status: status,
				OnInsertAfter: workflowEditorInsertAfter(props), OnSetOutcomes: props.OnSetOutcomes,
				CatalogHref: workflowDesignerHref(props.BaseHref, "", ""), Navigate: props.Navigate, OnSelectNode: props.OnSelectNode,
				OnInsert: workflowEditorInsert(props), OnRename: props.OnRename, OnUpdateNode: props.OnUpdateNode,
				OnSetOutcome: props.OnSetOutcome, OnClearOutcome: props.OnClearOutcome, OnBindInput: props.OnBindInput,
				OnRemoveNode: props.OnRemoveNode, OnOverlay: props.OnOverlay, OnHistory: props.OnHistory,
			}), "editor-"+props.Draft.DraftID),
		)
	}
	return workflowCatalogPage(props, status)
}

// workflowEditorInsert routes a library choice to the command that fits the
// draft. A draft that came from a governed template records an addition as a
// template change; any other draft simply gains the step.
func workflowEditorInsert(props WorkflowDesignerPageProps) func(WorkflowPaletteItem) {
	if props.OnInsert == nil && props.OnOverlay == nil && props.OnCreate == nil {
		return nil
	}
	return func(item WorkflowPaletteItem) {
		switch {
		case strings.EqualFold(item.Kind, "TEMPLATE"):
			if props.OnCreate != nil {
				selected := item
				props.OnCreate(WorkflowDraftCreateRequest{Template: &selected})
			}
		case props.Draft != nil && props.Draft.TemplateID != "" && props.OnOverlay != nil:
			props.OnOverlay(WorkflowTemplateOverlayChange{Operation: "ADD", EntryID: item.ID, EntryVersion: item.Version})
		case props.OnInsert != nil:
			props.OnInsert(item)
		}
	}
}

// workflowEditorInsertAfter is offered only for a plain draft. A governed
// template records each addition as a template change, which has its own
// command and no place to say "and connect it".
func workflowEditorInsertAfter(props WorkflowDesignerPageProps) func(WorkflowPaletteItem, WorkflowOutcomeChange) {
	if props.OnInsertAfter == nil || props.Draft == nil || props.Draft.TemplateID != "" {
		return nil
	}
	return props.OnInsertAfter
}

func workflowCatalogPage(props WorkflowDesignerPageProps, status ui.Node) ui.Node {
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
	if props.Selected != nil {
		workspace = html.Div(html.Props{Class: "workflow-published-workspace"},
			workflowPublishedVersionActions(props, *props.Selected),
			WorkflowViewer(WorkflowViewerProps{
				I18nProps:  props.I18nProps,
				ID:         "workflow-designer-viewer",
				Projection: *props.Selected,
			}),
		)
	}

	actions := make([]ui.Node, 0, 2)
	if props.CanCreate {
		actions = append(actions, html.WithKey(ui.CreateElement(workflowStartButton, workflowStartButtonProps{
			I18nProps: props.I18nProps, Label: props.Text("workflow_designer.create"), Primary: true, OnCreate: props.OnCreate,
			Request: WorkflowDraftCreateRequest{SemanticVersion: "0.1.0"},
		}), "start-blank"))
		for _, item := range props.Palette {
			if !strings.EqualFold(item.Kind, "TEMPLATE") {
				continue
			}
			template := item
			actions = append(actions, html.WithKey(ui.CreateElement(workflowStartButton, workflowStartButtonProps{
				I18nProps: props.I18nProps, Label: props.Text("workflow_palette.add_template", map[string]string{"name": item.Name}), OnCreate: props.OnCreate,
				Request: WorkflowDraftCreateRequest{Template: &template},
			}), "start-"+item.ID))
		}
	}

	return html.Section(html.Props{Class: "workflow-designer-page", Aria: map[string]string{"labelledby": "workflow-designer-heading"}},
		html.Header(html.Props{Class: "workflow-designer-header"},
			// The page head above already says what this page is. Repeating it
			// here in other words pushed the workflows a third of a screen down.
			html.H2(html.Props{ID: "workflow-designer-heading", Class: "sr-only"}, ui.Text(props.Text("workflow_designer.heading"))),
			html.Div(html.Props{Class: "workflow-designer-actions"}, actions...),
		),
		status,
		html.Div(html.Props{Class: "workflow-designer-layout"},
			html.Aside(html.Props{Class: "workflow-catalog", Aria: map[string]string{"labelledby": "workflow-catalog-heading"}},
				html.Div(html.Props{Class: "workflow-catalog-heading"},
					html.H3(html.Props{ID: "workflow-catalog-heading"}, ui.Text(props.Text("workflow_designer.catalog_title"))),
					html.Span(html.Props{Class: "count-badge"}, ui.Text(fmt.Sprintf("%d", len(catalog)))),
				),
				catalogBody,
			),
			html.Div(html.Props{Class: "workflow-designer-workspace"}, workspace),
		),
	)
}

type workflowStartButtonProps struct {
	I18nProps
	Label    string
	Primary  bool
	Request  WorkflowDraftCreateRequest
	OnCreate func(WorkflowDraftCreateRequest)
}

// workflowStartButton opens a new draft, blank or from a governed template.
// It keeps the workflow-draft-create class because the host fences exactly
// those buttons while a draft is being created.
func workflowStartButton(props workflowStartButtonProps) ui.Node {
	click := ui.UseEvent(func(ui.MouseEvent) {
		if props.OnCreate != nil {
			props.OnCreate(props.Request)
		}
	})
	class := "button secondary compact workflow-draft-create"
	if props.Primary {
		class = "button primary compact workflow-draft-create"
	}
	button := html.Props{Class: class, Type: "button", OnClick: click}
	if props.OnCreate == nil {
		button.Disabled = true
		button.Title = props.Text("workflow_designer.create_unavailable")
	}
	return html.Button(button, productIcon("plus", "button-icon"), ui.Text(props.Label))
}

func workflowPublishedVersionActions(props WorkflowDesignerPageProps, selected workflowview.View) ui.Node {
	return html.WithKey(ui.CreateElement(workflowVersionForm, workflowVersionFormProps{
		I18nProps: props.I18nProps, WorkflowID: selected.WorkflowID, Name: selected.Name, SemanticVersion: selected.SemanticVersion,
		PlanDigest: selected.PlanDigest, OnCreate: props.OnCreate,
	}), "version-"+selected.WorkflowID+"-"+selected.SemanticVersion)
}

type workflowVersionFormProps struct {
	I18nProps
	WorkflowID, Name, SemanticVersion, PlanDigest string
	OnCreate                                      func(WorkflowDraftCreateRequest)
}

// workflowVersionForm names the release a successor draft will become. The
// typed version is component state: it used to be a render-local variable,
// so any re-render put the suggested patch version back over the author's.
func workflowVersionForm(props workflowVersionFormProps) ui.Node {
	suggested := ""
	if value, err := workflowversion.NextPatchVersion(props.SemanticVersion); err == nil {
		suggested = value
	}
	version := ui.UseState(suggested)
	input := ui.UseEvent(func(event ui.InputEvent) { version.Set(strings.TrimSpace(event.GetValue())) })
	submit := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		if props.OnCreate != nil && strings.TrimSpace(version.Get()) != "" {
			props.OnCreate(WorkflowDraftCreateRequest{WorkflowID: props.WorkflowID, Name: props.Name, SemanticVersion: strings.TrimSpace(version.Get()), BaseVersionDigest: props.PlanDigest})
		}
	})
	field := html.Props{
		ID: "workflow-next-semantic-version", Name: "semantic_version", Type: "text", Value: version.Get(),
		Placeholder: "1.1.0", AutoComplete: "off", Required: true, OnInput: input,
		Pattern: `(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?`,
		Raw:     map[string]any{"spellcheck": "false"},
		Aria:    map[string]string{"describedby": "workflow-version-help"},
	}
	button := html.Props{Class: "button secondary compact", Type: "submit"}
	if props.OnCreate == nil {
		button.Disabled = true
		button.Title = props.Text("workflow_designer.create_unavailable")
	}
	return html.Section(html.Props{Class: "workflow-published-version", Aria: map[string]string{"labelledby": "workflow-published-version-title"}},
		html.Div(html.Props{},
			html.H3(html.Props{ID: "workflow-published-version-title"}, ui.Text(props.Text("workflow_version.published_title", map[string]string{"version": props.SemanticVersion}))),
			html.P(html.Props{ID: "workflow-version-help", Class: "muted"}, ui.Text(props.Text("workflow_version.published_help"))),
		),
		html.Form(html.Props{Class: "workflow-version-create", OnSubmit: submit},
			ui.CreateElement(LabeledControl, LabeledControlProps{For: field.ID, Label: props.Text("workflow_version.next_label"), Control: html.Input(field)}),
			html.Button(button, ui.Text(props.Text("workflow_version.create"))),
		),
	)
}

func workflowDesignerHref(base, workflowID, runID string) string {
	parsed, err := url.Parse(base)
	if err != nil {
		return base
	}
	values := parsed.Query()
	// A catalog address never carries a draft: following one always leaves
	// the editor.
	values.Del("draft")
	values.Del("node")
	values.Del("run")
	values.Del("workflow")
	if strings.TrimSpace(workflowID) != "" {
		values.Set("workflow", strings.TrimSpace(workflowID))
	}
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
.workflow-designer-header{display:flex;flex-wrap:wrap;align-items:flex-start;justify-content:flex-end;gap:var(--hcm-space-2)}
.workflow-designer-header h2{margin:0}.workflow-designer-header .muted{max-inline-size:60ch;margin-block:var(--hcm-space-1) 0}
.workflow-designer-actions{display:flex;flex-wrap:wrap;gap:var(--hcm-space-1)}
.workflow-designer-actions .button{display:inline-flex;align-items:center;gap:var(--hcm-space-1)}.workflow-designer-actions .button-icon{inline-size:1rem;block-size:1rem;flex:none}
.workflow-designer-layout{display:grid;grid-template-columns:minmax(14rem,18rem) minmax(0,1fr);gap:var(--hcm-space-4);align-items:start}
.workflow-catalog{position:sticky;inset-block-start:var(--hcm-space-2);max-block-size:calc(100dvh - var(--hcm-space-4));overflow:auto;display:grid;gap:var(--hcm-space-1);align-content:start}
.workflow-catalog-heading{display:flex;align-items:center;justify-content:space-between;gap:var(--hcm-space-1)}
.workflow-catalog-heading h3{margin:0;font-size:var(--hcm-font-size-small);font-weight:600;color:var(--muted)}
.workflow-catalog-list{list-style:none;margin:0;padding:0;display:grid;gap:.125rem}
.workflow-catalog-link{display:grid;gap:.125rem;padding:var(--hcm-space-1) var(--hcm-space-2);border:1px solid transparent;border-radius:var(--hcm-radius-control);color:var(--ink);text-decoration:none}
.workflow-catalog-link:hover,.workflow-catalog-link:focus-visible{background:var(--soft)}
.workflow-catalog-link.active{background:var(--soft);border-color:var(--accent)}
.workflow-catalog-title{font-weight:600}.workflow-catalog-meta{display:flex;align-items:center;justify-content:space-between;gap:var(--hcm-space-1);font-size:var(--hcm-font-size-small);color:var(--muted)}
.workflow-catalog-empty{padding-block:var(--hcm-space-2)}.workflow-catalog-empty p{margin-block-end:0}
.workflow-designer-workspace{min-inline-size:0}.workflow-designer-empty{min-block-size:24rem;display:grid;place-content:center;text-align:center;padding:var(--hcm-space-4)}
.workflow-designer-empty h2{margin-block:var(--hcm-space-2) var(--hcm-space-1)}.workflow-designer-empty p{max-inline-size:46ch;margin:0 auto}
.workflow-designer-empty-icon{inline-size:3rem;block-size:3rem;margin:auto;display:grid;place-items:center;border-radius:var(--hcm-radius-control);background:var(--soft);color:var(--accent)}
.workflow-designer-empty-icon svg{inline-size:1.5rem;block-size:1.5rem}
.workflow-published-workspace{display:grid;gap:var(--hcm-space-3)}
.workflow-published-version{display:flex;flex-wrap:wrap;align-items:end;justify-content:space-between;gap:var(--hcm-space-2) var(--hcm-space-3);padding-block-end:var(--hcm-space-3);border-block-end:1px solid var(--line)}
.workflow-published-version h3,.workflow-published-version p{margin:0}.workflow-published-version .muted{margin-block-start:var(--hcm-space-1);max-inline-size:60ch}
.workflow-version-create{display:flex;align-items:end;gap:var(--hcm-space-2);flex:0 1 24rem}.workflow-version-create .labeled-control{flex:1;display:grid;gap:.25rem}.workflow-version-create .button{white-space:nowrap}
@media (max-width:900px){.workflow-designer-layout{grid-template-columns:1fr}.workflow-catalog{position:static;max-block-size:none}.workflow-catalog-list{grid-template-columns:repeat(auto-fit,minmax(13rem,1fr))}}
@media (max-width:640px){.workflow-catalog-list{grid-template-columns:1fr}.workflow-designer-empty{min-block-size:18rem;padding:var(--hcm-space-3)}.workflow-version-create{flex-basis:100%}}`
}
