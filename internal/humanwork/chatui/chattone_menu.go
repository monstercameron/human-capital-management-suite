package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
)

// chattoneShortcutRows is how many styles carry an Alt+Shift+digit shortcut:
// the first three, which is every style the product ships.
const chattoneShortcutRows = 3

// chattoneTrigger is the one control the composer's tool row shows for writing
// styles: a sparkle icon and the word "Rewrite". It opens the menu of styles
// (chattoneMenu). The label is drawn beside the icon wherever the row has room
// and by CSS alone drops out on a phone, so the accessible name is an
// aria-label, not the label's text. While the model works the same button reads
// "Rewriting…" and is disabled.
func chattoneTrigger(p ChattoneToolbarProps, text func(string) string) ui.Node {
	label := text("rewrite")
	return html.Button(html.Props{ID: p.Target + "-chattone-trigger", Type: "button", Class: "tool-button chattone-trigger", Disabled: p.Disabled || p.Pending, Title: text("rewrite_tip"),
		Data: map[string]string{"chattone-action": "toggle"},
		Aria: map[string]string{"label": label, "haspopup": "menu", "expanded": "false", "controls": p.Target + "-chattone-menu"}},
		icon("sparkles"),
		html.Span(html.Props{Class: "chattone-trigger-label", Aria: map[string]string{"hidden": "true"}, Text: label}),
		html.Span(html.Props{Class: "chattone-trigger-busy", Aria: map[string]string{"hidden": "true"}, Text: text("rewriting")}))
}

// chattoneStyleLabel is the name a style is shown under: the three built-in
// styles are translated, a house style keeps the name its workspace gave it.
func chattoneStyleLabel(style chatrewrite.Style, text func(string) string) string {
	label := style.Label
	if (style.ID == "professional" && label == "Professional") || (style.ID == "friendly" && label == "Friendly") || (style.ID == "concise" && label == "Concise") {
		label = text(style.Register)
	}
	return label
}

// chattoneMenuRows are the rows of the menu, one per style: its name, one line
// saying what it does and, for the first three, the shortcut that picks it
// without opening the menu. The style the server suggests also carries a small
// "Suggested" tag and the reason, on that row only.
func chattoneMenuRows(p ChattoneToolbarProps, text func(string) string) []ui.Node {
	rows := make([]ui.Node, 0, len(p.Styles))
	for index, style := range p.Styles {
		label := chattoneStyleLabel(style, text)
		number := itoa(index + 1)
		noteID := p.Target + "-chattone-note-" + number
		aria := map[string]string{"label": label, "describedby": noteID}
		name := []ui.Node{html.Span(html.Props{Class: "chattone-row-name", Dir: "auto", Text: label})}
		note := []ui.Node{html.Span(html.Props{ID: noteID, Class: "chattone-row-note", Dir: "auto", Text: text(style.Register + "_help")})}
		if p.Suggestion != nil && p.Suggestion.StyleID == style.ID {
			name = append(name, html.Span(html.Props{Class: "chattone-suggested", Data: map[string]string{"suggested": "true"}, Text: text("suggested")}))
			note = append(note, html.Span(html.Props{Class: "chattone-row-reason", Dir: "auto", Text: text(p.Suggestion.ReasonKey)}))
		}
		children := []ui.Node{html.Span(html.Props{Class: "chattone-row-text"},
			html.Span(html.Props{Class: "chattone-row-head"}, name...),
			html.Span(html.Props{Class: "chattone-row-detail"}, note...))}
		if index < chattoneShortcutRows {
			aria["keyshortcuts"] = "Alt+Shift+" + number
			children = append(children, html.Span(html.Props{Class: "chattone-row-key", Dir: "ltr", Aria: map[string]string{"hidden": "true"}, Text: "Alt+Shift+" + number}))
		}
		rows = append(rows, html.Button(html.Props{Type: "button", Role: "menuitem", Class: "menu-item chattone-row", Disabled: p.Disabled || p.Pending,
			Data: map[string]string{"chattone-action": "rewrite", "style": style.ID, "index": number}, Aria: aria}, children...))
	}
	return rows
}

// chattoneMenu is the layer the trigger opens: the style rows, then the one
// sentence that says when the draft leaves the browser, so it is read before a
// style is chosen. It is a writing-style layer like the panel that shows the
// result, and the shared layer code opens, places and closes both.
func chattoneMenu(p ChattoneToolbarProps, text func(string) string) ui.Node {
	return anchoredChatLayer(html.Props{ID: p.Target + "-chattone-menu", Class: "chattone-menu", Hidden: true, Role: "group", Data: map[string]string{"chattone-menu": "true", "chattone-for": p.Target}, Aria: map[string]string{"label": text("menu")}}, "writing-style",
		html.Div(html.Props{Class: "chattone-menu-list", Role: "menu", Aria: map[string]string{"label": text("menu")}}, chattoneMenuRows(p, text)...),
		html.P(html.Props{Class: "chattone-menu-notice", Text: text("notice")}))
}
