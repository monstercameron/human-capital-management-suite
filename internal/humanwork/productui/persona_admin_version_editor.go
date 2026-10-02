package productui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func personaAdminVersionEditor(locale LocaleContext, client PersonaAdminClient, persona PersonaAdminPersona, documentServiceAvailable bool) ui.Node {
	if !personaAdminVersionEditorAvailable(persona) {
		return nil
	}
	id := personaAdminVersionEditorID(persona)
	channels := make([]ui.Node, 0, len(persona.ChannelClasses))
	for _, class := range []string{"PRIVATE", "PUBLIC"} {
		if !personaAdminHasChannel(persona.ChannelClasses, class) {
			continue
		}
		fieldID := id + "-channel-" + strings.ToLower(class)
		channels = append(channels, html.Label(html.Props{For: fieldID, Class: "persona-admin-channel"}, html.Input(html.Props{ID: fieldID, Name: "allowed_channels", Type: "checkbox", Value: class, Checked: true, Raw: map[string]any{"data-persona-channel": class}}), ui.Text(personaAdminVersionText(locale, "channel_"+strings.ToLower(class)))))
	}
	channels = append(channels, html.Span(html.Props{Class: "persona-admin-channel", Raw: map[string]any{"data-persona-channel-display": "DIRECT_MESSAGE"}}, ui.Text(agentUXR7Text(locale, "direct_always"))))
	explanationKey := "explanation_unpublished"
	if persona.Lifecycle == PersonaPublished || persona.Lifecycle == PersonaSuspended {
		explanationKey = "explanation_published"
	} else if persona.Lifecycle == PersonaInReview {
		explanationKey = "explanation_in_review"
	}
	next := personaAdminNextVersion(persona.Version)
	explanation := strings.NewReplacer(
		"{next}", personaAdminLocalizedNumber(locale, next),
		"{current}", personaAdminLocalizedNumber(locale, persona.Version),
		"{live}", personaAdminLocalizedNumber(locale, personaAdminLiveVersionValue(persona)),
	).Replace(personaAdminVersionText(locale, explanationKey))
	children := []ui.Node{
		html.H4(html.Props{Class: "persona-admin-version-title"}, ui.Text(strings.ReplaceAll(personaAdminVersionText(locale, "title"), "{agent}", persona.Name))),
		html.P(html.Props{Class: "muted"}, ui.Text(explanation)),
		html.Input(html.Props{Name: "persona_id", Type: "hidden", Value: persona.ID}),
		html.Input(html.Props{Name: "starter_id", Type: "hidden", Value: persona.StarterID}),
		html.Input(html.Props{Name: "starter_version", Type: "hidden", Value: strconv.FormatUint(uint64(persona.StarterVersion), 10)}),
		personaAdminVersionHandle(locale, id+"-handle", persona.Handle),
		personaAdminVersionField(locale, id+"-name", "display_name", persona.Name),
		personaAdminVersionPurpose(locale, id+"-purpose", persona.Purpose),
		agentDocBuiltInInstructions(locale, id+"-built-in-instructions", persona.Instructions),
		agentDocInstructionEditor(locale, id+"-instructions", personaAdminVersionInstructions(locale, persona), false),
		personaAdminInstructionMentions(locale, persona.DocumentReferences),
		personaAdminReferenceDocumentsSlot(locale, persona, documentServiceAvailable),
	}
	children = append(children, personaAdminChannelFieldset(locale, channels))
	children = append(children,
		html.Div(html.Props{Class: "persona-admin-version-actions"},
			html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: client == nil, Raw: map[string]any{"data-persona-version-submit": persona.ID}}, ui.Text(strings.ReplaceAll(personaAdminVersionText(locale, "save_version"), "{version}", personaAdminLocalizedNumber(locale, next)))),
			html.Button(html.Props{Class: "button secondary", Type: "button", Raw: map[string]any{"data-persona-editor-toggle": id}}, ui.Text(personaAdminVersionText(locale, "cancel"))),
		),
	)
	return html.Form(html.Props{ID: id, Class: "persona-admin-version-editor", Hidden: true, Raw: map[string]any{"data-persona-admin-command-form": "CREATE_VERSION", "data-persona-version-panel": persona.ID, "aria-label": personaAdminVersionText(locale, "editor_label") + ": " + persona.Name}}, html.Fieldset(html.Props{Class: "persona-admin-version-fields", Disabled: client == nil}, children...))
}

