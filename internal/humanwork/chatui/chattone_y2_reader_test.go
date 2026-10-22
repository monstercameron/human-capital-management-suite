package chatui

import (
	"strings"
	"testing"
)

// TestChattoneY2FailedReaderLeavesNoSkeleton: the page reads how each message is
// to be shown to this reader (a translation, a reworded view) after it has the
// messages. While that read has not answered, or has failed, the messages are
// drawn as written and no loading row is held in the timeline (the timeline's
// skeleton belongs to the messages read; the Saved panel keeps its own).
func TestChattoneY2FailedReaderLeavesNoSkeleton(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := Model{
			State: StateReady, Locale: locale, CurrentUser: "hanna", SelectedID: "general",
			Conversations: []Conversation{{ID: "general", Name: "general", Kind: PublicChannel, MemberCount: 12}},
			Messages: []Message{
				{ID: "m1", AuthorID: "walt", Author: "Walt Brennan", Body: "Morning all. Reminder that the Boston office closes early on Friday.", Revision: 1},
				{ID: "m2", AuthorID: "ari", Author: "Ari Chen", Body: "Thanks Walt.", Revision: 1},
			},
			// What the page holds while the reader read is in flight, failed or
			// given up: the reading feature is on and nothing has been selected.
			ReaderPending: true, ReaderPolicyRequired: true, ReaderSelections: nil,
			Callbacks: Callbacks{SendMessage: func(string, string) {}, SelectConversation: func(string) {}},
		}
		markup := render(t, m)
		for _, want := range []string{"Morning all. Reminder that the Boston office closes early on Friday.", "Thanks Walt."} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s: a message was not drawn while the reader read had no answer: %q missing", locale, want)
			}
		}
		for _, held := range []string{`class="chatux009-skeleton"`, `chatux009-skel-line`, `chatux009-skel-avatar`} {
			if strings.Contains(markup, held) {
				t.Fatalf("%s: a loading row is held in the timeline: %s", locale, held)
			}
		}
	}
}
