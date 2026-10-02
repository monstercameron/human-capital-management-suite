package chatui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
)

// TranslationAdminTerm is one glossary entry as the administration page lists it.
type TranslationAdminTerm struct {
	ID string `json:"id"`
	chatlang.Term
}

// TranslationAdminData is what the translation administration answers: the same
// shape the server sends, with no message text in it.
type TranslationAdminData struct {
	Workspace chatlang.Workspace     `json:"workspace"`
	Channel   *chatlang.Channel      `json:"channel,omitempty"`
	Effective chatlang.Reason        `json:"effective"`
	Glossary  []TranslationAdminTerm `json:"glossary"`
	Engine    struct {
		Name     string `json:"name"`
		Ready    bool   `json:"ready"`
		External bool   `json:"external"`
	} `json:"engine"`
	SpentMicros        int64    `json:"spent_micros"`
	BudgetMicros       int64    `json:"budget_micros"`
	Paused             bool     `json:"paused"`
	Supported          []string `json:"supported"`
	CanManageWorkspace bool     `json:"can_manage_workspace"`
	CanManageChannel   bool     `json:"can_manage_channel"`
}

// TranslationAdminModel is everything the form needs to draw. Each handler is
// bound by the client; a nil handler draws the control disabled.
type TranslationAdminModel struct {
	Locale       string
	Conversation string
	Data         TranslationAdminData
	// Loaded is false until the server has answered once.
	Loaded                 bool
	Loading, Failed, Saved bool
	// Denied is true when the server refused the caller's administration.
	Denied                              bool
	SaveWorkspace, SaveChannel, AddTerm ui.Handler
	RemoveTerm                          ui.Handler
}

// translationAdminText returns the text of a key in the person's language. The
// product catalog may carry it; when it does not (or answers with a missing-key
// marker) the table here answers, so a key is never shown on the page.
func translationAdminText(locale, key string, text func(string) string) string {
	values, ok := translationAdminCopy[key]
	if !ok {
		return ""
	}
	index := 0
	if strings.HasPrefix(locale, "de") {
		index = 1
	}
	if strings.HasPrefix(locale, "ar") {
		index = 2
	}
	return chatbug039Text(key, values[index], values[0])
}

