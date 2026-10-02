package chatui

import (
	"strings"
	"testing"
)

func agentUXS28Model(locale string) Model {
	m := chat4Fixture(locale, "sent", false)
	m.ShowDetails = true
	m.AmbientReads = []AmbientRead{{Agent: "task-catcher"}, {Agent: "reminder"}}
	m.Callbacks.SetAmbientOptOut = func(bool) {}
	m.Ambient = AmbientState{Manages: true, Grants: []AmbientGrant{{Agent: "reminder", Enabled: true}, {Agent: "task-catcher", Enabled: false}}}
	return m
}

// The conversation's administrator gets a switch "Reads every message here" for
// each installed agent in Conversation details, set to what the server says and
// carrying the agent and the choice it would make; a member gets the sentence
// and their own switch but not this one; a direct conversation between two
// people gets neither (AGENTUX-066).
func TestTodo_AGENTUX_066_GrantSwitch(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		m := agentUXS28Model(locale)
		page := render(t, m)
		chat4Require(t, page, `class="ambient-grants"`, `data-ambient-action="GRANT"`, `data-ambient-agent="task-catcher"`, `data-ambient-enabled="true"`, `data-ambient-agent="reminder"`, `data-ambient-enabled="false"`, `data-ambient-conversation="general"`, `role="switch"`, strings.ReplaceAll(laneText(m, agentux066GrantCopy, keyAmbientGrantSwitch), "'", "&#39;"))
		if locale == "en-US" && !strings.Contains(page, "Reads every message here") {
			t.Fatal("the English label is missing")
		}
		// Reminder is on, Task Catcher is off: the checked state follows the server.
		on := strings.Index(page, `data-ambient-agent="reminder"`)
		off := strings.Index(page, `data-ambient-agent="task-catcher"`)
		if on < 0 || off < 0 || !strings.Contains(page[max(0, on-400):on], `aria-checked="true"`) || !strings.Contains(page[max(0, off-400):off], `aria-checked="false"`) {
			t.Fatalf("the checked states do not follow the grants: %s", page[min(on, off):])
		}

		member := agentUXS28Model(locale)
		member.Ambient.Manages = false
		page = render(t, member)
		if strings.Contains(page, `data-ambient-action="GRANT"`) || !strings.Contains(page, `data-action="ambient-optout"`) {
			t.Fatal("a member was offered the administrator's switch, or lost their own")
		}

		direct := chat4Fixture(locale, "sent", true)
		direct.ShowDetails = true
		direct.Ambient = member.Ambient
		direct.Ambient.Manages = true
		if strings.Contains(render(t, direct), `data-ambient-action="GRANT"`) {
			t.Fatal("a direct conversation was offered the switch")
		}
	})
}

// An offer is shown under the message it was made for, for the person it was
// made for; another person's private offer is not drawn at all; an offer whose
// message is not on the loaded page closes the timeline; a failed refresh keeps
// the offers and adds a retry (AGENTUX-067, -068).
func TestTodo_AGENTUX_067_CardsInTimeline(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chat4Fixture(locale, "sent", false)
		m.Ambient.Cards = []AgentUXAmbientCard{
			{ID: "mine", Conversation: "general", Source: "question", AgentName: "Task Catcher", Kind: "TASK", Scope: "PRIVATE", Person: "alice", Reason: "self_commitment", Title: "Send the deck", State: "OFFERED", CanManage: true, Revision: 1},
			{ID: "theirs", Conversation: "general", Source: "question", AgentName: "Task Catcher", Kind: "TASK", Scope: "PRIVATE", Person: "luis", Reason: "self_commitment", Title: "Private to Luis", State: "OFFERED", CanManage: true, Revision: 1},
			{ID: "far", Conversation: "general", Source: "older-post", AgentName: "Reminder", Kind: "REMINDER", Scope: "PUBLIC", Reason: "deadline", Title: "Timesheets are due", State: "OFFERED", CanManage: true, Revision: 1},
		}
		page := render(t, m)
		if !strings.Contains(page, `data-ambient-card="mine"`) || !strings.Contains(page, "Send the deck") || !strings.Contains(page, `data-source-post="question"`) {
			t.Fatalf("%s: the person's own offer is not under its message", locale)
		}
		if strings.Contains(page, "Private to Luis") || strings.Contains(page, `data-ambient-card="theirs"`) {
			t.Fatalf("%s: another person's private offer was drawn", locale)
		}
		if !strings.Contains(page, `data-ambient-card="far"`) || !strings.Contains(page, `data-ambient-action="ADD"`) && !strings.Contains(page, `data-ambient-action="SET"`) {
			t.Fatalf("%s: the offer for a message off the page is missing", locale)
		}
		if strings.Index(page, `data-ambient-card="mine"`) < strings.Index(page, "explain our PTO policy") {
			t.Fatalf("%s: the offer is above its message", locale)
		}
		if strings.Contains(page, "⟦") || strings.Contains(page, "chat.ambient.") {
			t.Fatalf("%s: an untranslated key is on the page", locale)
		}
		m.Ambient.Failed = true
		page = render(t, m)
		if !strings.Contains(page, `data-ambient-action="REFRESH"`) || !strings.Contains(page, `data-ambient-card="mine"`) {
			t.Fatalf("%s: a failed refresh lost the offers or the retry", locale)
		}
	}
}
