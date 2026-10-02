package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-031. Chat preferences is one settings list: each row is a label, its
// current value and one control. Reading language is a single select that holds
// the current language and saves when it changes, with a brief tick beside it;
// the older "Change" button that opened a second labelled select is gone. The
// saving itself (the form's change handler, the settings client, the field ids
// it reads) is untouched: this file only draws the row.
//
// A switch row (Quiet hours, and the personal voice switch that sits beside it)
// is the same row: .chat-prefs-head with the label first, the value, then the
// switch at the end. Reading language puts its select where a switch would be.

// chatux031State is the state of the last save, as the status line and the tick
// draw it: "loading", "error", "saved" or "help" (nothing has been saved yet).
func chatux031State(m RenderingSettingsModel) string {
	switch {
	case m.Loading:
		return "loading"
	case m.Failed:
		return "error"
	case m.Saved:
		return "saved"
	}
	return "help"
}

// chatux031ReadingRow is the Reading language row: the label, the select that
// shows the current language and, for a moment after a save, the tick. The
// select keeps the id and name the form's change handler reads.
func chatux031ReadingRow(m RenderingSettingsModel, options []ui.Node, saved bool) ui.Node {
	title := chatbug045Text(Model{Locale: m.Locale}, keyChatbug045Reading)
	children := []ui.Node{
		html.Label(html.Props{Class: "chat-prefs-title", For: "chatrender-reading", Text: title}),
		html.Select(html.Props{ID: "chatrender-reading", Name: "reading", Class: "chatux031-select", Aria: map[string]string{"describedby": "chatrender-settings-status"}}, options...),
	}
	if saved {
		children = append(children, html.Span(html.Props{Class: "chatux031-tick", Aria: map[string]string{"hidden": "true"}, Text: "✓"}))
	}
	return html.Div(html.Props{Class: "chat-prefs-head chatux031-row"}, children...)
}

// chatux031ReadingSection is the Reading language section of the panel: the
// form (its row, its switch, More options) and the languages of the conversation
// under it. It carries the section marker the older row had.
func chatux031ReadingSection(_ string, form ui.Node, indicator ui.Node) ui.Node {
	children := []ui.Node{form}
	if indicator != nil {
		children = append(children, indicator)
	}
	return html.Section(html.Props{Class: "chat-prefs-section chatrender-personal", Data: map[string]string{"prefs-section": "reading-languages"}, Aria: map[string]string{"labelledby": "chatrender-settings-title"}}, children...)
}
