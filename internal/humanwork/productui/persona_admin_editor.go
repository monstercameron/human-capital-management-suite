package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PersonaAdminEditor renders the starter-based creation form. Server-owned
// templates establish all executable instructions and authority ceilings.
func PersonaAdminEditor(locale LocaleContext, client PersonaAdminClient, snapshot PersonaAdminSnapshot) ui.Node {
	if locale.Resolved == "" {
		locale = ResolveProductLocale(DefaultProductLocale)
	}
	starters := personaAdminReadyStarters(snapshot)
	ready := client != nil && len(starters) > 0
	options := make([]ui.Node, 0, len(starters)+1)
	if !ready {
		options = append(options, html.Option(html.Props{Value: "", Disabled: true, Selected: true}, ui.Text(personaAdminEditorText(locale, personaAdminStarterStatusKey(snapshot)))))
	}
	for _, starter := range starters {
		options = append(options, html.Option(html.Props{Value: starter.ID, Raw: map[string]any{"data-starter-name": starter.Name, "data-starter-handle": starter.Handle, "data-starter-purpose": starter.Purpose, "data-starter-version": starter.Version, "data-starter-manifest": starter.ManifestID, "data-starter-channels": strings.Join(starter.ChannelClasses, ",")}}, ui.Text(starter.Name)))
	}
	channelChoices := []struct{ value, label string }{{"PRIVATE", personaAdminEditorText(locale, "private_channels")}, {"PUBLIC", personaAdminEditorText(locale, "public_channels")}, {"EXTERNAL", personaAdminEditorText(locale, "external_channels")}}
	channels := make([]ui.Node, 0, len(channelChoices))
	for _, channel := range channelChoices {
		id := "persona-admin-channel-" + strings.ToLower(channel.value)
		allowed := ready && personaAdminHasChannel(starters[0].ChannelClasses, channel.value)
		channels = append(channels, html.Label(html.Props{Class: "persona-admin-channel", For: id}, html.Input(html.Props{ID: id, Type: "checkbox", Value: channel.value, Checked: allowed, Disabled: true, Raw: map[string]any{"data-persona-channel": channel.value}}), ui.Text(channel.label)))
	}
	statusKey := "ready"
	if !ready {
		statusKey = personaAdminStarterStatusKey(snapshot)
	}
	if !ready && len(snapshot.Personas) > 0 {
		message := personaAdminEditorText(locale, "additional_templates_unavailable")
		if snapshot.CommandStatus != "" {
			message = PersonaAdminCommandStatusText(locale, snapshot.CommandStatus)
		}
		return html.Section(html.Props{Class: "surface persona-admin-editor", Aria: map[string]string{"labelledby": "persona-admin-editor-title"}},
			html.H2(html.Props{ID: "persona-admin-editor-title"}, ui.Text(personaAdminEditorText(locale, "create_title"))),
			html.P(html.Props{ID: "persona-admin-editor-status", Class: "muted", Role: "status", Raw: map[string]any{"aria-live": "polite", "aria-atomic": "true"}}, ui.Text(message)),
		)
	}
	message := personaAdminEditorText(locale, statusKey)
	if snapshot.CommandStatus != "" {
		message = PersonaAdminCommandStatusText(locale, snapshot.CommandStatus)
	}
	status := html.P(html.Props{ID: "persona-admin-editor-status", Class: "muted", Role: "status", Raw: map[string]any{"aria-live": "polite", "aria-atomic": "true", "data-command-status": ""}}, ui.Text(message))
	first := PersonaAdminStarter{}
	var setup ui.Node
	if snapshot.LocalDevBootstrapAvailable && !ready {
		setup = html.Button(html.Props{ID: "persona-admin-local-setup", Class: "button secondary", Type: "button", Raw: map[string]any{"data-persona-local-setup": "true"}}, ui.Text(personaAdminEditorText(locale, "local_setup")))
	}
	if len(starters) > 0 {
		first = starters[0]
	}
	return html.Section(html.Props{Class: "surface persona-admin-editor", Aria: map[string]string{"labelledby": "persona-admin-editor-title"}, Raw: map[string]any{"data-persona-admin-editor": "true"}},
		html.H2(html.Props{ID: "persona-admin-editor-title"}, ui.Text(personaAdminEditorText(locale, "create_title"))),
		html.P(html.Props{Class: "muted"}, ui.Text(personaAdminEditorText(locale, "create_detail"))),
		setup,
		html.Form(html.Props{ID: "persona-admin-create-form", Class: "persona-admin-editor-form", OnSubmit: ui.UseEvent(func(event ui.FormEvent) { event.PreventDefault() }), Raw: map[string]any{"data-persona-admin-command-form": "CREATE_DRAFT", "aria-describedby": "persona-admin-editor-help persona-admin-editor-status"}},
			html.Div(html.Props{Class: "persona-admin-editor-grid"},
				personaAdminEditorSelect(locale, "starter", "persona-admin-starter", "starter_id", true, !ready, options...),
				personaAdminEditorInput(locale, "persona_id", "persona-admin-id", "persona_id", "text", true, "", !ready),
				personaAdminEditorReadOnlyInput(locale, "handle", "persona-admin-handle", first.Handle, !ready),
				personaAdminEditorReadOnlyInput(locale, "name", "persona-admin-name", first.Name, !ready),
				personaAdminEditorReadOnlyInput(locale, "purpose", "persona-admin-purpose-input", first.Purpose, !ready),
				personaAdminEditorInput(locale, "owner", "persona-admin-owner", "business_owner_id", "text", true, "", !ready),
				personaAdminEditorInput(locale, "steward", "persona-admin-steward", "technical_steward_id", "text", true, "", !ready),
				personaAdminEditorManifestInput(locale, first.ManifestID, !ready),
				personaAdminEditorInput(locale, "avatar", "persona-admin-avatar", "avatar_ref", "text", true, "avatar:default", !ready),
				personaAdminEditorInput(locale, "organization_scopes", "persona-admin-scopes", "organization_scopes", "text", true, "", !ready),
			),
			html.Div(html.Props{Class: "persona-admin-editor-field"}, html.Label(html.Props{For: "persona-admin-instructions"}, ui.Text(personaAdminEditorText(locale, "instructions"))), html.Textarea(html.Props{ID: "persona-admin-instructions", Rows: 4, ReadOnly: true, Disabled: !ready, Raw: map[string]any{"aria-describedby": "persona-admin-editor-help", "data-starter-instructions": "server-owned"}}, ui.Text(personaAdminEditorText(locale, "instructions_bound")))),
			personaAdminChannelFieldset(locale, channels),
			html.P(html.Props{ID: "persona-admin-editor-help", Class: "muted"}, ui.Text(personaAdminEditorText(locale, "server_bounds"))),
			status,
			html.Button(html.Props{ID: "persona-admin-create-submit", Class: "button primary", Type: "submit", Disabled: !ready}, ui.Text(personaAdminEditorText(locale, "create_action"))),
		),
	)
}

