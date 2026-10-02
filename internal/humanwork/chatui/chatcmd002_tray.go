package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// The bar under the conversation header is one line. It names the most recent
// open items, polls and lists posted as messages first and the channel's
// standing list and poll after them, and what does not fit goes behind "+N
// more", a small list in the same place a card opens. A row that wrapped onto
// a second line took that height from the messages below it.

// chatcmd002TrayVisible is how many chips the bar names before "+N more".
const chatcmd002TrayVisible = 2

// chatcmd002TrayMore is the tray state of the "+N more" list.
const chatcmd002TrayMore = "more"

var chatcmd002TrayCopy = map[string][3]string{
	"more":       {"+{n} more", "+{n} weitere", "+{n} أخرى"},
	"more-title": {"More polls and lists", "Weitere Umfragen und Listen", "استطلاعات وقوائم أخرى"},
	"more-label": {"{n} more polls and lists", "{n} weitere Umfragen und Listen", "{n} استطلاعات وقوائم أخرى"},
}

func chatcmd002TrayText(m Model, key string) string {
	copy := chatcmd002TrayCopy[key]
	return chatbug039Text(key, copy[chatbug039LocaleIndex(m.Locale)], copy[0])
}

// trayEntry is one thing the bar can name: the channel's to-do list or poll,
// or a card posted in the conversation.
type trayEntry struct {
	// which is "todo" or "poll" for the standing ones and "" for a card.
	which, glyph, label string
	// post is the message a card was posted in.
	post string
}

// button draws the entry as a chip in the bar or, with class "tray-more-row",
// as a row of the "+N more" list.
func (e trayEntry) button(m Model, open, class string, inList bool) ui.Node {
	label := html.Span(html.Props{Class: "tray-chip-label", Dir: "auto", Text: e.label})
	if e.which != "" {
		props := html.Props{Class: class, Type: "button", Data: map[string]string{"action": "tray-" + e.which}, Aria: map[string]string{"expanded": boolString(open == e.which)}, Title: e.label}
		if open == e.which {
			props.Class += " active"
		}
		return html.Button(props, icon(e.glyph), label)
	}
	action := "chatcmd002-jump"
	if inList {
		// The list closes when its row is chosen.
		action = "tray-more-jump"
	}
	return html.Button(html.Props{Class: class + " tray-card-chip", Type: "button", Title: chatcmd003Text(m, "go-to-message"),
		Data: map[string]string{"action": action, "id": e.post}}, icon(e.glyph), label)
}

// chatcmd002TrayEntries are the things the bar can name, most recent first: the
// open cards among the messages on the page, newest first, then the channel's
// standing list and poll, which are older than any card.
func chatcmd002TrayEntries(m Model) []trayEntry {
	var entries []trayEntry
	now := time.Now()
	for i := len(m.Messages) - 1; i >= 0; i-- {
		msg := m.Messages[i]
		if !strings.Contains(msg.Body, chat.Chatcmd002BodyMarker) {
			continue
		}
		card, ok := chat.Chatcmd002Decode(msg.Body)
		if view, loaded := m.Chatcmd002Views[msg.ID]; loaded {
			card, ok = view.Card, true
		}
		if !ok || card.Closed(now) {
			continue
		}
		glyph, label := "poll", excerpt(card.Title, 48)
		if card.Todo != nil {
			done := 0
			for _, item := range card.Todo.Items {
				if item.Completed {
					done++
				}
			}
			glyph = "checklist"
			label += " · " + chatcmd002Fill(chatcmd003Text(m, "done-count"), "done", m.nz(done), "total", m.nz(len(card.Todo.Items)))
		}
		entries = append(entries, trayEntry{glyph: glyph, label: label, post: msg.ID})
	}
	if total := len(m.ChannelTodo.Items); total > 0 {
		done := 0
		for _, item := range m.ChannelTodo.Items {
			if item.Completed {
				done++
			}
		}
		// Round 3 C-18: a chip is a summary of something that exists. An empty
		// to-do list opened from the header gets only its card.
		label := m.t(KeyTodoTitle) + " · " + m.tf(KeyTodoProgress, map[string]string{"done": m.nz(done), "total": m.nz(total)})
		entries = append(entries, trayEntry{which: "todo", glyph: "checklist", label: label})
	}
	if q := m.ChannelPoll.Question; q != "" {
		votes := m.tf(KeyPollVotes, map[string]string{"n": m.nz(m.ChannelPoll.TotalVotes)})
		if m.ChannelPoll.TotalVotes == 1 {
			votes = m.tf(KeyPollVoteOne, map[string]string{"n": m.nz(1)})
		}
		entries = append(entries, trayEntry{which: "poll", glyph: "poll", label: excerpt(q, 60) + " · " + votes})
	}
	return entries
}

