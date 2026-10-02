package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
)

func chatbug067Card(t *testing.T, state AgentShareState) string {
	t.Helper()
	m := chat4Fixture("en-US", "answered", false)
	m.PersonaActivityReady = true
	m.Callbacks.ShareAgentAnswer = func(string) {}
	if state.Status != "" {
		m.AgentShare = map[string]AgentShareState{"run": state}
	}
	return chatPolishMarkup(t, html.Div(html.Props{}, personaReplyRowsForPost(m, localUI{}, "question", time.Now())...), 1440, "light")
}

// A refused share says in one sentence why and offers no button, because
// pressing again cannot help; a failure keeps the button and says to try again;
// a share that worked says where the answer went.
func TestTodo_CHATBUG_067(t *testing.T) {
	const button = `data-action="agent-share"`
	if card := chatbug067Card(t, AgentShareState{}); !strings.Contains(card, button) || !strings.Contains(card, "Share to channel") {
		t.Fatal("a private answer in a public channel does not offer Share to channel")
	}
	for _, tc := range []struct {
		name  string
		state AgentShareState
		want  string
	}{
		{"the agent answers privately", AgentShareState{Status: AgentShareRefused, Reason: "agent"}, "Policy Helper always answers privately, so this stays private."},
		{"a named source is closed", AgentShareState{Status: AgentShareRefused, Reason: "audience", Source: "2026 holiday guide"}, "2026 holiday guide⁩ is not open to everyone in ⁨#general⁩, so this stays private."},
		{"sources are closed, none named", AgentShareState{Status: AgentShareRefused, Reason: "audience"}, "Not everyone in ⁨#general⁩ can open the sources, so this stays private."},
		{"too old", AgentShareState{Status: AgentShareRefused, Reason: "expired"}, "This answer is too old to share."},
		{"not the asker", AgentShareState{Status: AgentShareRefused, Reason: "denied"}, "Only the person who asked can share this answer."},
	} {
		card := chatbug067Card(t, tc.state)
		if !strings.Contains(card, tc.want) {
			t.Fatalf("%s: the card does not say %q", tc.name, tc.want)
		}
		if strings.Contains(card, button) || strings.Contains(card, "Try again") || strings.Contains(card, "Could not share") {
			t.Fatalf("%s: a refusal still offers to share or says to try again", tc.name)
		}
	}
	failed := chatbug067Card(t, AgentShareState{Status: AgentShareFailed})
	if !strings.Contains(failed, button) || !strings.Contains(failed, "Could not share this answer. Try again.") {
		t.Fatal("a failed share lost its button or its sentence")
	}
	shared := chatbug067Card(t, AgentShareState{Status: AgentShareShared})
	// CHATUX-026: the shared card says who it is shared with in its header, and
	// no longer says it is private.
	if !strings.Contains(shared, "Shared with ⁨#general⁩") || strings.Contains(shared, button) || strings.Contains(shared, "Only visible to you") {
		t.Fatal("a shared answer does not say where it went")
	}
}

// The refusals are written in the three languages and never show a key or a
// placeholder.
func TestTodo_CHATBUG_067_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, reason := range []string{"agent", "audience", "expired", "denied"} {
			m := chat4Fixture(locale, "answered", false)
			m.PersonaActivityReady = true
			m.Callbacks.ShareAgentAnswer = func(string) {}
			m.AgentShare = map[string]AgentShareState{"run": {Status: AgentShareRefused, Reason: reason, Source: "Paid time off policy"}}
			card := chatPolishMarkup(t, html.Div(html.Props{}, personaReplyRowsForPost(m, localUI{}, "question", time.Now())...), 390, "light")
			if strings.Contains(card, "chatux003.") || strings.Contains(card, "{source}") || strings.Contains(card, "{channel}") || strings.Contains(card, "{name}") || !strings.Contains(card, `class="agent-reply-share-note"`) {
				t.Fatalf("%s %s: the refusal is not written out: %s", locale, reason, card)
			}
			if reason == "audience" && !strings.Contains(card, "Paid time off policy") {
				t.Fatalf("%s: the closed source is not named", locale)
			}
		}
	}
}
