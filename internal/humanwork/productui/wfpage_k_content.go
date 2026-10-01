package productui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// WorkflowPageText is page-authored copy. Page copy is carried with the
// published page rather than looked up by a hard-coded persona or page name.
// The English value is also the safe fallback for an incomplete projection.
type WorkflowPageText struct {
	ENUS string `json:"en_us,omitempty"`
	DEDE string `json:"de_de,omitempty"`
	AR   string `json:"ar,omitempty"`
}

func (text WorkflowPageText) Resolve(locale LocaleContext) string {
	switch locale.normalized().Resolved {
	case "de-DE":
		if strings.TrimSpace(text.DEDE) != "" {
			return text.DEDE
		}
	case "ar":
		if strings.TrimSpace(text.AR) != "" {
			return text.AR
		}
	default:
		if strings.TrimSpace(text.ENUS) != "" {
			return text.ENUS
		}
	}
	if strings.TrimSpace(text.ENUS) != "" {
		return text.ENUS
	}
	if strings.TrimSpace(text.DEDE) != "" {
		return text.DEDE
	}
	return text.AR
}

const (
	WorkflowPageNote      = "note"
	WorkflowPageGuide     = "guide"
	WorkflowPageChecklist = "checklist"
)

// WorkflowPageRule is the intentionally small rule vocabulary available to
// page conditions. Rules are data, not executable page code, and therefore
// can be evaluated identically by the server and the renderer.
type WorkflowPageRule struct {
	Field    string   `json:"field,omitempty"`
	Equals   string   `json:"equals,omitempty"`
	NotEqual string   `json:"not_equal,omitempty"`
	Present  *bool    `json:"present,omitempty"`
	AnyOf    []string `json:"any_of,omitempty"`
}

func (rule WorkflowPageRule) Matches(values map[string]string) bool {
	if strings.TrimSpace(rule.Field) == "" {
		return true
	}
	value, present := values[rule.Field]
	if rule.Present != nil && present != *rule.Present {
		return false
	}
	if rule.Equals != "" && value != rule.Equals {
		return false
	}
	if rule.NotEqual != "" && value == rule.NotEqual {
		return false
	}
	if len(rule.AnyOf) > 0 {
		for _, candidate := range rule.AnyOf {
			if value == candidate {
				return true
			}
		}
		return false
	}
	return true
}

type WorkflowPageField struct {
	ID       string           `json:"id"`
	Label    WorkflowPageText `json:"label"`
	Required bool             `json:"required,omitempty"`
	Rule     WorkflowPageRule `json:"rule,omitempty"`
}

type WorkflowPageChecklistItem struct {
	ID       string           `json:"id"`
	Label    WorkflowPageText `json:"label"`
	Required bool             `json:"required,omitempty"`
}

type WorkflowPageContentBlock struct {
	ID         string                      `json:"id"`
	Kind       string                      `json:"kind"`
	Title      WorkflowPageText            `json:"title,omitempty"`
	Text       WorkflowPageText            `json:"text,omitempty"`
	Markdown   WorkflowPageText            `json:"markdown,omitempty"`
	Checklist  []WorkflowPageChecklistItem `json:"checklist,omitempty"`
	GateSubmit bool                        `json:"gate_submit,omitempty"`
}

type WorkflowPageSection struct {
	ID          string                     `json:"id"`
	Title       WorkflowPageText           `json:"title"`
	Description WorkflowPageText           `json:"description,omitempty"`
	Rule        WorkflowPageRule           `json:"rule,omitempty"`
	Fields      []WorkflowPageField        `json:"fields,omitempty"`
	Blocks      []WorkflowPageContentBlock `json:"blocks,omitempty"`
}

type WorkflowPageDefinition struct {
	ID       string                `json:"id"`
	Sections []WorkflowPageSection `json:"sections"`
}

type WorkflowPageSubmission struct {
	Values map[string]string
}

// WorkflowPageValidation is the server-side result used by both the submit
// path and the page's disabled-submit presentation.
type WorkflowPageValidation struct {
	VisibleFields []string
	VisibleValues map[string]string
	Errors        []string
	SubmitAllowed bool
}

