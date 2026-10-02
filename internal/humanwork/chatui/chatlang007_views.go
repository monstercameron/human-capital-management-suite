package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
)

// CHATLANG-007: translation has two homes. The workspace's settings (the
// switch, the languages offered, the monthly limit, the outside-service switch
// and the glossary) are on the Chat settings administration page, each setting
// saving as soon as it changes with "Saved" beside it. A channel's details hold
// one row, "Translation: Follow the workspace / On / Off", with "Never use an
// outside service"; it shows only while the workspace has translation on, and
// otherwise one line says it is off, with a link to the page for the people who
// may change that. Before, all of it sat in one channel's details in a narrow
// column with two Save buttons, and turning the workspace switch on hid the
// channel block until the panel was reopened.

const (
	translationModeWorkspace = "workspace"
	translationModeChannel   = "channel"
	// translationAdminPage is the Chat settings administration page.
	translationAdminPage = "/workspace/app/admin/chat-settings"
)

// translationAdminShell is what the administration shows before it has data, or
// when it cannot have any: the loading line, the refusal or the failure. It is
// nil once there is data to draw.
func translationAdminShell(m TranslationAdminModel, t func(string) string) ui.Node {
	dir := direction(m.Locale)
	switch {
	case !m.Loaded && !m.Failed:
		return html.Div(html.Props{Class: "chatlangadmin", Dir: dir, Role: "status", Aria: map[string]string{"busy": "true"}}, html.P(html.Props{Text: t("loading")}))
	case m.Denied:
		return html.Div(html.Props{Class: "chatlangadmin", Dir: dir, Role: "alert"}, html.P(html.Props{Text: t("denied")}))
	case m.Failed && !m.Loaded:
		return html.Div(html.Props{Class: "chatlangadmin", Dir: dir, Role: "alert"}, html.P(html.Props{Text: t(m.failureKey())}))
	}
	return nil
}

// translationSavedMark is "Saved" beside the setting whose change was saved
// last, spoken politely to assistive technology.
func translationSavedMark(m TranslationAdminModel, t func(string) string, field string) ui.Node {
	if !m.Saved || m.Loading || m.SavedField != field {
		return nil
	}
	return html.Span(html.Props{Class: "chatlangadmin-saved", Role: "status", Aria: map[string]string{"live": "polite"}}, icon("check"), html.Span(html.Props{Text: strings.TrimSuffix(t("saved"), ".")}))
}

