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
