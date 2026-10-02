package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-008: the thread pane says what its controls do. The header reads
// "Thread" with the channel as a link under it; the bell is the existing
// Follow action named for what it does; the reply box says where the reply
// goes. The service has no also-send-to-channel option on a thread reply
// (SendPostRequest carries a parent id and nothing else), so the pane offers
// no checkbox for it rather than a control that does nothing.

// chatux008Text is the reviewed copy of the thread pane's controls in the
// three product languages; a catalog placeholder never reaches the page.
func chatux008Text(m Model, key string) string {
	copy := map[string][3]string{
		"notify":          {"Notify me about replies", "Über Antworten benachrichtigen", "أبلغني بالردود"},
		"notify_off_tip":  {"Get a notification for each new reply in this thread", "Bei jeder neuen Antwort in diesem Thread benachrichtigt werden", "احصل على إشعار بكل رد جديد في هذه السلسلة"},
		"notify_on_tip":   {"Following: you get a notification for each reply. Select to stop.", "Folge ich: Sie werden über jede Antwort benachrichtigt. Zum Beenden auswählen.", "تتابع: ستصلك إشعارات بكل رد. اختر لإيقافها."},
		"reply_in_thread": {"Reply in thread", "Im Thread antworten", "الرد في السلسلة"},
	}
	values, ok := copy[key]
	if !ok {
		return ""
	}
	index := 0
	if strings.HasPrefix(m.Locale, "de") {
		index = 1
	}
	if strings.HasPrefix(m.Locale, "ar") {
		index = 2
	}
	return chatbug039Text(key, values[index], values[0])
}

// chatux008Bell is the bell glyph, drawn like the other chat icons.
func chatux008Bell() ui.Node {
	return html.Tag("svg", html.Props{Class: "chat-icon icon-bell", Raw: iconSVGAttrs},
		html.Tag("path", html.Props{Raw: map[string]any{"d": "M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9M13.7 21a2 2 0 0 1-3.4 0"}}))
}

// chatux008Heading is the thread pane's header. roomName is the channel's
// name as the pane writes it ("#random", already isolated for right-to-left).
func chatux008Heading(m Model, roomName string) ui.Node {
	tip := chatux008Text(m, "notify_off_tip")
	if m.ThreadFollowed {
		tip = chatux008Text(m, "notify_on_tip")
	}
	channel := m.selected()
	var link ui.Node = html.Span(html.Props{Class: "thread-channel-link", Dir: "auto", Text: roomName})
	if channel.ID != "" {
		link = html.A(html.Props{Class: "thread-channel-link", Href: ChannelReferenceURL(channel.ID), Dir: "auto", Data: map[string]string{"action": "thread-channel", "id": channel.ID}, Text: roomName})
	}
	return html.Div(html.Props{Class: "side-heading thread-heading"},
		html.Div(html.Props{Class: "thread-heading-text"},
			html.H2(html.Props{Text: m.t(KeyThread)}),
			link),
		html.Div(html.Props{Class: "side-heading-actions"},
			html.Button(html.Props{Class: "icon-button thread-notify", Type: "button", Disabled: m.Callbacks.SetThreadFollow == nil, Title: tip,
				Data: map[string]string{"action": "follow"}, Aria: map[string]string{"pressed": boolString(m.ThreadFollowed), "label": chatux008Text(m, "notify")}}, chatux008Bell()),
			html.Button(html.Props{Class: "icon-button thread-back", Type: "button", Disabled: m.Callbacks.CloseThread == nil, Data: map[string]string{"action": "close-thread"}, Aria: map[string]string{"label": m.t(KeyCloseThread)}, Title: m.t(KeyCloseThread)}, icon("close"), html.Span(html.Props{Class: "thread-back-label", Text: m.t(KeyCloseThread)})),
		))
}

// chatux008Click handles the channel link in the thread header: it closes the
// thread so the channel is what the person sees, without leaving the page. It
// reports whether the click was that link.
func chatux008Click(e ui.MouseEvent, m Model) bool {
	action, id, _ := eventAction(e)
	if action != "thread-channel" || id == "" {
		return false
	}
	e.PreventDefault()
	if m.Callbacks.CloseThread != nil {
		m.Callbacks.CloseThread()
	}
	return true
}
