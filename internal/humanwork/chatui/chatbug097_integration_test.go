package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// TestTodo_CHATBUG_097_Integration opens a thread on the served page: the pane
// carries the whole parent with the replies under it, the reply count in the
// timeline opens it, the composer is ready, and no element id appears twice in
// the document.
func TestTodo_CHATBUG_097_Integration(t *testing.T) {
	m, german, _, _ := chatlangModel("en-US")
	german.TimeLabel, german.Replies = "09:41", 6
	german.Attachments = []Attachment{{ID: "a1", Name: "plan.png", ContentType: "image/png", Width: 320, Height: 240, Bytes: 9000}}
	german.Chips = []ReactionChip{{Emoji: "tada", Count: 2, Mine: true}}
	m.Messages = []Message{german}
	m.ShowThread, m.ThreadParentID = true, german.ID
	m.ThreadMessages = []Message{{ID: "r1", AuthorID: "mona", Author: "Mona", Body: "On it.", TimeLabel: "09:52"}}
	m.Callbacks = walkCallbacks()
	page := render(t, m)

	// The pane: heading, whole parent, replies, composer with its Reply.
	pane := page[strings.Index(page, "thread-pane"):]
	for _, want := range []string{"Thread", "Hans", "09:41", chatlangEnglish, "attachment-image", "reaction-count", "thread-count", "On it.", "thread-composer", "send-button", "Reply", `data-action="close-thread"`} {
		if !strings.Contains(pane, want) {
			t.Errorf("the open thread misses %q", want)
		}
	}
	if strings.Index(pane, "thread-root") > strings.Index(pane, "thread-replies") {
		t.Error("the replies are not drawn under the parent")
	}

	// The timeline's reply count opens the thread.
	if !strings.Contains(page, `data-action="stats"`) || !strings.Contains(page, "6 replies") {
		t.Error("the timeline lost its thread button")
	}

	// No element id appears twice in the document with a thread open.
	ids := map[string]int{}
	for _, match := range regexp.MustCompile(`\sid="([^"]+)"`).FindAllStringSubmatch(page, -1) {
		ids[match[1]]++
	}
	for id, n := range ids {
		if n > 1 {
			t.Errorf("id %q appears %d times with a thread open", id, n)
		}
	}
}
