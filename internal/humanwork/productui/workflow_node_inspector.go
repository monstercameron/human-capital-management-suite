package productui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func workflowControlID(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, value)
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
	// Nothing is preselected. The list used to start on its first entry, so a
	// reason and one press of Replace swapped a governed step for whichever
	// block sorted first.
	initialReplacement := ""
	options := []ui.Node{html.Option(html.Props{Value: workflowNoTarget, Selected: true, Disabled: true}, ui.Text(props.Text("workflow_editor.choose_replacement")))}
	for _, item := range blocks {
		options = append(options, html.Option(html.Props{Value: fmt.Sprintf("%s@%d", item.ID, item.Version)}, ui.Text(workflowPaletteName(props.I18nProps, item))))
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
	replace := html.Props{Class: "button secondary compact", Type: "button", Disabled: disabled || !strings.Contains(replacementValue.Get(), "@")}
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
	return html.Section(html.Props{Class: "workflow-inspector-section", Aria: map[string]string{"labelledby": "workflow-overlay-title"}},
		html.H4(html.Props{ID: "workflow-overlay-title"}, ui.Text(props.Text("workflow_inspector.overlay"))),
		html.P(html.Props{ID: "workflow-overlay-reason-help", Class: "muted"}, ui.Text(props.Text("workflow_inspector.overlay_help"))),
		ui.CreateElement(LabeledControl, LabeledControlProps{For: reasonProps.ID, Label: props.Text("workflow_inspector.reason"), Control: html.Input(reasonProps)}),
		ui.CreateElement(LabeledControl, LabeledControlProps{For: replacement.ID, Label: props.Text("workflow_inspector.replacement"), Control: html.Select(replacement, options...)}),
		html.Div(html.Props{Class: "workflow-overlay-actions"}, html.Button(omit, ui.Text(props.Text("workflow_inspector.omit"))), html.Button(replace, ui.Text(props.Text("workflow_inspector.replace")))),
	)
}

func workflowOverlayHistory(i18n I18nProps, draft WorkflowDraftView) ui.Node {
	if len(draft.Overlays) == 0 {
		return nil
	}
	items := make([]ui.Node, 0, len(draft.Overlays))
	for _, overlay := range draft.Overlays {
		summary := displayWorkflowToken(WorkflowViewerProps{I18nProps: i18n}, "overlay", overlay.Operation)
		if overlay.TargetNodeID != "" {
			summary += " · " + DisplayLabel(overlay.TargetNodeID)
		}
		if overlay.EntryID != "" {
			summary += " · " + DisplayLabel(overlay.EntryID)
		}
		items = append(items, html.Li(html.Props{}, html.Strong(html.Props{}, ui.Text(summary)), html.Span(html.Props{Class: "muted"}, ui.Text(overlay.Reason))))
	}
	return html.Details(html.Props{Class: "workflow-overlay-history"}, html.Summary(html.Props{}, ui.Text(i18n.Text("workflow_inspector.history", map[string]string{"count": fmt.Sprint(len(items))}))), html.Ol(html.Props{}, items...))
}
