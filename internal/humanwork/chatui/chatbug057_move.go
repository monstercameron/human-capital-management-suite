package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// A channel's standing poll comes from before polls were messages: it is not
// in the conversation and a channel holds one. Polls have one model now, the
// card. A standing poll that still exists is moved into the conversation by a
// person who may end it (the people offered "Close poll"): its question and
// options become the draft of a card in the composer, that person looks at the
// preview and posts it in their own name, and the post closes the standing
// poll. Nothing is posted for anybody without their press. Its votes are not
// carried over, because a vote belongs to the person who cast it; the preview
// says so before the post.
//
// The channel's to-do list is not moved: it holds what a card does not (a rule
// per task for who may complete it, a pinned source message), and it stays the
// channel's standing list.

// chatbug057CanMove reports whether the viewer is offered the move: there is a
// standing poll, they may end it, and a card can be posted here.
func chatbug057CanMove(m Model) bool {
	return m.ChannelPoll.Question != "" && m.ChannelTeam.CanPin && m.Chatcmd002.CloseChannelPoll != nil && !m.ChannelPollPending && !m.ChannelPollLoading && m.ChannelPollError == "" && chatcmd003Available(m)
}

// chatbug057MoveLine is the standing poll written as the /poll line that makes
// the same card: the question and each option quoted, so nothing in them is
// read as a separator or a setting.
func chatbug057MoveLine(poll ChannelPoll) string {
	arguments := chat.Chatcmd003Arguments{Text: poll.Question, Explicit: true}
	for _, option := range poll.Options {
		arguments.Items = append(arguments.Items, option.Text)
	}
	return chat.Chatcmd001RenderLine("poll", arguments)
}

// chatbug057MoveNote is the line a poll preview carries while the channel has
// a standing poll: the offer to move it here, or, once it is the draft, what
// posting it does.
func chatbug057MoveNote(m Model, state chatcmd003Preview) ui.Node {
	if state.Draft.Card.Kind != "poll" || !chatbug057CanMove(m) {
		return nil
	}
	if state.Standing {
		return html.P(html.Props{ID: "chatbug057-standing", Class: "chatcmd003-note", Role: "status", Dir: "auto",
			Text: chatcmd002Fill(chatcmd003Text(m, "standing-closes"), "n", m.nz(m.ChannelPoll.TotalVotes))})
	}
	return html.Div(html.Props{Class: "chatcmd003-row"},
		html.P(html.Props{Class: "chatcmd003-note", Dir: "auto", Text: chatcmd002Fill(chatcmd003Text(m, "standing-move-note"), "question", excerpt(m.ChannelPoll.Question, 60))}),
		html.Button(html.Props{Class: "button secondary small", Type: "button", Disabled: state.Busy, Data: map[string]string{"action": "chatbug057-move", "id": state.Target}, Text: chatcmd003Text(m, "standing-move")}))
}
