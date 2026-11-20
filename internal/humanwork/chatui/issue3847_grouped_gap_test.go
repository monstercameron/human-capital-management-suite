//go:build !(js && wasm)

package chatui

import (
	"strings"
	"testing"
	"time"
)

// TestIssue3847_GroupedFollowUpHasNoEmptyAuthorBand: a grouped follow-up draws no
// author row, so the row that would hold it must not keep its 24px height; the
// follow-up then sits a few pixels under the message above.
func TestIssue3847_GroupedFollowUpHasNoEmptyAuthorBand(t *testing.T) {
	if got := chatbugCascadeValue(Stylesheet, ".chat-workspace .message-list .message.continued .message-meta", "min-height"); got != "0" {
		t.Fatalf("a grouped follow-up keeps an empty author band: min-height=%q", got)
	}
	if got := chatbugCascadeValue(Stylesheet, ".message.continued", "margin-top"); got != "0" {
		t.Fatalf("a grouped follow-up is separated by a full message gap: margin-top=%q", got)
	}
}

// TestIssue3847_SecondMessageFromTheSameAuthorIsGrouped: two quick messages from
// one author render the second without an author name or avatar button, in every
// language.
func TestIssue3847_SecondMessageFromTheSameAuthorIsGrouped(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chat4Fixture(locale, "none", false)
		now := time.Now().Add(-10 * time.Minute)
		m.Messages = []Message{
			{ID: "g1", AuthorID: "bob", Author: "Bob", Body: "grp one", TimeLabel: "9:00", SentAt: now},
			{ID: "g2", AuthorID: "bob", Author: "Bob", Body: "grp two", TimeLabel: "9:01", SentAt: now.Add(time.Minute)},
		}
		markup := render(t, m)
		first := strings.Index(markup, `data-message-id="g1"`)
		second := strings.Index(markup, `data-message-id="g2"`)
		if first < 0 || second < first {
			t.Fatalf("%s: messages missing or out of order", locale)
		}
		open := markup[strings.LastIndex(markup[:second], "<article"):second]
		if !strings.Contains(open, "continued") {
			t.Fatalf("%s: the follow-up is not grouped: %s", locale, open)
		}
		tail := markup[second:]
		if end := strings.Index(tail, "</article>"); end > 0 {
			tail = tail[:end]
		}
		if strings.Contains(tail, `message-author`) {
			t.Fatalf("%s: the follow-up repeats the author", locale)
		}
	}
}