// TranslationWorkspaceForm draws the workspace's translation settings and its
// glossary for the administration page. Nothing in it has a Save button: a
// change is saved when it is made.
func TranslationWorkspaceForm(m TranslationAdminModel, text func(string) string) ui.Node {
	t := func(key string) string { return translationAdminText(m.Locale, key, text) }
	if shell := translationAdminShell(m, t); shell != nil {
		return shell
	}
	d := m.Data
	if !d.CanManageWorkspace {
		return html.Div(html.Props{Class: "chatlangadmin", Dir: direction(m.Locale), Role: "alert"}, html.P(html.Props{Text: t("denied")}))
	}
	problem := ""
	switch {
	case m.Loading:
		problem = t("loading")
	case m.Failed:
		problem = t(m.failureKey())
	}
	check := func(id, name, label, hint string, checked bool, field string) ui.Node {
		return html.Div(html.Props{Class: "chatlangadmin-setting"},
			html.Label(html.Props{Class: "chatlangadmin-check", For: id},
				html.Input(html.Props{ID: id, Class: "switch", Role: "switch", Name: name, Type: "checkbox", Checked: checked, Disabled: m.Loading, Aria: map[string]string{"describedby": id + "-hint"}}),
				html.Span(html.Props{Text: label}), translationSavedMark(m, t, field)),
			html.P(html.Props{ID: id + "-hint", Class: "field-hint", Text: hint}))
	}
	languages := []ui.Node{html.Legend(html.Props{Text: t("languages")}), translationSavedMark(m, t, "language")}
	for _, language := range d.Supported {
		id := "chatlangadmin-language-" + language
		languages = append(languages, html.Label(html.Props{Class: "chatlangadmin-check", For: id},
			html.Input(html.Props{ID: id, Name: "language", Type: "checkbox", Value: language, Checked: translationAdminHas(d.Workspace.Languages, language), Disabled: m.Loading}),
			html.Span(html.Props{Text: t(language)})))
	}
	languages = append(languages, html.P(html.Props{Class: "field-hint", Text: t("languages_hint")}))
	limit := TranslationAdminAmount(d.BudgetMicros)
	fields := []ui.Node{
		check("chatlangadmin-enabled", "enabled", t("enabled"), t("enabled_hint"), d.Workspace.Enabled, "enabled"),
		html.Fieldset(html.Props{Class: "chatlangadmin-languages"}, languages...),
		html.Div(html.Props{Class: "chatlangadmin-setting"},
			html.Label(html.Props{For: "chatlangadmin-limit"}, ui.Text(t("limit")), translationSavedMark(m, t, "limit")),
			html.Input(html.Props{ID: "chatlangadmin-limit", Name: "limit", Type: "number", Min: "0", Step: "0.01", Value: limit, Disabled: m.Loading, Aria: map[string]string{"describedby": "chatlangadmin-limit-hint"}}),
			html.P(html.Props{ID: "chatlangadmin-limit-hint", Class: "field-hint", Text: t("limit_hint")}),
			html.P(html.Props{Class: "chatlangadmin-used", Text: strings.NewReplacer("{used}", TranslationAdminAmount(d.SpentMicros), "{limit}", limit).Replace(t("used"))})),
	}
	if d.Paused {
		fields = append(fields, html.P(html.Props{Class: "chatlangadmin-paused", Role: "status", Text: t("paused")}))
	}
	fields = append(fields, check("chatlangadmin-external", "external", t("external"), t("external_hint"), d.Workspace.ExternalAllowed, "external"))
	if d.Engine.Name != "" {
		fields = append(fields, html.P(html.Props{Class: "field-hint", Text: strings.ReplaceAll(t("engine"), "{name}", d.Engine.Name)}))
	}
	sections := []ui.Node{
		html.H2(html.Props{ID: "chatlangadmin-workspace-title", Text: t("title")}),
		html.P(html.Props{Class: "field-hint", Text: t("page_intro")}),
		html.P(html.Props{ID: "chatlangadmin-status", Role: "status", Aria: map[string]string{"live": "polite"}, Text: problem}),
		html.Form(html.Props{Class: "chatlangadmin-form", Data: map[string]string{"chatlangadmin": "workspace"}, OnChange: m.SaveWorkspace, OnSubmit: m.SaveWorkspace, Aria: map[string]string{"labelledby": "chatlangadmin-workspace-title"}}, fields...),
		translationGlossary(m, t),
	}
	return html.Section(html.Props{Class: "chatlangadmin chatlangadmin-page", Dir: direction(m.Locale)}, sections...)
}

// translationGlossary is the glossary: its terms, and the form that adds one.
func translationGlossary(m TranslationAdminModel, t func(string) string) ui.Node {
	d := m.Data
	list := []ui.Node{}
	for _, term := range d.Glossary {
		label := term.Source + " → "
		if term.Language == "" {
			label = term.Source + " (" + t("keeps") + ")"
		} else {
			label += term.Target + " (" + t(term.Language) + ")"
		}
		list = append(list, html.Li(html.Props{Class: "chatlangadmin-term"},
			html.Span(html.Props{Class: "chatlangadmin-term-text", Dir: "auto", Text: label}),
			html.Button(html.Props{Type: "button", Class: "button secondary", Text: t("remove"), Data: map[string]string{"term": term.ID}, OnClick: m.RemoveTerm, Disabled: m.Loading || m.RemoveTerm.Value() == nil, Aria: map[string]string{"label": t("remove") + ": " + term.Source}})))
	}
	glossary := []ui.Node{html.H3(html.Props{ID: "chatlangadmin-glossary-title", Text: t("glossary")}), html.P(html.Props{Class: "field-hint", Text: t("glossary_hint")})}
	if len(list) == 0 {
		glossary = append(glossary, html.P(html.Props{Text: t("glossary_empty")}))
	} else {
		glossary = append(glossary, html.Ul(html.Props{Class: "chatlangadmin-terms"}, list...))
	}
	languageOptions := []ui.Node{}
	for _, language := range d.Supported {
		languageOptions = append(languageOptions, html.Option(html.Props{Value: language, Text: t(language)}))
	}
	glossary = append(glossary, html.Form(html.Props{Class: "chatlangadmin-form", Data: map[string]string{"chatlangadmin": "term"}, OnSubmit: m.AddTerm, Aria: map[string]string{"labelledby": "chatlangadmin-glossary-title"}},
		html.Label(html.Props{For: "chatlangadmin-term", Text: t("term")}),
		html.Input(html.Props{ID: "chatlangadmin-term", Name: "term", Type: "text", MaxLength: 120, Disabled: m.Loading, Dir: "auto"}),
		html.Label(html.Props{For: "chatlangadmin-mode", Text: t("mode")}),
		html.Select(html.Props{ID: "chatlangadmin-mode", Name: "mode", Disabled: m.Loading}, html.Option(html.Props{Value: "keep", Text: t("keep")}), html.Option(html.Props{Value: "translate", Text: t("translate")})),
		html.Label(html.Props{For: "chatlangadmin-term-language", Text: t("language")}),
		html.Select(html.Props{ID: "chatlangadmin-term-language", Name: "language", Disabled: m.Loading}, languageOptions...),
		html.Label(html.Props{For: "chatlangadmin-target", Text: t("target")}),
		html.Input(html.Props{ID: "chatlangadmin-target", Name: "target", Type: "text", MaxLength: 120, Disabled: m.Loading, Dir: "auto"}),
		html.Div(html.Props{Class: "chatlangadmin-actions"}, html.Button(html.Props{Type: "submit", Class: "button", Text: t("add"), Disabled: m.Loading || m.AddTerm.Value() == nil}), translationSavedMark(m, t, "glossary")),
	))
	return html.Section(html.Props{Class: "chatlangadmin-glossary"}, glossary...)
}

