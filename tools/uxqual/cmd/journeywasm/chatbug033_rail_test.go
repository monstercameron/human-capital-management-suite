//go:build !(js && wasm)

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func chatbug033Entries() []agentRailEntry {
	return []agentRailEntry{
		{ConversationID: "dm-policy", AgentID: "policy-helper", Name: "Policy Helper", Purpose: "Answers questions about company policies from the documents it may read.", Icon: agenticon.Generate(agenticon.Input{Name: "Policy Helper"}), IconRevision: 1},
		{ConversationID: "dm-assistant", AgentID: "assistant", Name: "Assistant", Purpose: "Answers everyday questions and writes announcements from the documents you give it.", Icon: agenticon.Generate(agenticon.Input{Name: "Assistant"}), IconRevision: 1},
	}
}

// chatbug033List is a conversation list as the server sends it: the names the
// rooms were created with, and nothing about which of them are agents.
func chatbug033List() []chatui.Conversation {
	return []chatui.Conversation{
		{ID: "general", Name: "general", Kind: chatui.PublicChannel},
		{ID: "random", Name: "random", Kind: chatui.PublicChannel},
		{ID: "dm-policy", Name: "Policy Helper", Kind: chatui.DirectMessage},
		{ID: "dm-assistant", Name: "Assistant", Kind: chatui.DirectMessage},
	}
}

func chatbug033Model() chatui.Model {
	rooms := chatbug033List()
	return chatui.Model{Locale: "en-US", State: chatui.StateReady, CurrentUser: "walt", CurrentTenantID: "tenant", SelectedID: "general", Conversations: rooms,
		Sections:  []chatui.SidebarSection{{ID: "channels", Chats: chatbug033List()[:2]}, {ID: "direct", Chats: chatbug033List()[2:]}},
		Callbacks: chatui.Callbacks{SelectConversation: func(string) {}}}
}

