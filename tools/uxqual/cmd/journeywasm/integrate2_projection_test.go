package main

import (
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"testing"
)

func TestIntegrate2ProjectionIncludesEveryRevision(t *testing.T) {
	m := chatui.Model{SelectedID: "room", CurrentUser: "person", CurrentTenantID: "tenant", Messages: []chatui.Message{{ID: "timeline", Revision: 1}}, ThreadMessages: []chatui.Message{{ID: "reply", Revision: 2}}, ThreadParent: &chatui.Message{ID: "root", Revision: 3}, ChannelPins: []chatui.ChannelPin{{PostID: "pin", Revision: 4}}, ChatFeatures: &chatui.ChatFeatures{Renderings: true, Status: true, Locations: true}}
	if messages := integrate2ReaderMessages(m); len(messages) != 4 || messages[3].Revision != 4 {
		t.Fatal(messages)
	}
	key := integrate2ProjectionKey(m)
	m.ChannelPins[0].Revision++
	if integrate2ProjectionKey(m) == key {
		t.Fatal("edited pin did not refresh policy")
	}
	key = integrate2ProjectionKey(m)
	m.ChatFeatures.Locations = false
	if integrate2ProjectionKey(m) == key {
		t.Fatal("location capability did not refresh projection")
	}
}

// A failed reading request records nothing: the text the server delivered stays
// on the page (CHATBUG-019). Only the server's own mark withholds an original.
func TestIntegrate2PolicyFailureCannotRestoreOriginal(t *testing.T) {
	msg := chatui.Message{ID: "post", Revision: 2, Body: "authored text"}
	selected := map[string]chatui.ReaderSelection{}
	integrate2ApplyPolicyAnswer(selected, []chatui.Message{msg}, []string{msg.ID}, nil, errors.New("policy unavailable"))
	m := chatui.Model{Locale: "en-US", ReaderSelections: selected, ReaderPending: true, ReaderPolicyRequired: true}
	if len(selected) != 0 || chatui.ReaderMessageBody(m, msg) != msg.Body {
		t.Fatal("a failed reading request hid text already delivered", selected)
	}
	integrate2ApplyPolicyAnswer(selected, []chatui.Message{msg}, []string{msg.ID}, map[string]chatui.ReaderSelection{msg.ID: {Mark: chatrender.Mark{State: "unavailable"}}}, nil)
	if selected[msg.ID].Revision != 2 || chatui.ReaderMessageBody(m, msg) == msg.Body {
		t.Fatal("server-withheld original was exposed", selected)
	}
}

// Preparing a model never invents a mark: with no answer yet the message reads
// as written, a current selection is kept, and an edited message reads as its
// new text rather than a stale rendering.
func TestIntegrate2PolicyMarksBeforeProjection(t *testing.T) {
	m := chatui.Model{Locale: "en-US", Messages: []chatui.Message{{ID: "post", Revision: 1, Body: "original"}}}
	m.ReaderPolicyRequired, m.ReaderPending = true, true
	integrate2PrepareReaderSelections(&m)
	if len(m.ReaderSelections) != 0 || chatui.ReaderMessageBody(m, m.Messages[0]) != "original" {
		t.Fatal("a message with no answer yet did not read as written", m.ReaderSelections)
	}
	m.ReaderSelections = map[string]chatui.ReaderSelection{"post": {Revision: 1, Rendering: chatrender.Rendering{Message: "post", Revision: 1, Text: "selected"}, Mark: chatrender.Mark{State: "ready"}}}
	integrate2PrepareReaderSelections(&m)
	if chatui.ReaderMessageBody(m, m.Messages[0]) != "selected" {
		t.Fatal("current selection lost")
	}
	m.Messages[0].Revision, m.Messages[0].Body = 2, "edited"
	integrate2PrepareReaderSelections(&m)
	if got := chatui.ReaderMessageBody(m, m.Messages[0]); got != "edited" {
		t.Fatalf("edited revision used a stale selection: %q", got)
	}
}
