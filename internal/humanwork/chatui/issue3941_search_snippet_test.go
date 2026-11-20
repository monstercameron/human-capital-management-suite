package chatui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

var (
	issue3941Describedby = regexp.MustCompile(`aria-describedby="([^"]+)"`)
	issue3941ID          = regexp.MustCompile(`id="(chatsearch-snippet-[^"]+)"`)
)

// TestIssue3941_ResultButtonDescribedBySnippet: each message result's open
// button is described by that result's own excerpt, so a screen reader reads the
// excerpt with the button. Two results from the same author in the same
// conversation get different ids that point at different excerpts, in every
// language.
func TestIssue3941_ResultButtonDescribedBySnippet(t *testing.T) {
	at := time.Date(2026, time.October, 1, 11, 18, 0, 0, time.UTC)
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chatbug077Model()
		m.Locale = locale
		people := chatsearchPeopleOf(m)
		first := chatsearch.Row{Kind: chatsearch.Message, ID: "a", AuthorID: "loretta", Text: "The deadline moved to Friday.", At: at, Target: chatsearch.Target{ConversationID: "general", MessageID: "a", Sequence: 4}}
		second := chatsearch.Row{Kind: chatsearch.Message, ID: "b", AuthorID: "loretta", Text: "Deadline reminders go out Monday.", At: at.Add(time.Hour), Target: chatsearch.Target{ConversationID: "general", MessageID: "b", Sequence: 7}}
		ids := map[string]bool{}
		for _, row := range []chatsearch.Row{first, second} {
			got := renderNode(t, chatsearchResult(m, people, row, "deadline", false))
			ref := issue3941Describedby.FindStringSubmatch(got)
			if ref == nil {
				t.Fatalf("%s: the open button has no aria-describedby: %s", locale, got)
			}
			owner := issue3941ID.FindStringSubmatch(got)
			if owner == nil || owner[1] != ref[1] {
				t.Fatalf("%s: describedby %q does not name an element of the result: %s", locale, ref[1], got)
			}
			pos := strings.Index(got, `id="`+owner[1]+`"`)
			if !strings.Contains(got[pos:], strings.Fields(row.Text)[1]) {
				t.Fatalf("%s: the described element does not hold this result's excerpt: %s", locale, got)
			}
			if !strings.Contains(got, `aria-label="`) {
				t.Fatalf("%s: the button lost its accessible name: %s", locale, got)
			}
			ids[ref[1]] = true
		}
		if len(ids) != 2 {
			t.Fatalf("%s: two results share one snippet id: %v", locale, ids)
		}
	}
}

// TestIssue3941_PlainResultsHaveNoDangling: a result with no message excerpt
// (a person or a conversation) must not point at an element that is not there.
func TestIssue3941_PlainResultsHaveNoDangling(t *testing.T) {
	m := chatbug077Model()
	row := chatsearch.Row{Kind: chatsearch.Person, ID: "p", AuthorID: "loretta", Text: "Loretta", Target: chatsearch.Target{ConversationID: "loretta"}}
	if got := renderNode(t, chatsearchResult(m, chatsearchPeopleOf(m), row, "lor", false)); strings.Contains(got, "aria-describedby") {
		t.Fatalf("a result with no excerpt points at one: %s", got)
	}
}
