package productui

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workflowview"
)

// uxblindWorkflowCatalogDraft is deliberately derived from the server-owned
// publication status. Names are display data and must not decide whether a
// version is editable.
func uxblindWorkflowCatalogDraft(item WorkflowCatalogItem) bool {
	return strings.EqualFold(strings.TrimSpace(item.Status), "DRAFT")
}

func uxblindWorkflowCatalogLess(left, right WorkflowCatalogItem) bool {
	leftName, rightName := strings.ToLower(strings.TrimSpace(left.Name)), strings.ToLower(strings.TrimSpace(right.Name))
	if leftName == rightName {
		if left.WorkflowID == right.WorkflowID {
			return left.Version > right.Version
		}
		return left.WorkflowID < right.WorkflowID
	}
	return leftName < rightName
}

// uxblindWorkflowDraftHref accepts a draft ID only when the authorized
// projection supplied one. It never invents a mutable identifier from a
// workflow name or display ID.
func uxblindWorkflowDraftHref(base string, item WorkflowCatalogItem) string {
	draftID := strings.TrimSpace(item.DraftID)
	if draftID == "" {
		return ""
	}
	href := workflowDesignerHref(base, "", "")
	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}
	values := parsed.Query()
	values.Set("draft", draftID)
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

func uxblindWorkflowCatalogEntry(props WorkflowDesignerPageProps, item WorkflowCatalogItem, name, status, version, class, current string) ui.Node {
	href := workflowDesignerHref(props.BaseHref, item.WorkflowID, "")
	main := softwareLink(props.Navigate, html.Props{Class: class, Raw: map[string]any{"aria-current": current}}, href,
		html.Span(html.Props{Class: "workflow-catalog-title", Dir: "auto"}, ui.Text(name)),
		html.Span(html.Props{Class: "workflow-catalog-meta"},
			html.Span(html.Props{}, ui.Text(version)),
			html.Span(html.Props{Class: "status-chip", Data: map[string]string{"tone": workflowPublicationTone(item.Status)}}, ui.Text(status)),
		),
	)
	if !uxblindWorkflowCatalogDraft(item) {
		return html.Li(html.Props{}, main)
	}

	editHref := uxblindWorkflowDraftHref(props.BaseHref, item)
	var edit ui.Node
	if editHref != "" {
		draftEditLabel := uxblindWorkflowDraftCopy(props.Locale, "edit")
		edit = softwareLink(props.Navigate, html.Props{Class: "button secondary compact workflow-catalog-edit", Aria: map[string]string{"label": draftEditLabel}}, editHref, ui.Text(draftEditLabel))
	} else {
		edit = html.Span(html.Props{Class: "workflow-catalog-unavailable", Raw: map[string]any{"role": "note"}},
			ui.Text(uxblindWorkflowDraftCopy(props.Locale, "edit_unavailable")))
	}
	return html.Li(html.Props{Class: "workflow-catalog-draft"}, main, edit)
}

