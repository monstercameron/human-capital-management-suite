package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// channelTray shows a channel's to-do list and poll inside the chat, under
// the header: a slim bar of summary chips that is always visible once either
// exists, and the opened widget as a card above the timeline. Nothing here
// needs the details pane.
func channelTray(m Model, h handlers, open string) ui.Node {
	c := m.selected()
	if m.SelectedID == "" || (c.Kind != PublicChannel && c.Kind != PrivateChannel) {
		return html.Div(html.Props{Class: "channel-tray-slot"})
	}
	done, total := 0, len(m.ChannelTodo.Items)
	for _, item := range m.ChannelTodo.Items {
		if item.Completed {
			done++
		}
	}
	chips := []ui.Node{}
	if total > 0 || open == "todo" {
		label := m.t(KeyTodoTitle)
		if total > 0 {
			label += " · " + m.tf(KeyTodoProgress, map[string]string{"done": m.nz(done), "total": m.nz(total)})
		}
		chips = append(chips, trayChip(m, "todo", open, "checklist", label))
	}
	if m.ChannelPoll.Question != "" || open == "poll" {
		label := m.t(KeyPollTitle)
		if q := m.ChannelPoll.Question; q != "" {
			votes := m.tf(KeyPollVotes, map[string]string{"n": m.nz(m.ChannelPoll.TotalVotes)})
			if m.ChannelPoll.TotalVotes == 1 {
				votes = m.tf(KeyPollVoteOne, map[string]string{"n": m.nz(1)})
			}
			label = excerpt(q, 60) + " · " + votes
		}
		chips = append(chips, trayChip(m, "poll", open, "poll", label))
	}
	if len(chips) == 0 {
		return html.Div(html.Props{Class: "channel-tray-slot"})
	}
	children := []ui.Node{html.Div(html.Props{Class: "channel-tray-bar", Role: "toolbar", Aria: map[string]string{"label": m.t(KeyChannelTools)}}, chips...)}
	if open != "" {
		var body ui.Node
		title := m.t(KeyTodoTitle)
		if open == "poll" {
			title = m.t(KeyPollTitle)
			body = channelPollSection(m, h)
		} else {
			body = channelTodoSection(m, h)
		}
		children = append(children, html.Section(html.Props{Class: "channel-tray-card", Data: map[string]string{"tray": open}, Aria: map[string]string{"label": title}},
			html.Div(html.Props{Class: "channel-tray-head"}, html.H2(html.Props{Text: title}), actionButton("icon-button", "tray-close", "", m.t(KeyClose), false, icon("close"))),
			body))
	}
	return html.Div(html.Props{Class: "channel-tray", Data: map[string]string{"open": open}}, children...)
}

func trayChip(m Model, which, open, glyph, label string) ui.Node {
	class := "tray-chip"
	if open == which {
		class += " active"
	}
	return html.Button(html.Props{Class: class, Type: "button", Data: map[string]string{"action": "tray-" + which},
		Aria: map[string]string{"expanded": boolString(open == which)}}, icon(glyph), html.Span(html.Props{Class: "tray-chip-label", Text: label}))
}

// nz formats a count that may legitimately be zero. itoa, and the client's
// Number callback built on it, render zero as "", which left "of 1 done".
func (m Model) nz(v int) string {
	if s := m.n(v); s != "" {
		return s
	}
	return "0"
}

// todoRuleDisclosure folds a task's completion rule into one line ("Who can
// complete: Everyone") that opens to the full controls, instead of a labelled
// select under every task.
func todoRuleDisclosure(m Model, h handlers, item ChannelTodoItem) ui.Node {
	mode := item.CompletionMode
	if mode == "" {
		mode = "EVERYONE"
	}
	if !item.CanManageCompletionPolicy {
		return todoPolicyControls(m, h, item)
	}
	return html.Details(html.Props{Class: "channel-todo-rule"},
		html.Summary(html.Props{Text: m.t(KeyTodoModeLabel) + ": " + m.t(todoModeKey(mode))}),
		todoPolicyControls(m, h, item))
}
