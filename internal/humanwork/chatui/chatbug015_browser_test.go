package chatui

import (
	"strings"
	"testing"
	"time"
)

// CHATBUG-015 clause table:
//
//	the Assistant searches the conversation's documents before answering ... TestTodo_CHATBUG_015 (internal/application: the search runs
//	                                                                          first, the guide placed in #general is found, the model is
//	                                                                          given the search tool and the instruction to use it)
//	answers from the guide with a linked source ............................. TestTodo_CHATBUG_015_Browser (what the asker reads)
//	never "no company holiday documents" when the guide is placed ........... TestTodo_CHATBUG_015 (the answer is built from the hit), _Browser

// The asker's card for the headline question: the dates the guide lists, the
// guide as a link into the Documents hub, and nothing that says no document was
// provided.
func TestTodo_CHATBUG_015_Browser(t *testing.T) {
	answer := "Upcoming company holidays remaining in 2026:\n- Thanksgiving Day | Nov 26 | Thursday\n- Day after Thanksgiving | Nov 27 | Friday\n- Christmas Day | Dec 25 | Friday" +
		"\n\nSources\n- [2026 holiday guide · v1.0.0](" + chatbug021Guide + ") <!--chat.agent.source.readable:true-->"
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		now := time.Now()
		m := chat4Fixture(locale, "answered", false)
		m.Messages[0].Body = "@Assistant which company holidays are coming up in the rest of 2026"
		m.EphemeralMessages = []EphemeralMessage{{ID: "answer", ThreadID: "question", Body: answer, OnlyVisibleToYou: true, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}}
		m.PersonaActivityReady = true
		markup := chatbug047Rows(t, m)
		text := chatbug021Visible(markup)
		for _, want := range []string{"Thanksgiving Day", "Day after Thanksgiving", "Christmas Day", "2026 holiday guide"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s: the answer lacks %q: %s", locale, want, text)
			}
		}
		if strings.Count(markup, `href="`+chatbug021Attr(chatbug021Guide)+`"`) != 1 || strings.Contains(markup, "You cannot open this document") {
			t.Fatalf("%s: the guide is not one link the asker may open: %s", locale, markup)
		}
		for _, wrong := range []string{"don't have any", "no company holiday documents", "not provided"} {
			if strings.Contains(strings.ToLower(text), wrong) {
				t.Fatalf("%s: the card says %q: %s", locale, wrong, text)
			}
		}
	}
}
