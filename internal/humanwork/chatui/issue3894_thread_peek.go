package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3894: on a phone the thread covers the channel, so View in channel used
// to leave the reader looking at the same thread. The thread is now set aside
// (the channel and the highlighted message show) with a plain way back, while
// wide layouts keep thread and channel side by side as before.
const (
	keyIssue3894Back   = "chat.issue3894.back_to_thread"
	keyIssue3894Status = "chat.issue3894.showing_channel"
)

var issue3894Copy = map[string]map[string]string{
	"en-US": {keyIssue3894Back: "Back to thread", keyIssue3894Status: "Showing the message in the channel"},
	"de-DE": {keyIssue3894Back: "Zurück zum Thread", keyIssue3894Status: "Die Nachricht wird im Kanal angezeigt"},
	"ar":    {keyIssue3894Back: "العودة إلى الموضوع", keyIssue3894Status: "تُعرض الرسالة في القناة"},
}

// Issue3894Styles hides the thread column only inside the narrow rules and shows
// the way back there too; at any wider size nothing changes.
const Issue3894Styles = `.thread-peek-return{display:none}` +
	`@media(max-width:760px){.chat-workspace[data-thread-peek="true"] .chat-side{display:none}.chat-workspace[data-thread-peek="true"] .thread-peek-return{display:flex;align-items:center;justify-content:space-between;gap:8px;padding:4px 12px;border-block-end:1px solid var(--line);background:var(--surface)}.thread-peek-return .button{min-height:44px;min-width:44px}}` +
	`@container chat (max-width:760px){.chat-workspace[data-thread-peek="true"] .chat-side{display:none}.chat-workspace[data-thread-peek="true"] .thread-peek-return{display:flex;align-items:center;justify-content:space-between;gap:8px;padding:4px 12px;border-block-end:1px solid var(--line);background:var(--surface)}}`

// issue3894Peeking reports whether the thread is set aside for the channel.
func issue3894Peeking(m Model, box localUI) bool {
	return m.ShowThread && !m.searching() && box.threadPeek != "" && box.threadPeek == m.ThreadParentID
}

// issue3894Return is the bar with the way back to the thread; nil unless peeking.
func issue3894Return(m Model, box localUI) ui.Node {
	if !issue3894Peeking(m, box) {
		return nil
	}
	return html.Div(html.Props{Class: "thread-peek-return", Role: "status", Aria: map[string]string{"live": "polite"}},
		html.Span(html.Props{Text: laneText(m, issue3894Copy, keyIssue3894Status)}),
		html.Button(html.Props{Class: "button secondary small", ID: "chat-thread-return", Type: "button", Data: map[string]string{"action": "return-to-thread"}, Text: laneText(m, issue3894Copy, keyIssue3894Back)}))
}
