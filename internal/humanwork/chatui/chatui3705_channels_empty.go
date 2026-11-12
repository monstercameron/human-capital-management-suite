package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// chatui3705ChannelsEmpty is the hint under the Channels heading when the
// person is in no channel: a line of words and the Browse channels control, so
// the empty section says where channels come from (#3705).
func chatui3705ChannelsEmpty(m Model) ui.Node {
	return html.Div(html.Props{Class: "rail-empty"},
		html.Span(html.Props{Text: m.t(KeyChannelsEmpty)}),
		html.Button(html.Props{Class: "button secondary small", Type: "button", Disabled: m.Callbacks.OpenBrowse == nil,
			Data: map[string]string{"action": "open-browse"}, Text: m.t(KeyBrowse)}))
}
