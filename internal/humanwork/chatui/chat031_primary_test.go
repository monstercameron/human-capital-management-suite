package chatui

import (
	"strings"
	"testing"
)

// sendButtonDisabled reports whether the composer's send-button markup
// carries the disabled attribute, without being fooled by "disabled"
// appearing anywhere else on the page.
func sendButtonDisabled(t *testing.T, markup string) bool {
	t.Helper()
	start := strings.Index(markup, `class="send-button"`)
	if start < 0 {
		t.Fatal("composer send button did not render")
	}
	end := strings.Index(markup[start:], ">")
	if end < 0 {
		t.Fatal("unterminated send button markup")
	}
	return strings.Contains(markup[start:start+end], "disabled")
}

// TestTodo_CHAT_031 is the CHAT-031 PRIMARY matrix test. It proves the
// channel and message workspace shell renders the authorized rail, timeline
// and composer on both a desktop and a narrow layout, that a caller with no
// send authority gets a disabled composer rather than a silently broken one,
// and that the unavailable state replaces the timeline with an announced
// error instead of a blank or partially authorized workspace.
func TestTodo_CHAT_031(t *testing.T) {
	authorized := Model{
		State: StateReady, Locale: "en-US", CurrentUser: "morgan", SelectedID: "eng",
		Conversations: []Conversation{{ID: "eng", Name: "Engineering", Kind: PublicChannel, MemberCount: 12}},
		Messages:      []Message{{ID: "m1", AuthorID: "ari", Author: "Ari Chen", Body: "Release is ready"}},
		Callbacks:     Callbacks{SendMessage: func(string, string) {}, SelectConversation: func(string) {}},
	}

	desktop := render(t, authorized)
	for _, want := range []string{
		`class="chat-workspace"`, `role="navigation"`, `aria-label="Chat navigation"`,
		`role="log"`, `aria-label="Conversation messages"`,
		`id="chat-composer"`, `class="send-button"`, "Release is ready",
	} {
		if !strings.Contains(desktop, want) {
			t.Errorf("desktop workspace missing %q", want)
		}
	}
	if sendButtonDisabled(t, desktop) {
		t.Fatal("authorized caller's send action was disabled")
	}
	if strings.Contains(desktop, `data-sidebar-open="true"`) {
		t.Fatal("desktop layout must not default to the narrow open-rail state")
	}

	narrow := authorized
	narrow.SidebarOpen = true
	narrowMarkup := render(t, narrow)
	if !strings.Contains(narrowMarkup, `data-sidebar-open="true"`) {
		t.Fatal("narrow layout did not expose its open rail state")
	}
	if strings.Count(narrowMarkup, `class="chat-workspace"`) != 1 {
		t.Fatal("narrow layout must stay a single workspace root, not a second surface")
	}
	if !strings.Contains(narrowMarkup, `id="chat-composer"`) {
		t.Fatal("narrow layout dropped the composer")
	}

	unauthorized := Model{State: StateReady, SelectedID: "eng", Conversations: authorized.Conversations}
	locked := render(t, unauthorized)
	if !sendButtonDisabled(t, locked) {
		t.Fatal("workspace without an authorized send callback rendered an active composer")
	}

	unavailable := authorized
	unavailable.State, unavailable.Error = StateError, "Network unavailable"
	errMarkup := render(t, unavailable)
	if !strings.Contains(errMarkup, `role="alert"`) || !strings.Contains(errMarkup, "Network unavailable") {
		t.Fatal("unavailable workspace state did not announce the error")
	}
	if strings.Contains(errMarkup, "Release is ready") {
		t.Fatal("unavailable state still rendered the stale timeline")
	}
}
