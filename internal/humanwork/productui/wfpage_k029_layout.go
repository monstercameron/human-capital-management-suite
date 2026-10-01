package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type WorkflowPageLayoutField struct {
	ID       string
	Label    string
	Required bool
	Hidden   bool
}

type WorkflowPageLayoutSection struct {
	ID      string
	Label   string
	Columns int
	Fields  []WorkflowPageLayoutField
}

type WorkflowPageLayout struct {
	WorkflowID      string
	WorkflowVersion uint32
	BaseDigest      string
	Sections        []WorkflowPageLayoutSection
}

type WorkflowPageLayoutInput struct {
	ID       string
	Label    string
	Required bool
}

type WorkflowPageLayoutEditKind string

const (
	WorkflowPageMove   WorkflowPageLayoutEditKind = "MOVE"
	WorkflowPageGroup  WorkflowPageLayoutEditKind = "GROUP"
	WorkflowPageRename WorkflowPageLayoutEditKind = "RENAME"
	WorkflowPageHide   WorkflowPageLayoutEditKind = "HIDE"
)

type WorkflowPageMoveDirection string

const (
	WorkflowPageMoveEarlier WorkflowPageMoveDirection = "earlier"
	WorkflowPageMoveLater   WorkflowPageMoveDirection = "later"
)

type WorkflowPageLayoutEdit struct {
	Kind      WorkflowPageLayoutEditKind
	FieldID   string
	SectionID string
	Label     string
	Direction WorkflowPageMoveDirection
	Hidden    bool
}

var ErrWorkflowPageLayoutInvalid = errors.New("productui: invalid workflow page layout edit")

// GeneratedWorkflowPageLayout is the stable default from which tenant
// overrides start. It is intentionally separate from the workflow graph.
func GeneratedWorkflowPageLayout(workflowID string, workflowVersion uint32, inputs []WorkflowPageLayoutInput) WorkflowPageLayout {
	section := WorkflowPageLayoutSection{ID: "general", Label: "General", Columns: 1}
	for _, input := range inputs {
		if strings.TrimSpace(input.ID) == "" {
			continue
		}
		section.Fields = append(section.Fields, WorkflowPageLayoutField{ID: input.ID, Label: input.Label, Required: input.Required})
	}
	return WorkflowPageLayout{WorkflowID: workflowID, WorkflowVersion: workflowVersion, Sections: []WorkflowPageLayoutSection{section}}
}

