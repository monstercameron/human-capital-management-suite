package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-016: the header of a conversation with an agent put the agent's
// description and "Only you can see this conversation" on one line, and the
// line was cut in the middle of the second, which is the part that matters.
// The header now says who can see the conversation in a short mark beside the
// agent's name, which is never cut, and keeps the description on one line of
// its own that may be: the full text is in Conversation details.

// chatux016AgentHeading is the heading block of a direct conversation with an
// agent: the name, the Agent badge and the privacy mark on one line, and the
// agent's own description under it.
func chatux016AgentHeading(m Model, c Conversation, title string) ui.Node {
	mark := chatux016Text(m.Locale, "private")
	full := agentReplyFallback(m.Locale, "chat.agent.private_note", "Only you can see this conversation")
	children := []ui.Node{html.Div(html.Props{Class: "agent-identity-line"},
		html.H1(html.Props{Text: title}), AgentBadgeLabel(m.Locale),
		html.Span(html.Props{Class: "agent-header-private", Title: full}, icon("lock"), html.Span(html.Props{Text: mark}), html.Span(html.Props{Class: "sr-only", Text: " (" + full + ")"})))}
	if purpose := agentConversationPurpose(m, c); purpose != "" {
		children = append(children, html.P(html.Props{Class: "conversation-topic agent-header-purpose", Dir: "auto", Title: purpose, Text: purpose}))
	}
	return html.Div(html.Props{Class: "conversation-heading"}, children...)
}

func chatux016Text(locale, key string) string {
	copy := map[string][3]string{
		"private": {"Private to you", "Privat für Sie", "خاص بك"},
	}
	return chatbug039Text(key, copy[key][chatbug039LocaleIndex(locale)], copy[key][0])
}

// chatUX016Styles keeps the privacy mark whole at every width and lets the
// name, then the description, give way.
const chatUX016Styles = `.agent-identity-line{max-width:100%}` +
	`.agent-identity-line h1{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}` +
	`.agent-header-private{display:inline-flex;align-items:center;gap:4px;flex:none;white-space:nowrap;color:var(--muted);font-size:.75rem;font-weight:400}` +
	`.agent-header-private .chat-icon{width:13px;height:13px;flex:none}` +
	`.conversation-topic.agent-header-purpose{display:block;max-width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}`