// chatcmd002TrayChips is the bar's content: the first chips and, when there are
// more, the button that opens the rest.
func chatcmd002TrayChips(m Model, open string, entries []trayEntry) []ui.Node {
	shown := entries
	if len(shown) > chatcmd002TrayVisible {
		shown = shown[:chatcmd002TrayVisible]
	}
	chips := make([]ui.Node, 0, len(shown)+1)
	for _, entry := range shown {
		chips = append(chips, entry.button(m, open, "tray-chip", false))
	}
	if rest := len(entries) - len(shown); rest > 0 {
		class := "tray-chip tray-more"
		if open == chatcmd002TrayMore {
			class += " active"
		}
		chips = append(chips, html.Button(html.Props{Class: class, Type: "button", Data: map[string]string{"action": "tray-more"},
			Aria: map[string]string{"expanded": boolString(open == chatcmd002TrayMore), "label": chatcmd002Fill(chatcmd002TrayText(m, "more-label"), "n", m.nz(rest))}},
			html.Span(html.Props{Class: "tray-chip-label", Text: chatcmd002Fill(chatcmd002TrayText(m, "more"), "n", m.nz(rest))})))
	}
	return chips
}

// chatcmd002TrayMoreBody lists what the bar left out, one row each.
func chatcmd002TrayMoreBody(m Model, entries []trayEntry) ui.Node {
	if len(entries) <= chatcmd002TrayVisible {
		return nil
	}
	rows := make([]ui.Node, 0, len(entries)-chatcmd002TrayVisible)
	for _, entry := range entries[chatcmd002TrayVisible:] {
		rows = append(rows, html.Li(html.Props{}, entry.button(m, chatcmd002TrayMore, "tray-more-row", true)))
	}
	return html.Ul(html.Props{Class: "tray-more-list"}, rows...)
}

// chatcmd002TrayStyles keeps the bar to one line: chips shrink and cut their
// text with an ellipsis, "+N more" never shrinks.
// The message list fades in over its first 16px, so the row needs room under
// it: without it the first message read as cut by the chips.
const chatcmd002TrayStyles = `.chat-workspace .channel-tray{padding-bottom:8px}.chat-workspace .channel-tray-bar{flex-wrap:nowrap;overflow:hidden;min-width:0}` +
	`.chat-workspace .channel-tray-bar>.tray-chip{flex:0 1 auto;min-width:0;max-width:min(100%,22rem)}.chat-workspace .channel-tray-bar>.tray-chip.tray-more{flex:none}` +
	`.chat-workspace .tray-more-list{display:grid;gap:4px;margin:0;padding:0;list-style:none}` +
	`.chat-workspace .tray-more-row{display:flex;align-items:center;gap:8px;width:100%;min-height:36px;padding:4px 10px;border:1px solid transparent;border-radius:var(--hcm-radius-control);background:transparent;color:var(--ink);font:inherit;font-size:.875rem;text-align:start;cursor:pointer}` +
	`.chat-workspace .tray-more-row:hover{background:var(--soft)}.chat-workspace .tray-more-row:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:1px}` +
	`.chat-workspace .tray-more-row .chat-icon{flex:none;width:14px;height:14px;color:var(--accent)}.chat-workspace .tray-more-row .tray-chip-label{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}`
