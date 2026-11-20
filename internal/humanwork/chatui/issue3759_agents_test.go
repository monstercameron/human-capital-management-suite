package chatui

import (
	"strings"
	"testing"
)

func issue3759Model(locale string) Model {
	return Model{Locale: locale, SelectedID: "room", CurrentUser: "owner", CurrentTenantID: "t1",
		Conversations:               []Conversation{{ID: "room", Kind: PublicChannel, OwnerID: "owner"}},
		PersonaLookup:               PersonaLookupReady,
		PersonaLookupConversationID: "room",
		Callbacks:                   Callbacks{SendMessageWithReferences: func(string, string, []ChatReference) {}},
	}
}

func issue3759Menu(t *testing.T, m Model) string {
	t.Helper()
	return agentUX029Menu(t, m, "@")
}

func TestIssue3759_MentionMenuEmptyOffersAddAgentToOwner(t *testing.T) {
	for locale, want := range map[string]string{
		"en-US": "Add an agent to this conversation",
		"de-DE": "Agent zu dieser Unterhaltung hinzufügen",
		"ar":    "إضافة وكيل إلى هذه المحادثة",
	} {
		menu := issue3759Menu(t, issue3759Model(locale))
		if !strings.Contains(menu, want) || !strings.Contains(menu, `href="/workspace/app/admin/personas?locale=`) {
			t.Fatalf("%s: empty @ menu does not offer the add action to the owner: %s", locale, menu)
		}
	}
	admin := issue3759Model("en-US")
	admin.CurrentUser, admin.IsTenantAdmin = "someone", true
	if menu := issue3759Menu(t, admin); !strings.Contains(menu, "Add an agent to this conversation") {
		t.Fatalf("tenant admin is not offered the add action: %s", menu)
	}
}

func TestIssue3759_MentionMenuAddAgentNotForOthers(t *testing.T) {
	other := issue3759Model("en-US")
	other.CurrentUser = "member"
	if menu := issue3759Menu(t, other); strings.Contains(menu, "Add an agent") {
		t.Fatalf("a plain member is offered the add action: %s", menu)
	}
	// Not offered while the lookup is unsettled or when agents exist.
	loading := issue3759Model("en-US")
	loading.PersonaLookup = PersonaLookupLoading
	if menu := issue3759Menu(t, loading); strings.Contains(menu, "Add an agent") {
		t.Fatalf("add action shown while agents load: %s", menu)
	}
}

func TestIssue3759_DetailsAddAgentWithNoAgents(t *testing.T) {
	m := issue3759Model("en-US")
	c := m.selected()
	html := renderNode(t, chatux005Members(m, handlers{}, c))
	if !strings.Contains(html, "Add an agent to this conversation") || strings.Contains(html, `id="chat-agents-here"`) {
		t.Fatalf("owner sees no add action (or an empty agents list) in details: %s", html)
	}
	m.CurrentUser = "member"
	if html := renderNode(t, chatux005Members(m, handlers{}, m.selected())); strings.Contains(html, "Add an agent") {
		t.Fatalf("plain member sees the add action in details: %s", html)
	}
}

func TestIssue3759_AddAgentNotOfferedInDirectMessage(t *testing.T) {
	m := issue3759Model("en-US")
	m.Conversations = []Conversation{{ID: "room", Kind: DirectMessage, OwnerID: "owner"}}
	if menu := issue3759Menu(t, m); strings.Contains(menu, "Add an agent") {
		t.Fatalf("a direct message offers the add action in the @ menu: %s", menu)
	}
	if out := renderNode(t, chatux005Members(m, handlers{}, m.selected())); strings.Contains(out, "Add an agent") {
		t.Fatalf("a direct message offers the add action in details: %s", out)
	}
}

func TestIssue3759_MentionAddAgentIsAnActionRow(t *testing.T) {
	menu := issue3759Menu(t, issue3759Model("en-US"))
	if !strings.Contains(menu, `class="mention-action-row mention-add-agent"`) {
		t.Fatalf("the add action does not use the menu action-row style: %s", menu)
	}
}
