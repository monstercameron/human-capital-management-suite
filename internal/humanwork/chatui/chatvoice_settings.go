package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Voice switches (CHATVOICE-002 and -005). Three switches decide whether a
// voice message may be sent: the workspace's, the channel's and the person's.
// The server enforces them when a message is sent; this file is where people
// change them (one row each, in the workspace Chat settings, in Manage channel
// and in Chat preferences) and where the composer learns that voice is off so it
// can say why instead of hiding the control.

const chatvoiceSettingsCopyTable = "\noff_workspace\u0000Voice messages are turned off in this workspace\u0000Sprachnachrichten sind in diesem Arbeitsbereich ausgeschaltet\u0000الرسائل الصوتية متوقفة في مساحة العمل هذه\u0000" +
	"\noff_channel\u0000Voice messages are turned off in this channel\u0000Sprachnachrichten sind in diesem Kanal ausgeschaltet\u0000الرسائل الصوتية متوقفة في هذه القناة\u0000" +
	"\noff_personal\u0000Voice messages are turned off in your Chat preferences\u0000Sprachnachrichten sind in Ihren Chat-Einstellungen ausgeschaltet\u0000الرسائل الصوتية متوقفة في تفضيلات الدردشة لديك\u0000" +
	"\ntitle\u0000Voice messages\u0000Sprachnachrichten\u0000الرسائل الصوتية\u0000" +
	"\nws_intro\u0000People can record up to two minutes in a direct or group conversation, and in a channel where an administrator has turned voice on. Each voice message is transcribed so it can be read and searched.\u0000Personen können in Direktnachrichten und Gruppen sowie in Kanälen, in denen eine Administration Sprache aktiviert hat, bis zu zwei Minuten aufnehmen. Jede Sprachnachricht wird transkribiert, damit sie gelesen und durchsucht werden kann.\u0000يمكن للأشخاص تسجيل ما يصل إلى دقيقتين في المحادثات المباشرة والجماعية، وفي القناة التي فعّل مسؤول الصوت فيها. تُنسخ كل رسالة صوتية إلى نص لتُقرأ ويُبحث فيها.\u0000" +
	"\nws_switch\u0000Allow voice messages in this workspace\u0000Sprachnachrichten in diesem Arbeitsbereich erlauben\u0000السماح بالرسائل الصوتية في مساحة العمل هذه\u0000" +
	"\nws_hint\u0000Turn off to stop everyone from sending voice messages in every conversation. Messages already sent stay playable and readable.\u0000Ausschalten, damit niemand in irgendeiner Unterhaltung Sprachnachrichten sendet. Bereits gesendete Nachrichten bleiben abspielbar und lesbar.\u0000أوقف التشغيل لمنع الجميع من إرسال رسائل صوتية في كل المحادثات. تبقى الرسائل المرسلة قابلة للتشغيل والقراءة.\u0000" +
	"\nch_switch\u0000Allow voice messages in this channel\u0000Sprachnachrichten in diesem Kanal erlauben\u0000السماح بالرسائل الصوتية في هذه القناة\u0000" +
	"\nch_hint\u0000Off until you turn it on. When on, members can record up to two minutes here, unless voice is off in the workspace or in their own preferences.\u0000Aus, bis Sie es einschalten. Eingeschaltet können Mitglieder hier bis zu zwei Minuten aufnehmen, sofern Sprache im Arbeitsbereich oder in ihren eigenen Einstellungen nicht ausgeschaltet ist.\u0000متوقف حتى تشغّله. عند التشغيل يمكن للأعضاء تسجيل ما يصل إلى دقيقتين هنا، ما لم يكن الصوت متوقفًا في مساحة العمل أو في تفضيلاتهم.\u0000" +
	"\nme_switch\u0000Let me record voice messages\u0000Ich möchte Sprachnachrichten aufnehmen können\u0000السماح لي بتسجيل الرسائل الصوتية\u0000" +
	"\nme_hint\u0000Turn off to stop recording voice messages. You can still play and read the ones others send you.\u0000Ausschalten, um keine Sprachnachrichten mehr aufzunehmen. Die von anderen gesendeten können Sie weiterhin abspielen und lesen.\u0000أوقف التشغيل لإيقاف تسجيل الرسائل الصوتية. يمكنك مع ذلك تشغيل وقراءة الرسائل التي يرسلها الآخرون إليك.\u0000" +
	"\nengine\u0000Transcription engine\u0000Transkriptionsmodul\u0000محرك النسخ\u0000" +
	"\nengine_fixture\u0000Local development fixture. No speech model is called and transcripts are placeholders.\u0000Lokales Entwicklungs-Fixture. Es wird kein Sprachmodell aufgerufen und Transkripte sind Platzhalter.\u0000نموذج تطوير محلي. لا يُستدعى أي نموذج كلام والنصوص مجرد عناصر نائبة.\u0000" +
	"\nengine_outside\u0000Transcripts: {transcriber}. Listen: {speech}. Both are reached through the model gateway.\u0000Transkripte: {transcriber}. Vorlesen: {speech}. Beides läuft über das Modell-Gateway.\u0000النسخ: {transcriber}. الاستماع: {speech}. يتم الوصول إلى كليهما عبر بوابة النماذج.\u0000" +
	"\nengine_none\u0000No engine is set up ({note}). Voice messages are posted, transcripts say they are unavailable and Listen is not offered.\u0000Es ist kein Modul eingerichtet ({note}). Sprachnachrichten werden gesendet, Transkripte melden sich als nicht verfügbar und Vorlesen wird nicht angeboten.\u0000لم يُعدّ أي محرك ({note}). تُرسل الرسائل الصوتية وتُظهر النصوص أنها غير متاحة ولا يُعرض الاستماع.\u0000" +
	"\noutside_yes\u0000Outside services are allowed in this workspace, so audio and text may be sent to the model above.\u0000Externe Dienste sind in diesem Arbeitsbereich erlaubt, daher dürfen Audio und Text an das oben genannte Modell gesendet werden.\u0000الخدمات الخارجية مسموح بها في مساحة العمل هذه، لذا قد تُرسل الملفات الصوتية والنصوص إلى النموذج المذكور أعلاه.\u0000" +
	"\noutside_no\u0000Outside services are not allowed in this workspace: no audio or text is sent to a model.\u0000Externe Dienste sind in diesem Arbeitsbereich nicht erlaubt: Audio und Text werden an kein Modell gesendet.\u0000الخدمات الخارجية غير مسموح بها في مساحة العمل هذه: لا تُرسل أي ملفات صوتية أو نصوص إلى نموذج.\u0000" +
	"\noutside_unknown\u0000Whether outside services are allowed could not be read.\u0000Ob externe Dienste erlaubt sind, konnte nicht gelesen werden.\u0000تعذرت قراءة ما إذا كانت الخدمات الخارجية مسموحًا بها.\u0000" +
	"\nloading\u0000Loading voice settings…\u0000Spracheinstellungen werden geladen…\u0000جارٍ تحميل إعدادات الصوت…\u0000" +
	"\nloadfailed\u0000Voice settings could not load. Try again.\u0000Die Spracheinstellungen konnten nicht geladen werden. Versuchen Sie es erneut.\u0000تعذر تحميل إعدادات الصوت. حاول مجددًا.\u0000" +
	"\nsaving\u0000Saving…\u0000Wird gespeichert…\u0000جارٍ الحفظ…\u0000" +
	"\nsaved\u0000Saved\u0000Gespeichert\u0000تم الحفظ\u0000" +
	"\nsavefailed\u0000Could not save. The setting is unchanged. Try again.\u0000Speichern fehlgeschlagen. Die Einstellung ist unverändert. Versuchen Sie es erneut.\u0000تعذر الحفظ. الإعداد لم يتغير. حاول مجددًا.\u0000" +
	"\nretry\u0000Try again\u0000Erneut versuchen\u0000حاول مجددًا\u0000" +
	"\nunavailable\u0000Voice messages are not set up on this server.\u0000Sprachnachrichten sind auf diesem Server nicht eingerichtet.\u0000الرسائل الصوتية غير مُعدّة على هذا الخادم.\u0000" +
	"\ndenied\u0000Only a workspace administrator can change voice messages for the workspace.\u0000Nur eine Administration des Arbeitsbereichs kann Sprachnachrichten für den Arbeitsbereich ändern.\u0000يمكن لمسؤول مساحة العمل فقط تغيير الرسائل الصوتية لمساحة العمل.\u0000" +
	"\non\u0000On\u0000An\u0000مفعّل\u0000" +
	"\noff\u0000Off\u0000Aus\u0000متوقف\u0000"

