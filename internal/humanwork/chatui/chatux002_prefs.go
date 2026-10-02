package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-002: the conversation list holds conversations and nothing else. The
// settings that used to be rows under it live behind two controls in the list's
// headings: Chat preferences (a gear in the Conversations heading) and the
// Channels menu (a plus in the Channels heading). Both open the shared popover
// layers the page already has, so a plain click opens them and nothing waits
// for a timer or a focus event.

// The layer kinds of the two panels. Both belong to the sidebar settings group:
// one of them is open at a time and a click elsewhere closes it.
const (
	chatux002PrefsKind    = "chat-prefs"
	chatux002ChannelsKind = "channels-menu"
)

// chatux002Layer is the body of a disclosure drawn as a popover layer.
func chatux002Layer(kind, label string, children ...ui.Node) ui.Node {
	body := html.Props{
		Class: "chat-disclosure-body chat-settings-layer chatux002-layer",
		Role:  "dialog", Hidden: true,
		Aria: map[string]string{"label": label},
		Data: map[string]string{"chat-disclosure-body": "true"},
	}
	return anchoredChatLayer(body, kind, children...)
}

// chatux002QuietValue is Quiet hours' current value: the hours when it is on,
// "Off" when it is not.
func chatux002QuietValue(m Model) string {
	if !m.Preferences.QuietHours {
		return m.t(KeyOff)
	}
	return quietClock(m, m.Preferences.QuietStartMinute) + "–" + quietClock(m, m.Preferences.QuietEndMinute)
}

// chatux002PrefsAvailable is whether Chat preferences has anything to offer. It
// always has Quiet hours (disabled with a reason when the page cannot save it),
// so the control is drawn unless the deployment withholds every preference.
func chatux002PrefsAvailable(m Model) bool {
	return m.Callbacks.SavePreferences != nil || m.ChatFeatures == nil || m.ChatFeatures.Renderings
}

// chatux002PrefsControl is the gear beside the new-conversation control and the
// one panel it opens: Quiet hours, and Reading languages when the language
// service answers.
func chatux002PrefsControl(m Model, h handlers) ui.Node {
	if !chatux002PrefsAvailable(m) {
		return nil
	}
	label := chatux002Text(m, keyChatux002Prefs)
	// CHATBUG-045: Writing style and Emoji skin tone follow Reading language; the
	// rows are keyed, so one appearing late never takes another's place.
	rows := chatbug045PanelRows(m, h)
	return html.Div(html.Props{Class: "rail-prefs-control", Data: map[string]string{"chat-disclosure": "true"}},
		html.Button(html.Props{Class: "icon-button chatux002-gear", Type: "button", Title: label,
			Data: map[string]string{"chat-disclosure-toggle": "true"},
			Aria: map[string]string{"label": label, "expanded": "false", "haspopup": "dialog"}}, icon("settings")),
		chatux002Layer(chatux002PrefsKind, label, rows...))
}

// chatux002QuietMoon is the one small moon beside the Conversations heading
// while quiet hours are on. Nothing is drawn while they are off.
func chatux002QuietMoon(m Model) ui.Node {
	if !m.Preferences.QuietHours {
		return nil
	}
	tip := chatux002Textf(m, keyChatux002QuietOnTip, map[string]string{"from": quietClock(m, m.Preferences.QuietStartMinute), "until": quietClock(m, m.Preferences.QuietEndMinute)})
	return html.Span(html.Props{Class: "rail-quiet-moon", Title: tip, Role: "img", Aria: map[string]string{"label": tip}, Data: map[string]string{"quiet-hours-on": "true"}}, icon("moon"))
}