// TranslationChannelRow is the Translation row in a channel's details. It is
// nil while the settings are loading or when this person may not change the
// channel; one line when the workspace has translation off; and otherwise a
// disclosure row whose value is the channel's choice.
func TranslationChannelRow(m TranslationAdminModel, text func(string) string) ui.Node {
	return translationChannelRow(m, text, nil)
}

// translationChannelRow is TranslationChannelRow, optionally drawn as the
// content of the details panel's own row (CHATUX-027): then a setting that
// could not load or save says so inside the row, in full, with Try again.
func translationChannelRow(m TranslationAdminModel, text func(string) string, section sectionWrap) ui.Node {
	t := func(key string) string { return translationAdminText(m.Locale, key, text) }
	if m.Denied || (!m.Loaded && !m.Failed) {
		return nil
	}
	if !m.Loaded {
		if section != nil {
			return section(sectionContent{Label: t("title"), Failed: true, Body: chatux027Error(Model{Locale: m.Locale, Text: text}, t(m.failureKey()), m.Retry, "")})
		}
		return manageNote(t(m.failureKey()))
	}
	d := m.Data
	if !d.Workspace.Enabled {
		line := []ui.Node{html.Span(html.Props{Text: t("workspace_off_line")})}
		if d.CanManageWorkspace {
			line = append(line, ui.Text(" "), html.A(html.Props{Class: "chatlangadmin-link", Href: translationAdminPage + "?locale=" + m.Locale, Text: t("open_admin")}))
		}
		if section != nil {
			return section(sectionContent{Label: t("title"), Body: html.Div(html.Props{Class: "chatlangadmin-off", Dir: direction(m.Locale)}, line...)})
		}
		return html.Div(html.Props{Class: "manage-note chatlangadmin-off", Dir: direction(m.Locale)}, line...)
	}
	if !d.CanManageChannel || d.Channel == nil {
		return nil
	}
	current := string(d.Channel.Translation)
	if current == "" {
		current = "inherit"
	}
	label := map[string]string{"inherit": t("follow"), "on": t("on"), "off": t("off")}[current]
	options := []ui.Node{}
	for _, v := range [][2]string{{"inherit", "follow"}, {"on", "on"}, {"off", "off"}} {
		options = append(options, html.Option(html.Props{Value: v[0], Text: t(v[1]), Selected: v[0] == current}))
	}
	status := ""
	if m.Loading {
		status = t("loading")
	} else if m.Failed && section == nil {
		status = t(m.failureKey())
	}
	form := html.Form(html.Props{Class: "chatlangadmin-channel-form", Dir: direction(m.Locale), Data: map[string]string{"chatlangadmin": "channel"}, OnChange: m.SaveChannel, OnSubmit: m.SaveChannel},
		html.Label(html.Props{For: "chatlangadmin-channel-switch", Text: t("channel_switch")}),
		html.Select(html.Props{ID: "chatlangadmin-channel-switch", Class: "chat-input", Name: "translation", Disabled: m.Loading}, options...),
		html.Label(html.Props{Class: "chatlangadmin-check", For: "chatlangadmin-channel-barred"},
			html.Input(html.Props{ID: "chatlangadmin-channel-barred", Name: "barred", Type: "checkbox", Checked: d.Channel.External == chatlang.ExternalBarred, Disabled: m.Loading}),
			html.Span(html.Props{Text: t("never_outside")})),
		html.P(html.Props{Class: "chatlangadmin-effective", Role: "status", Text: t("status_" + string(d.Effective))}),
		html.P(html.Props{Class: "chatlangadmin-status", Role: "status", Aria: map[string]string{"live": "polite"}, Text: status}),
		translationSavedMark(m, t, "channel"))
	if section != nil {
		body := []ui.Node{form}
		if m.Failed {
			body = append(body, chatux027Error(Model{Locale: m.Locale, Text: text}, t(m.failureKey()), m.Retry, ""))
		}
		return section(sectionContent{Label: t("title"), Value: ui.Text(label), Failed: m.Failed, Body: html.Div(html.Props{}, body...)})
	}
	return manageRow("chatlangadmin-entry", t("title"), ui.Text(label), form)
}

