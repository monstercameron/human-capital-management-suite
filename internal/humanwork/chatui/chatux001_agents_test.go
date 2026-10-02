package chatui

import (
	"strings"
	"testing"
)

func chatux001Roster(conversation string, names ...string) []ResolvedPersonaMention {
	var out []ResolvedPersonaMention
	for _, name := range names {
		out = append(out, ResolvedPersonaMention{Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "t", ID: "agent-" + name, Display: name, ConversationID: conversation}})
	}
	return out
}

func chatux001Subtitle(t *testing.T, m Model) string {
	t.Helper()
	markup := renderNode(t, timeline(m, handlers{}))
	start := strings.Index(markup, `class="conversation-topic"`)
	end := strings.Index(markup[start:], "</p>")
	if start < 0 || end < 0 {
		t.Fatalf("no subtitle in the header: %s", markup)
	}
	return markup[start : start+end]
}

// TestTodo_CHATUX_001_AgentSubtitle: "Public · 18 members · 2 agents" reads the
// same whichever of the conversation and the agent roster arrives first, draws
// the agents the moment the roster arrives, and keeps them if the roster is
// later cleared (a read that lost a race or failed).
func TestTodo_CHATUX_001_AgentSubtitle(t *testing.T) {
	room := Conversation{ID: "general", Name: "general", Kind: PublicChannel, Joined: true, MemberCount: 18}
	base := Model{State: StateReady, Locale: "en-US", SelectedID: "general", CurrentTenantID: "t", CurrentUser: "walt", Conversations: []Conversation{room}}

	// Order one: the conversation first, the roster after.
	before := chatux001Subtitle(t, base)
	if !strings.Contains(before, "18 members") || strings.Contains(before, "agent") {
		t.Fatalf("before the roster: %s", before)
	}
	arrived := base
	arrived.ResolvedPersonaMentions = chatux001Roster("general", "Assistant", "Policy Helper")
	if got := chatux001Subtitle(t, arrived); !strings.Contains(got, "18 members") || !strings.Contains(got, "2 agents") {
		t.Fatalf("after the roster arrives the subtitle did not draw the agents: %s", got)
	}

	// Order two: the roster first, the conversation (member count) after.
	rosterFirst := Model{State: StateReady, Locale: "en-US", SelectedID: "general", CurrentTenantID: "t", ResolvedPersonaMentions: chatux001Roster("general", "Assistant", "Policy Helper"), Conversations: []Conversation{{ID: "general", Name: "general", Kind: PublicChannel, Joined: true}}}
	if got := chatux001Subtitle(t, rosterFirst); !strings.Contains(got, "2 agents") || strings.Contains(got, "members") {
		t.Fatalf("roster before the member count: %s", got)
	}
	rosterFirst.Conversations[0].MemberCount = 18
	if got := chatux001Subtitle(t, rosterFirst); !strings.Contains(got, "18 members") || !strings.Contains(got, "2 agents") {
		t.Fatalf("both arrived: %s", got)
	}

	// The roster is cleared (a refresh replaced it, or a read failed): the count
	// the last good read left keeps the agents in the subtitle.
	cleared := arrived
	cleared.AgentCounts = map[string]int{"general": ConversationAgentCount(arrived)}
	cleared.ResolvedPersonaMentions = nil
	if got := chatux001Subtitle(t, cleared); !strings.Contains(got, "18 members") || !strings.Contains(got, "2 agents") {
		t.Fatalf("a cleared roster dropped the agents from the subtitle: %s", got)
	}
	// A count belongs to its conversation, and a read that found none says none.
	other := cleared
	other.SelectedID = "random"
	other.Conversations = []Conversation{{ID: "random", Name: "random", Kind: PublicChannel, Joined: true, MemberCount: 4}}
	if got := chatux001Subtitle(t, other); strings.Contains(got, "agent") {
		t.Fatalf("another conversation took general's agent count: %s", got)
	}
	cleared.AgentCounts = map[string]int{"general": 0}
	if got := chatux001Subtitle(t, cleared); strings.Contains(got, "agent") {
		t.Fatalf("a read that found no agents still shows a count: %s", got)
	}
	// One agent reads as one.
	cleared.AgentCounts = map[string]int{"general": 1}
	if got := chatux001Subtitle(t, cleared); !strings.Contains(got, "1 agent") || strings.Contains(got, "1 agents") {
		t.Fatalf("one agent: %s", got)
	}
}
