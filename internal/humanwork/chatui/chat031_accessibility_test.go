package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHAT_031_Accessibility pins the workspace's landmark names, skip
// target, composer labels, and announcements for loading and unavailable chat.
func TestTodo_CHAT_031_Accessibility(t *testing.T) {
	model := Model{
		State:      StateReady,
		Locale:     "en-US",
		SelectedID: "engineering",
		Conversations: []Conversation{{
			ID: "engineering", Name: "Engineering", Kind: PublicChannel, MemberCount: 12,
		}},
		Messages:  []Message{{ID: "release", Author: "Ari Chen", Body: "Release is ready"}},
		Callbacks: Callbacks{SendMessage: func(string, string) {}, SelectConversation: func(string) {}},
	}
	markup := render(t, model)

	for _, want := range []string{
		`<a class="chat-skip" href="#chat-main">Skip to conversation</a>`,
		`id="chat-main"`, `role="log"`, `aria-label="Conversation messages"`, `aria-live="polite"`,
		`<label class="sr-only" for="chat-composer">Message</label>`,
		`id="chat-composer"`, `aria-describedby="composer-help"`,
		`id="composer-help"`, `aria-label="Send"`,
		`class="chat-rail chat-sidebar"`, `role="navigation"`, `aria-label="Chat navigation"`,
		`class="chat-composer"`, `aria-label="Send a message"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("accessible workspace is missing %q", want)
		}
	}

	model.State = StateLoading
	loading := render(t, model)
	if !strings.Contains(loading, `role="status"`) || !strings.Contains(loading, `aria-busy="true"`) || !strings.Contains(loading, "Loading") {
		t.Fatal("loading state must be announced as a busy status")
	}

	model.State, model.Error = StateError, "Network unavailable"
	unavailable := render(t, model)
	if !strings.Contains(unavailable, `role="alert"`) || !strings.Contains(unavailable, "Network unavailable") || !strings.Contains(unavailable, "Try again") {
		t.Fatal("unavailable state must announce the error and expose recovery")
	}
	if !strings.Contains(unavailable, `id="chat-composer"`) || !strings.Contains(unavailable, `aria-label="Send a message"`) {
		t.Fatal("unavailable state must retain the named composer")
	}
}
