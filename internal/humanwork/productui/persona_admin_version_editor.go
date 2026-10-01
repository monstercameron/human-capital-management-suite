package productui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func personaAdminVersionEditor(locale LocaleContext, client PersonaAdminClient, persona PersonaAdminPersona) ui.Node {
	if persona.StarterID == "" || persona.StarterVersion == 0 || len(persona.ChannelClasses) == 0 {
		return nil
	}
	id := "persona-admin-version-" + safeAgentDOMToken(persona.ID)
	channels := make([]ui.Node, 0, len(persona.ChannelClasses))
	for _, class := range []string{"PRIVATE", "PUBLIC"} {
		if !personaAdminHasChannel(persona.ChannelClasses, class) {
			continue
		}
		fieldID := id + "-channel-" + strings.ToLower(class)
		channels = append(channels, html.Label(html.Props{For: fieldID, Class: "persona-admin-channel"}, html.Input(html.Props{ID: fieldID, Name: "allowed_channels", Type: "checkbox", Value: class, Checked: true, Raw: map[string]any{"data-persona-channel": class}}), ui.Text(personaAdminVersionText(locale, "channel_"+strings.ToLower(class)))))
	}
	children := []ui.Node{
		html.Input(html.Props{Name: "persona_id", Type: "hidden", Value: persona.ID}),
		html.Input(html.Props{Name: "starter_id", Type: "hidden", Value: persona.StarterID}),
		html.Input(html.Props{Name: "starter_version", Type: "hidden", Value: strconv.FormatUint(uint64(persona.StarterVersion), 10)}),
		personaAdminVersionField(locale, id+"-handle", "handle", strings.TrimPrefix(persona.Handle, "@")),
		personaAdminVersionField(locale, id+"-name", "display_name", persona.Name),
		personaAdminVersionField(locale, id+"-purpose", "purpose", persona.Purpose),
		html.Div(html.Props{Class: "persona-admin-editor-field"}, html.Label(html.Props{For: id + "-instructions"}, ui.Text(personaAdminVersionText(locale, "instructions"))), html.Textarea(html.Props{ID: id + "-instructions", Rows: 3, ReadOnly: true, Raw: map[string]any{"readonly": "readonly", "aria-readonly": "true", "data-starter-instructions": "server-owned"}}, ui.Text(personaAdminVersionText(locale, "instructions_pinned")))),
	}
	children = append(children, personaAdminChannelFieldset(locale, channels))
	children = append(children,
		html.Button(html.Props{Class: "button secondary", Type: "submit", Disabled: client == nil, Raw: map[string]any{"data-persona-version-submit": persona.ID}}, ui.Text(personaAdminVersionText(locale, "create_version"))),
	)
	return html.Form(html.Props{ID: id, Class: "persona-admin-version-editor", Raw: map[string]any{"data-persona-admin-command-form": "CREATE_VERSION", "aria-label": personaAdminVersionText(locale, "editor_label") + ": " + persona.Name, "aria-describedby": "persona-admin-editor-status"}}, html.Fieldset(html.Props{Class: "persona-admin-version-fields", Disabled: client == nil}, children...))
}

func personaAdminVersionField(locale LocaleContext, id, name, value string) ui.Node {
	key := name
	if name == "display_name" {
		key = "name"
	}
	return html.Div(html.Props{Class: "persona-admin-editor-field"}, html.Label(html.Props{For: id}, ui.Text(personaAdminVersionText(locale, key))), html.Input(html.Props{ID: id, Name: name, Type: "text", Required: true, Value: value}))
}

func personaAdminHasChannel(channels []string, wanted string) bool {
	for _, channel := range channels {
		if channel == wanted {
			return true
		}
	}
	return false
}

func personaAdminVersionText(locale LocaleContext, key string) string {
	translations := map[string][3]string{
		"editor_label":   {"Create a new persona version", "Neue Persona-Version erstellen", "إنشاء إصدار شخصية جديد"},
		"create_version": {"Create version", "Version erstellen", "إنشاء إصدار"},
		"handle":         {"Handle", "Handle", "المعرّف"}, "name": {"Display name", "Anzeigename", "الاسم المعروض"}, "purpose": {"Purpose", "Zweck", "الغرض"},
		"instructions": {"Instructions", "Anweisungen", "التعليمات"}, "instructions_pinned": {"Executable instructions remain pinned to the exact approved manifest.", "Ausführbare Anweisungen bleiben an das genehmigte Manifest gebunden.", "تبقى التعليمات التنفيذية مرتبطة بالبيان المعتمد بدقة."},
		"channel_private": {"Private channels", "Private Kanäle", "قنوات خاصة"}, "channel_public": {"Public channels", "Öffentliche Kanäle", "قنوات عامة"},
	}
	values, ok := translations[key]
	if !ok {
		return key
	}
	index := 0
	switch locale.Resolved {
	case "de-DE":
		index = 1
	case "ar":
		index = 2
	}
	return values[index]
}
