package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// TestTodo_AGENTP_019_Browser draws the mention menu as a member sees it in a
// channel that has two installed agents and a person who shares a name with
// one of them: people and agents are separate groups, an agent row opens a
// profile card from its own button, the card says what the agent does, what
// data it can reach and who owns it, and an agent the server did not list for
// this member is in no part of the markup.
func TestTodo_AGENTP_019_Browser(t *testing.T) {
	model := Model{
		SelectedID: "room", Conversations: []Conversation{{ID: "room", Name: "people-ops", Kind: PublicChannel, Joined: true}},
		Members: []Member{{ID: "person-17", Name: "Policy Helen"}},
		ResolvedPersonaMentions: []ResolvedPersonaMention{
			{Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "t1", ID: "policy-helper", Display: "Policy Helper", ConversationID: "room"}, Handle: "policy-helper",
				Purpose: "Answer policy questions", Owner: "People Operations", Version: "v3", Skills: []PersonaMentionSkill{{Name: "Read policy", Tier: "T0"}},
				DataClasses: []string{"Policy data"}, CannotDo: []string{"Change records"}, ReplyPlacement: PersonaReplyInThread},
			resolvedPersonaMention("t1", "leave-helper", "Leave Helper", "room"),
		},
		PersonaLookup: PersonaLookupReady, PersonaLookupConversationID: "room",
		Callbacks: Callbacks{SendMessageWithReferences: func(string, string, []ChatReference) {}},
	}
	closed := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Query: "pol", Open: true}, "chat-composer"))
	headings := chatPolishNodes(t, closed, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "mention-heading") })
	if len(headings) != 2 {
		t.Fatalf("the menu has %d group headings, want People and Agents: %s", len(headings), closed)
	}
	for _, hidden := range []string{"Comp Analyst", "Payroll Agent", "Uninstalled Agent"} {
		if strings.Contains(closed, hidden) {
			t.Errorf("an agent this member may not invoke (%s) is in the menu: %s", hidden, closed)
		}
	}
	if strings.Contains(closed, "mention-profile-card") {
		t.Errorf("the profile card is open before it was asked for: %s", closed)
	}
	info := chatPolishNodes(t, closed, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "mention-agent-info") })
	if len(info) != 1 || chatPolishAttr(info[0], "aria-expanded") != "false" || chatPolishAttr(info[0], "data-action") != "mention-details" || !strings.Contains(chatPolishAttr(info[0], "aria-label"), "Policy Helper") {
		t.Fatalf("the agent row has no button that names and opens its details: %s", closed)
	}

	open := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Query: "pol", Open: true, Active: 0, Details: true}, "chat-composer"))
	card := chatPolishNodes(t, open, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "mention-profile-card") })
	if len(card) != 1 || chatPolishAttr(card[0], "role") != "region" || chatPolishAttr(card[0], "aria-label") != "Agent details" {
		t.Fatalf("the open card is not one labelled region: %s", open)
	}
	for _, want := range []string{"Purpose: ", "Answer policy questions", "Owner: ", "People Operations", "Version: ", "v3", "Data this agent can reach", "Policy data", "Acts with your current access.", "What it cannot do", "Change records", "Replies go: ", "in this thread"} {
		if !strings.Contains(open, want) {
			t.Errorf("the profile card lacks %q: %s", want, open)
		}
	}
}