// VoiceSettingsCopy is one piece of voice settings text in the reader's language.
func VoiceSettingsCopy(locale, key string) string {
	return integrate1Copy(chatvoiceSettingsCopyTable, locale, key)
}

// VoiceSettingsData is what the server answers about voice for one person: the
// switches in effect, whether they may change the shared ones and, for a
// workspace administrator, the engine. It is the server's VoicePolicyView.
type VoiceSettingsData struct {
	Allowed                    bool
	Reason                     string
	Workspace, Channel, Person bool
	IsChannel                  bool
	CanManageWorkspace         bool
	CanManageChannel           bool
	Engine                     *VoiceEngineData
}

// VoiceEngineData is the engine behind voice messages as the server reports it.
type VoiceEngineData struct {
	Kind, Transcriber, Speech, Note string
	OutsideAllowed, OutsideKnown    bool
}

// VoiceAccessView is what the composer needs to know about one conversation:
// whether voice may be sent there and, when it may not, the server's reason code.
type VoiceAccessView struct {
	Allowed bool
	Reason  string
}

// voiceOffReasonKey is the copy key of a reason that means "voice is turned
// off", or "" for any other answer (including "unavailable": a server without
// the media pieces offers no microphone and says nothing about switches).
func voiceOffReasonKey(reason string) string {
	switch reason {
	case "workspace_off":
		return "off_workspace"
	case "channel_off":
		return "off_channel"
	case "personal_off":
		return "off_personal"
	}
	return ""
}

