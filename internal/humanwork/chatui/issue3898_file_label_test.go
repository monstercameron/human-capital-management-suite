package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// The open control of a file result is named by the file as well as by where it
// is, so a person moving between results hears which file each one opens.
func TestIssue3898_FileResultButtonNamesTheFile(t *testing.T) {
	row := chatsearch.Row{Kind: chatsearch.File, ID: "f1", Text: "team-photo.png", Target: chatsearch.Target{ConversationID: "general", MessageID: "m1", Sequence: 3}}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chatbug077Model()
		m.Locale = locale
		got := renderNode(t, chatsearchResult(m, chatsearchPeopleOf(m), row, "png", false))
		_, rest, ok := strings.Cut(got, `aria-label="`)
		if !ok {
			t.Fatalf("%s: no accessible name: %s", locale, got)
		}
		label, _, _ := strings.Cut(rest, `"`)
		if !strings.Contains(label, "team-photo.png") || !strings.Contains(label, "#general") {
			t.Fatalf("%s: the open control is named %q; it must carry the file name and its conversation", locale, label)
		}
		if !strings.HasPrefix(label, chatsearchText(locale, "open")) {
			t.Fatalf("%s: the name %q lost the Open prefix", locale, label)
		}
		if !strings.Contains(got, `data-chatsearch-action="open"`) || !strings.Contains(got, "team-photo.png") {
			t.Fatalf("%s: the open action or the visible file name is gone: %s", locale, got)
		}
	}
	// A very long name is shortened in the accessible name, not dropped.
	long := row
	long.Text = strings.Repeat("a", 300) + ".png"
	m := chatbug077Model()
	got := renderNode(t, chatsearchResult(m, chatsearchPeopleOf(m), long, "", false))
	_, rest, _ := strings.Cut(got, `aria-label="`)
	label, _, _ := strings.Cut(rest, `"`)
	if len([]rune(label)) > 200 || !strings.Contains(label, "aaaa") {
		t.Fatalf("long name label has %d runes: %q", len([]rune(label)), label)
	}
}
