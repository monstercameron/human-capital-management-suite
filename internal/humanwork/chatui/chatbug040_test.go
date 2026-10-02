package chatui

import (
	"strings"
	"testing"
)

func chatbug040Asked(locale string) Model {
	m := chat4Fixture(locale, "sent", false)
	m.PersonaInvocations = nil
	m.PersonaActivityReady = false
	return m
}

// A message the viewer wrote that names an agent keeps room for its answer card
// until the agent activity has answered; nobody else's message does, and neither
// does a direct conversation with an agent, whose answers are ordinary messages.
func TestTodo_CHATBUG_040(t *testing.T) {
	m := chatbug040Asked("en-US")
	if chatbug040ReservedRow(m, "question") == nil {
		t.Fatal("a question to an agent has no room reserved for its card")
	}
	if chatbug040ReservedRow(m, "no-such-message") != nil {
		t.Fatal("room was reserved under a message that does not exist")
	}
	other := chatbug040Asked("en-US")
	other.CurrentUser = "bob"
	if chatbug040ReservedRow(other, "question") != nil {
		t.Fatal("room was reserved under a question somebody else asked: only the asker is answered")
	}
	ready := chatbug040Asked("en-US")
	ready.PersonaActivityReady = true
	if chatbug040ReservedRow(ready, "question") != nil {
		t.Fatal("room is still reserved after the agent activity answered that there is none")
	}
	direct := chat4Fixture("en-US", "sent", true)
	direct.PersonaInvocations, direct.PersonaActivityReady = nil, false
	if chatbug040ReservedRow(direct, "question") != nil {
		t.Fatal("room was reserved in a direct conversation with an agent")
	}
	plain := chatbug040Asked("en-US")
	plain.Messages[0].PersonaReferences = nil
	if chatbug040ReservedRow(plain, "question") != nil {
		t.Fatal("room was reserved under a message that names no agent")
	}
	// The placeholder and every answer card keep the same minimum height.
	for _, want := range []string{".chatbug040-reserve,.chat-ephemeral.agent-reply-row{min-block-size:" + ChatBug040CardMinHeight} {
		if !strings.Contains(ChatBug040Styles, want) {
			t.Fatalf("styles do not give the placeholder and the card one height: %q", want)
		}
	}
	if !strings.Contains(Stylesheet, ChatBug040Styles) {
		t.Fatal("the placeholder styles are not part of the page stylesheet")
	}
}

// The first paint of a conversation holds the placeholder under the question; the
// card replaces it, in the same place, when the activity arrives; and nothing is
// left behind once it has.
func TestTodo_CHATBUG_040_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		first := render(t, chatbug040Asked(locale))
		if got := strings.Count(first, `data-agent-reply-state="reserved"`); got != 1 {
			t.Fatalf("%s: first paint holds %d placeholders, want 1: the question has none", locale, got)
		}
		if !strings.Contains(first, `data-reserved-for="question"`) || !strings.Contains(first, `aria-hidden="true"`) {
			t.Fatalf("%s: the placeholder is not tied to its question or is exposed to assistive technology", locale)
		}
		if strings.Contains(first, `agent-reply-state="answered-private"`) {
			t.Fatalf("%s: a card was drawn before the activity answered", locale)
		}

		answered := chat4Fixture(locale, "answered", false)
		answered.PersonaActivityReady = true
		after := render(t, answered)
		if strings.Contains(after, `data-agent-reply-state="reserved"`) {
			t.Fatalf("%s: the placeholder is still there beside the answer", locale)
		}
		if !strings.Contains(after, `agent-reply-state="answered-private"`) {
			t.Fatalf("%s: the answer card is missing once the activity arrived", locale)
		}

		none := chatbug040Asked(locale)
		none.PersonaActivityReady = true
		if strings.Contains(render(t, none), `data-agent-reply-state="reserved"`) {
			t.Fatalf("%s: a question the agent will not answer keeps its room", locale)
		}
	}
}
