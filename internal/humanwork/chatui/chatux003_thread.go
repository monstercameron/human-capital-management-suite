package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// chatux003ThreadAgentReply is an agent's answer in the thread pane, where an
// answer posted to the channel is read (it is a reply under the question). It is
// the same message the channel shows for an agent: the agent's name and badge
// with who asked, the answer, the sources as chips, and the rating controls for
// the person who asked. It reports false for a message that is not an agent's.
func chatux003ThreadAgentReply(m Model, h handlers, msg Message) ([]ui.Node, bool) {
	if !msg.PersonaActor.valid() || m.selected().Agent {
		return nil, false
	}
	envelope := parseAgentReplyEnvelope(msg.Body)
	envelope.Body = agentAnswerPresentBody(readerReplyBody(m, msg, envelope.Body), envelope.Sources)
	content := []ui.Node{
		html.Div(html.Props{Class: "message-meta"}, html.Strong(html.Props{Class: "message-author", Dir: "auto", Text: msg.Author}), chatux003MessageBadge(m, msg), html.Time(html.Props{Class: "message-time", Text: msg.TimeLabel})),
		html.Div(chatlangBodyProps(m, msg, html.Props{Class: "message-body", Dir: "auto"}), markdownMessageBody(m, envelope.Body)...),
	}
	content = append(content, renderAgentReplySources(m, envelope)...)
	if feedback := renderAgentFeedback(m, h.local, msg.ID); feedback != nil {
		content = append(content, feedback)
	}
	content = append(content, chatlangExtras(m, h, msg)...)
	content = append(content, integrate2MessageLocations(m, msg)...)
	// The Sources row is the reference for an agent's documents: no second
	// preview card for a title the answer links.
	embedBody := agentAnswerPreviewBody(envelope.Body, envelope.Sources)
	content = append(content, linkEmbeds(m, embedBody)...)
	content = append(content, docPreviewEmbeds(m, embedBody)...)
	content = append(content, projectPreviewEmbeds(m, embedBody)...)
	content = append(content, journeyPreviewEmbeds(m, embedBody)...)
	content = append(content, threadMessageMenu(m, msg)...)
	return content, true
}
