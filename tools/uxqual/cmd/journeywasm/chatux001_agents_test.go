package main

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func chatux001Directory(conversation string, names ...string) personaChatDirectory {
	var out personaChatDirectory
	for _, name := range names {
		out.Personas = append(out.Personas, personaChatProfile{Reference: personaChatReference{Kind: "AGENT_MENTION", TenantID: "northwind", ID: "agent-" + name, Display: name, ConversationID: conversation}})
	}
	return out
}

// TestTodo_CHATUX_001_AgentCountKept: the agent count of the header subtitle is
// set when an agent read succeeds and survives what used to drop it: a listing
// refresh that replaced the roster while the read was in flight, and a later
// read that failed. A read that succeeds with no agents says none.
func TestTodo_CHATUX_001_AgentCountKept(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "northwind", Subject: "avery", Locale: "en-US"}
	state := newChatStateForTest(t)
	state.mutate(func(m *chatui.Model) {
		m.SelectedID, m.CurrentTenantID, m.CurrentUser = "general", "northwind", "avery"
		m.Conversations = []chatui.Conversation{{ID: "general", Name: "general", Kind: chatui.PublicChannel, MemberCount: 18}}
		applyPersonaDirectoryResult(m, chatux001Directory("general", "Assistant", "Policy Helper"), cfg, "general", nil, func() {})
	})
	got := state.snapshot()
	if got.AgentCounts["general"] != 2 || len(got.ResolvedPersonaMentions) != 2 {
		t.Fatalf("after the read: counts %v, roster %d", got.AgentCounts, len(got.ResolvedPersonaMentions))
	}

	// A listing refresh whose snapshot began before the roster arrived (and for
	// a conversation that was not yet open) replaces the roster with nothing.
	loaded := got
	loaded.SelectedID = "other"
	loaded.ResolvedPersonaMentions = nil
	loaded.AgentCounts = nil
	if !state.adoptLoadedChatProjection(state.currentGeneration(), loaded, chatCursor{}, false) {
		t.Fatal("the refresh was rejected")
	}
	if kept := state.snapshot(); kept.AgentCounts["general"] != 2 {
		t.Fatalf("a listing refresh dropped the agent count: %v", kept.AgentCounts)
	}

	// A read that fails clears the roster and keeps the last good count.
	state.mutate(func(m *chatui.Model) {
		m.SelectedID = "general"
		applyPersonaDirectoryResult(m, personaChatDirectory{}, cfg, "general", errors.New("timeout"), func() {})
	})
	failed := state.snapshot()
	if len(failed.ResolvedPersonaMentions) != 0 || failed.AgentCounts["general"] != 2 {
		t.Fatalf("after a failed read: roster %d, counts %v", len(failed.ResolvedPersonaMentions), failed.AgentCounts)
	}

	// A read that answers with no agents (or a refusal) says none.
	state.mutate(func(m *chatui.Model) {
		applyPersonaDirectoryResult(m, chatux001Directory("general"), cfg, "general", nil, func() {})
	})
	if none := state.snapshot(); none.AgentCounts["general"] != 0 {
		t.Fatalf("a read that found no agents left a count: %v", none.AgentCounts)
	}
}

func TestChatux001WithAgentCountDoesNotEditTheMapItWasGiven(t *testing.T) {
	first := chatux001WithAgentCount(nil, "a", 2)
	second := chatux001WithAgentCount(first, "b", 1)
	if first["b"] != 0 || second["a"] != 2 || second["b"] != 1 {
		t.Fatalf("copy on write: %v then %v", first, second)
	}
	if same := chatux001WithAgentCount(second, "a", 2); len(same) != 2 {
		t.Fatalf("an unchanged count rewrote the map: %v", same)
	}
}
