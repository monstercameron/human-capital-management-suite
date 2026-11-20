package chatui

import (
	"net/url"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// chat3759AddAgentLink is the Add-an-agent action of the conversation details
// Members section (ISSUE-3759). It is drawn only for a conversation with no
// agent yet, to a viewer who may place one. It deliberately carries no
// chat-agents-here id: an empty conversation must not show the agents list
// (chatux005_test, agentux064_test).
func chat3759AddAgentLink(m Model) ui.Node {
	return html.A(html.Props{Class: "details-agents-manage", Href: "/workspace/app/admin/personas?locale=" + url.QueryEscape(m.Locale), Text: mentionMenuText(m, KeyMentionAddAgent)})
}

// chat3759MayPlaceAgents reports whether the viewer may place agents in the
// conversation: its owner or a tenant administrator, the same people the
// details panel already offers Manage in Agent setup.
func chat3759MayPlaceAgents(m Model, c Conversation) bool {
	return m.IsTenantAdmin || (m.CurrentUser != "" && c.OwnerID == m.CurrentUser)
}

// chat3759MentionAddAgent is the Add-an-agent row of the @ menu. It is drawn
// only once the agent lookup has answered with no agent for this conversation
// and only to a viewer who may place one, so the menu never hints at agents the
// viewer cannot see (ISSUE-3759).
func chat3759MentionAddAgent(m Model, target string, lookup PersonaLookupState) ui.Node {
	c := m.selected()
	if lookup != PersonaLookupReady || len(chatConversationAgents(m)) > 0 || c.Kind == DirectMessage ||
		!m.personaMentionsEnabled(target) || !chat3759MayPlaceAgents(m, c) {
		return nil
	}
	// The menu's own action-row look (full width, accent colour, no underline).
	return html.A(html.Props{Class: "mention-action-row mention-add-agent", Href: "/workspace/app/admin/personas?locale=" + url.QueryEscape(m.Locale), Text: mentionMenuText(m, KeyMentionAddAgent)})
}