// TranslationAdminSettings is the workspace's translation settings and
// glossary, for the Chat settings administration page. It asks the translation
// administration itself, so the page only has to place it.
func TranslationAdminSettings(locale string, text func(string) string) ui.Node {
	return ui.CreateElement(translationAdminPanel, translationAdminProps{Locale: locale, Mode: translationModeWorkspace, Text: text})
}

// translationSettingsEntry is the Translation row of Conversation details:
// present only for a person who may administer the conversation, and only when
// the server has an engine composed.
func translationSettingsEntry(m Model, c Conversation) ui.Node {
	if m.ChatFeatures == nil || !m.ChatFeatures.Translation || !canAdministerConversation(m, c) {
		return nil
	}
	// Keyed by conversation so opening another one reads its own settings.
	return html.WithKey(ui.CreateElement(translationAdminPanel, translationAdminProps{Locale: m.Locale, Conversation: c.ID, Mode: translationModeChannel, Text: m.Text}), "chatlangadmin-"+c.ID)
}

// TranslationAdminPageStyles is the administration page's own sheet. The chat
// stylesheet is scoped to the chat workspace, which the page is not inside, so
// the page asks for the translation rules itself.
const TranslationAdminPageStyles = ChatlangAdminStyles + ChatlangPageStyles

// ChatlangPageStyles finishes the page's settings: each setting is a block with
// its "Saved" beside it, and the heading and the hints read at the page's size.
const ChatlangPageStyles = `.chatlangadmin-page{border:0;padding:calc(var(--hcm-space-4) * var(--hcm-density))}.chatlangadmin-page h2,.chatlangadmin-page h3{margin:0}.chatlangadmin-setting{display:grid;gap:4px}.chatlangadmin-setting>label:not(.chatlangadmin-check){display:flex;flex-wrap:wrap;align-items:baseline;gap:8px;font-weight:600}.chatlangadmin-saved{display:inline-flex;align-items:center;gap:4px;color:var(--hcm-color-success);font-size:.8125rem;font-weight:600}.chatlangadmin-saved .chat-icon{inline-size:14px;block-size:14px}.chatlangadmin-actions{display:flex;flex-wrap:wrap;align-items:center;gap:8px}.chatlangadmin-channel-form{display:flex;flex-direction:column;gap:6px}.chatlangadmin-channel-form label{font-size:.75rem;color:var(--hcm-color-text-muted)}.chatlangadmin-channel-form .chatlangadmin-check{min-block-size:36px;color:var(--hcm-color-text)}.chatlangadmin-channel-form .chatlangadmin-check input{inline-size:16px;block-size:16px}.chatlangadmin-channel-form p{margin:0;font-size:.75rem;color:var(--hcm-color-text-muted)}.chatlangadmin-channel-form p:empty{display:none}.chatlangadmin-off{color:var(--hcm-color-text-muted)}.chatlangadmin-link{color:var(--hcm-color-brand-primary)}`
