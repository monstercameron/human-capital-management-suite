package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// CHATBUG-045: the reading-language settings inside Chat preferences save every
// change at once. The form shows what a person reaches for first, the language
// they read in and whether messages are translated into it, and keeps the two
// lists of further languages (read without translation, never translate from),
// and the "apply to this conversation" choice, under one "More options"
// disclosure, the lists as chips. The form is the one the settings client reads
// (renderingReadSettings): the same field names and ids as the stand-alone form.

var chatbug045Languages = []string{"en", "de", "fr", "es", "pt", "ar", "ja", "hi"}

// chatbug045ReadingHost is the one element the reading row always lives in. The
// row is a placeholder until the settings service has answered, and a component
// whose root changes from a placeholder to a section is appended to the end of
// its parent by the reconciler, which put Reading languages after the rows that
// follow it. Inside a host that never changes, the swap happens in place and the
// row keeps its position among its siblings.
func chatbug045ReadingHost(state string, child ui.Node) ui.Node {
	return html.Div(html.Props{Class: "chatrender-personal-host", Data: map[string]string{"reading-state": state}}, child)
}

// chatbug045ReadingForm is RenderingSettings for a form that saves on change:
// no Save button, nothing turns off while a save is in flight, and the change
// event of the form is the submit handler.
func chatbug045ReadingForm(m RenderingSettingsModel) ui.Node {
	t := func(k string) string { return RenderingText(m.Locale, k) }
	options := []ui.Node{}
	for _, l := range chatbug045Languages {
		options = append(options, html.Option(html.Props{Value: l, Text: t(l), Selected: l == chatrender.Language(m.Preference.ReadingLanguage)}))
	}
	status := "help"
	if m.Loading {
		status = "loading"
	} else if m.Failed {
		status = "error"
	} else if m.Saved {
		status = "saved"
	}
	more := []ui.Node{
		chatbug045Chips(m, "further", "further"),
		chatbug045Chips(m, "source", "never"),
	}
	if m.Conversation {
		more = append(more, html.Label(html.Props{Class: "chatrender-check", For: "chatrender-room"}, html.Input(html.Props{ID: "chatrender-room", Name: "conversation", Type: "checkbox"}), html.Span(html.Props{Text: t("scope")})))
	}
	fields := []ui.Node{
		html.H3(html.Props{ID: "chatrender-settings-title", Text: t("title")}),
		html.P(html.Props{ID: "chatrender-settings-status", Role: "status", Text: t(status)}),
		html.Label(html.Props{For: "chatrender-reading", Text: t("reading")}),
		html.Select(html.Props{ID: "chatrender-reading", Name: "reading", Aria: map[string]string{"describedby": "chatrender-settings-status"}}, options...),
		html.Label(html.Props{Class: "chatrender-check chatrender-switch", For: "chatrender-translate"},
			html.Input(html.Props{ID: "chatrender-translate", Class: "switch", Role: "switch", Name: "translate", Type: "checkbox", Checked: m.Preference.Translate, Disabled: m.TranslationUnavailable, Title: ChatFeatureUnavailable(m.Locale)}),
			html.Span(html.Props{Text: t("translate")})),
		html.Div(html.Props{Class: "chatrender-more", Data: map[string]string{"chat-disclosure": "true"}},
			chatPolishDisclosureLabel(html.Props{Class: "chatrender-more-toggle", Text: chatbug045Text(Model{Locale: m.Locale}, keyChatbug045More)}),
			html.Div(html.Props{Class: "chat-disclosure-body chatrender-more-body", Hidden: true, Data: map[string]string{"chat-disclosure-body": "true"}}, more...)),
	}
	return html.Form(html.Props{Class: "chatrender-settings", Dir: direction(m.Locale), OnSubmit: m.Submit, OnChange: m.Submit, Aria: map[string]string{"labelledby": "chatrender-settings-title"}}, fields...)
}

// chatbug045Chips is one list of languages drawn as chips: a checkbox each, named
// as the stand-alone form names them, so the settings client reads both alike.
func chatbug045Chips(m RenderingSettingsModel, key, name string) ui.Node {
	chips := make([]ui.Node, 0, len(chatbug045Languages))
	for _, l := range chatbug045Languages {
		checked := false
		if name == "further" {
			for _, known := range m.Preference.FurtherLanguages {
				if chatrender.Language(known) == l {
					checked = true
				}
			}
		} else {
			on, exists := m.Preference.SourceOverrides[l]
			checked = exists && !on
		}
		id := "chatrender-" + name + "-" + l
		chips = append(chips, html.Label(html.Props{Class: "chatrender-chip", For: id},
			html.Input(html.Props{ID: id, Name: name, Type: "checkbox", Value: l, Checked: checked}),
			html.Span(html.Props{Text: RenderingText(m.Locale, l)})))
	}
	return html.Fieldset(html.Props{Class: "chatrender-languages chatrender-chips"},
		html.Legend(html.Props{Text: RenderingText(m.Locale, key)}),
		html.Div(html.Props{Class: "chatrender-chip-row"}, chips...))
}