func uxblindWorkflowCatalogBody(props WorkflowDesignerPageProps, published, drafts, references []ui.Node, activeName string) ui.Node {
	showReferences := ui.UseState(false)
	toggleReferences := ui.UseEvent(func(ui.MouseEvent) { showReferences.Set(!showReferences.Get()) })
	sections := make([]ui.Node, 0, 3)
	if len(published) > 0 {
		sections = append(sections, html.Section(html.Props{Class: "workflow-catalog-section", Aria: map[string]string{"labelledby": "workflow-catalog-published-heading"}},
			html.H4(html.Props{ID: "workflow-catalog-published-heading"},
				ui.Text(props.Text("workflow_designer.catalog_title")),
				html.Span(html.Props{Class: "count-badge"}, ui.Text(props.Locale.FormatNumber(fmt.Sprintf("%d", len(published)), 0))),
			),
			html.Ul(html.Props{Class: "workflow-catalog-list", Raw: map[string]any{"role": "list"}}, published...),
		))
	}
	if len(drafts) > 0 {
		sections = append(sections, html.Section(html.Props{Class: "workflow-catalog-section workflow-catalog-drafts", Aria: map[string]string{"labelledby": "workflow-catalog-drafts-heading"}},
			html.H4(html.Props{ID: "workflow-catalog-drafts-heading"},
				ui.Text(props.Text("workflow_designer.drafts_heading")),
				html.Span(html.Props{Class: "count-badge"}, ui.Text(props.Locale.FormatNumber(fmt.Sprintf("%d", len(drafts)), 0))),
			),
			html.Ul(html.Props{Class: "workflow-catalog-list", Raw: map[string]any{"role": "list"}}, drafts...),
		))
	}
	if len(sections) == 0 {
		sections = append(sections, html.Div(html.Props{Class: "workflow-catalog-empty", Raw: map[string]any{"role": "status"}},
			html.Strong(html.Props{}, ui.Text(props.Text("workflow_designer.empty_catalog_title"))),
			html.P(html.Props{Class: "muted"}, ui.Text(props.Text("workflow_designer.empty_catalog_detail"))),
		))
	}
	children := []ui.Node{html.P(html.Props{Class: "muted workflow-catalog-note"}, ui.Text(uxblindWorkflowCatalogNote(props, activeName)))}
	children = append(children, sections...)
	if len(references) > 0 {
		labelKey := "workflow_designer.references_toggle"
		if showReferences.Get() {
			labelKey = "workflow_designer.references_hide"
		}
		children = append(children, html.Button(html.Props{
			Class: "button secondary compact workflow-catalog-references-toggle", Type: "button", OnClick: toggleReferences,
			Aria: map[string]string{"expanded": fmt.Sprintf("%t", showReferences.Get()), "controls": "workflow-catalog-references"},
		}, ui.Text(props.Text(labelKey))))
		children = append(children, html.Section(html.Props{
			ID: "workflow-catalog-references", Class: "workflow-catalog-section workflow-catalog-references", Hidden: !showReferences.Get(),
			Aria: map[string]string{"labelledby": "workflow-catalog-references-heading"},
		}, html.H4(html.Props{ID: "workflow-catalog-references-heading"},
			ui.Text(props.Text("workflow_designer.references_heading")),
			html.Span(html.Props{Class: "count-badge"}, ui.Text(props.Locale.FormatNumber(fmt.Sprintf("%d", len(references)), 0))),
		), html.Ul(html.Props{Class: "workflow-catalog-list", Raw: map[string]any{"role": "list"}}, references...)))
	}
	return html.Div(html.Props{}, children...)
}

func uxblindWorkflowActiveName(catalog []WorkflowCatalogItem) string {
	for _, item := range catalog {
		if !workflowCatalogReviewOnly(item) && strings.EqualFold(strings.TrimSpace(item.Status), "ACTIVE") {
			if name := strings.TrimSpace(item.Name); name != "" {
				return name
			}
		}
	}
	return ""
}

func uxblindWorkflowCatalogNote(props WorkflowDesignerPageProps, _ string) string {
	return props.Text("workflow_designer.catalog_detail")
}

func uxblindWorkflowPublicationDraft(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "DRAFT")
}

func uxblindWorkflowDraftWorkspace(props WorkflowDesignerPageProps, selected workflowview.View) ui.Node {
	return html.Div(html.Props{Class: "workflow-draft-workspace", Aria: map[string]string{"labelledby": "workflow-draft-workspace-title"}},
		html.H3(html.Props{ID: "workflow-draft-workspace-title"}, ui.Text(uxblindWorkflowDraftCopy(props.Locale, "title"))),
		html.P(html.Props{Class: "muted"}, ui.Text(props.Text("workflow_draft.eyebrow"))),
		html.Div(html.Props{Class: "workflow-draft-selection", Data: map[string]string{"workflow-id": selected.WorkflowID}},
			html.Strong(html.Props{Dir: "auto"}, ui.Text(selected.Name)),
			html.Span(html.Props{Class: "status-chip", Data: map[string]string{"tone": "info"}}, ui.Text(props.Text("workflow_viewer.publication.draft"))),
			html.Span(html.Props{Class: "workflow-catalog-unavailable", Raw: map[string]any{"role": "note"}}, ui.Text(uxblindWorkflowDraftCopy(props.Locale, "edit_unavailable"))),
		),
	)
}

func uxblindWorkflowDraftCopy(locale LocaleContext, kind string) string {
	switch locale.normalized().Resolved {
	case "de-DE":
		if kind == "edit" {
			return "Entwurf bearbeiten"
		}
		if kind == "edit_unavailable" {
			return "Bearbeiten aus dieser Ansicht nicht verfügbar"
		}
		return "Workflow-Entwürfe"
	case "ar":
		if kind == "edit" {
			return "تحرير المسودة"
		}
		if kind == "edit_unavailable" {
			return "التحرير غير متاح من هذا العرض"
		}
		return "مسودات سير العمل"
	default:
		if kind == "edit" {
			return "Edit draft"
		}
		if kind == "edit_unavailable" {
			return "Editing is unavailable from this view."
		}
		return "Draft workflows"
	}
}

func uxblindAppearanceThemeChanged(draft, published CustomerTheme) bool {
	return !reflect.DeepEqual(NormalizeCustomerTheme(draft), NormalizeCustomerTheme(published))
}

type uxblindAppearanceDraftState struct {
	Source CustomerTheme
	Draft  CustomerTheme
}