func personaAdminVersionEditorAvailable(persona PersonaAdminPersona) bool {
	return persona.StarterID != "" && persona.StarterVersion != 0 && len(persona.ChannelClasses) > 0
}

func personaAdminVersionEditorID(persona PersonaAdminPersona) string {
	return "persona-admin-version-" + safeAgentDOMToken(persona.ID)
}

func personaAdminNextVersion(current string) string {
	version, err := strconv.Atoi(strings.TrimSpace(current))
	if err != nil || version < 0 {
		return personaAdminVersionText(ResolveProductLocale(DefaultProductLocale), "next_version")
	}
	return strconv.Itoa(version + 1)
}

func personaAdminVersionField(locale LocaleContext, id, name, value string) ui.Node {
	key := name
	if name == "display_name" {
		key = "name"
	}
	direction := "auto"
	if name == "handle" {
		direction = "ltr"
	}
	return html.Div(html.Props{Class: "persona-admin-editor-field"}, html.Label(html.Props{For: id}, ui.Text(personaAdminVersionText(locale, key))), html.Input(html.Props{ID: id, Name: name, Type: "text", Required: true, Dir: direction, Raw: map[string]any{"value": value}}))
}

func personaAdminVersionHandle(locale LocaleContext, id, handle string) ui.Node {
	raw := strings.TrimPrefix(strings.TrimSpace(handle), "@")
	return html.Div(html.Props{Class: "persona-admin-editor-field"},
		html.Label(html.Props{For: id}, ui.Text(personaAdminVersionText(locale, "handle"))),
		html.Input(html.Props{ID: id, Type: "text", ReadOnly: true, Value: "@" + raw, Dir: "ltr", Raw: map[string]any{"aria-readonly": "true"}}),
		html.Input(html.Props{Name: "handle", Type: "hidden", Value: raw}),
	)
}

func personaAdminVersionPurpose(locale LocaleContext, id, value string) ui.Node {
	const limit = 240
	return html.Div(html.Props{Class: "persona-admin-editor-field"},
		html.Label(html.Props{For: id}, ui.Text(personaAdminVersionText(locale, "purpose"))),
		html.Textarea(html.Props{ID: id, Name: "purpose", Rows: 3, Required: true, Dir: "auto", Raw: map[string]any{"maxlength": limit, "data-agent-purpose-autosize": "true", "aria-describedby": id + "-count"}}, ui.Text(value)),
		html.Small(html.Props{ID: id + "-count", Class: "muted", Raw: map[string]any{"data-purpose-counter-template": personaAdminVersionText(locale, "purpose_count")}}, ui.Text(strings.NewReplacer("{count}", locale.FormatNumber(strconv.Itoa(len([]rune(value))), 0), "{limit}", locale.FormatNumber(strconv.Itoa(limit), 0)).Replace(personaAdminVersionText(locale, "purpose_count")))),
	)
}

func personaAdminInstructionMentions(locale LocaleContext, references []PersonaAdminDocumentReference) ui.Node {
	if len(references) == 0 {
		return nil
	}
	chips := make([]ui.Node, 0, len(references)+1)
	chips = append(chips, html.Small(html.Props{Class: "muted"}, ui.Text(personaAdminVersionText(locale, "mentions"))))
	for _, reference := range references {
		chips = append(chips, html.Span(html.Props{Class: "persona-admin-instruction-chip", Dir: "auto"}, ui.Text("@"+AgentDocInstructionMentionLabel(reference, references))))
	}
	return html.Div(html.Props{Class: "persona-admin-instruction-mentions"}, chips...)
}

