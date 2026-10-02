package chatui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

func chatbug033Model() Model {
	policy := AgentIconFixture(agenticon.Input{Name: "Policy Helper"})
	assistant := AgentIconFixture(agenticon.Input{Name: "Assistant"})
	return Model{
		Locale: "en-US", State: StateReady, CurrentUser: "walt", CurrentTenantID: "tenant", SelectedID: "general",
		Conversations: []Conversation{
			{ID: "general", Name: "general", Kind: PublicChannel},
			{ID: "dm-policy", Name: "Policy Helper", Kind: DirectMessage, Agent: true, AgentID: "policy-helper", Icon: policy, IconRevision: 1, AgentPurpose: "Answers questions about company policies from the documents it may read."},
			{ID: "dm-assistant", Name: "Assistant", Kind: DirectMessage, Agent: true, AgentID: "assistant", Icon: assistant, IconRevision: 1, AgentPurpose: "Answers everyday questions and writes announcements from the documents you give it."},
		},
		Callbacks: Callbacks{SelectConversation: func(string) {}},
	}
}

// chatbug033Row is the markup of one sidebar row.
func chatbug033Row(t *testing.T, markup, id string) string {
	t.Helper()
	start := strings.Index(markup, `data-conversation-id="`+id+`"`)
	if start < 0 {
		t.Fatalf("no sidebar row for %s", id)
	}
	rest := markup[start:]
	// The row proper ends where its "more" menu button begins.
	if end := strings.Index(rest, `class="rail-row-more"`); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// TestTodo_CHATBUG_033 is the one lookup: an agent wears its own stored icon in
// the sidebar row, the header, the details panel's member list and beside its
// messages, from the first paint and with another conversation open; an agent
// with no stored icon wears a fallback of its own that no other agent has; and
// the header line is the agent's own description.
func TestTodo_CHATBUG_033(t *testing.T) {
	m := chatbug033Model()
	policy, assistant := m.Conversations[1].Icon, m.Conversations[2].Icon
	if policy == assistant {
		t.Fatal("the two fixture agents must have different icons")
	}
	policySVG, assistantSVG := renderNode(t, agenticon.Node(policy)), renderNode(t, agenticon.Node(assistant))
	neutralSVG := renderNode(t, agenticon.Node(agenticon.Value{}))

	// Another conversation is open and the directory of that conversation knows
	// no agents: the rows still draw the icons they hold.
	markup := render(t, m)
	if !strings.Contains(chatbug033Row(t, markup, "dm-policy"), policySVG) || !strings.Contains(chatbug033Row(t, markup, "dm-assistant"), assistantSVG) {
		t.Fatalf("a sidebar row does not draw its agent's own icon: %s", markup)
	}
	if strings.Contains(markup, neutralSVG) {
		t.Fatal("the shared neutral glyph is drawn")
	}

	// The agent's lookup survives a model that carries no icon on the row: the
	// directory of the open conversation is one source, the members another.
	bare := m
	bare.Conversations = append([]Conversation(nil), m.Conversations...)
	bare.Conversations[1].Icon, bare.Conversations[2].Icon = agenticon.Value{}, agenticon.Value{}
	bare.ResolvedPersonaMentions = []ResolvedPersonaMention{{Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "policy-helper", Display: "Policy Helper", ConversationID: "general"}, Icon: policy, Purpose: "Answers questions about company policies from the documents it may read."}}
	found := render(t, bare)
	if !strings.Contains(chatbug033Row(t, found, "dm-policy"), policySVG) {
		t.Fatal("the row did not find its agent's icon in the directory")
	}

	// An agent with no stored icon at all draws its own fallback: a real glyph, not
	// the neutral one, and different for each agent.
	none := m
	none.Conversations = append([]Conversation(nil), m.Conversations...)
	none.Conversations[1].Icon, none.Conversations[2].Icon = agenticon.Value{}, agenticon.Value{}
	fallbacks := render(t, none)
	rowOne, rowTwo := chatbug033Row(t, fallbacks, "dm-policy"), chatbug033Row(t, fallbacks, "dm-assistant")
	svg := regexp.MustCompile(`<svg[^>]*class="agent-icon".*?</svg>`)
	one, two := svg.FindString(rowOne), svg.FindString(rowTwo)
	if one == "" || two == "" || one == two || strings.Contains(fallbacks, neutralSVG) {
		t.Fatalf("fallbacks are missing, shared or neutral:\n%s\n%s", one, two)
	}
	want := agenticon.Fallbacks([]string{"policy-helper", "assistant"})
	if one != renderNode(t, agenticon.Node(want["policy-helper"])) || two != renderNode(t, agenticon.Node(want["assistant"])) {
		t.Fatal("a fallback is not derived from the agent's id through the generator")
	}

	// While the server's agent list is still being read, nothing is drawn for an
	// agent whose icon is not known: not a letter, not the shared glyph.
	pending := none
	pending.AgentRailPending = true
	waiting := render(t, pending)
	waitingRow := chatbug033Row(t, waiting, "dm-policy")
	if !strings.Contains(waitingRow, "agent-icon-pending") || strings.Contains(waitingRow, "<svg") || strings.Contains(waitingRow, ">PH<") || !strings.Contains(waitingRow, "Policy Helper") || !strings.Contains(waitingRow, "Agent") {
		t.Fatalf("a row waiting for its icon must keep its name and badge and draw an empty slot: %s", waitingRow)
	}

	// The header line is the agent's own description, never another agent's.
	for _, tc := range []struct{ id, own, other string }{
		{"dm-assistant", "Answers everyday questions and writes announcements from the documents you give it.", "Answers from policy documents"},
		{"dm-policy", "Answers questions about company policies from the documents it may read.", "writes announcements"},
	} {
		open := m
		open.SelectedID = tc.id
		header := render(t, open)
		if !strings.Contains(header, `class="conversation-topic agent-header-purpose"`) || !strings.Contains(header, tc.own) || !strings.Contains(header, "Only you can see this conversation") || strings.Contains(header, tc.other) {
			t.Fatalf("%s: the header line is not the agent's own description: %s", tc.id, header)
		}
	}
	// Until the description is known the line says only what is true of every
	// one of these conversations.
	blank := m
	blank.Conversations = append([]Conversation(nil), m.Conversations...)
	blank.Conversations[2].AgentPurpose = ""
	blank.SelectedID = "dm-assistant"
	if header := render(t, blank); strings.Contains(header, "Answers from policy documents") || !strings.Contains(header, "Only you can see this conversation") {
		t.Fatalf("a conversation with no description borrowed one: %s", header)
	}
}