func chatvoiceAccessOf(m Model) (VoiceAccessView, bool) {
	view, ok := m.VoiceAccess[m.SelectedID]
	return view, ok && m.SelectedID != ""
}

// composerVoiceOffered is whether the Add menu shows a voice item for the
// selected conversation. A direct or group conversation offers it until the
// server says otherwise (it enforces the switches at send in any case); a
// channel offers it only once the server has said where it stands, because voice
// is off there until an administrator turns it on.
func composerVoiceOffered(m Model) bool {
	view, known := chatvoiceAccessOf(m)
	listed := !known || view.Allowed || voiceOffReasonKey(view.Reason) != ""
	switch m.selected().Kind {
	case DirectMessage, GroupChat:
		return listed
	case PublicChannel, PrivateChannel:
		return known && listed
	}
	return false
}

// composerVoiceOffText is why the voice item is switched off, in words; "" when
// it is not.
func composerVoiceOffText(m Model) string {
	if !composerVoiceOffered(m) {
		return ""
	}
	if view, known := chatvoiceAccessOf(m); known && !view.Allowed {
		return VoiceSettingsCopy(m.Locale, voiceOffReasonKey(view.Reason))
	}
	return ""
}

// voiceSettingsState is what a switch row knows between renders.
type voiceSettingsState struct {
	Data                             VoiceSettingsData
	Loaded, Loading                  bool
	LoadFailed, Saving, Saved, Fails bool
	Denied, Unavailable              bool
}

// voiceSettingsProps names the one switch a row changes.
type voiceSettingsProps struct {
	Locale, Scope, Tenant, Conversation string
	// Section is the Manage channel panel's row shape (channel rows only).
	Section sectionWrap
}

