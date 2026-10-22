package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_AGENTUX_081_AgentProfile: an agent's name opens the agent's own
// profile, never the person panel: what it does, what it reads, its owner,
// and Ask. A person's name still opens the person panel.
func TestTodo_AGENTUX_081_AgentProfile(t *testing.T) {
	persona := ResolvedPersonaMention{
		Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "agent-helper", Display: "Helper", ConversationID: "general"},
		Handle:    "helper", Purpose: "Answer tenant-wide policy questions.", Owner: "Walt Brennan", Version: "6",
		DataClasses: []string{"policy"}, CannotDo: []string{"record"},
		Skills: []PersonaMentionSkill{{Name: "Search", Tier: "T0"}},
	}
	m := Model{State: StateReady, Locale: "en-US", CurrentUser: "alice", SelectedID: "general",
		Conversations:           []Conversation{{ID: "general", Name: "general", Kind: PublicChannel}},
		ResolvedPersonaMentions: []ResolvedPersonaMention{persona},
		Callbacks:               Callbacks{OpenPerson: func(string) {}},
	}
	agent := renderNode(t, personButton(m, "agent-helper", "Helper", "message-author person-name", ui.Text("Helper")))
	if !strings.Contains(agent, `data-action="agent-profile-open"`) || strings.Contains(agent, `data-action="open-person"`) {
		t.Fatalf("agent name does not open the agent profile: %s", agent)
	}
	person := renderNode(t, personButton(m, "worker-1", "Walt Brennan", "message-author person-name", ui.Text("Walt Brennan")))
	if !strings.Contains(person, `data-action="open-person"`) {
		t.Fatalf("person name lost the person panel: %s", person)
	}
	dialog := renderNode(t, agentProfileDialog(m, "agent-helper"))
	for _, want := range []string{
		"Answer tenant-wide policy questions.",
		"Walt Brennan",
		"agent-summary-reads",
		`data-action="agent-ask-here"`,
	} {
		if !strings.Contains(dialog, want) {
			t.Errorf("agent profile missing %q: %s", want, dialog)
		}
	}
	if strings.Contains(dialog, "Person details") || strings.Contains(dialog, "Not available") {
		t.Errorf("agent profile shows the person panel: %s", dialog)
	}
}