func (layout WorkflowPageLayout) Digest() string {
	encoded, _ := json.Marshal(layout)
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func ApplyWorkflowPageLayoutEdit(layout *WorkflowPageLayout, edit WorkflowPageLayoutEdit) error {
	if layout == nil || strings.TrimSpace(edit.FieldID) == "" {
		return ErrWorkflowPageLayoutInvalid
	}
	sectionIndex, fieldIndex := workflowPageLayoutField(layout, edit.FieldID)
	if sectionIndex < 0 {
		return fmt.Errorf("%w: field %q not found", ErrWorkflowPageLayoutInvalid, edit.FieldID)
	}
	field := &layout.Sections[sectionIndex].Fields[fieldIndex]
	switch edit.Kind {
	case WorkflowPageMove:
		target := fieldIndex - 1
		if edit.Direction == WorkflowPageMoveLater {
			target = fieldIndex + 1
		}
		if edit.Direction != WorkflowPageMoveEarlier && edit.Direction != WorkflowPageMoveLater {
			return ErrWorkflowPageLayoutInvalid
		}
		if target >= 0 && target < len(layout.Sections[sectionIndex].Fields) {
			fields := layout.Sections[sectionIndex].Fields
			fields[fieldIndex], fields[target] = fields[target], fields[fieldIndex]
		}
	case WorkflowPageGroup:
		if strings.TrimSpace(edit.SectionID) == "" {
			return ErrWorkflowPageLayoutInvalid
		}
		for index := range layout.Sections {
			if layout.Sections[index].ID == edit.SectionID {
				fieldCopy := *field
				layout.Sections[sectionIndex].Fields = append(layout.Sections[sectionIndex].Fields[:fieldIndex], layout.Sections[sectionIndex].Fields[fieldIndex+1:]...)
				layout.Sections[index].Fields = append(layout.Sections[index].Fields, fieldCopy)
				return nil
			}
		}
		return fmt.Errorf("%w: section %q not found", ErrWorkflowPageLayoutInvalid, edit.SectionID)
	case WorkflowPageRename:
		if strings.TrimSpace(edit.Label) == "" {
			return ErrWorkflowPageLayoutInvalid
		}
		field.Label = strings.TrimSpace(edit.Label)
	case WorkflowPageHide:
		if field.Required && edit.Hidden {
			return fmt.Errorf("%w: required field %q cannot be hidden", ErrWorkflowPageLayoutInvalid, edit.FieldID)
		}
		field.Hidden = edit.Hidden
	default:
		return ErrWorkflowPageLayoutInvalid
	}
	return nil
}

type WorkflowPageOutlineItem struct {
	SectionID string
	FieldID   string
	Label     string
	Required  bool
	Hidden    bool
}

func WorkflowPageLayoutOutline(layout WorkflowPageLayout) []WorkflowPageOutlineItem {
	result := make([]WorkflowPageOutlineItem, 0)
	for _, section := range layout.Sections {
		for _, field := range section.Fields {
			result = append(result, WorkflowPageOutlineItem{SectionID: section.ID, FieldID: field.ID, Label: field.Label, Required: field.Required, Hidden: field.Hidden})
		}
	}
	return result
}

type WorkflowPageDesignerProps struct {
	I18nProps
	Layout       WorkflowPageLayout
	WorkflowHref string
	OnEdit       func(WorkflowPageLayoutEdit)
}

// WorkflowPageDesigner renders the same edit commands as the outline. Each
// drag-equivalent action is a button with a keyboard shortcut; the server
// still applies the authoritative edit through ApplyWorkflowPageLayoutEdit.
func WorkflowPageDesigner(props WorkflowPageDesignerProps) ui.Node {
	items := make([]ui.Node, 0)
	for _, section := range props.Layout.Sections {
		items = append(items, html.Li(html.Props{Data: map[string]string{"workflow-page-section": section.ID}}, html.H2(html.Props{}, ui.Text(section.Label)), html.Ol(html.Props{Data: map[string]string{"workflow-page-outline": section.ID}}, workflowPageDesignerFields(props, section)...)))
	}
	return html.Main(html.Props{Class: "workflow-page-designer", Raw: map[string]any{"aria-label": "Workflow page designer", "data-loaded-route": "designer"}},
		html.H1(html.Props{}, ui.Text("Page designer")),
		html.P(html.Props{Class: "muted"}, ui.Text("Arrange the generated page layout for this workflow version.")),
		html.A(html.Props{Href: props.WorkflowHref, Class: "button secondary"}, ui.Text("Open workflow version")),
		html.H2(html.Props{}, ui.Text("Outline editor")),
		html.Ol(html.Props{Role: "tree", Data: map[string]string{"workflow-page-layout": "outline"}}, items...),
	)
}

func workflowPageDesignerFields(props WorkflowPageDesignerProps, section WorkflowPageLayoutSection) []ui.Node {
	result := make([]ui.Node, 0, len(section.Fields))
	for _, field := range section.Fields {
		controls := []ui.Node{
			html.Button(html.Props{Type: "button", Aria: map[string]string{"label": "Move earlier " + field.Label}, Raw: map[string]any{"aria-keyshortcuts": "Alt+ArrowUp"}, Data: map[string]string{"workflow-page-edit": "move-earlier", "field-id": field.ID}, OnClick: ui.UseEvent(func(ui.MouseEvent) {
				if props.OnEdit != nil {
					props.OnEdit(WorkflowPageLayoutEdit{Kind: WorkflowPageMove, FieldID: field.ID, Direction: WorkflowPageMoveEarlier})
				}
			})}, ui.Text("Move up")),
			html.Button(html.Props{Type: "button", Aria: map[string]string{"label": "Move later " + field.Label}, Raw: map[string]any{"aria-keyshortcuts": "Alt+ArrowDown"}, Data: map[string]string{"workflow-page-edit": "move-later", "field-id": field.ID}, OnClick: ui.UseEvent(func(propsEvent ui.MouseEvent) {
				if props.OnEdit != nil {
					props.OnEdit(WorkflowPageLayoutEdit{Kind: WorkflowPageMove, FieldID: field.ID, Direction: WorkflowPageMoveLater})
				}
			})}, ui.Text("Move down")),
		}
		if !field.Required {
			controls = append(controls, html.Button(html.Props{Type: "button", Aria: map[string]string{"label": "Hide " + field.Label}, Data: map[string]string{"workflow-page-edit": "hide", "field-id": field.ID}, OnClick: ui.UseEvent(func(ui.MouseEvent) {
				if props.OnEdit != nil {
					props.OnEdit(WorkflowPageLayoutEdit{Kind: WorkflowPageHide, FieldID: field.ID, Hidden: true})
				}
			})}, ui.Text("Hide")))
		}
		result = append(result, html.Li(html.Props{Role: "treeitem", Data: map[string]string{"workflow-page-field": field.ID, "required": fmt.Sprint(field.Required)}}, html.Span(html.Props{}, ui.Text(field.Label)), html.Span(html.Props{Class: "workflow-page-designer-controls"}, controls...)))
	}
	return result
}

func workflowPageLayoutField(layout *WorkflowPageLayout, fieldID string) (int, int) {
	for sectionIndex := range layout.Sections {
		for fieldIndex := range layout.Sections[sectionIndex].Fields {
			if layout.Sections[sectionIndex].Fields[fieldIndex].ID == fieldID {
				return sectionIndex, fieldIndex
			}
		}
	}
	return -1, -1
}

func WorkflowPageDesignerHref(view View, workflowID string, workflowVersion uint32) string {
	return statefulHref(view, PageWorkflowDesigner, "page_designer", "1", "workflow", workflowID, "workflow_version", itoa64(int64(workflowVersion)))
}
