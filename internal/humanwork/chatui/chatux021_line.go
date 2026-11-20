package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// chatux021MembershipLine draws the system line posted when somebody adds a
// person to the conversation, "Walt Brennan added Loretta Haynes" (CHATUX-021).
// The post's body names the added person's identifier; the sentence is written
// here, from names the page already holds, in the reader's language. It is a
// line in the timeline, not a message: no avatar, no actions.
func chatux021MembershipLine(m Model, msg Message) (ui.Node, bool) {
	line, ok := chatux021LineText(m, msg)
	if !ok {
		return nil, false
	}
	children := []ui.Node{html.Span(html.Props{Class: "chatux021-line-text", Dir: "auto", Text: line})}
	if link, ok := issue3728LineLink(m, msg); ok {
		children = append(children, link)
	}
	if msg.TimeLabel != "" {
		children = append(children, html.Time(html.Props{Class: "message-time", Text: msg.TimeLabel}))
	}
	return html.Div(html.Props{Class: "message-row chatux021-system-line", Data: map[string]string{"message-id": msg.ID}}, children...), true
}

// chatux021IsLine reports a membership system line. A line never groups with
// the messages around it: the next message by the same person still shows who
// wrote it.
func chatux021IsLine(msg Message) bool {
	if _, card := chat.ParseClockCard(msg.Body); card {
		return true
	}
	if IsFormCardBody(msg.Body) {
		return true
	}
	if _, stage := chat.ParseJourneyStage(msg.Body); stage {
		return true
	}
	_, ok := chat.ParseMembershipAdded(msg.Body)
	return ok
}

// chatux021LineText is the sentence a membership system line stands for, in
// the reader's language, or false for any other message. Every surface that
// shows a message as text (the Saved panel, a preview, a notice) uses it, so the
// body's marker and the added person's identifier are never printed.
func chatux021LineText(m Model, msg Message) (string, bool) {
	subject, ok := chat.ParseMembershipAdded(msg.Body)
	if !ok {
		if text, card := issue3808CardText(m, msg); card {
			return text, true
		}
		if text, stage := issue3728LineText(m, msg); stage {
			return text, true
		}
		return issue3795CardText(m, msg)
	}
	someone := laneText(m, chatux021Copy, keyChatux021SomeoneElse)
	actor := strings.TrimSpace(msg.Author)
	if actor == "" || actor == msg.AuthorID {
		actor = chatKnownName(m, msg.AuthorID)
	}
	if actor == "" {
		actor = someone
	}
	name := chatKnownName(m, subject)
	if name == "" {
		name = someone
	}
	return laneTextf(m, chatux021Copy, keyChatux021MemberAdded, map[string]string{"actor": actor, "name": name}), true
}
