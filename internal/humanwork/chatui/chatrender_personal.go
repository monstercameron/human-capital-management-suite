package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

type renderingPersonalProps struct{ Locale, Conversation string }
type renderingPersonalState struct {
	Preference             chatrender.Preference
	Loading, Failed, Saved bool
	Unavailable            bool
	// Checking is true until the settings service has answered once. The row is
	// not drawn while it is: a row that appeared and then turned grey or
	// vanished under the reader's hand was the old behaviour.
	Checking                          bool
	Counts                            map[string]int
	LanguagesLoading, LanguagesFailed bool
}

func RenderingPersonalSettings(locale, conversation string) ui.Node {
	return ui.CreateElement(renderingPersonalPanel, renderingPersonalProps{Locale: locale, Conversation: conversation})
}
func renderingPersonalPanel(props renderingPersonalProps) ui.Node {
	opened := ui.UseState(false)
	state := ui.UseState(renderingPersonalState{Preference: chatrender.DefaultPreference(props.Locale), Loading: false, Checking: renderingChecksAvailability, LanguagesLoading: props.Conversation != ""})
	// The service is asked once when the row mounts (and when the conversation
	// changes), never when the row is opened: opening must not turn the controls
	// off, and a service that is not there must remove the row before anyone
	// reaches for it, not close the panel they just opened.
	ui.UseEffectOf(func() func() {
		return renderingLoadSettings(props.Conversation, func(pref chatrender.Preference, err error) {
			// CHATBUG-045: the answer is applied on the render thread, to the page's
			// copy of the row's state (chatbug045ReadingStore), so a row drawn by
			// another instance than the one that asked still hears it.
			ui.PostAsync(func() {
				state.Set(chatbug045ReadingChange(state.Get(), func(current *renderingPersonalState) {
					if err == nil {
						current.Preference = pref
					}
					current.Checking = false
					current.Unavailable = err != nil
				}))
				chatbug045Redraw()
			})
		})
	}, struct{ Room string }{props.Conversation})
	ui.UseEffectOf(func() func() {
		if !opened.Get() {
			return nil
		}
		return renderingLoadLanguages(props.Conversation, func(counts map[string]int, err error) {
			ui.PostAsync(func() {
				state.Set(chatbug045ReadingChange(state.Get(), func(current *renderingPersonalState) {
					current.Counts = counts
					current.LanguagesLoading = false
					current.LanguagesFailed = err != nil
				}))
				chatbug045Redraw()
			})
		})
	}, struct {
		Room string
		Open bool
	}{props.Conversation, opened.Get()})
	submit := ui.UseEvent(func(event ui.Event) {
		event.PreventDefault()
		// CHATBUG-045: ticking "Apply to this conversation" only chooses where the
		// next change is saved; it saves nothing itself.
		if renderingChangeIsScope(event) {
			return
		}
		current := chatbug045ReadingCurrent(state.Get())
		pref, room, err := renderingReadSettings(event, current.Preference, props.Conversation)
		if err != nil {
			state.Set(chatbug045ReadingChange(state.Get(), func(current *renderingPersonalState) { current.Failed = true }))
			return
		}
		previous := current.Preference
		state.Set(chatbug045ReadingChange(state.Get(), func(current *renderingPersonalState) {
			current.Preference = pref
			current.Loading = true
			current.Failed = false
			current.Saved = false
		}))
		renderingSaveSettings(room, pref, func(err error) {
			ui.PostAsync(func() {
				state.Set(chatbug045ReadingChange(state.Get(), func(current *renderingPersonalState) {
					if err != nil {
						current.Preference = previous
					}
					current.Loading = false
					current.Failed = err != nil
					current.Saved = err == nil
				}))
				chatbug045Redraw()
			})
		})
	})
	toggle := ui.UseEvent(func() { opened.Set(true) })
	current := chatbug045ReadingCurrent(state.Get())
	var indicator ui.Node
	if props.Conversation != "" && !current.LanguagesFailed {
		indicator = RenderingLanguageStatusIndicator(props.Locale, current.Counts, current.LanguagesLoading, current.LanguagesFailed)
	}
	return renderingPersonalView(props, current, submit, toggle, indicator)
}

func renderingPersonalView(props renderingPersonalProps, current renderingPersonalState, submit, toggle ui.Handler, indicator ui.Node) ui.Node {
	// No service, no row: a greyed-out row that cannot be used is not drawn at
	// all. The slot keeps the component mounted so its answer is remembered.
	if current.Unavailable || current.Checking {
		return chatbug045ReadingHost(chatbug045ReadingState(current), html.Span(html.Props{Class: "chatrender-personal-slot", Aria: map[string]string{"hidden": "true"}}))
	}
	// CHATUX-002: Reading languages is a section of the Chat preferences panel
	// under its own heading, with the current reading language beside it. The
	// settings form opens under it with Change; asking for it is what loads the
	// conversation's language counts, as opening the old row did.
	// CHATBUG-045: the row is named for the one language it shows, "Reading language".
	title := chatbug045Text(Model{Locale: props.Locale}, keyChatbug045Reading)
	change := chatux002Text(Model{Locale: props.Locale}, keyChatux002Change)
	toggleButton := chatPolishDisclosureLabel(html.Props{Class: "chat-prefs-change", Text: change, OnClick: toggle, Title: title, Aria: map[string]string{"describedby": "chat-prefs-reading-languages-title"}})
	return chatbug045ReadingHost("ready", chatux002DisclosureRow("reading-languages", title, RenderingText(props.Locale, chatrender.Language(current.Preference.ReadingLanguage)), "chatrender-personal", toggleButton,
		RenderingSettings(RenderingSettingsModel{Locale: props.Locale, Preference: current.Preference, Conversation: props.Conversation != "", TranslationUnavailable: false, Loading: current.Loading, Failed: current.Failed, Saved: current.Saved, Submit: submit, AutoSave: true}), indicator))
}