// translationAdminCopy holds every visible string in en-US, de-DE and ar.
var translationAdminCopy = map[string][3]string{
	"title":                  {"Translation", "Übersetzung", "الترجمة"},
	"intro":                  {"Readers can see messages in their own language. The message as written is always kept as the record.", "Leser sehen Nachrichten in ihrer eigenen Sprache. Die Nachricht im Wortlaut bleibt immer als Nachweis erhalten.", "يستطيع القراء رؤية الرسائل بلغتهم. تبقى الرسالة كما كُتبت محفوظة دائماً كسجل."},
	"manage":                 {"Manage translation", "Übersetzung verwalten", "أدر الترجمة"},
	"loading":                {"Loading translation settings. Please wait.", "Übersetzungseinstellungen werden geladen. Bitte warten.", "جار تحميل إعدادات الترجمة. يرجى الانتظار."},
	"error":                  {"Translation settings could not load or save. Try again.", "Übersetzungseinstellungen konnten nicht geladen oder gespeichert werden. Versuchen Sie es erneut.", "تعذر تحميل إعدادات الترجمة أو حفظها. حاول مجدداً."},
	"retry":                  {"Try again", "Erneut versuchen", "حاول مجدداً"},
	"denied":                 {"Only a workspace administrator or this channel's manager can change translation.", "Nur Arbeitsbereichsadministratoren oder die Verwaltung dieses Kanals können die Übersetzung ändern.", "يمكن فقط لمسؤول مساحة العمل أو مدير هذه القناة تغيير الترجمة."},
	"saved":                  {"Saved.", "Gespeichert.", "تم الحفظ."},
	"save":                   {"Save", "Speichern", "احفظ"},
	"workspace":              {"Whole workspace", "Gesamter Arbeitsbereich", "مساحة العمل كاملة"},
	"enabled":                {"Translate messages into each reader's language", "Nachrichten in die Sprache jedes Lesers übersetzen", "ترجم الرسائل إلى لغة كل قارئ"},
	"enabled_hint":           {"Off until you turn it on. People also choose, in their own settings, whether to read translations.", "Aus, bis Sie es einschalten. Jede Person entscheidet außerdem in ihren Einstellungen, ob sie Übersetzungen lesen möchte.", "متوقفة حتى تشغّلها. ويختار كل شخص في إعداداته ما إذا كان يريد قراءة الترجمات."},
	"languages":              {"Languages offered", "Angebotene Sprachen", "اللغات المتاحة"},
	"languages_hint":         {"Leave all unticked to offer every language.", "Lassen Sie alle abgewählt, um jede Sprache anzubieten.", "اترك الكل غير محدد لإتاحة جميع اللغات."},
	"limit":                  {"Monthly limit (USD)", "Monatliches Limit (USD)", "الحد الشهري (دولار أمريكي)"},
	"limit_hint":             {"When the limit is reached, translation stops until next month and messages show in the language they were written in.", "Ist das Limit erreicht, pausiert die Übersetzung bis zum nächsten Monat und Nachrichten erscheinen in ihrer Originalsprache.", "عند بلوغ الحد تتوقف الترجمة حتى الشهر التالي وتظهر الرسائل باللغة التي كُتبت بها."},
	"used":                   {"Used this month: {used} of {limit}", "Diesen Monat verbraucht: {used} von {limit}", "المستخدم هذا الشهر: {used} من {limit}"},
	"paused":                 {"Translation is paused: this month's limit is used. It resumes next month, or when you raise the limit.", "Die Übersetzung pausiert: Das Limit dieses Monats ist aufgebraucht. Sie läuft im nächsten Monat weiter oder sobald Sie das Limit erhöhen.", "الترجمة متوقفة: استُنفد حد هذا الشهر. ستستأنف الشهر القادم أو عند رفع الحد."},
	"external":               {"Allow sending text to an outside translation service", "Senden von Text an einen externen Übersetzungsdienst erlauben", "السماح بإرسال النص إلى خدمة ترجمة خارجية"},
	"external_hint":          {"Turn off to keep every message inside this deployment; nothing is translated while no engine inside it exists.", "Ausschalten, damit jede Nachricht in dieser Installation bleibt; solange es dort keine Übersetzungsmaschine gibt, wird nichts übersetzt.", "أوقفه لإبقاء كل رسالة داخل هذا النشر؛ لا يُترجم شيء ما دام لا يوجد محرك بداخله."},
	"engine":                 {"Translation engine: {name}", "Übersetzungsmaschine: {name}", "محرك الترجمة: {name}"},
	"channel":                {"This channel", "Dieser Kanal", "هذه القناة"},
	"channel_switch":         {"Translation in this channel", "Übersetzung in diesem Kanal", "الترجمة في هذه القناة"},
	"follow":                 {"Follow the workspace", "Wie der Arbeitsbereich", "اتبع مساحة العمل"},
	"on":                     {"On", "An", "مفعّلة"},
	"off":                    {"Off", "Aus", "متوقفة"},
	"barred":                 {"Never send this channel's messages to an outside translation service", "Nachrichten dieses Kanals nie an einen externen Übersetzungsdienst senden", "لا ترسل رسائل هذه القناة إلى خدمة ترجمة خارجية أبداً"},
	"status_":                {"Translation is on in this channel.", "Die Übersetzung ist in diesem Kanal eingeschaltet.", "الترجمة مفعّلة في هذه القناة."},
	"status_workspace_off":   {"Translation is off for this workspace. A workspace administrator can turn it on.", "Die Übersetzung ist für diesen Arbeitsbereich ausgeschaltet. Ein Arbeitsbereichsadministrator kann sie einschalten.", "الترجمة متوقفة لمساحة العمل هذه. يمكن لمسؤول مساحة العمل تشغيلها."},
	"status_channel_off":     {"Translation is off in this channel.", "Die Übersetzung ist in diesem Kanal ausgeschaltet.", "الترجمة متوقفة في هذه القناة."},
	"status_external_barred": {"Messages here are never sent to an outside translation service, so they are not translated.", "Nachrichten hier werden nie an einen externen Übersetzungsdienst gesendet und daher nicht übersetzt.", "لا تُرسل الرسائل هنا إلى خدمة ترجمة خارجية أبداً، لذلك لا تُترجم."},
	"glossary":               {"Glossary", "Glossar", "المسرد"},
	"glossary_hint":          {"Terms that must be translated a set way, or never translated, such as product names.", "Begriffe, die auf eine feste Weise übersetzt oder nie übersetzt werden, etwa Produktnamen.", "مصطلحات يجب ترجمتها بطريقة محددة أو عدم ترجمتها أبداً، مثل أسماء المنتجات."},
	"glossary_empty":         {"No terms yet.", "Noch keine Begriffe.", "لا توجد مصطلحات بعد."},
	"term":                   {"Term", "Begriff", "المصطلح"},
	"mode":                   {"How to translate it", "Wie übersetzen", "كيفية ترجمته"},
	"keep":                   {"Keep as written", "Unverändert lassen", "أبقه كما هو"},
	"translate":              {"Translate as", "Übersetzen als", "ترجمه إلى"},
	"language":               {"Language", "Sprache", "اللغة"},
	"target":                 {"Required translation", "Vorgeschriebene Übersetzung", "الترجمة المطلوبة"},
	"add":                    {"Add term", "Begriff hinzufügen", "أضف مصطلحاً"},
	"remove":                 {"Remove", "Entfernen", "أزل"},
	"keeps":                  {"kept as written", "unverändert", "يبقى كما هو"},
	"en":                     {"English", "English", "English"},
	"de":                     {"Deutsch", "Deutsch", "Deutsch"},
	"fr":                     {"Français", "Français", "Français"},
	"es":                     {"Español", "Español", "Español"},
	"pt":                     {"Português", "Português", "Português"},
	"ar":                     {"العربية", "العربية", "العربية"},
	"ja":                     {"日本語", "日本語", "日本語"},
	"hi":                     {"हिन्दी", "हिन्दी", "हिन्दी"},
}