// TestTodo_CHATBUG_033_Browser draws the whole conversation page of a direct
// conversation with an agent: the sidebar rows of both agents (name, Agent
// badge, own icon), the header, a message the agent wrote, and the member list.
func TestTodo_CHATBUG_033_Browser(t *testing.T) {
	m := chatbug033Model()
	m.SelectedID = "dm-assistant"
	m.ShowDetails = true
	m.Members = []Member{{ID: "walt", Name: "Walt Brennan"}, {ID: "assistant", Name: "Assistant", Agent: true}}
	m.Messages = []Message{{ID: "reply", AuthorID: "assistant", Author: "Assistant", Body: "Open enrollment runs November 2 to 20.", TimeLabel: "9:32", PersonaActor: &PersonaActor{PersonaID: "assistant", AgentID: "assistant", Trusted: true}}}
	policySVG, assistantSVG := renderNode(t, agenticon.Node(m.Conversations[1].Icon)), renderNode(t, agenticon.Node(m.Conversations[2].Icon))
	markup := render(t, m)
	for _, id := range []string{"dm-policy", "dm-assistant"} {
		row := chatbug033Row(t, markup, id)
		name := map[string]string{"dm-policy": "Policy Helper", "dm-assistant": "Assistant"}[id]
		icon := map[string]string{"dm-policy": policySVG, "dm-assistant": assistantSVG}[id]
		if !strings.Contains(row, ">"+name+"<") || !strings.Contains(row, `class="agent-badge agent-badge-label"`) || !strings.Contains(row, icon) || strings.Contains(row, "Conversation") || strings.Contains(row, "agent-icon-pending") {
			t.Fatalf("the %s row is not name, badge and its own icon on first paint: %s", id, row)
		}
	}
	// The Assistant's header, its message and the member list all draw the
	// Assistant's icon, and the Policy Helper's icon appears only on its own row.
	if got := strings.Count(markup, assistantSVG); got < 4 {
		t.Fatalf("the Assistant's icon should appear on its row, in the header, beside its message and in the member list: %d", got)
	}
	if got := strings.Count(markup, policySVG); got != 1 {
		t.Fatalf("the Policy Helper's icon is drawn %d times, want once on its own row", got)
	}
	if strings.Contains(markup, "Answers from policy documents") {
		t.Fatal("the Assistant's header carries the Policy Helper's line")
	}
	// A row that has not been told whether it is an agent's waits with a name
	// slot and an empty avatar: never "Conversation" and never a letter.
	waiting := Model{Locale: "en-US", State: StateReady, SelectedID: "general", AgentRailPending: true, Conversations: []Conversation{{ID: "general", Name: "general", Kind: PublicChannel}, {ID: "dm-x", Name: "dm-x", Kind: DirectMessage}}, Callbacks: Callbacks{SelectConversation: func(string) {}}}
	row := chatbug033Row(t, render(t, waiting), "dm-x")
	if strings.Contains(row, ">Conversation<") || strings.Contains(row, "<svg") || !strings.Contains(row, "agent-icon-pending") {
		t.Fatalf("a direct row waiting for the agent list drew a placeholder name or avatar: %s", row)
	}
}
