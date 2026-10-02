package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func integrate1MessageAvatar(m Model, msg Message, class string) ui.Node {
	if msg.PersonaActor != nil && msg.PersonaActor.valid() {
		value := msg.PersonaActor.Icon
		conversation := m.selected()
		if !value.Valid() && conversation.Agent && conversation.AgentID == msg.PersonaActor.AgentID {
			value = conversation.Icon
		}
		for _, persona := range m.ResolvedPersonaMentions {
			if !value.Valid() && persona.Reference.ID == msg.PersonaActor.PersonaID {
				value = persona.Icon
			}
		}
		if !value.Valid() {
			value = storedAgentIcon(m, []string{msg.PersonaActor.PersonaID, msg.PersonaActor.AgentID}, msg.Author)
		}
		if !agentIdentityKnown(m, msg.PersonaActor) {
			// CHATBUG-088: an agent the model cannot name yet is drawn as an empty
			// slot, not with a picture made up from its identifier.
			return agentDMAvatar(msg.Author, class+" agent-dm-avatar")
		}
		return agentDMAvatar(msg.Author, class+" agent-dm-avatar", agentIconFor(m, []string{msg.PersonaActor.AgentID, msg.PersonaActor.PersonaID}, msg.Author, value))
	}
	return personButton(m, msg.AuthorID, msg.Author, "person-avatar-button", personAvatar(m, msg.AuthorID, msg.Author, class))
}

func integrate1InlineWidgets(m Model) ui.Node {
	// CHATBUG-082: not here while Conversation details says it.
	if m.ChannelWidgetsError != "" && !chatbug082DetailsSaysFailure(m) {
		return html.Div(html.Props{Class: "channel-tray"}, html.Div(html.Props{Class: "widget-inline-notice", Role: "alert"}, html.P(html.Props{Text: modAuthorErrorOr(m, m.ChannelWidgetsError, m.t(KeyWidgetError))}), actionButton("button secondary small", "widget-retry", "", m.t(KeyRetry), m.Callbacks.RetryChannelWidgets == nil, ui.Text(m.t(KeyRetry)))))
	}
	return inlineChannelWidgets(m)
}
