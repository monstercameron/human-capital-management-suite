package chatui

import (
	"strings"
	"testing"
)

// ISSUE-3759: the details Members section offers the add action in a
// conversation with no agent, linked to the page where agents are placed.
func TestChat3759_DetailsAddAgentLink(t *testing.T) {
	m := Model{Locale: "en-US"}
	link := renderNode(t, chat3759AddAgentLink(m))
	if !strings.Contains(link, "Add an agent to this conversation") || !strings.Contains(link, `href="/workspace/app/admin/personas?locale=en-US"`) {
		t.Fatalf("details add-agent link is wrong: %s", link)
	}
	if strings.Contains(link, `id="chat-agents-here"`) {
		t.Fatalf("details add-agent link carries the agents list id: %s", link)
	}
}
