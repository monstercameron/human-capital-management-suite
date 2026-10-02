package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// agentUX029Step is one thing that can happen while a person types "@pol": the
// conversation's members arrive, the agent lookup starts, it answers, it fails,
// or the person types.
type agentUX029Step string

const (
	agentUX029Members agentUX029Step = "members arrive"
	agentUX029Loading agentUX029Step = "lookup starts"
	agentUX029Ready   agentUX029Step = "lookup answers"
	agentUX029Typed   agentUX029Step = "types @pol"
)

// agentUX029Menu draws the mention menu the way the workspace does, from the
// text in the box and the model as it is at this moment.
func agentUX029Menu(t *testing.T, m Model, typed string) string {
	t.Helper()
	people := make([]mentionCandidate, 0, len(m.Members))
	for _, member := range m.Members {
		people = append(people, mentionCandidate{ID: member.ID, Name: member.Name, Member: true})
	}
	decision := decideMentionMenu(typed, len(typed), "chat-composer", m.PersonaLookup, people, m.ResolvedPersonaMentions)
	markup, err := ui.RenderToString(mentionMenu(m, decision.State, "chat-composer"))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

// The mention menu opens on the first try in every order of loading and typing:
// whatever has arrived when "@pol" is typed, the menu is open; while the agent
// list is on its way the Agents group says so, also under people who match; and
// once everything has arrived the agent is there to pick, without another key.
func TestTodo_AGENTUX_029(t *testing.T) {
	agent := ResolvedPersonaMention{Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "policy-helper", Display: "Policy Helper", ConversationID: "room"}, Handle: "policy-helper", Purpose: "Answer policy questions"}
	members := []Member{{ID: "polly", Name: "Polly Adams"}, {ID: "camila", Name: "Camila Morales"}}
	orders := [][]agentUX029Step{
		{agentUX029Members, agentUX029Loading, agentUX029Ready, agentUX029Typed},
		{agentUX029Members, agentUX029Loading, agentUX029Typed, agentUX029Ready},
		{agentUX029Members, agentUX029Typed, agentUX029Loading, agentUX029Ready},
		{agentUX029Typed, agentUX029Members, agentUX029Loading, agentUX029Ready},
		{agentUX029Typed, agentUX029Loading, agentUX029Members, agentUX029Ready},
		{agentUX029Typed, agentUX029Loading, agentUX029Ready, agentUX029Members},
		{agentUX029Loading, agentUX029Typed, agentUX029Ready, agentUX029Members},
		{agentUX029Loading, agentUX029Typed, agentUX029Members, agentUX029Ready},
		{agentUX029Loading, agentUX029Ready, agentUX029Typed, agentUX029Members},
		{agentUX029Loading, agentUX029Members, agentUX029Typed, agentUX029Ready},
		{agentUX029Loading, agentUX029Members, agentUX029Ready, agentUX029Typed},
		{agentUX029Loading, agentUX029Ready, agentUX029Members, agentUX029Typed},
	}
	for _, order := range orders {
		name := ""
		m := Model{SelectedID: "room", Callbacks: Callbacks{SendMessageWithReferences: func(string, string, []ChatReference) {}, RetryPersonaMentions: func() {}}}
		typed := ""
		for _, step := range order {
			name += string(step) + "; "
			switch step {
			case agentUX029Members:
				m.Members = members
			case agentUX029Loading:
				m.PersonaLookup, m.PersonaLookupConversationID = PersonaLookupLoading, "room"
			case agentUX029Ready:
				m.PersonaLookup, m.PersonaLookupConversationID, m.ResolvedPersonaMentions = PersonaLookupReady, "room", []ResolvedPersonaMention{agent}
			case agentUX029Typed:
				typed = "@pol"
			}
			if typed == "" {
				continue
			}
			menu := agentUX029Menu(t, m, typed)
			// Open: the list itself, or the line that says nobody matches yet;
			// never the empty slot of a closed menu.
			if strings.Contains(menu, `class="mention-slot"`) || !strings.Contains(menu, `id="chat-composer-mentions"`) {
				t.Fatalf("%s the menu is closed: %s", name, menu)
			}
			if m.PersonaLookup == PersonaLookupLoading && !strings.Contains(menu, "Loading agents…") {
				t.Fatalf("%s the menu does not say the agents are on their way: %s", name, menu)
			}
			if m.PersonaLookup == PersonaLookupLoading && len(m.Members) > 0 && !strings.Contains(menu, "Polly Adams") {
				t.Fatalf("%s the matching person is missing while agents load: %s", name, menu)
			}
			if m.PersonaLookup == PersonaLookupReady && (!strings.Contains(menu, "Policy Helper") || strings.Contains(menu, "Loading agents…")) {
				t.Fatalf("%s the agent is not offered once the lookup answered: %s", name, menu)
			}
		}
		// Everything has arrived and nothing more was typed: the agent and the
		// matching person are both there.
		final := agentUX029Menu(t, m, typed)
		if !strings.Contains(final, "Policy Helper") || !strings.Contains(final, "Polly Adams") || strings.Contains(final, "Camila Morales") {
			t.Fatalf("%s the final menu: %s", name, final)
		}
	}

	// A lookup that failed offers a retry, also under people who match.
	failed := Model{SelectedID: "room", Members: members, PersonaLookup: PersonaLookupFailed, PersonaLookupConversationID: "room", Callbacks: Callbacks{SendMessageWithReferences: func(string, string, []ChatReference) {}, RetryPersonaMentions: func() {}}}
	for _, typed := range []string{"@", "@pol", "@zzz"} {
		menu := agentUX029Menu(t, failed, typed)
		if !strings.Contains(menu, "The agent list could not be loaded.") || !strings.Contains(menu, `data-action="persona-mention-retry"`) {
			t.Fatalf("typing %q after a failed lookup offers no retry: %s", typed, menu)
		}
	}
	// A lookup that answered with no match says so only when nobody matches.
	ready := Model{SelectedID: "room", Members: members, PersonaLookup: PersonaLookupReady, PersonaLookupConversationID: "room", ResolvedPersonaMentions: []ResolvedPersonaMention{agent}, Callbacks: Callbacks{SendMessageWithReferences: func(string, string, []ChatReference) {}}}
	if menu := agentUX029Menu(t, ready, "@cam"); strings.Contains(menu, "No agents match this search.") || !strings.Contains(menu, "Camila Morales") {
		t.Fatalf("a person matched and the menu still says no agent matches: %s", menu)
	}

	// The open menu and the typed text are the page's own: adopting a newly read
	// conversation changes neither, and going to another conversation closes it.
	store := mentionStore{box: &mentionBox{conversationID: "room", state: mentionState{Target: "chat-composer", Query: "pol", Open: true, End: 4}}}
	store.ForConversation("room")
	if !store.box.state.Open || store.box.state.Query != "pol" {
		t.Fatalf("a new read of the same conversation closed the menu: %+v", store.box.state)
	}
	store.ForConversation("another")
	if store.box.state.Open || store.box.state.Query != "" {
		t.Fatalf("the menu followed the person to another conversation: %+v", store.box.state)
	}
}