func personaAdminVersionInstructions(locale LocaleContext, persona PersonaAdminPersona) string {
	text := AgentDocInstructionDisplayText(persona.Guidance, persona.DocumentReferences)
	for strings.Contains(text, "]  ") {
		text = strings.ReplaceAll(text, "]  ", "] ")
	}
	return text
}

func personaAdminReferenceDocumentsSlot(locale LocaleContext, persona PersonaAdminPersona, available bool) ui.Node {
	return html.Div(html.Props{Class: "persona-admin-reference-documents-slot"}, AgentDocumentReferencePicker(locale, AgentDocumentReferencePickerModel{
		ID: "persona-reference-" + safeAgentDOMToken(persona.ID), Available: available, State: AgentDocumentPickerIdle,
		DefaultVersionMode: "PINNED", References: persona.DocumentReferences,
	}))
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
		"editor_label":            {"Create a new agent version", "Neue Agentenversion erstellen", "إنشاء إصدار جديد للوكيل"},
		"disclosure":              {"Edit", "Bearbeiten", "تحرير"},
		"title":                   {"Create a new version of {agent}", "Neue Version von {agent} erstellen", "إنشاء إصدار جديد من {agent}"},
		"explanation_published":   {"Creates version {next} as a draft. Version {current} stays live until you publish.", "Erstellt Version {next} als Entwurf. Version {current} bleibt aktiv, bis Sie veröffentlichen.", "ينشئ الإصدار {next} كمسودة. يظل الإصدار {current} متاحاً حتى تنشر."},
		"explanation_unpublished": {"Creates version {next} as a draft without changing version {current}.", "Erstellt Version {next} als Entwurf, ohne Version {current} zu ändern.", "ينشئ الإصدار {next} كمسودة من دون تغيير الإصدار {current}."},
		"explanation_in_review":   {"Version {current} stays in review. Version {live} stays live. This card will show version {next}; version {current} remains under Version history.", "Version {current} bleibt in Prüfung. Version {live} bleibt aktiv. Diese Karte zeigt anschließend Version {next}; Version {current} bleibt unter Versionsverlauf verfügbar.", "يبقى الإصدار {current} قيد المراجعة. يبقى الإصدار {live} مباشرًا. ستعرض هذه البطاقة الإصدار {next}؛ ويبقى الإصدار {current} ضمن سجل الإصدارات."},
		"next_version":            {"next", "nächste", "التالي"},
		"create_version":          {"Create version", "Version erstellen", "إنشاء إصدار"},
		"save_version":            {"Save as version {version} (draft)", "Als Version {version} (Entwurf) speichern", "حفظ كإصدار {version} (مسودة)"},
		"cancel":                  {"Cancel", "Abbrechen", "إلغاء"},
		"handle":                  {"Agent address", "Agentenadresse", "عنوان الوكيل"}, "name": {"Display name", "Anzeigename", "الاسم المعروض"}, "purpose": {"Purpose", "Zweck", "الغرض"},
		"purpose_count": {"{count} of {limit} characters", "{count} von {limit} Zeichen", "{count} من {limit} حرفاً"},
		"mentions":      {"Document mentions", "Dokumenterwähnungen", "إشارات المستندات"},
		"instructions":  {"Instructions", "Anweisungen", "التعليمات"}, "instructions_pinned": {"Executable instructions remain pinned to the exact approved manifest.", "Ausführbare Anweisungen bleiben an das genehmigte Manifest gebunden.", "تبقى التعليمات التنفيذية مرتبطة بالبيان المعتمد بدقة."},
		"channel_private": {"Private channels", "Private Kanäle", "قنوات خاصة"}, "channel_public": {"Public channels", "Öffentliche Kanäle", "قنوات عامة"}, "channel_direct": {"Direct messages", "Direktnachrichten", "رسائل مباشرة"},
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
