package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AccessibilityPreferencesProps is presentation-only. Browser storage or a
// future governed account-preference service supplies its callbacks.
type AccessibilityPreferencesProps struct {
	I18nProps
	Value      AccessibilityPreferences
	TextSizes  []AccessibilityOption
	Contrasts  []AccessibilityOption
	Motions    []AccessibilityOption
	LinkStyles []AccessibilityOption
	OnPreview  func(AccessibilityPreferences)
	OnSave     func(AccessibilityPreferences)
	OnReset    func()
}

func AccessibilityPreferencesPanel(props AccessibilityPreferencesProps) ui.Node {
	draft := NormalizeAccessibilityPreferences(props.Value)
	return html.Section(html.Props{Class: "surface accessibility-preferences", Data: map[string]string{"hcm-setting-group": "accessibility"}, Raw: map[string]any{"aria-labelledby": "accessibility-title"}},
		ui.CreateElement(SectionHeading, SectionHeadingProps{
			ID: "accessibility-title", Title: props.Text("accessibility.title"), Description: props.Text("accessibility.description"), Level: 3,
		}),
		html.Form(html.Props{Class: "accessibility-form", OnSubmit: saveAccessibilityPreferences(props.OnSave, &draft)},
			accessibilityChoices(props, "text-size", props.Text("accessibility.text_size"), props.Text("accessibility.text_size_help"), draft.TextSize, props.TextSizes, func(value string) {
				draft.TextSize = value
				previewAccessibilityPreferences(props.OnPreview, draft)
			}),
			accessibilityChoices(props, "contrast", props.Text("accessibility.contrast"), props.Text("accessibility.contrast_help"), draft.Contrast, props.Contrasts, func(value string) {
				draft.Contrast = value
				previewAccessibilityPreferences(props.OnPreview, draft)
			}),
			accessibilityChoices(props, "motion-preference", props.Text("accessibility.motion"), props.Text("accessibility.motion_help"), draft.Motion, props.Motions, func(value string) {
				draft.Motion = value
				previewAccessibilityPreferences(props.OnPreview, draft)
			}),
			accessibilityChoices(props, "links", props.Text("accessibility.links"), props.Text("accessibility.links_help"), draft.Links, props.LinkStyles, func(value string) {
				draft.Links = value
				previewAccessibilityPreferences(props.OnPreview, draft)
			}),
			html.Div(html.Props{Class: "accessibility-actions", Data: map[string]string{"hcm-sticky-actions": "true"}},
				html.Button(html.Props{Class: "button primary", Type: "submit", Data: map[string]string{"hcm-action": "save-preferences"}}, ui.Text(props.Text("accessibility.save"))),
				html.Button(accessibilityResetProps(props.OnReset), ui.Text(props.Text("accessibility.reset"))),
			),
			html.P(html.Props{ID: "accessibility-status", Class: "accessibility-status", Raw: map[string]any{"role": "status", "aria-live": "polite", "aria-atomic": "true"}}, ui.Text(props.Text("accessibility.status"))),
		),
	)
}

func accessibilityChoices(props AccessibilityPreferencesProps, name, title, help, selected string, options []AccessibilityOption, onChange func(string)) ui.Node {
	items := make([]ui.Node, 0, len(options))
	for _, option := range options {
		option := option
		id := "accessibility-" + name + "-" + option.ID
		input := html.Props{ID: id, Type: "radio", Name: name, Value: option.ID, Checked: option.ID == selected}
		if onChange != nil {
			input.OnChange = ui.UseEvent(func(ui.InputEvent) { onChange(option.ID) })
		}
		detail := option.Detail
		if option.DescriptionKey != "" {
			detail = props.Text(option.DescriptionKey)
		}
		items = append(items, html.Label(html.Props{Class: "accessibility-choice", For: id},
			html.Input(input),
			html.Span(html.Props{}, html.Strong(html.Props{}, ui.Text(props.Text(option.LabelKey))), html.Small(html.Props{}, ui.Text(detail))),
		))
	}
	helpID := "accessibility-" + name + "-help"
	return html.Fieldset(html.Props{Class: "accessibility-group accessibility-group-" + name, Raw: map[string]any{"aria-describedby": helpID}},
		html.Legend(html.Props{}, ui.Text(title)),
		html.P(html.Props{ID: helpID, Class: "muted"}, ui.Text(help)),
		html.Div(html.Props{Class: "accessibility-options"}, items...),
	)
}

func accessibilityResetProps(reset func()) html.Props {
	props := html.Props{Class: "button secondary", Type: "button"}
	if reset != nil {
		props.OnClick = ui.UseEvent(func(ui.MouseEvent) { reset() })
	}
	return props
}

func saveAccessibilityPreferences(save func(AccessibilityPreferences), draft *AccessibilityPreferences) ui.Handler {
	if save == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		save(NormalizeAccessibilityPreferences(*draft))
	})
}

func previewAccessibilityPreferences(preview func(AccessibilityPreferences), value AccessibilityPreferences) {
	if preview != nil {
		preview(NormalizeAccessibilityPreferences(value))
	}
}