// TranslationAdminAmount writes millionths of a unit as a two-decimal amount.
func TranslationAdminAmount(micros int64) string {
	if micros < 0 {
		micros = 0
	}
	whole, cents := micros/1_000_000, (micros%1_000_000)/10_000
	return strconv.FormatInt(whole, 10) + "." + strings.Repeat("0", 2-len(strconv.FormatInt(cents, 10))) + strconv.FormatInt(cents, 10)
}

func translationAdminHas(list []string, language string) bool {
	for _, l := range list {
		if l == language {
			return true
		}
	}
	return false
}

// TranslationAdminForm draws the administration: a short form for the
// workspace, one for the channel, and the glossary. Settings the caller may not
// change are not drawn.
func TranslationAdminForm(m TranslationAdminModel, text func(string) string) ui.Node {
	t := func(key string) string { return translationAdminText(m.Locale, key, text) }
	dir := direction(m.Locale)
	if !m.Loaded && !m.Failed {
		return html.Div(html.Props{Class: "chatlangadmin", Dir: dir, Role: "status", Aria: map[string]string{"busy": "true"}}, html.P(html.Props{Text: t("loading")}))
	}
	if m.Denied {
		return html.Div(html.Props{Class: "chatlangadmin", Dir: dir, Role: "alert"}, html.P(html.Props{Text: t("denied")}))
	}
	if m.Failed && !m.Loaded {
		return html.Div(html.Props{Class: "chatlangadmin", Dir: dir, Role: "alert"}, html.P(html.Props{Text: t("error")}))
	}
	d := m.Data
	status := ""
	switch {
	case m.Loading:
		status = t("loading")
	case m.Failed:
		status = t("error")
	case m.Saved:
		status = t("saved")
	}
	sections := []ui.Node{html.P(html.Props{Class: "field-hint", Text: t("intro")}), html.P(html.Props{ID: "chatlangadmin-status", Role: "status", Aria: map[string]string{"live": "polite"}, Text: status})}
	if d.CanManageWorkspace {
		fields := []ui.Node{html.H3(html.Props{ID: "chatlangadmin-workspace-title", Text: t("workspace")}),
			html.Label(html.Props{Class: "chatlangadmin-check", For: "chatlangadmin-enabled"}, html.Input(html.Props{ID: "chatlangadmin-enabled", Name: "enabled", Type: "checkbox", Checked: d.Workspace.Enabled, Disabled: m.Loading, Aria: map[string]string{"describedby": "chatlangadmin-enabled-hint"}}), html.Span(html.Props{Text: t("enabled")})),
			html.P(html.Props{ID: "chatlangadmin-enabled-hint", Class: "field-hint", Text: t("enabled_hint")}),
		}
		languages := []ui.Node{html.Legend(html.Props{Text: t("languages")})}
		for _, language := range d.Supported {
			id := "chatlangadmin-language-" + language
			languages = append(languages, html.Label(html.Props{Class: "chatlangadmin-check", For: id}, html.Input(html.Props{ID: id, Name: "language", Type: "checkbox", Value: language, Checked: translationAdminHas(d.Workspace.Languages, language), Disabled: m.Loading}), html.Span(html.Props{Text: t(language)})))
		}
		languages = append(languages, html.P(html.Props{Class: "field-hint", Text: t("languages_hint")}))
		fields = append(fields, html.Fieldset(html.Props{Class: "chatlangadmin-languages"}, languages...))
		limit := TranslationAdminAmount(d.BudgetMicros)
		fields = append(fields,
			html.Label(html.Props{For: "chatlangadmin-limit", Text: t("limit")}),
			html.Input(html.Props{ID: "chatlangadmin-limit", Name: "limit", Type: "number", Min: "0", Step: "0.01", Value: limit, Disabled: m.Loading, Aria: map[string]string{"describedby": "chatlangadmin-limit-hint"}}),
			html.P(html.Props{ID: "chatlangadmin-limit-hint", Class: "field-hint", Text: t("limit_hint")}),
			html.P(html.Props{Class: "chatlangadmin-used", Text: strings.NewReplacer("{used}", TranslationAdminAmount(d.SpentMicros), "{limit}", limit).Replace(t("used"))}),
		)
		if d.Paused {
			fields = append(fields, html.P(html.Props{Class: "chatlangadmin-paused", Role: "status", Text: t("paused")}))
		}
		fields = append(fields,
			html.Label(html.Props{Class: "chatlangadmin-check", For: "chatlangadmin-external"}, html.Input(html.Props{ID: "chatlangadmin-external", Name: "external", Type: "checkbox", Checked: d.Workspace.ExternalAllowed, Disabled: m.Loading, Aria: map[string]string{"describedby": "chatlangadmin-external-hint"}}), html.Span(html.Props{Text: t("external")})),
			html.P(html.Props{ID: "chatlangadmin-external-hint", Class: "field-hint", Text: t("external_hint")}),
		)
		if d.Engine.Name != "" {
			fields = append(fields, html.P(html.Props{Class: "field-hint", Text: strings.ReplaceAll(t("engine"), "{name}", d.Engine.Name)}))
		}
		fields = append(fields, html.Button(html.Props{Type: "submit", Class: "button", Text: t("save"), Disabled: m.Loading || m.SaveWorkspace.Value() == nil}))
		sections = append(sections, html.Form(html.Props{Class: "chatlangadmin-form", Data: map[string]string{"chatlangadmin": "workspace"}, OnSubmit: m.SaveWorkspace, Aria: map[string]string{"labelledby": "chatlangadmin-workspace-title"}}, fields...))
	}
	if d.CanManageChannel && d.Channel != nil {
		options := func(values [][2]string, current string) []ui.Node {
			var out []ui.Node
			for _, v := range values {
				out = append(out, html.Option(html.Props{Value: v[0], Text: t(v[1]), Selected: v[0] == current}))
			}
			return out
		}
		current := string(d.Channel.Translation)
		if current == "" {
			current = "inherit"
		}
		fields := []ui.Node{html.H3(html.Props{ID: "chatlangadmin-channel-title", Text: t("channel")}),
			html.P(html.Props{Class: "chatlangadmin-effective", Role: "status", Text: t("status_" + string(d.Effective))}),
			html.Label(html.Props{For: "chatlangadmin-channel-switch", Text: t("channel_switch")}),
			html.Select(html.Props{ID: "chatlangadmin-channel-switch", Name: "translation", Disabled: m.Loading}, options([][2]string{{"inherit", "follow"}, {"on", "on"}, {"off", "off"}}, current)...),
			html.Label(html.Props{Class: "chatlangadmin-check", For: "chatlangadmin-channel-barred"}, html.Input(html.Props{ID: "chatlangadmin-channel-barred", Name: "barred", Type: "checkbox", Checked: d.Channel.External == chatlang.ExternalBarred, Disabled: m.Loading}), html.Span(html.Props{Text: t("barred")})),
			html.Button(html.Props{Type: "submit", Class: "button", Text: t("save"), Disabled: m.Loading || m.SaveChannel.Value() == nil}),
		}
		sections = append(sections, html.Form(html.Props{Class: "chatlangadmin-form", Data: map[string]string{"chatlangadmin": "channel"}, OnSubmit: m.SaveChannel, Aria: map[string]string{"labelledby": "chatlangadmin-channel-title"}}, fields...))
	}
	if d.CanManageWorkspace {
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
			html.Button(html.Props{Type: "submit", Class: "button", Text: t("add"), Disabled: m.Loading || m.AddTerm.Value() == nil}),
		))
		sections = append(sections, html.Section(html.Props{Class: "chatlangadmin-glossary"}, glossary...))
	}
	return html.Div(html.Props{Class: "chatlangadmin", Dir: dir}, sections...)
}

