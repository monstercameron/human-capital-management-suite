package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Browse channels (CHATUX-025): the list fills the height the window has, and a
// channel the person is in has a Leave button on its row, with the same two
// presses as the sidebar's row menu: the first turns the button into a
// question, the second leaves.

var chatux025Copy = map[string][3]string{
	"leave":         {"Leave", "Verlassen", "مغادرة"},
	"leave-confirm": {"Press again to leave", "Zum Verlassen erneut drücken", "اضغط مرة أخرى للمغادرة"},
	"leave-label":   {"Leave {name}", "{name} verlassen", "مغادرة {name}"},
}

func chatux025Text(m Model, key string) string {
	copy := chatux025Copy[key]
	return chatbug039Text(key, copy[chatbug039LocaleIndex(m.Locale)], copy[0])
}

// chatux025LeaveButton is the Leave button of a joined row of Browse channels,
// or nil when the person cannot leave that conversation.
func chatux025LeaveButton(m Model, h handlers, c Conversation) ui.Node {
	if !c.Joined || !chatux020Leaves(c) {
		return nil
	}
	label, action, class := chatux025Text(m, "leave"), "rail-leave", "button secondary small browse-leave"
	if h.local.railLeave == c.ID {
		label, action, class = chatux025Text(m, "leave-confirm"), "rail-leave-confirm", "button secondary small danger browse-leave"
	}
	return html.Button(html.Props{Class: class, Type: "button", Disabled: m.Callbacks.LeaveConversation == nil,
		Aria: map[string]string{"label": chatcmd002Fill(chatux025Text(m, "leave-label"), "name", c.Name)},
		Data: map[string]string{"action": action, "id": c.ID}, Text: label})
}

// chatux025Styles gives the dialog the height the window has. The heading and
// the footer keep their size and the list takes the rest and scrolls inside
// itself, so a workspace with many channels does not show six rows in half of
// the window.
const chatux025Styles = `.chat-workspace .browse-dialog{display:flex;flex-direction:column;overflow:hidden;max-height:calc(100dvh - max(16px,10vh) - 16px)}` +
	`.chat-workspace .browse-dialog>*{flex:none}.chat-workspace .browse-dialog>.browse-list{flex:1 1 auto;min-height:0;max-height:none;overflow-y:auto}` +
	`.chat-workspace .browse-dialog>.dialog-empty{flex:1 1 auto;min-height:0}` +
	`.chat-workspace .browse-leave.danger{border-color:var(--hcm-color-danger);color:var(--hcm-color-danger)}` +
	`@media(max-width:560px){.chat-workspace .browse-dialog{max-height:88dvh}}`

// chatux025ThreadStamp is the time under the thread parent's author with its
// day in front ("Yesterday · 5:17 AM"): the pane shows one message out of the
// day dividers that give a message in the channel its day.
func chatux025ThreadStamp(m Model, root Message) string {
	if root.SentAt.IsZero() {
		return root.TimeLabel
	}
	day, clock := dayLabel(m, root.SentAt), chat5Clock(m.Locale, root.SentAt)
	if clock == "" {
		return day
	}
	return day + " · " + clock
}