const (
	voiceScopeWorkspace = "workspace"
	voiceScopeChannel   = "channel"
	voiceScopePerson    = "person"
)

// VoiceWorkspaceSettingsPanel is the workspace's voice settings for the Chat
// settings page: the workspace switch and, read-only, the engine.
func VoiceWorkspaceSettingsPanel(locale string) ui.Node {
	return ui.CreateElement(voiceSettingsPanel, voiceSettingsProps{Locale: locale, Scope: voiceScopeWorkspace})
}

func (p voiceSettingsProps) readAction() (string, map[string]string) {
	if p.Scope == voiceScopeChannel {
		return "policy", map[string]string{"TenantID": p.Tenant, "ConversationID": p.Conversation}
	}
	return "settings", map[string]string{"TenantID": p.Tenant}
}

func (d VoiceSettingsData) value(scope string) bool {
	switch scope {
	case voiceScopeWorkspace:
		return d.Workspace
	case voiceScopeChannel:
		return d.Channel
	}
	return d.Person
}

func voiceSettingsPanel(props voiceSettingsProps) ui.Node {
	state := ui.UseState(voiceSettingsState{})
	apply := func(data VoiceSettingsData, status int, err error, saved bool) {
		current := state.Get()
		current.Loading, current.Saving = false, false
		switch {
		case err == nil:
			current.Data, current.Loaded, current.LoadFailed, current.Denied = data, true, false, false
			current.Saved, current.Fails = saved, false
		case status == 403 && !current.Loaded:
			current.Denied, current.Loaded = true, true
		case status == 503 && !current.Loaded:
			// The server answered that voice messages are not composed.
			current.Unavailable, current.Loaded = true, true
		default:
			// The last good value stays on show; the line says what failed.
			current.LoadFailed = !current.Loaded
			current.Saved, current.Fails = false, current.Loaded
			if current.Loaded {
				voiceSettingsReset("chatvoice-switch-"+props.Scope, current.Data.value(props.Scope))
			}
		}
		state.Set(current)
	}
	read := func(saved bool) func() {
		action, body := props.readAction()
		return voiceSettingsCall(action, body, func(data VoiceSettingsData, status int, err error) { apply(data, status, err, saved) })
	}
	ui.UseEffectOf(func() func() { return read(false) }, struct{ Scope, Tenant, Conversation string }{props.Scope, props.Tenant, props.Conversation})
	change := ui.UseEvent(func(e ui.ChangeEvent) {
		enabled := e.IsChecked()
		current := state.Get()
		current.Saving, current.Saved, current.Fails = true, false, false
		state.Set(current)
		body := map[string]any{"TenantID": props.Tenant, "Scope": props.Scope, "ConversationID": props.Conversation, "Enabled": enabled}
		voiceSettingsCall("switch", body, func(_ VoiceSettingsData, status int, err error) {
			if err != nil {
				// Nothing was saved: show the value the server holds, not the one the
				// switch was moved to.
				apply(VoiceSettingsData{}, status, err, false)
				return
			}
			voiceSettingsNotify()
			read(true)
		})
	})
	retry := ui.UseEvent(func() {
		current := state.Get()
		current.Loading, current.LoadFailed = true, false
		state.Set(current)
		read(false)
	})
	return voiceSettingsView(props, state.Get(), change, retry)
}