// ChatBug045ReadingStyles lays the reading settings out: the translate choice as
// a row with its switch at the end, the lists as chips, and the heads of the
// panel's rows so that a title, its value and its control never overlap: they
// wrap onto another line instead.
const ChatBug045ReadingStyles = `.chatrender-personal-host{display:contents}.chat-prefs-section+.chatrender-personal-host>.chat-prefs-section,.chatrender-personal-host+.chat-prefs-section{border-block-start:1px solid var(--line);padding-block-start:10px}` +
	`.chatrender-check.chatrender-switch{flex-direction:row-reverse;justify-content:space-between;min-block-size:44px}` +
	`.chat-prefs-inline-body .chatrender-settings{display:grid;gap:8px;font-size:.8125rem;line-height:1.35}.chat-prefs-inline-body .chatrender-settings>p{margin:0;color:var(--muted)}.chat-prefs-inline-body .chatrender-settings>label:not(.chatrender-check){color:var(--muted)}` +
	`.chat-prefs-inline-body .chatrender-indicator{display:grid;gap:4px;font-size:.8125rem}.chat-prefs-inline-body .chatrender-indicator>h3{margin:0;color:var(--muted);font-size:.8125rem;font-weight:600}.chat-prefs-inline-body .chatrender-indicator>p{margin:0}` +
	`.chatrender-more{display:grid;gap:8px}.chatrender-more-body{display:grid;gap:10px;padding-block-start:4px}.chatrender-more-body[hidden]{display:none}` +
	`.chatrender-chips{min-inline-size:0;margin:0;padding:0;border:0}.chatrender-chips>legend{padding:0;margin-block-end:6px;color:var(--muted);font-size:.8125rem}` +
	`.chatrender-chip-row{display:flex;flex-wrap:wrap;gap:6px}.chatrender-chip{position:relative;display:inline-flex;min-inline-size:0}` +
	`.chatrender-chip>input{position:absolute;inset:0;inline-size:100%;block-size:100%;margin:0;opacity:0;cursor:pointer}` +
	`.chatrender-chip>span{display:inline-flex;align-items:center;min-block-size:28px;padding:2px 10px;border:1px solid var(--line);border-radius:999px;color:var(--ink);font-size:.8125rem}` +
	`.chatrender-chip>input:checked+span{border-color:var(--accent);background:color-mix(in srgb,var(--accent) 14%,transparent)}` +
	`.chatrender-chip>input:focus-visible+span{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.chat-prefs-head{flex-wrap:wrap;row-gap:2px}.chat-prefs-title{flex:1 1 auto;white-space:normal;overflow-wrap:anywhere}` +
	`.chat-prefs-value{flex:0 1 auto;max-inline-size:100%;overflow-wrap:anywhere}` +
	`.chat-prefs-head>:is(.chat-prefs-change,.switch){margin-inline-start:auto}`

// chatbug045ReadingState names why the reading row is not drawn yet, for the page's
// own DOM: the host carries it as data-reading-state, so a row that does not
// appear can be told from a row still waiting for the settings service.
func chatbug045ReadingState(current renderingPersonalState) string {
	if current.Unavailable {
		return "unavailable"
	}
	return "checking"
}

// chatbug045Redraw asks the workspace to draw again, so every instance of the
// reading row reads what the service answered.
func chatbug045Redraw() {
	if chatEmojiHost.apply != nil {
		chatEmojiHost.writeFull(func(*localUI) {})
	}
}

// chatbug045ReadingStore is the reading row's state for the page. The row is a
// component, and the answers of the settings service, of the language count and of
// a save reached the state of one instance of it from goroutines; on some page
// loads the instance that asked and the instance on the page were not the same one,
// and the row stayed on "checking", or showed the language it had before the
// change. The state is therefore kept here, on the render thread, and every
// instance draws from it; the component's own state is only what makes it draw
// again.
var chatbug045ReadingStore struct {
	known bool
	state renderingPersonalState
}

// chatbug045ReadingCurrent is the row's state: the page's once anything has
// changed it, the instance's own before that.
func chatbug045ReadingCurrent(local renderingPersonalState) renderingPersonalState {
	if chatbug045ReadingStore.known {
		return chatbug045ReadingStore.state
	}
	return local
}

// chatbug045ReadingChange applies a change to the row's state and returns the new
// state. It runs on the render thread.
func chatbug045ReadingChange(local renderingPersonalState, change func(*renderingPersonalState)) renderingPersonalState {
	next := chatbug045ReadingCurrent(local)
	change(&next)
	chatbug045ReadingStore.state, chatbug045ReadingStore.known = next, true
	return next
}
