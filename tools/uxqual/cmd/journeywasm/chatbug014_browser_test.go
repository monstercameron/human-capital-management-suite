//go:build !(js && wasm)

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatbug014FirstPaint renders model the way the first paint draws it: through
// the page builder, with the callbacks a fresh load installs.
func chatbug014FirstPaint(t *testing.T, state *chatState) string {
	t.Helper()
	model := state.snapshot()
	model.Callbacks.SelectConversation = func(string) {}
	model.Callbacks.SendMessage = func(string, string) {}
	markup, err := ui.RenderToString(chatui.Build(model))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

// chatbug014ComposerField is the channel composer's text field.
func chatbug014ComposerField(t *testing.T, where, markup string) string {
	t.Helper()
	start := strings.Index(markup, `id="chat-composer"`)
	if start < 0 {
		t.Fatalf("%s: the first paint has no composer field", where)
	}
	open := strings.LastIndex(markup[:start], "<textarea")
	if open < 0 {
		t.Fatalf("%s: the composer field is not a text area", where)
	}
	end := strings.Index(markup[open:], ">")
	if end < 0 {
		t.Fatalf("%s: the composer field tag never ends", where)
	}
	return markup[open : open+end]
}

// TestTodo_CHATBUG_014_Browser: the first paint is the timeline skeleton plus a
// usable composer, and nothing heavier. The composer is enabled while the
// messages are still being read, and no deferred surface (thread, details,
// members, reactions) renders until its read lands. (The wall-clock half of
// the clause, usable within three seconds, is measured in the browser; this
// pins the DOM half: what the first paint holds.)
func TestTodo_CHATBUG_014_Browser(t *testing.T) {
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) {
		model.State, model.Locale, model.CurrentUser, model.CurrentTenantID = chatui.StateLoading, "en-US", "avery", "northwind"
		model.SelectedID = "general"
		model.Conversations = []chatui.Conversation{{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true}}
	})

	// A fresh load: the skeleton is on screen and the composer already works.
	loading := chatbug014FirstPaint(t, state)
	if !strings.Contains(loading, "chatux009-skeleton") || strings.Count(loading, "chatux009-skel-row") < 5 {
		t.Fatal("the first paint does not hold the five-row timeline skeleton")
	}
	if !strings.Contains(loading, `class="chat-composer"`) || !strings.Contains(loading, `data-send-capable="true"`) {
		t.Fatal("the first paint has no send-capable composer")
	}
	if field := chatbug014ComposerField(t, "loading", loading); strings.Contains(field, "disabled") {
		t.Fatalf("the composer is disabled while the timeline loads: %s", field)
	}
	if !strings.Contains(loading, `type="submit"`) {
		t.Fatal("the first paint's composer has no send button")
	}
	// A closed details panel is an empty collapsed placeholder, never content.
	if !strings.Contains(loading, "chat-side chat-details collapsed") {
		t.Fatal("the first paint lost the collapsed details placeholder")
	}
	for _, heavy := range []string{"thread-pane", "chat-details-", "member-row", "reaction-row", "agent-reply-sources", "chat-doc-embed"} {
		if strings.Contains(loading, heavy) {
			t.Errorf("the first paint renders %s before its read has landed", heavy)
		}
	}

	// The first ready paint: the messages are drawn, the composer is still
	// usable, and every deferred read is still absent.
	state.mutate(func(model *chatui.Model) {
		model.State = chatui.StateReady
		model.Messages = []chatui.Message{
			{ID: "p1", AuthorID: "blake", Author: "Blake", Body: "kickoff notes", TimeLabel: "9:00", SentAt: now},
			{ID: "p2", AuthorID: "avery", Author: "Avery", Body: "on it", TimeLabel: "9:01", SentAt: now.Add(time.Minute)},
		}
	})
	ready := chatbug014FirstPaint(t, state)
	for _, body := range []string{"kickoff notes", "on it"} {
		if !strings.Contains(ready, body) {
			t.Errorf("the first ready paint does not draw %q", body)
		}
	}
	if !strings.Contains(ready, `data-send-capable="true"`) {
		t.Error("the composer stopped being send-capable once the messages landed")
	}
	if field := chatbug014ComposerField(t, "ready", ready); strings.Contains(field, "disabled") {
		t.Errorf("the composer is disabled on the first ready paint: %s", field)
	}
	for _, heavy := range []string{"thread-pane", "chat-details-", "member-row", "reaction-row", "agent-reply-sources", "chat-doc-embed"} {
		if strings.Contains(ready, heavy) {
			t.Errorf("the first ready paint renders %s with no deferred read behind it", heavy)
		}
	}
}
