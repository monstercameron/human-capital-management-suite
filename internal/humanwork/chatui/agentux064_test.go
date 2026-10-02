package chatui

import (
	"strings"
	"testing"
)

// agentUX064Model is the general channel with two agents the viewer may use in
// it: Policy Helper reads the documents placed in the channel, Assistant reads
// workspace documents. The details panel is open.
func agentUX064Model(locale string) Model {
	m := chat4Fixture(locale, "sent", false)
	m.ShowDetails = true
	m.ResolvedPersonaMentions[0].DocumentScope = "CHANNEL_DOCUMENTS"
	second := m.ResolvedPersonaMentions[0]
	second.Reference.ID, second.Reference.Display = "assistant", "Assistant"
	second.Handle, second.Purpose, second.DocumentScope = "assistant", "Answers everyday questions", "WORKSPACE_DOCUMENTS"
	m.ResolvedPersonaMentions = append(m.ResolvedPersonaMentions, second)
	return m
}

// The conversation says which agents are in it before anyone types "@": the
// header counts them and opens the list, and the details panel names each agent
// with its purpose and what it reads there, with "Ask" for each. An owner is
// offered Agent setup.
func TestTodo_AGENTUX_064(t *testing.T) {
	m := agentUX064Model("en-US")
	m.Conversations[0].OwnerID = m.CurrentUser
	page := render(t, m)
	if !strings.Contains(page, "2 agents") || !strings.Contains(page, `data-action="agents-here"`) {
		t.Fatalf("the header does not count the agents beside the member count: %s", page)
	}
	for _, want := range []string{`id="chat-agents-here"`, "Policy Helper", "Answer policy questions", "Reads documents in this channel", "Assistant", "Answers everyday questions", "Reads workspace documents", "Manage in Agent setup"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the details panel lacks %q: %s", want, page)
		}
	}
	if strings.Count(page, `data-action="agent-ask-here"`) != 2 || strings.Count(page, "agent-badge") < 2 {
		t.Fatalf("each agent needs an Ask control and the Agent badge: %s", page)
	}
	// "Ask" puts that agent's mention in the composer.
	store := mentionStore{box: &mentionBox{}}
	selectChatAgent(m, store, "assistant")
	if got := store.PersonaReferences("chat-composer", m.SelectedID, ""); len(got) != 1 || got[0].ID != "assistant" {
		t.Fatalf("Ask did not start a mention of Assistant: %+v", got)
	}
	// One agent is "1 agent"; none shows neither a count nor a list.
	m.ResolvedPersonaMentions = m.ResolvedPersonaMentions[:1]
	if one := render(t, m); !strings.Contains(one, "1 agent") || strings.Contains(one, "2 agents") {
		t.Fatalf("one agent is counted wrongly: %s", one)
	}
	m.ResolvedPersonaMentions = nil
	if none := render(t, m); strings.Contains(none, `data-action="agents-here"`) || strings.Contains(none, `id="chat-agents-here"`) {
		t.Fatalf("a conversation with no agent shows an agent count or list: %s", none)
	}
}

// A viewer sees only the agents the server resolved for this person in this
// conversation. An entry for another tenant or another conversation, or a
// repeat, is not listed; only an owner or administrator is offered "Manage in
// Agent setup"; and Ask is not live for a viewer who cannot post.
func TestTodo_AGENTUX_064_Security(t *testing.T) {
	m := agentUX064Model("en-US")
	foreign := m.ResolvedPersonaMentions[0]
	foreign.Reference.TenantID, foreign.Reference.Display = "other-tenant", "Payroll Agent"
	elsewhere := m.ResolvedPersonaMentions[0]
	elsewhere.Reference.ConversationID, elsewhere.Reference.Display, elsewhere.Reference.ID = "other-room", "HR Agent", "hr-agent"
	repeat := m.ResolvedPersonaMentions[0]
	m.ResolvedPersonaMentions = append(m.ResolvedPersonaMentions, foreign, elsewhere, repeat)
	m.Conversations[0].OwnerID = "someone-else"
	m.IsTenantAdmin = false
	page := render(t, m)
	for _, leaked := range []string{"Payroll Agent", "HR Agent"} {
		if strings.Contains(page, leaked) {
			t.Fatalf("the page lists %q, an agent this viewer may not use here", leaked)
		}
	}
	if strings.Count(page, `data-action="agent-ask-here"`) != 2 || !strings.Contains(page, "2 agents") {
		t.Fatalf("a repeated or foreign entry changed the list or the count: %s", page)
	}
	if strings.Contains(page, "Manage in Agent setup") || strings.Contains(page, "/admin/personas") {
		t.Fatalf("a viewer who owns nothing is offered Agent setup: %s", page)
	}
	m.Callbacks.SendMessageWithReferences = nil
	if ask := render(t, m); strings.Contains(ask, `data-action="agent-ask-here"`) && !strings.Contains(ask, "disabled") {
		t.Fatalf("Ask is live for a viewer who cannot post: %s", ask)
	}
	m.IsTenantAdmin = true
	if admin := render(t, m); !strings.Contains(admin, "Manage in Agent setup") {
		t.Fatalf("an administrator is not offered Agent setup: %s", admin)
	}
}

// At every width and in every language the header count and the list render,
// and the list keeps its names, purposes, read scopes and Ask controls.
func TestTodo_AGENTUX_064_Browser(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		m := agentUX064Model(locale)
		m.Conversations[0].OwnerID = m.CurrentUser
		header := renderAgentUXChat3Node(t, timeline(m, handlers{}), width)
		chat4Require(t, header, agentCountLabel(m, 2), `data-action="agents-here"`)
		page := render(t, m)
		chat4Require(t, page, `id="chat-agents-here"`, "Policy Helper", "Assistant", chat5Text(m, "chat.agents.reads_channel"), chat5Text(m, "chat.agents.reads_workspace"), chat5Text(m, "chat.agents.manage"), `data-action="agent-ask-here"`)
		if strings.Count(page, `id="chat-agents-here"`) != 1 {
			t.Fatal("the agents heading id is not unique")
		}
	})
}