func chatbug033Render(t *testing.T, model chatui.Model) string {
	t.Helper()
	markup, err := ui.RenderToString(chatui.Build(model))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

// TestTodo_CHATBUG_033 covers what the client does with the server's agent list:
// both agents' rows carry the stored icon, name, description and Agent flag from
// the first commit, and the icons survive every later update of the list, a
// refresh that carries none, a switch to another conversation and a read that
// knows only a name.
func TestTodo_CHATBUG_033(t *testing.T) {
	entries := chatbug033Entries()
	model := chatbug033Model()
	model.AgentRailPending = true
	if !applyAgentRail(&model, entries) {
		t.Fatal("the agent list changed nothing")
	}
	if model.AgentRailPending || !model.AgentIconsReady {
		t.Fatalf("the rows are still waiting: pending=%v ready=%v", model.AgentRailPending, model.AgentIconsReady)
	}
	rows := func(m chatui.Model) []chatui.Conversation {
		return []chatui.Conversation{m.Conversations[2], m.Conversations[3], m.Sections[1].Chats[0], m.Sections[1].Chats[1]}
	}
	for i, row := range rows(model) {
		want := entries[i%2]
		if !row.Agent || row.AgentID != want.AgentID || row.Name != want.Name || row.Icon != want.Icon || row.AgentPurpose != want.Purpose || !row.Icon.Valid() {
			t.Fatalf("row %d does not carry its agent's own identity: %+v", i, row)
		}
	}
	if model.Conversations[0].Agent || model.Conversations[1].Agent {
		t.Fatal("a channel was marked as an agent conversation")
	}
	if model.Conversations[2].Icon == model.Conversations[3].Icon {
		t.Fatal("two agents share one icon")
	}

	// A refresh of the list carries no icon: the server's rows are plain.
	refreshed := chatui.Model{Locale: "en-US", CurrentUser: "walt", CurrentTenantID: "tenant", SelectedID: "random", Conversations: chatbug033List()}
	preserveAgentConversationIdentity(model, &refreshed)
	for _, row := range []chatui.Conversation{refreshed.Conversations[2], refreshed.Conversations[3]} {
		if !row.Agent || !row.Icon.Valid() || row.AgentPurpose == "" {
			t.Fatalf("a list refresh dropped an agent's identity: %+v", row)
		}
	}
	// The same refresh through the cache: the rows are rebuilt from the server's
	// answer, which is held, not asked again.
	var cache agentRailCache
	identity := agentRailIdentity("tenant", "walt")
	cache.store(identity, chatbug033List(), entries)
	cached, current := cache.state(identity, chatbug033List())
	if !current || len(cached) != 2 {
		t.Fatalf("the answer is not held: current=%v %+v", current, cached)
	}
	rebuilt := chatui.Model{Conversations: chatbug033List()}
	applyAgentRail(&rebuilt, cached)
	if rebuilt.Conversations[2].Icon != entries[0].Icon || rebuilt.Conversations[3].Icon != entries[1].Icon {
		t.Fatal("the held answer did not restore the icons")
	}
	// Switching to another conversation, whose own directory knows no agents,
	// and a read that knows only a name, change nothing.
	switched := refreshed
	switched.SelectedID, switched.ResolvedPersonaMentions = "general", nil
	if applyAgentDirectConversation(&switched, "dm-policy", "policy-helper", "Policy Helper") {
		t.Fatal("a name-only read repainted an unchanged row")
	}
	if switched.Conversations[2].Icon != entries[0].Icon || switched.Conversations[2].AgentPurpose != entries[0].Purpose {
		t.Fatalf("a conversation switch dropped the icon: %+v", switched.Conversations[2])
	}

	// A new direct conversation, an expired answer and another person each ask
	// the server again.
	if _, current := cache.state(identity, append(chatbug033List(), chatui.Conversation{ID: "dm-new", Kind: chatui.DirectMessage})); current {
		t.Fatal("a direct conversation the answer does not cover was taken as covered")
	}
	if _, current := cache.state(agentRailIdentity("tenant", "someone-else"), chatbug033List()); current {
		t.Fatal("one person's answer was used for another")
	}
	cache.at = time.Now().Add(-2 * agentRailFreshFor)
	if _, current := cache.state(identity, chatbug033List()); current || !cache.has(identity) {
		t.Fatal("an expired answer was taken as current, or forgotten")
	}
	// Only a person's own direct conversation is ever marked.
	if applyAgentDirectConversation(&model, "general", "policy-helper", "Policy Helper") {
		t.Fatal("a channel was turned into an agent conversation")
	}
}

// TestTodo_CHATBUG_033_Browser renders the page from the client's model: on first
// paint both agents' sidebar rows read their name, show the Agent badge and wear
// their own icon; while the list is on its way the rows keep their name and badge
// with an empty avatar; and after switching conversations and refreshing the
// list, the icons are still there.
func TestTodo_CHATBUG_033_Browser(t *testing.T) {
	entries := chatbug033Entries()
	policy, assistant := entries[0].Icon, entries[1].Icon
	policySVG, err := ui.RenderToString(agenticon.Node(policy))
	if err != nil {
		t.Fatal(err)
	}
	assistantSVG, err := ui.RenderToString(agenticon.Node(assistant))
	if err != nil {
		t.Fatal(err)
	}
	row := func(markup, id string) string {
		start := strings.Index(markup, `data-conversation-id="`+id+`"`)
		if start < 0 {
			t.Fatalf("no row for %s", id)
		}
		rest := markup[start:]
		if end := strings.Index(rest, `class="rail-row-more"`); end >= 0 {
			rest = rest[:end]
		}
		return rest
	}
	check := func(step string, markup string) {
		t.Helper()
		for id, want := range map[string]struct{ name, icon string }{"dm-policy": {"Policy Helper", policySVG}, "dm-assistant": {"Assistant", assistantSVG}} {
			r := row(markup, id)
			if !strings.Contains(r, ">"+want.name+"<") || !strings.Contains(r, "Agent") || !strings.Contains(r, want.icon) || strings.Contains(r, ">Conversation<") || strings.Contains(r, "agent-icon-pending") {
				t.Fatalf("%s: the %s row is not its name, the Agent badge and its own icon: %s", step, want.name, r)
			}
		}
	}

	// Still waiting for the server's list: no letter, no "Conversation", no icon.
	model := chatbug033Model()
	model.AgentRailPending = true
	waiting := chatbug033Render(t, model)
	for _, id := range []string{"dm-policy", "dm-assistant"} {
		r := row(waiting, id)
		if strings.Contains(r, ">Conversation<") || strings.Contains(r, "<svg") || !strings.Contains(r, "agent-icon-pending") {
			t.Fatalf("a row waiting for the agent list drew a placeholder: %s", r)
		}
	}

	applyAgentRail(&model, entries)
	check("first paint", chatbug033Render(t, model))

	// General to random, then a refresh of the list that carries no icon.
	model.SelectedID = "random"
	model.ResolvedPersonaMentions = nil
	check("after switching conversation", chatbug033Render(t, model))
	refreshed := chatui.Model{Locale: "en-US", State: chatui.StateReady, CurrentUser: "walt", CurrentTenantID: "tenant", SelectedID: "random", Conversations: chatbug033List(), Callbacks: chatui.Callbacks{SelectConversation: func(string) {}}}
	preserveAgentConversationIdentity(model, &refreshed)
	check("after a list refresh", chatbug033Render(t, refreshed))

	// The Assistant's conversation reads the Assistant's own description.
	refreshed.SelectedID = "dm-assistant"
	header := chatbug033Render(t, refreshed)
	if !strings.Contains(header, entries[1].Purpose) || strings.Contains(header, "Answers from policy documents") {
		t.Fatalf("the Assistant's header is not its own description: %s", header)
	}
}
