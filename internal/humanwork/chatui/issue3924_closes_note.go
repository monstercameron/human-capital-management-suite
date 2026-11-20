package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// What the poll preview says about its closing time (#3924, #3925): whose time
// zone a chosen time is in, and, for a poll whose results wait for closing but
// that has no closing time, that somebody has to close it. The note offers the
// nearest automatic closing time as one press and leaves "Never" a deliberate
// choice; where the card cannot be closed by hand it says so instead of
// suggesting it.

func chatcmd003ClosesNotes(m Model, state chatcmd003Preview) []ui.Node {
	p := state.Draft.Card.Poll
	if p == nil {
		return nil
	}
	if p.ClosesAt != nil {
		return []ui.Node{html.P(html.Props{ID: "chatcmd003-zone-note", Class: "chatcmd003-note", Dir: "auto", Text: chatcmd003Text(m, "zone-everyone")})}
	}
	if p.Results != "after-closing" {
		return nil
	}
	key := "closes-manual"
	if m.Chatcmd002.Mutate == nil {
		key = "closes-nomanual"
	}
	return []ui.Node{html.Div(html.Props{Class: "chatcmd003-row"},
		html.P(html.Props{ID: "chatcmd003-closes-note", Class: "chatcmd003-note", Role: "status", Dir: "auto", Text: chatcmd003Text(m, key)}),
		html.Button(html.Props{ID: "chatcmd003-closes-suggest", Class: "button secondary small", Type: "button", Disabled: state.Busy,
			Data: map[string]string{"action": "chatcmd003-set", "id": "closes", "extra": "today"}, Text: chatcmd003Text(m, "closes-suggest")}))}
}