// ChatlangAdminStyles are the administration's layout, from the product's own
// tokens only.
const ChatlangAdminStyles = `.chatlangadmin{box-sizing:border-box;min-width:0;max-width:100%;display:grid;gap:16px;padding:12px;color:var(--hcm-color-text);background:var(--hcm-color-surface);border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);overflow-wrap:anywhere}.chatlangadmin *{box-sizing:border-box;min-width:0}.chatlangadmin-form,.chatlangadmin-glossary{display:grid;gap:8px}.chatlangadmin input[type=text],.chatlangadmin input[type=number],.chatlangadmin select{width:100%;min-height:44px;padding:8px;color:var(--hcm-color-text);background:var(--hcm-color-surface);border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control)}.chatlangadmin button{min-height:44px;max-width:100%;white-space:normal}.chatlangadmin-check{display:flex;align-items:center;gap:8px;min-height:44px}.chatlangadmin-check input{flex:none;width:24px;height:24px;accent-color:var(--hcm-color-brand-primary)}.chatlangadmin-languages{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,140px),1fr));gap:4px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control)}.chatlangadmin-languages legend,.chatlangadmin-languages p{grid-column:1/-1}.chatlangadmin-terms{list-style:none;margin:0;padding:0;display:grid;gap:8px}.chatlangadmin-term{display:flex;flex-wrap:wrap;align-items:center;gap:8px}.chatlangadmin-term-text{flex:1 1 160px}.chatlangadmin-paused{color:var(--hcm-color-danger)}.chatlangadmin-used,.chatlangadmin-effective{color:var(--hcm-color-text-muted)}.chatlangadmin :focus-visible{outline:2px solid var(--hcm-color-brand-primary);outline-offset:2px}@media(max-width:390px){.chatlangadmin{padding:8px}}@media(prefers-reduced-motion:reduce){.chatlangadmin *{animation:none;transition:none}}`

