package main

import (
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// TestTodo_CHATBUG_019 pins the two halves of the regression that replaced
// every message body with the "view is unavailable" sentence: the reading
// request lost its query, and its failure hid the text already on the page.
func TestTodo_CHATBUG_019(t *testing.T) {
	cfg := journeyclient.Config{TunnelURL: "ws://127.0.0.1:8290/tunnel"}
	query := url.Values{"conversation": {"room"}, "message": {"one", "two"}}.Encode()
	got, err := personaChatURL(cfg, "/api/chat/renderings/v1/reader?"+query, "")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/api/chat/renderings/v1/reader" || strings.Contains(got, "%3F") {
		t.Fatalf("the query was escaped into the path: %s", got)
	}
	if parsed.Query().Get("conversation") != "room" || len(parsed.Query()["message"]) != 2 {
		t.Fatalf("the query was not kept: %s", got)
	}
	plain, err := personaChatURL(cfg, "/api/chat/agents", "room")
	if err != nil || !strings.HasSuffix(plain, "/api/chat/agents?conversation_id=room") {
		t.Fatalf("the conversation query changed: %s %v", plain, err)
	}

	// A failed reading request records nothing, so every message stays as written.
	messages := []chatui.Message{{ID: "one", Revision: 2, Body: "text"}, {ID: "two", Revision: 1, Body: "more"}}
	selected := map[string]chatui.ReaderSelection{}
	failed := map[string]chatui.ReaderSelection{"one": {Mark: chatrender.Mark{State: "unavailable"}}}
	integrate2ApplyReaderAnswer(selected, messages, failed, errors.New("404"))
	integrate2ApplyPolicyAnswer(selected, messages, []string{"one", "two"}, failed, errors.New("404"))
	integrate2ApplyPolicyAnswer(selected, messages, []string{"one", "two"}, nil, nil)
	if len(selected) != 0 {
		t.Fatalf("a failed or empty reading answer hid text: %v", selected)
	}
	for _, msg := range messages {
		if got := chatui.ReaderMessageBody(chatui.Model{Locale: "en-US", ReaderPending: true, ReaderPolicyRequired: true, ReaderSelections: selected}, msg); got != msg.Body {
			t.Fatalf("message %s did not stay as written after a failed request: %q", msg.ID, got)
		}
	}
	// What the server answers is recorded at the message's own revision, and a
	// mark that withholds the original still replaces the text.
	answer := map[string]chatui.ReaderSelection{
		"one": {Rendering: chatrender.Rendering{Message: "one", Revision: 2, Text: "chosen", Tone: chatrender.AsWritten}, Mark: chatrender.Mark{State: "ready"}},
		"two": {Mark: chatrender.Mark{State: "unavailable"}},
	}
	integrate2ApplyPolicyAnswer(selected, messages, []string{"one", "two"}, answer, nil)
	model := chatui.Model{Locale: "en-US", ReaderPending: true, ReaderPolicyRequired: true, ReaderSelections: selected}
	if selected["one"].Revision != 2 || selected["two"].Revision != 1 {
		t.Fatalf("answers were not tied to the message revision: %v", selected)
	}
	if got := chatui.ReaderMessageBody(model, messages[0]); got != "chosen" {
		t.Fatalf("the selected rendering was not shown: %q", got)
	}
	if got := chatui.ReaderMessageBody(model, messages[1]); got != chatui.RenderingText("en-US", "unavailable") {
		t.Fatalf("a server-withheld original was exposed: %q", got)
	}

	// The client must go through the helper and never invent a mark itself.
	source, err := os.ReadFile("integrate2_browser_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	projection, err := os.ReadFile("integrate2_projection.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{string(source), string(projection)} {
		if strings.Contains(text, `Mark{State: "unavailable"}`) || strings.Contains(text, `Mark{State:"unavailable"}`) {
			t.Fatal("the client must not invent an unavailable mark: a failed reading request keeps the message as written")
		}
	}
	if !strings.Contains(string(source), "integrate2ApplyPolicyAnswer(") || !strings.Contains(string(projection), "integrate2ApplyReaderAnswer(") {
		t.Fatal("the reading answer must go through integrate2ApplyPolicyAnswer, which delegates to integrate2ApplyReaderAnswer (chatbug019_reader_selection.go)")
	}
}
