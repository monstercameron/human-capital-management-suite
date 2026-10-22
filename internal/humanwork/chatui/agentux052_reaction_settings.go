package chatui

import (
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// agentReactionSettingsReadBound is the longest a reaction settings read may
// run before the row fails it into the retry state (CHATBUG-100). It sits past
// the request's own timeout as a backstop for transports that do not enforce
// one, so a read that never answers still ends within its bound.
const agentReactionSettingsReadBound = 10 * time.Second

// Agent reaction switches (AGENTUX-052). Two switches decide whether an agent
// may react to a question: the workspace's and the channel's, the channel's
// winning where it is set. The server enforces them when the reaction is
// committed; this file is where people change them (one row each, in the
// workspace Chat settings and in Manage channel).

const agentReactionSettingsCopyTable = "\ntitle\u0000Agent reactions\u0000Reaktionen von Agents\u0000تفاعلات الوكلاء\u0000" +
	"\nws_intro\u0000Agents react to questions while they work, then swap in the reaction that fits the outcome. Turn reactions off to keep every conversation quiet. Answers are unaffected.\u0000Agents reagieren auf Fragen, während sie arbeiten, und tauschen die Reaktion danach gegen die zum Ergebnis passende ein. Schalten Sie Reaktionen aus, damit jede Unterhaltung ruhig bleibt. Antworten sind davon nicht betroffen.\u0000تتفاعل الوكلاء مع الأسئلة أثناء العمل، ثم تستبدل التفاعل بما يناسب النتيجة. أوقف التفاعلات لإبقاء كل المحادثات هادئة. تبقى الإجابات دون تغيير.\u0000" +
	"\nws_switch\u0000Let agents react in this workspace\u0000Agents dürfen in diesem Arbeitsbereich reagieren\u0000السماح للوكلاء بالتفاعل في مساحة العمل هذه\u0000" +
	"\nws_hint\u0000Turn off to stop agents from reacting in every conversation. Answers are unaffected.\u0000Ausschalten, damit Agents in keiner Unterhaltung mehr reagieren. Antworten sind davon nicht betroffen.\u0000أوقف التشغيل لمنع الوكلاء من التفاعل في كل المحادثات. تبقى الإجابات دون تغيير.\u0000" +
	"\nch_switch\u0000Let agents react in this channel\u0000Agents dürfen in diesem Kanal reagieren\u0000السماح للوكلاء بالتفاعل في هذه القناة\u0000" +
	"\nch_hint\u0000On, following the workspace, until you turn it off. When off, agents answer here without reacting.\u0000An, folgt dem Arbeitsbereich, bis Sie es ausschalten. Ausgeschaltet antworten Agents hier, ohne zu reagieren.\u0000مفعّل، يتبع مساحة العمل، حتى توقفه. عند الإيقاف تجيب الوكلاء هنا دون تفاعل.\u0000" +
	"\nloading\u0000Loading reaction settings…\u0000Reaktionseinstellungen werden geladen…\u0000جارٍ تحميل إعدادات التفاعلات…\u0000" +
	"\nloadfailed\u0000Reaction settings could not load. Try again.\u0000Die Reaktionseinstellungen konnten nicht geladen werden. Versuchen Sie es erneut.\u0000تعذر تحميل إعدادات التفاعلات. حاول مجددًا.\u0000" +
	"\nsaving\u0000Saving…\u0000Wird gespeichert…\u0000جارٍ الحفظ…\u0000" +
	"\nsaved\u0000Saved\u0000Gespeichert\u0000تم الحفظ\u0000" +
	"\nsavefailed\u0000Could not save. The setting is unchanged. Try again.\u0000Speichern fehlgeschlagen. Die Einstellung ist unverändert. Versuchen Sie es erneut.\u0000تعذر الحفظ. الإعداد لم يتغير. حاول مجددًا.\u0000" +
	"\nretry\u0000Try again\u0000Erneut versuchen\u0000حاول مجددًا\u0000" +
	"\nunavailable\u0000Agent reactions are not set up on this server.\u0000Reaktionen von Agents sind auf diesem Server nicht eingerichtet.\u0000تفاعلات الوكلاء غير مُعدّة على هذا الخادم.\u0000" +
	"\ndenied\u0000Only a workspace administrator can change agent reactions for the workspace.\u0000Nur eine Administration des Arbeitsbereichs kann Reaktionen von Agents für den Arbeitsbereich ändern.\u0000يمكن لمسؤول مساحة العمل فقط تغيير تفاعلات الوكلاء لمساحة العمل.\u0000" +
	"\non\u0000On\u0000An\u0000مفعّل\u0000" +
	"\noff\u0000Off\u0000Aus\u0000متوقف\u0000"

// AgentReactionSettingsCopy is one piece of reaction settings text in the
// reader's language.
func AgentReactionSettingsCopy(locale, key string) string {
	return integrate1Copy(agentReactionSettingsCopyTable, locale, key)
}

// AgentReactionSettingsData is what the server answers about agent reactions
// for one person: the switches in effect and whether they may change the
// shared ones. It is the server's AgentReactionPolicyView.
type AgentReactionSettingsData struct {
	Workspace, Channel, IsChannel bool
	CanManageWorkspace            bool
	CanManageChannel              bool
}

// agentReactionSettingsState is what a switch row knows between renders.
type agentReactionSettingsState struct {
	Data                             AgentReactionSettingsData
	Loaded, Loading                  bool
	LoadFailed, Saving, Saved, Fails bool
	Denied, Unavailable              bool
}

// agentReactionSettingsProps names the one switch a row changes.
type agentReactionSettingsProps struct {
	Locale, Scope, Tenant, Conversation string
	// Section is the Manage channel panel's row shape (channel rows only).
	Section sectionWrap
}

const (
	agentReactionScopeWorkspace = "workspace"
	agentReactionScopeChannel   = "channel"
)

// AgentReactionWorkspaceSettingsPanel is the workspace's agent reaction
// settings for the Chat settings page: the workspace switch.
func AgentReactionWorkspaceSettingsPanel(locale string) ui.Node {
	return ui.CreateElement(agentReactionSettingsPanel, agentReactionSettingsProps{Locale: locale, Scope: agentReactionScopeWorkspace})
}

func (p agentReactionSettingsProps) readAction() (string, map[string]string) {
	if p.Scope == agentReactionScopeChannel {
		return "policy", map[string]string{"TenantID": p.Tenant, "ConversationID": p.Conversation}
	}
	return "settings", map[string]string{"TenantID": p.Tenant}
}

func (d AgentReactionSettingsData) value(scope string) bool {
	if scope == agentReactionScopeChannel {
		return d.Channel
	}
	return d.Workspace
}

// agentReactionSettingsStore is each row's state for the page, by store key.
// Like the voice rows, the state lives for the page and not for one instance
// of the component: the instance that asked the server could be gone before
// the answer came, which would leave the row on its loading line for good.
var agentReactionSettingsStore = map[string]agentReactionSettingsState{}

func (p agentReactionSettingsProps) storeKey() string {
	return p.Scope + "\x00" + p.Tenant + "\x00" + p.Conversation
}

// agentReactionSettingsCurrent is the row's state: the page's once anything
// has changed it, the instance's own before that.
func agentReactionSettingsCurrent(key string, local agentReactionSettingsState) agentReactionSettingsState {
	if stored, ok := agentReactionSettingsStore[key]; ok {
		return stored
	}
	return local
}

// agentReactionSettingsChange applies a change to the row's state, keeps it
// for the page and returns it. It runs on the render thread.
func agentReactionSettingsChange(key string, local agentReactionSettingsState, change func(*agentReactionSettingsState)) agentReactionSettingsState {
	next := agentReactionSettingsCurrent(key, local)
	change(&next)
	agentReactionSettingsStore[key] = next
	return next
}

// agentReactionSettingsAnswered is the row's state once the server has
// answered a read or a save. restore is true when a save failed after a value
// was on show: the switch must be put back to the position the server holds.
func agentReactionSettingsAnswered(current agentReactionSettingsState, data AgentReactionSettingsData, status int, err error, saved bool) (next agentReactionSettingsState, restore bool) {
	current.Loading, current.Saving = false, false
	switch {
	case err == nil:
		current.Data, current.Loaded, current.LoadFailed, current.Denied = data, true, false, false
		current.Saved, current.Fails = saved, false
	case status == 403 && !current.Loaded:
		current.Denied, current.Loaded = true, true
	case status == 503 && !current.Loaded:
		// The server answered that agent reactions are not composed.
		current.Unavailable, current.Loaded = true, true
	default:
		// The last good value stays on show; the line says what failed.
		current.LoadFailed = !current.Loaded
		current.Saved, current.Fails = false, current.Loaded
		restore = current.Loaded
	}
	return current, restore
}

func agentReactionSettingsPanel(props agentReactionSettingsProps) ui.Node {
	key := props.storeKey()
	local := ui.UseState(agentReactionSettingsState{})
	// The page's copy of the row's state is what is drawn; the component's own
	// state only makes it draw again.
	draw := func(next agentReactionSettingsState) {
		local.Set(next)
		chatbug045Redraw()
	}
	apply := func(data AgentReactionSettingsData, status int, err error, saved bool) {
		restore := false
		next := agentReactionSettingsChange(key, local.Get(), func(current *agentReactionSettingsState) {
			*current, restore = agentReactionSettingsAnswered(*current, data, status, err, saved)
		})
		if restore {
			agentReactionSettingsReset("agentreactions-switch-"+props.Scope, next.Data.value(props.Scope))
		}
		draw(next)
	}
	// ask sends one request and applies its answer on the render thread,
	// whichever instance of the row is on the page by then.
	ask := func(action string, body any, done func(AgentReactionSettingsData, int, error)) {
		agentReactionSettingsCall(action, body, func(data AgentReactionSettingsData, status int, err error) {
			ui.PostAsync(func() { done(data, status, err) })
		})
	}
	read := func(saved bool) {
		action, body := props.readAction()
		ask(action, body, func(data AgentReactionSettingsData, status int, err error) { apply(data, status, err, saved) })
	}
	ui.UseEffectOf(func() func() {
		// A read another instance of this row already sent is not sent twice.
		if agentReactionSettingsCurrent(key, local.Get()).Loading {
			return nil
		}
		agentReactionSettingsChange(key, local.Get(), func(current *agentReactionSettingsState) { current.Loading, current.LoadFailed = true, false })
		read(false)
		return nil
	}, struct{ Scope, Tenant, Conversation string }{props.Scope, props.Tenant, props.Conversation})
	change := ui.UseEvent(func(e ui.ChangeEvent) {
		enabled := e.IsChecked()
		draw(agentReactionSettingsChange(key, local.Get(), func(current *agentReactionSettingsState) {
			current.Saving, current.Saved, current.Fails = true, false, false
		}))
		body := map[string]any{"TenantID": props.Tenant, "Scope": props.Scope, "ConversationID": props.Conversation, "Enabled": enabled}
		ask("switch", body, func(_ AgentReactionSettingsData, status int, err error) {
			if err != nil {
				// Nothing was saved: show the value the server holds, not the
				// one the switch was moved to.
				apply(AgentReactionSettingsData{}, status, err, false)
				return
			}
			read(true)
		})
	})
	retry := ui.UseEvent(func() {
		draw(agentReactionSettingsChange(key, local.Get(), func(current *agentReactionSettingsState) { current.Loading, current.LoadFailed = true, false }))
		read(false)
	})
	return agentReactionSettingsView(props, agentReactionSettingsCurrent(key, local.Get()), change, retry)
}

// agentReactionSettingsView draws the row for one scope in the shape of the
// place it lives in. It never shows a switch position it has not read.
func agentReactionSettingsView(props agentReactionSettingsProps, s agentReactionSettingsState, change ui.Handler, retry ui.Handler) ui.Node {
	t := func(key string) string { return AgentReactionSettingsCopy(props.Locale, key) }
	dir := direction(props.Locale)
	id := "agentreactions-switch-" + props.Scope
	title := t("title")
	var status ui.Node
	switch {
	case s.Saving:
		status = html.Span(html.Props{Class: "agentreactions-settings-state", Role: "status", Aria: map[string]string{"live": "polite"}, Text: t("saving")})
	case s.Saved:
		status = html.Span(html.Props{Class: "chatlangadmin-saved agentreactions-settings-state", Role: "status", Aria: map[string]string{"live": "polite"}}, icon("check"), html.Span(html.Props{Text: t("saved")}))
	case s.Fails:
		status = html.Span(html.Props{Class: "agentreactions-settings-state agentreactions-settings-failed", Role: "alert", Text: t("savefailed")})
	default:
		status = html.Span(html.Props{Class: "agentreactions-settings-state", Role: "status", Aria: map[string]string{"live": "polite"}})
	}
	switch {
	case !s.Loaded && !s.LoadFailed:
		return agentReactionSettingsShell(props, title, dir, ChatLoadingFrame(LoadingFrame{Locale: props.Locale, Shape: LoadingShapeSection, Rows: 1, Status: t("loading"), OnRetry: &retry}), false)
	case s.LoadFailed:
		return agentReactionSettingsShell(props, title, dir, ChatLoadFailed(props.Locale, t("loadfailed"), nil, &retry), true)
	case s.Unavailable && props.Scope != agentReactionScopeWorkspace:
		return nil
	case s.Unavailable:
		return agentReactionSettingsShell(props, title, dir, html.P(html.Props{Text: t("unavailable")}), false)
	case props.Scope == agentReactionScopeChannel && !s.Data.CanManageChannel:
		// The server did not agree this person administers the channel: no row.
		return nil
	case props.Scope == agentReactionScopeWorkspace && !s.Data.CanManageWorkspace:
		return agentReactionSettingsShell(props, title, dir, html.P(html.Props{Role: "alert", Text: t("denied")}), false)
	}
	switchKey, hintKey := "ws_switch", "ws_hint"
	if props.Scope == agentReactionScopeChannel {
		switchKey, hintKey = "ch_switch", "ch_hint"
	}
	on := s.Data.value(props.Scope)
	control := html.Input(html.Props{ID: id, Class: "switch", Type: "checkbox", Role: "switch", Checked: on, Disabled: s.Saving || change.Value() == nil, OnChange: change, Aria: map[string]string{"label": t(switchKey), "describedby": id + "-hint"}})
	hint := html.P(html.Props{ID: id + "-hint", Class: "field-hint", Text: t(hintKey)})
	value := t("off")
	if on {
		value = t("on")
	}
	if props.Scope == agentReactionScopeChannel {
		body := html.Div(html.Props{Class: "agentreactions-settings-body", Dir: dir},
			html.Label(html.Props{Class: "chatlangadmin-check", For: id}, control, html.Span(html.Props{Text: t(switchKey)}), status), hint)
		return agentReactionSettingsRow(props, title, value, body, false)
	}
	return html.Section(html.Props{Class: "chatlangadmin chatlangadmin-page agentreactionsadmin", Dir: dir, Aria: map[string]string{"labelledby": "agentreactions-title"}},
		html.H2(html.Props{ID: "agentreactions-title", Text: title}),
		html.P(html.Props{Class: "field-hint", Text: t("ws_intro")}),
		html.Div(html.Props{Class: "chatlangadmin-setting"}, html.Label(html.Props{Class: "chatlangadmin-check", For: id}, control, html.Span(html.Props{Text: t(switchKey)}), status), hint),
	)
}

// agentReactionSettingsShell is the row before its answer is in, or in place
// of it.
func agentReactionSettingsShell(props agentReactionSettingsProps, title, dir string, body ui.Node, failed bool) ui.Node {
	if props.Scope == agentReactionScopeChannel {
		return agentReactionSettingsRow(props, title, "", body, failed)
	}
	return html.Section(html.Props{Class: "chatlangadmin chatlangadmin-page agentreactionsadmin", Dir: dir, Aria: map[string]string{"labelledby": "agentreactions-title"}}, html.H2(html.Props{ID: "agentreactions-title", Text: title}), body)
}

// agentReactionSettingsRow puts a channel's content in the Manage channel
// panel's row.
func agentReactionSettingsRow(props agentReactionSettingsProps, title, value string, body ui.Node, failed bool) ui.Node {
	var shown ui.Node
	if value != "" {
		shown = html.Span(html.Props{Text: value})
	}
	if props.Section == nil {
		return html.Div(html.Props{Class: "agentreactions-entry"}, body)
	}
	return props.Section(sectionContent{Label: title, Value: shown, Body: body, Failed: failed})
}

// agentReactionChannelRow is the Agent reactions row of Manage channel: one
// switch, for people who administer the channel. It loads the channel's switch
// itself and shows nothing when the server says this person may not change it,
// or where reactions are not composed.
func agentReactionChannelRow(m Model, h handlers, c Conversation) ui.Node {
	if (c.Kind != PublicChannel && c.Kind != PrivateChannel) || !canAdministerConversation(m, c) || c.ID == "" {
		return nil
	}
	tenant := c.HostTenantID
	if tenant == "" {
		tenant = m.CurrentTenantID
	}
	wrap := chatux027Wrap(m, h, chatux027ManageScope(m), "reactions", "agentreactions-entry")
	return html.WithKey(ui.CreateElement(agentReactionSettingsPanel, agentReactionSettingsProps{Locale: m.Locale, Scope: agentReactionScopeChannel, Tenant: tenant, Conversation: c.ID, Section: wrap}), "agentreactions-"+c.ID)
}