// voiceSettingsView draws the row for one scope in the shape of the place it
// lives in. It never shows a switch position it has not read.
func voiceSettingsView(props voiceSettingsProps, s voiceSettingsState, change ui.Handler, retry ui.Handler) ui.Node {
	t := func(key string) string { return VoiceSettingsCopy(props.Locale, key) }
	dir := direction(props.Locale)
	id := "chatvoice-switch-" + props.Scope
	title := t("title")
	var status ui.Node
	switch {
	case s.Saving:
		status = html.Span(html.Props{Class: "chatvoice-settings-state", Role: "status", Aria: map[string]string{"live": "polite"}, Text: t("saving")})
	case s.Saved:
		status = html.Span(html.Props{Class: "chatlangadmin-saved chatvoice-settings-state", Role: "status", Aria: map[string]string{"live": "polite"}}, icon("check"), html.Span(html.Props{Text: t("saved")}))
	case s.Fails:
		status = html.Span(html.Props{Class: "chatvoice-settings-state chatvoice-settings-failed", Role: "alert", Text: t("savefailed")})
	default:
		status = html.Span(html.Props{Class: "chatvoice-settings-state", Role: "status", Aria: map[string]string{"live": "polite"}})
	}
	retryButton := html.Button(html.Props{Type: "button", Class: "button secondary", Text: t("retry"), OnClick: retry})
	switch {
	case !s.Loaded && !s.LoadFailed:
		return voiceSettingsShell(props, title, dir, html.P(html.Props{Role: "status", Aria: map[string]string{"busy": "true"}, Text: t("loading")}), false)
	case s.LoadFailed:
		return voiceSettingsShell(props, title, dir, html.Div(html.Props{Role: "alert"}, html.P(html.Props{Text: t("loadfailed")}), retryButton), true)
	case s.Unavailable && props.Scope != voiceScopeWorkspace:
		return nil
	case s.Unavailable:
		return voiceSettingsShell(props, title, dir, html.P(html.Props{Text: t("unavailable")}), false)
	case props.Scope == voiceScopeChannel && !s.Data.CanManageChannel:
		// The server did not agree this person administers the channel: no row.
		return nil
	case props.Scope == voiceScopeWorkspace && !s.Data.CanManageWorkspace:
		return voiceSettingsShell(props, title, dir, html.P(html.Props{Role: "alert", Text: t("denied")}), false)
	}
	switchKey, hintKey := "me_switch", "me_hint"
	switch props.Scope {
	case voiceScopeWorkspace:
		switchKey, hintKey = "ws_switch", "ws_hint"
	case voiceScopeChannel:
		switchKey, hintKey = "ch_switch", "ch_hint"
	}
	on := s.Data.value(props.Scope)
	control := html.Input(html.Props{ID: id, Class: "switch", Type: "checkbox", Role: "switch", Checked: on, Disabled: s.Saving || change.Value() == nil, OnChange: change, Aria: map[string]string{"label": t(switchKey), "describedby": id + "-hint"}})
	hint := html.P(html.Props{ID: id + "-hint", Class: "field-hint", Text: t(hintKey)})
	value := t("off")
	if on {
		value = t("on")
	}
	var extra []ui.Node
	if props.Scope == voiceScopeWorkspace {
		extra = voiceEngineLines(props.Locale, s.Data.Engine)
	}
	switch props.Scope {
	case voiceScopePerson:
		return chatux002Row("voice", title, value, control, append([]ui.Node{hint, status}, extra...)...)
	case voiceScopeChannel:
		body := html.Div(html.Props{Class: "chatvoice-settings-body", Dir: dir},
			html.Label(html.Props{Class: "chatlangadmin-check", For: id}, control, html.Span(html.Props{Text: t(switchKey)}), status), hint)
		return voiceSettingsRow(props, title, value, body, false)
	}
	rows := []ui.Node{
		html.H2(html.Props{ID: "chatvoice-title", Text: title}),
		html.P(html.Props{Class: "field-hint", Text: t("ws_intro")}),
		html.Div(html.Props{Class: "chatlangadmin-setting"}, html.Label(html.Props{Class: "chatlangadmin-check", For: id}, control, html.Span(html.Props{Text: t(switchKey)}), status), hint),
	}
	rows = append(rows, extra...)
	return html.Section(html.Props{Class: "chatlangadmin chatlangadmin-page chatvoiceadmin", Dir: dir, Aria: map[string]string{"labelledby": "chatvoice-title"}}, rows...)
}

