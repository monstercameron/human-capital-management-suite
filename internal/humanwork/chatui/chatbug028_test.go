package chatui

import (
	"strings"
	"testing"
)

// chatbug028Model is a conversation whose agent directory answered and holds
// no agent: the shape of #random on the review server.
func chatbug028Model(state PersonaLookupState) Model {
	model := agentUXMentionModel(state)
	model.ResolvedPersonaMentions = nil
	return model
}

// TestTodo_CHATBUG_028 covers the composer's @ menu and the per-conversation
// draft. The "/" list is in TestTodo_CHATBUG_028_Commands; the writing-style
// controls belong to another entry (CHATLANG).
func TestTodo_CHATBUG_028(t *testing.T) {
	open := mentionState{Target: "chat-composer", Open: true}

	// A directory that answered with no agents: people only, no Agents group,
	// no explanation, no error, no retry.
	for _, query := range []string{"", "ca", "zzz"} {
		model := chatbug028Model(PersonaLookupReady)
		markup := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Open: true, Query: query}, "chat-composer"))
		for _, banned := range []string{"mention-heading agents", "Agents", "could not be loaded", "Try again", "No agents", "persona-mention-retry"} {
			if strings.Contains(markup, banned) {
				t.Errorf("query %q: a conversation with no agents showed %q: %s", query, banned, markup)
			}
		}
		if query == "" && (!strings.Contains(markup, "Camila Morales") || !strings.Contains(markup, "mention-heading people")) {
			t.Errorf("the people list went missing with the agents group: %s", markup)
		}
	}

	// A real failure still says so, quietly, with a way to try again.
	failed := renderNode(t, mentionMenu(chatbug028Model(PersonaLookupFailed), open, "chat-composer"))
	for _, want := range []string{"The agent list could not be loaded.", `data-action="persona-mention-retry"`} {
		if !strings.Contains(failed, want) {
			t.Errorf("a failed lookup lost %q: %s", want, failed)
		}
	}

	// A conversation that does have agents keeps its group, and a search that
	// matches no agent there still says so.
	withAgents := renderNode(t, mentionMenu(agentUXMentionModel(PersonaLookupReady), open, "chat-composer"))
	if !strings.Contains(withAgents, "mention-heading agents") || !strings.Contains(withAgents, "Policy Helper") {
		t.Errorf("the agents group disappeared for a conversation that has agents: %s", withAgents)
	}
	noMatch := agentUXMentionModel(PersonaLookupReady)
	noMatch.Members = nil
	if markup := renderNode(t, mentionMenu(noMatch, mentionState{Target: "chat-composer", Open: true, Query: "zzz"}, "chat-composer")); !strings.Contains(markup, "No agents match this search.") {
		t.Errorf("a search with no agent match lost its line: %s", markup)
	}

	// Switching conversation must not carry the composer text.
	model := Model{SelectedID: "random", Draft: "/"}
	model.Select("general")
	if model.Draft != "" {
		t.Fatalf("composer in #general = %q after leaving #random with \"/\" typed, want empty", model.Draft)
	}
	model.SetDraft("welcome back")
	model.Select("announcements")
	if model.Draft != "" {
		t.Fatalf("composer in #announcements = %q, want empty", model.Draft)
	}
	model.Select("random")
	if model.Draft != "/" {
		t.Fatalf("composer back in #random = %q, want the \"/\" typed there", model.Draft)
	}
	model.Select("general")
	if model.Draft != "welcome back" {
		t.Fatalf("composer back in #general = %q, want its own draft", model.Draft)
	}
}

// TestTodo_CHATBUG_028_Browser: the rendered composer says which conversation
// its text belongs to, and only ever carries that conversation's draft. The
// browser's field sync reads that attribute to replace a focused box on a switch.
func TestTodo_CHATBUG_028_Browser(t *testing.T) {
	model := Model{
		SelectedID:    "random",
		Draft:         "/",
		Conversations: []Conversation{{ID: "random", Name: "random", Kind: PublicChannel}, {ID: "general", Name: "general", Kind: PublicChannel}},
		Callbacks:     Callbacks{SendMessage: func(string, string) {}},
	}
	random := renderNode(t, composer(model, handlers{}))
	for _, want := range []string{`data-draft-scope="random"`, `data-chat-value="/"`} {
		if !strings.Contains(random, want) {
			t.Errorf("composer in #random missing %q: %s", want, random)
		}
	}
	model.Select("general")
	general := renderNode(t, composer(model, handlers{}))
	if !strings.Contains(general, `data-draft-scope="general"`) {
		t.Errorf("composer in #general does not name its conversation: %s", general)
	}
	if strings.Contains(general, `data-chat-value="/"`) || strings.Contains(general, "data-draft-scope=\"random\"") {
		t.Errorf("composer in #general still carries the draft of #random: %s", general)
	}
}