// translationSettingsEntry is the Translation section of Conversation details:
// present only for a person who may administer the conversation, and only when
// the server has an engine composed. The settings open inside the section.
func translationSettingsEntry(m Model, c Conversation) ui.Node {
	if m.ChatFeatures == nil || !m.ChatFeatures.Translation || !canAdministerConversation(m, c) {
		return nil
	}
	// Keyed by conversation so opening another one starts closed and reloads.
	return html.WithKey(ui.CreateElement(translationSettingsSection, translationSettingsProps{Model: m}), "chatlangadmin-"+c.ID)
}

type translationSettingsProps struct{ Model Model }

func translationSettingsSection(props translationSettingsProps) ui.Node {
	m := props.Model
	open := ui.UseState(false)
	toggle := ui.UseEvent(func() { open.Update(func(current bool) bool { return !current }) })
	t := func(key string) string { return translationAdminText(m.Locale, key, m.Text) }
	children := []ui.Node{html.H3(html.Props{Text: t("title")}), html.P(html.Props{Class: "field-hint", Text: t("intro")}),
		html.Button(html.Props{Class: "chat-disclosure-button", Type: "button", OnClick: toggle, Aria: map[string]string{"expanded": boolString(open.Get())}},
			html.Span(html.Props{Text: t("manage")}), icon("chevron-down"))}
	if open.Get() {
		children = append(children, html.Div(html.Props{Class: "chat-disclosure-body chatlangadmin-entry-body"}, ui.CreateElement(translationAdminPanel, translationAdminProps{Locale: m.Locale, Conversation: m.SelectedID, Text: m.Text})))
	}
	return html.Section(html.Props{Class: "details-section chatlangadmin-entry", Dir: direction(m.Locale)}, children...)
}