// voiceEngineLines are the read-only lines about the engine, for an
// administrator: which engine and model, and whether outside services are
// allowed in the workspace.
func voiceEngineLines(locale string, e *VoiceEngineData) []ui.Node {
	if e == nil {
		return nil
	}
	t := func(key string) string { return VoiceSettingsCopy(locale, key) }
	fill := strings.NewReplacer("{transcriber}", e.Transcriber, "{speech}", e.Speech, "{note}", e.Note)
	line := t("engine_none")
	switch e.Kind {
	case "fixture":
		line = t("engine_fixture")
	case "outside":
		line = t("engine_outside")
	}
	outside := t("outside_unknown")
	switch {
	case e.Kind != "outside":
		outside = ""
	case e.OutsideKnown && e.OutsideAllowed:
		outside = t("outside_yes")
	case e.OutsideKnown:
		outside = t("outside_no")
	}
	nodes := []ui.Node{
		html.H3(html.Props{ID: "chatvoice-engine-title", Class: "chatvoice-engine-title", Text: t("engine")}),
		html.P(html.Props{Class: "chatvoice-engine-line", Dir: "auto", Data: map[string]string{"chatvoice-engine": e.Kind}, Text: fill.Replace(line)}),
	}
	if outside != "" {
		nodes = append(nodes, html.P(html.Props{Class: "field-hint chatvoice-engine-outside", Dir: "auto", Text: outside}))
	}
	return nodes
}

// voiceSettingsShell is the row before its answer is in, or in place of it.
func voiceSettingsShell(props voiceSettingsProps, title, dir string, body ui.Node, failed bool) ui.Node {
	switch props.Scope {
	case voiceScopePerson:
		return chatux002Row("voice", title, "", nil, body)
	case voiceScopeChannel:
		return voiceSettingsRow(props, title, "", body, failed)
	}
	return html.Section(html.Props{Class: "chatlangadmin chatlangadmin-page chatvoiceadmin", Dir: dir, Aria: map[string]string{"labelledby": "chatvoice-title"}}, html.H2(html.Props{ID: "chatvoice-title", Text: title}), body)
}

// voiceSettingsRow puts a channel's content in the Manage channel panel's row.
func voiceSettingsRow(props voiceSettingsProps, title, value string, body ui.Node, failed bool) ui.Node {
	var shown ui.Node
	if value != "" {
		shown = html.Span(html.Props{Text: value})
	}
	if props.Section == nil {
		return html.Div(html.Props{Class: "chatvoice-entry"}, body)
	}
	return props.Section(sectionContent{Label: title, Value: shown, Body: body, Failed: failed})
}

// chatvoiceComposed is whether the server composed voice messages, as its
// features say: until then (and where it is not) there is nothing to switch.
func chatvoiceComposed(m Model) bool { return m.ChatFeatures != nil && m.ChatFeatures.Voice }

// chatvoiceChannelRow is the Voice messages row of Manage channel: one switch,
// for people who administer the channel. It loads the channel's switch itself
// and shows nothing when the server says this person may not change it.
func chatvoiceChannelRow(m Model, h handlers, c Conversation) ui.Node {
	if !chatvoiceComposed(m) || (c.Kind != PublicChannel && c.Kind != PrivateChannel) || !canAdministerConversation(m, c) || c.ID == "" {
		return nil
	}
	tenant := c.HostTenantID
	if tenant == "" {
		tenant = m.CurrentTenantID
	}
	wrap := chatux027Wrap(m, h, chatux027ManageScope(m), "voice", "chatvoice-entry")
	return html.WithKey(ui.CreateElement(voiceSettingsPanel, voiceSettingsProps{Locale: m.Locale, Scope: voiceScopeChannel, Tenant: tenant, Conversation: c.ID, Section: wrap}), "chatvoice-"+c.ID)
}

// chatvoicePersonalRow is the Voice messages row of Chat preferences, beside
// Quiet hours: the person's own switch.
func chatvoicePersonalRow(m Model) ui.Node {
	if !chatvoiceComposed(m) {
		return nil
	}
	return ui.CreateElement(voiceSettingsPanel, voiceSettingsProps{Locale: m.Locale, Scope: voiceScopePerson, Tenant: m.CurrentTenantID})
}
