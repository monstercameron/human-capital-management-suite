package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// The reading-language row in Chat preferences is "Reading language" with its
// value, never overlapping, and its form no longer holds "Apply to this
// conversation": that choice lives in Conversation details.
func TestTodo_CHATBUG_058(t *testing.T) {
	row := renderNode(t, RenderingPersonalSettings("en-US", "general"))
	for _, want := range []string{"Reading language", "Translate messages into my language", "More options"} {
		if !strings.Contains(row, want) {
			t.Errorf("the preferences row misses %q: %s", want, row)
		}
	}
	for _, gone := range []string{"Apply to this conversation", `name="conversation"`, "chatrender-room"} {
		if strings.Contains(row, gone) {
			t.Errorf("the preferences row still holds %q: %s", gone, row)
		}
	}
	// The label and the value are two elements in a head that wraps, not one
	// string drawn over the other.
	if !strings.Contains(ChatBug045ReadingStyles, ".chat-prefs-head{flex-wrap:wrap") || !strings.Contains(ChatBug045ReadingStyles, ".chat-prefs-title{flex:1 1 auto;white-space:normal;overflow-wrap:anywhere}") {
		t.Error("the row's head does not wrap")
	}
	// The two lists are chips under the one disclosure.
	if strings.Count(row, "chatrender-chip") < 16 || strings.Count(row, `data-chat-disclosure="true"`) < 2 {
		t.Errorf("the lists are not chips under More options: %d chips", strings.Count(row, "chatrender-chip"))
	}
}

// Conversation details has one row, "Reading in this conversation", that opens to
// the language and the translate switch and saves every change.
func TestTodo_CHATBUG_058_ConversationRow(t *testing.T) {
	m := chatux021Model()
	got := renderNode(t, chatbug058ConversationReading(m))
	for _, want := range []string{"Reading in this conversation", "Read messages in", "Translate messages into my language", `id="chatbug058-reading"`, `id="chatbug058-translate"`, "Only for this conversation."} {
		if !strings.Contains(got, want) {
			t.Errorf("the details row misses %q: %s", want, got)
		}
	}
	if strings.Count(got, "<option") != len(chatbug045Languages) {
		t.Errorf("%d languages offered, want %d", strings.Count(got, "<option"), len(chatbug045Languages))
	}
	// No second Save button: the form saves as it changes.
	if strings.Contains(got, `type="submit"`) || strings.Contains(got, "Save") {
		t.Errorf("the row has a Save button: %s", got)
	}
	// It is part of the panel, after Notifications, for any conversation.
	panel := chatux021Details(t, m, handlers{})
	if strings.Index(panel, "Reading in this conversation") < strings.Index(panel, "Notifications for me") {
		t.Errorf("the row is not after Notifications: %s", panel)
	}
	dm := m
	dm.Conversations = []Conversation{{ID: "helper", Name: "Jake", Kind: DirectMessage, Joined: true}}
	dm.SelectedID = "helper"
	if got := chatux021Details(t, dm, handlers{}); !strings.Contains(got, "Reading in this conversation") {
		t.Errorf("a direct message cannot set its reading language: %s", got)
	}
	// With no reading-language service composed there is no row.
	off := m
	off.ChatFeatures = &ChatFeatures{Renderings: false}
	if chatbug058ConversationReading(off) != nil {
		t.Error("the row is offered with no reading-language service")
	}
	if chatbug058ConversationReading(Model{}) != nil {
		t.Error("a row with no conversation")
	}
	// The value at the right is the language in force, and "Saved" and failures
	// are said in a live region.
	for _, tc := range []struct {
		say   string
		state chatbug058State
	}{
		{"German", chatbug058State{Preference: chatrender.Preference{ReadingLanguage: "de"}}},
		{"Settings could not be saved", chatbug058State{Preference: chatrender.DefaultPreference("en-US"), Failed: true}},
		{"Language settings saved", chatbug058State{Preference: chatrender.DefaultPreference("en-US"), Saved: true}},
	} {
		view := renderNode(t, chatbug058View(chatbug058Props{Locale: "en-US", Conversation: "general"}, tc.state, ui.Handler{}))
		if !strings.Contains(view, tc.say) {
			t.Errorf("the row does not say %q: %s", tc.say, view)
		}
	}
	// While the service has not answered, or is not there, nothing is drawn.
	for _, hidden := range []chatbug058State{{Checking: true}, {Unavailable: true}} {
		if chatbug058View(chatbug058Props{Locale: "en-US"}, hidden, ui.Handler{}) != nil {
			t.Errorf("a row for %+v", hidden)
		}
	}
}
