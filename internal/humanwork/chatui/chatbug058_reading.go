package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// CHATBUG-058: the person's reading language for one conversation lives in
// Conversation details, as one row ("Reading in this conversation") that opens
// to the language and the translate switch and saves every change at once. It
// replaces the "Apply to this conversation" box that used to sit inside Chat
// preferences, where ticking it changed where the next change would be saved.
// The row asks the same settings service, for this conversation's scope.

const (
	keyChatbug058Row  = "chat.bug058.row"
	keyChatbug058Note = "chat.bug058.note"
)

var chatbug058Copy = map[string]map[string]string{
	"en-US": {keyChatbug058Row: "Reading in this conversation", keyChatbug058Note: "Only for this conversation. Your own reading language is in Chat preferences."},
	"de-DE": {keyChatbug058Row: "Lesen in diesem Gespräch", keyChatbug058Note: "Nur für dieses Gespräch. Ihre eigene Lesesprache steht in den Chat-Einstellungen."},
	"ar":    {keyChatbug058Row: "القراءة في هذه المحادثة", keyChatbug058Note: "لهذه المحادثة فقط. لغة القراءة العامة في تفضيلات الدردشة."},
}

const (
	chatbug058ReadingField   = "chatbug058-reading"
	chatbug058TranslateField = "chatbug058-translate"
)

type chatbug058Props struct {
	Locale, Conversation string
	// Section, when set, is the details panel's row (CHATUX-027).
	Section sectionWrap
}

type chatbug058State struct {
	Preference             chatrender.Preference
	Checking, Unavailable  bool
	Loading, Failed, Saved bool
}

// chatbug058ConversationReading is the details row, or nil when the workspace
// has no reading-language service composed.
func chatbug058ConversationReading(m Model) ui.Node {
	if m.SelectedID == "" || (m.ChatFeatures != nil && !m.ChatFeatures.Renderings) {
		return nil
	}
	// Keyed by conversation: another conversation reads its own scope.
	return html.WithKey(ui.CreateElement(chatbug058Panel, chatbug058Props{Locale: m.Locale, Conversation: m.SelectedID}), "chatbug058-"+m.SelectedID)
}

func chatbug058Panel(p chatbug058Props) ui.Node {
	state := ui.UseState(chatbug058State{Preference: chatrender.DefaultPreference(p.Locale), Checking: renderingChecksAvailability})
	ui.UseEffectOf(func() func() {
		return renderingLoadSettings(p.Conversation, func(pref chatrender.Preference, err error) {
			ui.PostAsync(func() {
				state.Update(func(current chatbug058State) chatbug058State {
					if err == nil {
						current.Preference = pref
					}
					current.Checking, current.Unavailable = false, err != nil
					return current
				})
			})
		})
	}, struct{ Room string }{p.Conversation})
	change := ui.UseEvent(func(e ui.Event) {
		e.PreventDefault()
		current := state.Get()
		next := current.Preference
		next.ReadingLanguage = domValue(chatbug058ReadingField)
		next.Translate = filterChecked(chatbug058TranslateField)
		if next.Validate() != nil {
			state.Update(func(c chatbug058State) chatbug058State { c.Failed = true; return c })
			return
		}
		state.Update(func(c chatbug058State) chatbug058State {
			c.Preference, c.Loading, c.Failed, c.Saved = next, true, false, false
			return c
		})
		renderingSaveSettings(p.Conversation, next, func(err error) {
			ui.PostAsync(func() {
				state.Update(func(c chatbug058State) chatbug058State {
					if err != nil {
						c.Preference = current.Preference
					}
					c.Loading, c.Failed, c.Saved = false, err != nil, err == nil
					return c
				})
			})
		})
	})
	return chatbug058View(p, state.Get(), change)
}

// chatbug058View draws the row from its state: nothing while the service has
// not answered or is not there (a row that cannot be used is not drawn), and
// otherwise the row with the language at its right.
func chatbug058View(p chatbug058Props, s chatbug058State, change ui.Handler) ui.Node {
	if s.Checking || s.Unavailable {
		return nil
	}
	m := Model{Locale: p.Locale}
	t := func(key string) string { return RenderingText(p.Locale, key) }
	options := make([]ui.Node, 0, len(chatbug045Languages))
	for _, l := range chatbug045Languages {
		options = append(options, html.Option(html.Props{Value: l, Text: t(l), Selected: l == chatrender.Language(s.Preference.ReadingLanguage)}))
	}
	status := ""
	switch {
	case s.Failed:
		status = t("error")
	case s.Saved:
		status = t("saved")
	}
	form := html.Form(html.Props{Class: "chatbug058-form", Dir: direction(p.Locale), OnChange: change, OnSubmit: change},
		html.P(html.Props{Class: "field-hint", Text: laneText(m, chatbug058Copy, keyChatbug058Note)}),
		html.Label(html.Props{For: chatbug058ReadingField, Text: t("reading")}),
		html.Select(html.Props{ID: chatbug058ReadingField, Class: "chat-input", Name: "reading"}, options...),
		html.Label(html.Props{Class: "chatbug058-switch", For: chatbug058TranslateField},
			html.Input(html.Props{ID: chatbug058TranslateField, Class: "switch", Type: "checkbox", Name: "translate", Checked: s.Preference.Translate}),
			html.Span(html.Props{Text: t("translate")})),
		html.P(html.Props{Class: "chatbug058-status", Role: "status", Aria: map[string]string{"live": "polite"}, Text: status}))
	if p.Section != nil {
		return html.Section(html.Props{Class: "details-section details-reading"},
			p.Section(sectionContent{Label: laneText(m, chatbug058Copy, keyChatbug058Row), Value: ui.Text(t(chatrender.Language(s.Preference.ReadingLanguage))), Body: form, Failed: s.Failed}))
	}
	return html.Section(html.Props{Class: "details-section details-reading"},
		chatPolishDisclosure(html.Props{Class: "channel-widget-edit"},
			chatPolishDisclosureLabel(html.Props{Class: "channel-widget-summary"},
				html.Span(html.Props{Class: "channel-widget-summary-label", Text: laneText(m, chatbug058Copy, keyChatbug058Row)}),
				html.Span(html.Props{Class: "channel-widget-summary-value", Text: t(chatrender.Language(s.Preference.ReadingLanguage))})),
			form))
}