// chatux002ChannelsMenu is the plus in a section's heading and the panel it
// opens: Add channels, Browse channels and New section. sectionCreate is the
// existing New section disclosure and its form, moved here whole.
func chatux002ChannelsMenu(m Model, sectionCreate ui.Node) ui.Node {
	label := chatux002Text(m, keyChatux002ChannelMenu)
	item := func(action, glyph, name, note string, disabled bool) ui.Node {
		return html.Button(html.Props{Class: "menu-item chatux002-item", Type: "button", Disabled: disabled, Data: map[string]string{"action": action}},
			html.Span(html.Props{Class: "chatux002-item-icon", Aria: map[string]string{"hidden": "true"}}, icon(glyph)),
			html.Span(html.Props{Class: "chatux002-item-text"},
				html.Span(html.Props{Class: "chatux002-item-name", Dir: "auto", Text: name}),
				html.Span(html.Props{Class: "chatux002-item-note", Dir: "auto", Text: note})))
	}
	return html.Div(html.Props{Class: "section-menu", Data: map[string]string{"chat-disclosure": "true"}},
		html.Button(html.Props{Class: "section-menu-trigger", Type: "button", Title: label,
			Data: map[string]string{"chat-disclosure-toggle": "true"},
			Aria: map[string]string{"label": label, "expanded": "false", "haspopup": "dialog"}}, icon("plus")),
		chatux002Layer(chatux002ChannelsKind, label,
			item("open-create", "plus", chatux002Text(m, keyChatux002AddName), chatux002Text(m, keyChatux002AddNote), m.Callbacks.OpenCreate == nil),
			item("open-browse", "browse", m.t(KeyBrowse), chatux002Text(m, keyChatux002BrowseNote), m.Callbacks.OpenBrowse == nil),
			sectionCreate))
}

// chatux002MenuHost is the section whose heading carries the Channels menu: the
// Channels section, or the first section when a layout has none.
func chatux002MenuHost(sections []SidebarSection) string {
	for _, section := range sections {
		if section.ID == "channels" {
			return section.ID
		}
	}
	if len(sections) > 0 {
		return sections[0].ID
	}
	return ""
}

// chatux002Row is one setting of the Chat preferences panel: its name, its
// current value at the end of the row, and its own control after the value (a
// switch, or a button that opens its form). What the control reveals goes
// beneath, inside the same section.
func chatux002Row(id, title, value string, control ui.Node, children ...ui.Node) ui.Node {
	headingID := "chat-prefs-" + id + "-title"
	head := []ui.Node{html.H3(html.Props{ID: headingID, Class: "chat-prefs-title", Text: title})}
	if value != "" {
		head = append(head, html.Span(html.Props{Class: "chat-prefs-value", Dir: "auto", Data: map[string]string{"prefs-value": id}, Text: value}))
	}
	if control != nil {
		head = append(head, control)
	}
	nodes := []ui.Node{html.Div(html.Props{Class: "chat-prefs-head"}, head...)}
	nodes = append(nodes, children...)
	return html.Section(html.Props{Class: "chat-prefs-section", Data: map[string]string{"prefs-section": id}, Aria: map[string]string{"labelledby": headingID}}, nodes...)
}

// chatux002DisclosureRow is a setting whose control opens its form beneath it:
// the same row as chatux002Row, drawn as the shared disclosure so that a plain
// click opens it (the toggle is the row's button, the form is its body).
func chatux002DisclosureRow(id, title, value string, class string, toggle ui.Node, body ...ui.Node) ui.Node {
	headingID := "chat-prefs-" + id + "-title"
	head := []ui.Node{html.H3(html.Props{ID: headingID, Class: "chat-prefs-title", Text: title})}
	if value != "" {
		head = append(head, html.Span(html.Props{Class: "chat-prefs-value", Dir: "auto", Data: map[string]string{"prefs-value": id}, Text: value}))
	}
	head = append(head, toggle)
	return html.Section(html.Props{Class: "chat-prefs-section " + class, Data: map[string]string{"prefs-section": id, "chat-disclosure": "true"}, Aria: map[string]string{"labelledby": headingID}},
		html.Div(html.Props{Class: "chat-prefs-head"}, head...),
		html.Div(html.Props{Class: "chat-disclosure-body chat-prefs-inline-body", Hidden: true, Data: map[string]string{"chat-disclosure-body": "true"}}, body...))
}
