package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// TestTodo_CHATBUG_019 pins the reader rules: the text the server delivered
// is shown at once, a different rendering replaces it only when one was
// selected for this reader, and a placeholder sentence appears only when the
// server's own mark withholds the original.
func TestTodo_CHATBUG_019(t *testing.T) {
	msg := Message{ID: "post", Revision: 1, Body: "Delivered text"}
	pending := RenderingText("en-US", "pending")
	unavailable := RenderingText("en-US", "unavailable")
	if pending == "" || unavailable == "" || pending == unavailable {
		t.Fatalf("placeholder copy missing: %q %q", pending, unavailable)
	}
	base := Model{Locale: "en-US", ReaderPending: true}

	// Still loading: no selection yet, the reading feature is on.
	if got := ReaderMessageBody(base, msg); got != msg.Body {
		t.Fatalf("a message with no selection hid its text: %q", got)
	}
	empty := base
	empty.ReaderSelections = map[string]ReaderSelection{}
	if got := ReaderMessageBody(empty, msg); got != msg.Body {
		t.Fatalf("an empty selection map hid the text: %q", got)
	}

	// A different rendering selected for this reader replaces the text.
	chosen := base
	chosen.ReaderSelections = map[string]ReaderSelection{msg.ID: {Rendering: chatrender.Rendering{Message: msg.ID, Revision: 1, Text: "Selected text", Tone: chatrender.AsWritten}, Mark: chatrender.Mark{State: "ready"}}}
	if got := ReaderMessageBody(chosen, msg); got != "Selected text" {
		t.Fatalf("a selected rendering was not shown: %q", got)
	}

	// The server withheld the original: the sentence stands in for it.
	for state, want := range map[string]string{"pending": pending, "unavailable": unavailable} {
		withheld := base
		withheld.ReaderSelections = map[string]ReaderSelection{msg.ID: {Mark: chatrender.Mark{State: state}}}
		if got := ReaderMessageBody(withheld, msg); got != want {
			t.Fatalf("withheld %s original was exposed: %q", state, got)
		}
	}

	// A pending mark whose original the server allows (a translation is on its
	// way) never hides the text.
	waiting := base
	waiting.ReaderSelections = map[string]ReaderSelection{msg.ID: {Mark: chatrender.Mark{State: "pending", CanShowOriginal: true}}}
	if got := ReaderMessageBody(waiting, msg); got != msg.Body {
		t.Fatalf("a pending translation hid an allowed original: %q", got)
	}

	// The server answered "original": that text is shown.
	// integrate-2's ReaderPolicyRequired (set with ReaderPending in the browser) and every kind of
	// body: no selection yet means the text the server delivered, never a sentence.
	announcement, err := AnnouncementMessageBody(AgentAnnouncementMessage{AgentName: "Policy Helper", Text: "Open enrollment ends Friday"})
	if err != nil {
		t.Fatal(err)
	}
	required := base
	required.ReaderPolicyRequired = true
	for _, body := range []string{"@Policy Helper how many PTO hours carry over?", "See doc:policy-2026 for the rule", announcement, "Plain text"} {
		sent := Message{ID: "other", Revision: 1, Body: body}
		if got := ReaderMessageBody(required, sent); got != body {
			t.Fatalf("a message with no selection was replaced under ReaderPolicyRequired: %q", got)
		}
		if got := ReaderMessageBody(required, Message{ID: "other", Revision: 0, Body: body}); got != body {
			t.Fatalf("a message without a revision was replaced: %q", got)
		}
	}
	// A server withholding on another message does not touch this one.
	required.ReaderSelections = map[string]ReaderSelection{"elsewhere": {Mark: chatrender.Mark{State: "unavailable"}}}
	if got := ReaderMessageBody(required, msg); got != msg.Body {
		t.Fatalf("another message's mark hid this text: %q", got)
	}

	original := base
	original.ReaderSelections = map[string]ReaderSelection{msg.ID: {Rendering: chatrender.Rendering{Message: msg.ID, Revision: 1, Text: msg.Body, Tone: chatrender.AsWritten}, Mark: chatrender.Mark{State: "original", CanShowOriginal: true}}}
	if got := ReaderMessageBody(original, msg); got != msg.Body {
		t.Fatalf("an original answer changed the text: %q", got)
	}
}

// TestTodo_CHATBUG_019_Browser renders every surface that shows a message body
// with the reading feature on and no selection delivered yet.
func TestTodo_CHATBUG_019_Browser(t *testing.T) {
	msg := Message{ID: "post", Revision: 1, AuthorID: "person", Author: "Alex", Body: "Delivered paragraph", Sequence: 1}
	reply := Message{ID: "reply", Revision: 1, AuthorID: "person", Author: "Alex", Body: "Delivered reply", Sequence: 2}
	m := Model{Locale: "en-US", State: StateReady, SelectedID: "room", CurrentUser: "reader", CurrentTenantID: "tenant", ReaderPending: true, ReaderSelections: map[string]ReaderSelection{},
		Conversations: []Conversation{{ID: "room", Kind: PublicChannel, Name: "General", Joined: true}}, Messages: []Message{msg}, ThreadParentID: msg.ID, ThreadParent: &msg, ThreadMessages: []Message{reply}, ShowThread: true,
		ChannelPins: []ChannelPin{{PostID: msg.ID, Revision: 1, Body: msg.Body}}}
	surfaces := map[string]struct {
		render func(Model) ui.Node
		want   []string
	}{
		"channel list": {func(m Model) ui.Node { return message(m, handlers{}, msg, false) }, []string{msg.Body}},
		"thread":       {func(m Model) ui.Node { return threadPane(m, handlers{}) }, []string{msg.Body, reply.Body}},
		"pins":         {chatux019PinnedSection, []string{msg.Body}},
	}
	placeholders := []string{RenderingText("en-US", "pending"), RenderingText("en-US", "unavailable")}
	// The same page with the lane's ReaderPolicyRequired set, and bodies that mention an agent or
	// reference a document: none of them is held back while no selection has arrived.
	bodies := map[string]string{"agent mention": "@Policy Helper how many PTO hours carry over?", "document reference": "The carry-over rule is in doc:policy-2026 for everyone"}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			special := Message{ID: "special", Revision: 1, AuthorID: "person", Author: "Walt Brennan", Body: body, Sequence: 3}
			required := m
			required.ReaderPolicyRequired = true
			required.Messages = []Message{special}
			got := renderNode(t, message(required, handlers{}, special, false))
			if !strings.Contains(got, "carry") {
				t.Fatalf("the delivered text is not on the page: %s", got)
			}
			for _, placeholder := range placeholders {
				if strings.Contains(got, placeholder) {
					t.Fatalf("placeholder %q shown for %s with no selection: %s", placeholder, name, got)
				}
			}
		})
	}
	m.ReaderPolicyRequired = true
	for name, surface := range surfaces {
		t.Run(name, func(t *testing.T) {
			got := renderNode(t, surface.render(m))
			for _, want := range surface.want {
				if !strings.Contains(got, want) {
					t.Fatalf("the delivered text %q is not on the page: %s", want, got)
				}
			}
			for _, placeholder := range placeholders {
				if strings.Contains(got, placeholder) {
					t.Fatalf("placeholder %q shown with no selection: %s", placeholder, got)
				}
			}
		})
	}
}