// ValidateWorkflowPage filters first, then validates. This ordering is
// security-sensitive: values belonging to a hidden conditional section are
// absent from both the submitted map and the required-field checks.
func ValidateWorkflowPage(page WorkflowPageDefinition, values map[string]string, completed map[string]bool) WorkflowPageValidation {
	result := WorkflowPageValidation{VisibleValues: make(map[string]string)}
	for _, section := range page.Sections {
		if !section.Rule.Matches(values) {
			continue
		}
		for _, field := range section.Fields {
			if strings.TrimSpace(field.ID) == "" || !field.Rule.Matches(values) {
				continue
			}
			result.VisibleFields = append(result.VisibleFields, field.ID)
			if value, ok := values[field.ID]; ok {
				result.VisibleValues[field.ID] = value
			}
			if field.Required && strings.TrimSpace(values[field.ID]) == "" {
				result.Errors = append(result.Errors, fmt.Sprintf("field %q is required", field.ID))
			}
		}
		for _, block := range section.Blocks {
			if block.Kind != WorkflowPageChecklist || !block.GateSubmit {
				continue
			}
			for _, item := range block.Checklist {
				if item.Required && !completed[item.ID] {
					result.Errors = append(result.Errors, fmt.Sprintf("checklist item %q is incomplete", item.ID))
				}
			}
		}
	}
	sort.Strings(result.VisibleFields)
	sort.Strings(result.Errors)
	result.SubmitAllowed = len(result.Errors) == 0
	return result
}

func ProjectWorkflowPageSubmission(page WorkflowPageDefinition, values map[string]string, completed map[string]bool) (WorkflowPageSubmission, WorkflowPageValidation) {
	validation := ValidateWorkflowPage(page, values, completed)
	if !validation.SubmitAllowed {
		return WorkflowPageSubmission{}, validation
	}
	return WorkflowPageSubmission{Values: copyStringMap(validation.VisibleValues)}, validation
}

// RenderWorkflowPageContent renders only admitted sections. Markdown is
// passed through docsASTMarkdownNodes, which turns raw HTML and unsafe links
// into inert text instead of browser content.
func RenderWorkflowPageContent(view View, page WorkflowPageDefinition, values map[string]string, completed map[string]bool) ui.Node {
	sections := make([]ui.Node, 0, len(page.Sections))
	for _, section := range page.Sections {
		if !section.Rule.Matches(values) {
			continue
		}
		children := make([]ui.Node, 0, len(section.Blocks)+2)
		if title := section.Title.Resolve(view.Locale); title != "" {
			children = append(children, html.H2(html.Props{Dir: "auto", Text: title}))
		}
		if description := section.Description.Resolve(view.Locale); description != "" {
			children = append(children, html.P(html.Props{Class: "muted", Dir: "auto", Text: description}))
		}
		for _, block := range section.Blocks {
			children = append(children, renderWorkflowPageBlock(view, block, completed))
		}
		sections = append(sections, html.Section(html.Props{Class: "workflow-page-section", Data: map[string]string{"workflow-page-section": section.ID}}, children...))
	}
	return html.Div(html.Props{Class: "workflow-page-content", Dir: string(view.Locale.Direction)}, sections...)
}

func renderWorkflowPageBlock(view View, block WorkflowPageContentBlock, completed map[string]bool) ui.Node {
	title := block.Title.Resolve(view.Locale)
	switch block.Kind {
	case WorkflowPageNote:
		children := append([]ui.Node{workflowPageBlockTitle(title)}, docsASTMarkdownNodes(view, block.Markdown.Resolve(view.Locale))...)
		return html.Aside(html.Props{Class: "workflow-page-note", Dir: "auto", Data: map[string]string{"workflow-page-block": block.ID}}, children...)
	case WorkflowPageGuide:
		return html.Div(html.Props{Class: "workflow-page-guide", Dir: "auto", Data: map[string]string{"workflow-page-block": block.ID}}, workflowPageBlockTitle(title), html.P(html.Props{Dir: "auto", Text: block.Text.Resolve(view.Locale)}))
	case WorkflowPageChecklist:
		items := make([]ui.Node, 0, len(block.Checklist)+1)
		items = append(items, workflowPageBlockTitle(title))
		for _, item := range block.Checklist {
			id := "workflow-page-checklist-" + item.ID
			items = append(items, html.Label(html.Props{Class: "workflow-page-checklist-item", For: id}, html.Input(html.Props{ID: id, Type: "checkbox", Checked: completed[item.ID], Data: map[string]string{"workflow-page-checklist": item.ID}}), ui.Text(item.Label.Resolve(view.Locale))))
		}
		return html.Fieldset(html.Props{Class: "workflow-page-checklist", Data: map[string]string{"workflow-page-block": block.ID}}, items...)
	default:
		return html.Div(html.Props{Class: "workflow-page-block-unsupported", Data: map[string]string{"workflow-page-block": block.ID}}, ui.Text(title))
	}
}

func workflowPageBlockTitle(title string) ui.Node {
	if title == "" {
		return ui.Text("")
	}
	return html.H3(html.Props{Dir: "auto", Text: title})
}

func copyStringMap(values map[string]string) map[string]string {
	copy := make(map[string]string, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}