func personaAdminReadyStarters(snapshot PersonaAdminSnapshot) []PersonaAdminStarter {
	if !snapshot.StarterCatalogAvailable {
		return nil
	}
	starters := make([]PersonaAdminStarter, 0, len(snapshot.Starters))
	for _, starter := range snapshot.Starters {
		if strings.TrimSpace(starter.ID) == "" || starter.Version == 0 || strings.TrimSpace(starter.ManifestID) == "" || !hasPersonaAdminValue(starter.SkillGrantIDs) || !hasPersonaAdminValue(starter.ChannelClasses) {
			continue
		}
		starters = append(starters, starter)
	}
	return starters
}

func hasPersonaAdminValue(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func personaAdminStarterStatusKey(snapshot PersonaAdminSnapshot) string {
	if !snapshot.StarterCatalogAvailable {
		return "provisioning_unavailable"
	}
	return "no_ready_starters"
}

func personaAdminChannelFieldset(locale LocaleContext, channels []ui.Node) ui.Node {
	children := make([]ui.Node, 0, len(channels)+1)
	children = append(children, html.Legend(html.Props{}, ui.Text(personaAdminEditorText(locale, "allowed_channels"))))
	children = append(children, channels...)
	return html.Fieldset(html.Props{Class: "persona-admin-channel-choices", Raw: map[string]any{"aria-describedby": "persona-admin-editor-help"}}, children...)
}

func personaAdminEditorInput(locale LocaleContext, key, id, name, kind string, required bool, value string, disabled bool) ui.Node {
	return html.Div(html.Props{Class: "persona-admin-editor-field"}, html.Label(html.Props{For: id}, ui.Text(personaAdminEditorText(locale, key))), html.Input(html.Props{ID: id, Name: name, Type: kind, Required: required, Value: value, Disabled: disabled}))
}

func personaAdminEditorReadOnlyInput(locale LocaleContext, key, id, value string, disabled bool) ui.Node {
	return html.Div(html.Props{Class: "persona-admin-editor-field"}, html.Label(html.Props{For: id}, ui.Text(personaAdminEditorText(locale, key))), html.Input(html.Props{ID: id, Type: "text", ReadOnly: true, Value: value, Disabled: disabled, Raw: map[string]any{"data-starter-default": key, "readonly": "readonly", "aria-readonly": "true"}}))
}

func personaAdminEditorManifestInput(locale LocaleContext, value string, disabled bool) ui.Node {
	id := "persona-admin-manifest"
	return html.Div(html.Props{Class: "persona-admin-editor-field"}, html.Label(html.Props{For: id}, ui.Text(personaAdminEditorText(locale, "manifest"))), html.Input(html.Props{ID: id, Name: "manifest_id", Type: "text", ReadOnly: true, Value: value, Disabled: disabled, Raw: map[string]any{"readonly": "readonly", "aria-readonly": "true", "data-starter-default": "manifest"}}))
}

func personaAdminEditorSelect(locale LocaleContext, key, id, name string, required, disabled bool, options ...ui.Node) ui.Node {
	return html.Div(html.Props{Class: "persona-admin-editor-field"}, html.Label(html.Props{For: id}, ui.Text(personaAdminEditorText(locale, key))), html.Select(html.Props{ID: id, Name: name, Required: required, Disabled: disabled}, options...))
}

func personaAdminEditorText(locale LocaleContext, key string) string {
	translations := map[string][3]string{
		"additional_templates_unavailable": {"Configure existing personas in the catalog. Additional starter templates have not been configured.", "Konfigurieren Sie vorhandene Personas im Katalog. Weitere Vorlagen sind noch nicht eingerichtet.", "يمكنك إعداد الشخصيات الموجودة في الكتالوج. لم تُعدّ قوالب إضافية بعد."},
		"local_setup":                      {"Set up demo Policy Helper", "Demo-Policy-Helper einrichten", "إعداد مساعد السياسات التجريبي"},
		"create_title":                     {"Create a persona draft", "Persona-Entwurf erstellen", "إنشاء مسودة شخصية"},
		"create_detail":                    {"Start from a reviewed template. Server policy supplies executable instructions and the maximum allowed channels.", "Beginnen Sie mit einer geprüften Vorlage. Die Serverrichtlinie liefert Anweisungen und die maximal zulässigen Kanäle.", "ابدأ بقالب مُراجع. تحدد سياسة الخادم التعليمات والقنوات المسموح بها كحد أقصى."},
		"starter":                          {"Starter", "Vorlage", "القالب"}, "persona_id": {"Persona ID", "Persona-ID", "معرّف الشخصية"}, "handle": {"Handle", "Handle", "المعرّف"}, "name": {"Name", "Name", "الاسم"}, "purpose": {"Purpose", "Zweck", "الغرض"},
		"owner": {"Business owner", "Fachverantwortliche Person", "المالك المسؤول"}, "steward": {"Technical steward", "Technische Betreuung", "المسؤول التقني"}, "manifest": {"Agent manifest", "Agent-Manifest", "بيان الوكيل"}, "avatar": {"Avatar reference", "Avatar-Referenz", "مرجع الصورة"}, "organization_scopes": {"Organization scopes (comma-separated)", "Organisationsbereiche (durch Komma getrennt)", "نطاقات المؤسسة (مفصولة بفواصل)"},
		"instructions": {"Instructions", "Anweisungen", "التعليمات"}, "instructions_bound": {"Instructions are pinned to the approved starter manifest and cannot be changed here.", "Die Anweisungen sind an das genehmigte Starter-Manifest gebunden und können hier nicht geändert werden.", "التعليمات مرتبطة ببيان القالب المعتمد ولا يمكن تغييرها هنا."},
		"allowed_channels": {"Allowed channels", "Zulässige Kanäle", "القنوات المسموح بها"}, "private_channels": {"Private channels", "Private Kanäle", "قنوات خاصة"}, "public_channels": {"Public channels", "Öffentliche Kanäle", "قنوات عامة"}, "external_channels": {"External channels", "Externe Kanäle", "قنوات خارجية"},
		"server_bounds":            {"The first draft uses the starter's approved channels. Narrow them in a new version. Owner and steward must be different people.", "Der erste Entwurf nutzt die genehmigten Kanäle der Vorlage. Grenzen Sie sie in einer neuen Version ein. Eigentümer und Betreuung müssen verschieden sein.", "تستخدم المسودة الأولى قنوات القالب المعتمدة. يمكنك تضييقها في إصدار جديد. يجب أن يختلف المالك والمسؤول."},
		"ready":                    {"Ready to create a draft from an approved starter.", "Bereit, einen Entwurf aus einer genehmigten Vorlage zu erstellen.", "جاهز لإنشاء مسودة من قالب معتمد."},
		"provisioning_unavailable": {"Approved starters are unavailable because the manifest and skill grant catalog is not connected. Draft creation is disabled.", "Genehmigte Vorlagen sind nicht verfügbar, da der Katalog für Manifeste und Fähigkeitsfreigaben nicht verbunden ist. Die Erstellung ist deaktiviert.", "القوالب المعتمدة غير متاحة لأن دليل البيانات والمهارات غير متصل. تم تعطيل إنشاء المسودة."},
		"no_ready_starters":        {"No starter has a registered manifest and active skill grants yet. Draft creation is disabled until one is provisioned.", "Für keine Vorlage gibt es bisher ein registriertes Manifest und aktive Fähigkeitsfreigaben. Die Erstellung bleibt deaktiviert, bis eine Vorlage bereitgestellt ist.", "لا يوجد قالب ببيان مسجل ومنح مهارات نشطة حتى الآن. تم تعطيل الإنشاء إلى حين توفير قالب."},
		"create_action":            {"Create draft", "Entwurf erstellen", "إنشاء المسودة"},
		"command_success":          {"Command completed. The server confirmed the change.", "Befehl abgeschlossen. Der Server hat die Änderung bestätigt.", "اكتمل الأمر. أكد الخادم التغيير."},
		"command_forbidden":        {"You do not have permission to make this change. Ask a persona administrator for access.", "Sie haben keine Berechtigung für diese Änderung. Wenden Sie sich an eine Persona-Administration.", "ليست لديك صلاحية لإجراء هذا التغيير. اطلب الوصول من مسؤول الشخصيات."},
		"command_invalid":          {"The draft was rejected. Check the required fields, then confirm the selected starter still has a registered manifest and active skill grants. No change was confirmed.", "Der Entwurf wurde abgelehnt. Prüfen Sie die Pflichtfelder und ob die Vorlage weiterhin ein registriertes Manifest und aktive Fähigkeitsfreigaben hat. Es wurde keine Änderung bestätigt.", "تم رفض المسودة. تحقق من الحقول المطلوبة ومن أن القالب ما زال يحتوي على بيان مسجل ومنح مهارات نشطة. لم يتم تأكيد أي تغيير."},
		"command_conflict":         {"The persona changed before this command completed. Refresh the page and review its current state.", "Die Persona wurde vor Abschluss des Befehls geändert. Aktualisieren Sie die Seite und prüfen Sie den aktuellen Status.", "تغيرت الشخصية قبل اكتمال الأمر. حدّث الصفحة وراجع حالتها الحالية."},
		"command_unavailable":      {"The server could not complete this command. No change was confirmed. Try again later or contact an administrator.", "Der Server konnte den Befehl nicht ausführen. Es wurde keine Änderung bestätigt. Versuchen Sie es später erneut oder wenden Sie sich an die Administration.", "تعذر على الخادم تنفيذ الأمر. لم يتم تأكيد أي تغيير. حاول لاحقاً أو اتصل بالمسؤول."},
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

// PersonaAdminCommandStatusText returns a localized, non-sensitive explanation
// for a command result code. Unknown backend codes are treated as unavailable.
func PersonaAdminCommandStatusText(locale LocaleContext, code string) string {
	key := map[string]string{
		"success": "command_success", "forbidden": "command_forbidden", "invalid": "command_invalid",
		"conflict": "command_conflict", "unavailable": "command_unavailable",
	}[code]
	if key == "" {
		key = "command_unavailable"
	}
	return personaAdminEditorText(locale, key)
}
