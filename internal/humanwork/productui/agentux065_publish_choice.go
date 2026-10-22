package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The author's publish choice (AGENTUX-065): three plain choices with
// their computed labels, never a typed number. Choices smaller than the
// change requires stay visible but disabled; a first publication names its
// one computed label instead of offering choices.
func personaAdminPublishChoice(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	preview := persona.PublishPreview
	if !preview.Available {
		return nil
	}
	if preview.Greatest == "" {
		label := "v" + strings.TrimPrefix(preview.Patch, "v")
		return html.P(html.Props{Class: "persona-admin-publish-choice", Raw: map[string]any{"data-version-first-publication": preview.Patch}},
			ui.Text(personaAdminText(locale, "publish_choice_first")+" · "+label))
	}
	choices := []struct {
		value, next, copyKey string
	}{
		{"patch", preview.Patch, "publish_choice_patch"},
		{"minor", preview.Minor, "publish_choice_minor"},
		{"major", preview.Major, "publish_choice_major"},
	}
	rank := map[string]int{"patch": 0, "minor": 1, "major": 2}
	required := rank[preview.Required]
	options := make([]ui.Node, 0, len(choices))
	for _, choice := range choices {
		label := personaAdminText(locale, choice.copyKey) + " → v" + strings.TrimPrefix(choice.next, "v")
		props := html.Props{Type: "radio", Name: "version_bump", Value: choice.value, Checked: choice.value == preview.Required, Disabled: rank[choice.value] < required}
		if rank[choice.value] < required {
			label += " · " + personaAdminText(locale, "publish_choice_too_small")
		}
		id := "persona-admin-version-bump-" + safeAgentDOMToken(persona.ID) + "-" + choice.value
		props.ID = id
		options = append(options, html.Label(html.Props{For: id}, html.Input(props), ui.Text(" "+label)))
	}
	return html.Fieldset(html.Props{Class: "persona-admin-publish-choice", Raw: map[string]any{"data-version-bump-picker": "true"}},
		html.Legend(html.Props{}, ui.Text(personaAdminText(locale, "publish_choice_heading"))),
		html.Div(html.Props{Class: "persona-admin-publish-options"}, options...),
	)
}
