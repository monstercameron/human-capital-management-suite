package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue #900005.
//
// RTL message bodies: a body whose language is not known used to be drawn with
// dir="auto". The browser resolves that to the direction of the text and
// overrides the stylesheet's direction, so an English message in an Arabic page
// started at the far left, away from its author. In a right-to-left page such a
// body now takes the page's direction; its paragraphs keep dir="auto", so each
// script is still ordered by its own direction (see markdownBlockProps).
//
// Keyboard shortcuts: the sheet #3744 added opens from the question mark key and,
// from here, from a row in Chat preferences.

// issue900005BodyDir is the dir attribute of a message body in the page of m.
func issue900005BodyDir(m Model, dir string) string {
	if dir == "auto" && m.Direction == "rtl" {
		return "rtl"
	}
	return dir
}

// issue900005ShortcutsAction is the data-action of the visible entry point.
const issue900005ShortcutsAction = "shortcuts-open"

// issue900005ShortcutsRow is the Keyboard shortcuts row of Chat preferences: the
// key that opens the list, and a button for people who do not use a keyboard or
// do not know the key.
func issue900005ShortcutsRow(m Model) ui.Node {
	title := issue3744Text(m.Locale, "title")
	return chatux002Row("shortcuts", title, "?", nil,
		html.Div(html.Props{Class: "chat-prefs-choices", Role: "group", Aria: map[string]string{"label": title}},
			html.Button(html.Props{Class: "chat-prefs-choice", Type: "button", Dir: "auto", Text: issue3744Text(m.Locale, "open"),
				Data: map[string]string{"action": issue900005ShortcutsAction},
				Aria: map[string]string{"haspopup": "dialog"}})),
		html.P(html.Props{Class: "field-hint chat-prefs-note", Dir: "auto", Text: issue3744Text(m.Locale, "open_note")}))
}

// Issue900005Styles hides the "Results for ..." summary of the search page from
// the eye while nothing has focus on it: the page header already says it, and a
// second line under the filters read as a second heading. The line stays in the
// accessibility tree and stays focusable by script, which is how the browser
// client returns focus to the results (#3857); while focused it is drawn so the
// person can see where focus is.
const Issue900005Styles = `.chatsearch-scope:not(:focus){position:absolute;inline-size:1px;block-size:1px;margin:-1px;padding:0;overflow:hidden;clip-path:inset(50%);white-space:nowrap}`
